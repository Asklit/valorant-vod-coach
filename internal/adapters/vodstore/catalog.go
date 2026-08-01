package vodstore

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sort"

	"github.com/asklit/valorant-vod-coach/internal/app"
)

type CatalogStore struct {
	Files   LocalStore
	Catalog app.UploadCatalog
}

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
	if err := s.Catalog.SaveUpload(ctx, uploadRecord(asset)); err != nil {
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
	return uploadedAsset(record), nil
}

func (s CatalogStore) Update(ctx context.Context, label string, ownerID string, includeAll bool, request UpdateUploadRequest) (UploadedAsset, error) {
	previous, err := s.Resolve(ctx, label, ownerID, includeAll)
	if err != nil {
		return UploadedAsset{}, err
	}
	asset, err := s.Files.Update(ctx, label, ownerID, includeAll, request)
	if err != nil {
		return UploadedAsset{}, err
	}
	if err := s.Catalog.SaveUpload(ctx, uploadRecord(asset)); err != nil {
		_, _ = s.Files.Update(context.Background(), label, previous.Upload.VOD.OwnerID, true, UpdateUploadRequest{
			Title: previous.Upload.VOD.Title,
			Rank:  string(previous.Upload.VOD.Rank),
			Map:   previous.Upload.VOD.Map,
			Agent: previous.Upload.VOD.Agent,
		})
		return UploadedAsset{}, err
	}
	return asset, nil
}

func (s CatalogStore) Delete(ctx context.Context, label string, ownerID string, includeAll bool) (UploadedAsset, error) {
	asset, err := s.Resolve(ctx, label, ownerID, includeAll)
	if err != nil {
		return UploadedAsset{}, err
	}
	if err := s.Catalog.DeleteUpload(ctx, label, ownerID, includeAll); err != nil {
		return UploadedAsset{}, err
	}
	deleted, err := s.Files.Delete(ctx, label, asset.Upload.VOD.OwnerID, true)
	if errors.Is(err, ErrUploadNotFound) {
		return asset, nil
	}
	if err != nil {
		return UploadedAsset{}, err
	}
	return deleted, nil
}

func uploadRecord(asset UploadedAsset) app.UploadRecord {
	return app.UploadRecord{
		VOD:           asset.Upload.VOD,
		VideoPath:     asset.Path,
		VideoFilename: asset.Upload.VideoFilename,
		SizeBytes:     asset.Upload.SizeBytes,
		Media:         asset.Upload.Media,
		UpdatedAt:     asset.Upload.UpdatedAt,
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
		Path: record.VideoPath,
	}
}

var _ Store = LocalStore{}
var _ Store = CatalogStore{}
