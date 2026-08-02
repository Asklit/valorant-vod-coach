package vodstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

type CatalogStore struct {
	Files        LocalStore
	Catalog      app.UploadCatalog
	Objects      app.BlobStore
	ObjectPrefix string
}

type PostDeleteCleanupError struct {
	Cause error
}

func (e PostDeleteCleanupError) Error() string {
	return "uploaded VOD was deleted, but storage cleanup is pending: " + e.Cause.Error()
}

func (e PostDeleteCleanupError) Unwrap() error { return e.Cause }

func (s CatalogStore) Stage(ctx context.Context, reader io.Reader) (StagedUpload, error) {
	return s.Files.Stage(ctx, reader)
}

func (s CatalogStore) Discard(staged StagedUpload) error {
	return s.Files.Discard(staged)
}

func (s CatalogStore) Create(ctx context.Context, request CreateUploadRequest) (UploadedAsset, error) {
	asset, err := s.Files.Create(ctx, request)
	if err != nil {
		return UploadedAsset{}, err
	}
	if s.Objects != nil {
		asset.ObjectKey, err = UploadObjectKey(s.objectPrefix(), asset.Upload.VOD.OwnerID, asset.Upload.VOD.Label, asset.Upload.VideoFilename)
		if err == nil {
			contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(asset.Path)))
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			err = s.Objects.UploadFile(ctx, asset.ObjectKey, asset.Path, contentType)
		}
		if err != nil {
			_, _ = s.Files.Delete(context.Background(), asset.Upload.VOD.Label, asset.Upload.VOD.OwnerID, false)
			return UploadedAsset{}, fmt.Errorf("publish uploaded VOD: %w", err)
		}
	}
	if err := s.Catalog.SaveUpload(ctx, uploadRecord(asset)); err != nil {
		if asset.ObjectKey != "" && s.Objects != nil {
			_ = s.Objects.DeleteObject(context.Background(), asset.ObjectKey)
		}
		_, _ = s.Files.Delete(context.Background(), asset.Upload.VOD.Label, asset.Upload.VOD.OwnerID, false)
		return UploadedAsset{}, err
	}
	return asset, nil
}

func (s CatalogStore) List(ctx context.Context, ownerID string, includeAll bool) ([]UploadedAsset, error) {
	records, err := s.Catalog.ListUploads(ctx, ownerID, includeAll)
	if err != nil {
		return nil, err
	}
	assets := make([]UploadedAsset, 0, len(records))
	for _, record := range records {
		assets = append(assets, uploadedAsset(record))
	}
	sort.Slice(assets, func(i, j int) bool {
		return assets[i].Upload.VOD.UploadedAt.After(assets[j].Upload.VOD.UploadedAt)
	})
	return assets, nil
}

func (s CatalogStore) Resolve(ctx context.Context, label string, ownerID string, includeAll bool) (UploadedAsset, error) {
	record, found, err := s.Catalog.FindUpload(ctx, label, ownerID, includeAll)
	if err != nil {
		return UploadedAsset{}, err
	}
	if !found {
		return UploadedAsset{}, ErrUploadNotFound
	}
	asset := uploadedAsset(record)
	localPath, err := s.cachePath(asset)
	if err != nil {
		return UploadedAsset{}, err
	}
	asset.Path = localPath
	if info, statErr := os.Stat(localPath); statErr == nil && info.Mode().IsRegular() {
		return asset, nil
	}
	if asset.ObjectKey == "" || s.Objects == nil {
		return UploadedAsset{}, fmt.Errorf("uploaded VOD content is unavailable: %s", label)
	}
	if err := s.Objects.DownloadFile(ctx, asset.ObjectKey, localPath); err != nil {
		return UploadedAsset{}, fmt.Errorf("materialize uploaded VOD: %w", err)
	}
	if err := writeMetadata(filepath.Join(filepath.Dir(localPath), MetadataJSONName), asset.Upload); err != nil {
		return UploadedAsset{}, fmt.Errorf("write upload cache metadata: %w", err)
	}
	return asset, nil
}

func (s CatalogStore) Update(ctx context.Context, label string, ownerID string, includeAll bool, request UpdateUploadRequest) (UploadedAsset, error) {
	record, found, err := s.Catalog.FindUpload(ctx, label, ownerID, includeAll)
	if err != nil {
		return UploadedAsset{}, err
	}
	if !found {
		return UploadedAsset{}, ErrUploadNotFound
	}
	previous := uploadedAsset(record)
	if previous.Path, err = s.cachePath(previous); err != nil {
		return UploadedAsset{}, err
	}
	title, rank, mapName, agent, err := normalizeMetadata(request.Title, request.Rank, request.Map, request.Agent)
	if err != nil {
		return UploadedAsset{}, err
	}
	asset := previous
	asset.Upload.VOD.Title = title
	asset.Upload.VOD.Rank = domain.Rank(rank)
	asset.Upload.VOD.Map = mapName
	asset.Upload.VOD.Agent = agent
	asset.Upload.UpdatedAt = s.Files.now()
	if err := s.Catalog.SaveUpload(ctx, uploadRecord(asset)); err != nil {
		return UploadedAsset{}, err
	}
	if _, err := os.Stat(asset.Path); err == nil {
		_ = writeMetadata(filepath.Join(filepath.Dir(asset.Path), MetadataJSONName), asset.Upload)
	}
	return asset, nil
}

func (s CatalogStore) Delete(ctx context.Context, label string, ownerID string, includeAll bool) (UploadedAsset, error) {
	record, found, err := s.Catalog.FindUpload(ctx, label, ownerID, includeAll)
	if err != nil {
		return UploadedAsset{}, err
	}
	if !found {
		return UploadedAsset{}, ErrUploadNotFound
	}
	asset := uploadedAsset(record)
	if err := s.Catalog.DeleteUpload(ctx, label, ownerID, includeAll); err != nil {
		return UploadedAsset{}, err
	}
	localPath, pathErr := s.cachePath(asset)
	var cleanupErrors []error
	if pathErr != nil {
		cleanupErrors = append(cleanupErrors, pathErr)
	} else if err := os.RemoveAll(filepath.Dir(localPath)); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("remove upload cache: %w", err))
	}
	if asset.ObjectKey != "" && s.Objects != nil {
		if err := s.Objects.DeleteObject(ctx, asset.ObjectKey); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	if cleanupErr := errors.Join(cleanupErrors...); cleanupErr != nil {
		return asset, PostDeleteCleanupError{Cause: cleanupErr}
	}
	return asset, nil
}

func uploadRecord(asset UploadedAsset) app.UploadRecord {
	return app.UploadRecord{
		VOD:            asset.Upload.VOD,
		VideoPath:      asset.Path,
		VideoObjectKey: asset.ObjectKey,
		VideoFilename:  asset.Upload.VideoFilename,
		SizeBytes:      asset.Upload.SizeBytes,
		Media:          asset.Upload.Media,
		UpdatedAt:      asset.Upload.UpdatedAt,
	}
}

func uploadedAsset(record app.UploadRecord) UploadedAsset {
	filename := record.VideoFilename
	if filename == "" {
		filename = filepath.Base(record.VideoPath)
	}
	return UploadedAsset{
		Upload: UploadedVOD{
			SchemaVersion: MetadataSchemaVersion,
			VOD:           record.VOD,
			VideoFilename: filename,
			SizeBytes:     record.SizeBytes,
			Media:         record.Media,
			UpdatedAt:     record.UpdatedAt,
		},
		Path:      record.VideoPath,
		ObjectKey: record.VideoObjectKey,
	}
}

func UploadObjectKey(prefix string, ownerID string, label string, filename string) (string, error) {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		prefix = "uploads"
	}
	filename = strings.TrimSpace(filename)
	if safeSegment(ownerID) != ownerID || safeSegment(label) != label || filename == "." || filename == "" || filepath.Base(filename) != filename {
		return "", errors.New("upload object key contains an invalid resource segment")
	}
	return path.Join(prefix, ownerID, label, filename), nil
}

func (s CatalogStore) cachePath(asset UploadedAsset) (string, error) {
	filename := strings.TrimSpace(asset.Upload.VideoFilename)
	ownerID := asset.Upload.VOD.OwnerID
	label := asset.Upload.VOD.Label
	if safeSegment(ownerID) != ownerID || safeSegment(label) != label || filename == "." || filename == "" || filepath.Base(filename) != filename {
		return "", errors.New("upload cache path contains an invalid resource segment")
	}
	directory := filepath.Join(s.Files.Root, ownerID, label)
	if !s.Files.isAssetDir(directory) {
		return "", errors.New("upload cache path is outside its root")
	}
	return filepath.Join(directory, filename), nil
}

func (s CatalogStore) objectPrefix() string {
	if value := strings.Trim(strings.TrimSpace(s.ObjectPrefix), "/"); value != "" {
		return value
	}
	return "uploads"
}

var _ Store = LocalStore{}
var _ Store = CatalogStore{}
