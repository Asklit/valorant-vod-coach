package vodstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/asklit/valorant-vod-coach/internal/adapters/dataset"
	"github.com/asklit/valorant-vod-coach/internal/adapters/media"
	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

const (
	MetadataSchemaVersion = 2
	MetadataJSONName      = "metadata.json"
	DefaultMaxUploadBytes = int64(6 << 30)
)

var ErrUploadNotFound = errors.New("uploaded VOD not found")

type Store interface {
	Stage(ctx context.Context, reader io.Reader) (StagedUpload, error)
	Discard(staged StagedUpload) error
	Create(ctx context.Context, request CreateUploadRequest) (UploadedAsset, error)
	List(ctx context.Context, ownerID string, includeAll bool) ([]UploadedAsset, error)
	Resolve(ctx context.Context, label string, ownerID string, includeAll bool) (UploadedAsset, error)
	Update(ctx context.Context, label string, ownerID string, includeAll bool, request UpdateUploadRequest) (UploadedAsset, error)
	Delete(ctx context.Context, label string, ownerID string, includeAll bool) (UploadedAsset, error)
}

type LocalStore struct {
	Root           string
	FFprobePath    string
	MaxUploadBytes int64
	Clock          func() time.Time
}

type StagedUpload struct {
	Path      string
	SizeBytes int64
}

type CreateUploadRequest struct {
	OwnerID          string
	Title            string
	Rank             string
	Map              string
	Agent            string
	OriginalFilename string
	Staged           StagedUpload
}

type UpdateUploadRequest struct {
	Title string
	Rank  string
	Map   string
	Agent string
}

type UploadedVOD struct {
	SchemaVersion int                 `json:"schema_version"`
	VOD           domain.VOD          `json:"vod"`
	VideoFilename string              `json:"video_filename"`
	SizeBytes     int64               `json:"size_bytes"`
	Media         domain.MediaSummary `json:"media"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

type UploadedAsset struct {
	Upload UploadedVOD
	Path   string
}

func (s LocalStore) Stage(ctx context.Context, reader io.Reader) (StagedUpload, error) {
	if err := ctx.Err(); err != nil {
		return StagedUpload{}, err
	}
	if reader == nil {
		return StagedUpload{}, errors.New("video file is required")
	}
	root := strings.TrimSpace(s.Root)
	if root == "" {
		return StagedUpload{}, errors.New("upload root is required")
	}
	stagingRoot := filepath.Join(root, ".staging")
	if err := os.MkdirAll(stagingRoot, 0o700); err != nil {
		return StagedUpload{}, err
	}
	temp, err := os.CreateTemp(stagingRoot, "vod-*.part")
	if err != nil {
		return StagedUpload{}, err
	}
	path := temp.Name()
	success := false
	defer func() {
		temp.Close()
		if !success {
			os.Remove(path)
		}
	}()
	limit := s.maxUploadBytes()
	written, err := io.Copy(temp, io.LimitReader(reader, limit+1))
	if err != nil {
		return StagedUpload{}, fmt.Errorf("store upload: %w", err)
	}
	if written == 0 {
		return StagedUpload{}, errors.New("video file is empty")
	}
	if written > limit {
		return StagedUpload{}, fmt.Errorf("video exceeds the %d byte upload limit", limit)
	}
	if err := temp.Sync(); err != nil {
		return StagedUpload{}, err
	}
	if err := temp.Close(); err != nil {
		return StagedUpload{}, err
	}
	success = true
	return StagedUpload{Path: path, SizeBytes: written}, nil
}

func (s LocalStore) Discard(staged StagedUpload) error {
	if !s.isStagedPath(staged.Path) {
		return errors.New("invalid staged upload path")
	}
	err := os.Remove(staged.Path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s LocalStore) Create(ctx context.Context, request CreateUploadRequest) (UploadedAsset, error) {
	if err := ctx.Err(); err != nil {
		return UploadedAsset{}, err
	}
	request, ext, err := normalizeCreateRequest(request)
	if err != nil {
		return UploadedAsset{}, err
	}
	if !s.isStagedPath(request.Staged.Path) {
		return UploadedAsset{}, errors.New("invalid staged upload path")
	}
	defer s.Discard(request.Staged)

	probeCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	probe, err := media.RunProbe(probeCtx, defaultString(s.FFprobePath, "ffprobe"), request.Staged.Path)
	if err != nil {
		return UploadedAsset{}, fmt.Errorf("validate uploaded video: %w", err)
	}
	video, ok := media.VideoStream(probe.Metadata)
	if !ok || video.Width <= 0 || video.Height <= 0 {
		return UploadedAsset{}, errors.New("uploaded file does not contain a valid video stream")
	}

	now := s.now()
	label, err := uploadLabel(now)
	if err != nil {
		return UploadedAsset{}, err
	}
	videoFilename := "video" + ext
	ownerSegment := safeSegment(request.OwnerID)
	finalDir := filepath.Join(s.Root, ownerSegment, label)
	if err := os.MkdirAll(finalDir, 0o700); err != nil {
		return UploadedAsset{}, err
	}
	finalPath := filepath.Join(finalDir, videoFilename)
	if err := os.Rename(request.Staged.Path, finalPath); err != nil {
		return UploadedAsset{}, err
	}
	created := false
	defer func() {
		if !created {
			os.Remove(finalPath)
			os.Remove(finalDir)
		}
	}()

	mediaSummary := summarizeProbe(probe.Metadata)
	vod := domain.VOD{
		Label:                   label,
		VideoID:                 label,
		Rank:                    domain.Rank(request.Rank),
		Title:                   request.Title,
		Channel:                 "Uploaded VOD",
		ManifestDurationSeconds: mediaSummary.DurationSeconds,
		Map:                     request.Map,
		Agent:                   request.Agent,
		OwnerID:                 request.OwnerID,
		SourceType:              "upload",
		OriginalFilename:        request.OriginalFilename,
		UploadedAt:              now,
	}
	upload := UploadedVOD{
		SchemaVersion: MetadataSchemaVersion,
		VOD:           vod,
		VideoFilename: videoFilename,
		SizeBytes:     request.Staged.SizeBytes,
		Media:         mediaSummary,
		UpdatedAt:     now,
	}
	if err := writeMetadata(filepath.Join(finalDir, MetadataJSONName), upload); err != nil {
		return UploadedAsset{}, err
	}
	created = true
	return UploadedAsset{Upload: upload, Path: finalPath}, nil
}

func (s LocalStore) List(ctx context.Context, ownerID string, includeAll bool) ([]UploadedAsset, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root := strings.TrimSpace(s.Root)
	if root == "" {
		return nil, errors.New("upload root is required")
	}
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return []UploadedAsset{}, nil
	}
	var metadataPaths []string
	if includeAll {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && (entry.Name() == ".staging" || entry.Name() == ".trash") {
				return filepath.SkipDir
			}
			if !entry.IsDir() && entry.Name() == MetadataJSONName {
				metadataPaths = append(metadataPaths, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		ownerRoot := filepath.Join(root, safeSegment(ownerID))
		entries, err := os.ReadDir(ownerRoot)
		if os.IsNotExist(err) {
			return []UploadedAsset{}, nil
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				metadataPaths = append(metadataPaths, filepath.Join(ownerRoot, entry.Name(), MetadataJSONName))
			}
		}
	}

	assets := make([]UploadedAsset, 0, len(metadataPaths))
	for _, path := range metadataPaths {
		asset, err := readAsset(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !includeAll && asset.Upload.VOD.OwnerID != ownerID {
			continue
		}
		assets = append(assets, asset)
	}
	sort.Slice(assets, func(i, j int) bool {
		return assets[i].Upload.VOD.UploadedAt.After(assets[j].Upload.VOD.UploadedAt)
	})
	return assets, nil
}

func (s LocalStore) Resolve(ctx context.Context, label string, ownerID string, includeAll bool) (UploadedAsset, error) {
	assets, err := s.List(ctx, ownerID, includeAll)
	if err != nil {
		return UploadedAsset{}, err
	}
	for _, asset := range assets {
		if asset.Upload.VOD.Label == strings.TrimSpace(label) {
			return asset, nil
		}
	}
	return UploadedAsset{}, ErrUploadNotFound
}

func (s LocalStore) Update(ctx context.Context, label string, ownerID string, includeAll bool, request UpdateUploadRequest) (UploadedAsset, error) {
	if err := ctx.Err(); err != nil {
		return UploadedAsset{}, err
	}
	title, rank, mapName, agent, err := normalizeMetadata(request.Title, request.Rank, request.Map, request.Agent)
	if err != nil {
		return UploadedAsset{}, err
	}
	asset, err := s.Resolve(ctx, label, ownerID, includeAll)
	if err != nil {
		return UploadedAsset{}, err
	}
	asset.Upload.SchemaVersion = MetadataSchemaVersion
	asset.Upload.VOD.Title = title
	asset.Upload.VOD.Rank = domain.Rank(rank)
	asset.Upload.VOD.Map = mapName
	asset.Upload.VOD.Agent = agent
	asset.Upload.UpdatedAt = s.now()
	if err := writeMetadata(filepath.Join(filepath.Dir(asset.Path), MetadataJSONName), asset.Upload); err != nil {
		return UploadedAsset{}, err
	}
	return asset, nil
}

func (s LocalStore) Delete(ctx context.Context, label string, ownerID string, includeAll bool) (UploadedAsset, error) {
	if err := ctx.Err(); err != nil {
		return UploadedAsset{}, err
	}
	asset, err := s.Resolve(ctx, label, ownerID, includeAll)
	if err != nil {
		return UploadedAsset{}, err
	}
	assetDir := filepath.Dir(asset.Path)
	if !s.isAssetDir(assetDir) {
		return UploadedAsset{}, errors.New("invalid uploaded VOD directory")
	}
	trashRoot := filepath.Join(s.Root, ".trash")
	if err := os.MkdirAll(trashRoot, 0o700); err != nil {
		return UploadedAsset{}, err
	}
	tombstone := filepath.Join(trashRoot, filepath.Base(assetDir)+"-"+time.Now().UTC().Format("20060102t150405.000000000"))
	if err := os.Rename(assetDir, tombstone); err != nil {
		return UploadedAsset{}, err
	}
	if err := os.RemoveAll(tombstone); err != nil {
		return UploadedAsset{}, fmt.Errorf("remove uploaded VOD: %w", err)
	}
	return asset, nil
}

type OwnedResolver struct {
	Store      Store
	OwnerID    string
	IncludeAll bool
	Fallback   app.VODResolver
}

func (r OwnedResolver) ResolveVOD(ctx context.Context, label string) (domain.VOD, string, error) {
	asset, err := r.Store.Resolve(ctx, label, r.OwnerID, r.IncludeAll)
	if err == nil {
		return asset.Upload.VOD, asset.Path, nil
	}
	if !errors.Is(err, ErrUploadNotFound) {
		return domain.VOD{}, "", err
	}
	if r.Fallback == nil {
		return domain.VOD{}, "", fmt.Errorf("unknown VOD label %q", label)
	}
	return r.Fallback.ResolveVOD(ctx, label)
}

func normalizeCreateRequest(request CreateUploadRequest) (CreateUploadRequest, string, error) {
	request.OwnerID = strings.TrimSpace(request.OwnerID)
	request.OriginalFilename = filepath.Base(strings.TrimSpace(request.OriginalFilename))
	if request.OwnerID == "" {
		return request, "", errors.New("owner_id is required")
	}
	title, rank, mapName, agent, err := normalizeMetadata(request.Title, request.Rank, request.Map, request.Agent)
	if err != nil {
		return request, "", err
	}
	request.Title, request.Rank, request.Map, request.Agent = title, rank, mapName, agent
	ext := strings.ToLower(filepath.Ext(request.OriginalFilename))
	if !allowedVideoExtension(ext) {
		return request, "", fmt.Errorf("unsupported video extension %q", ext)
	}
	if request.Staged.Path == "" || request.Staged.SizeBytes <= 0 {
		return request, "", errors.New("staged video is required")
	}
	return request, ext, nil
}

func normalizeMetadata(title string, rank string, mapName string, agent string) (string, string, string, string, error) {
	title = cleanText(title)
	rank = strings.ToLower(strings.TrimSpace(rank))
	mapName = cleanText(mapName)
	agent = cleanText(agent)
	if title == "" {
		return "", "", "", "", errors.New("title is required")
	}
	if len(title) > 160 || len(mapName) > 64 || len(agent) > 64 {
		return "", "", "", "", errors.New("upload metadata is too long")
	}
	if !dataset.IsValidRank(dataset.Rank(rank)) {
		return "", "", "", "", fmt.Errorf("unsupported rank %q", rank)
	}
	return title, rank, mapName, agent, nil
}

func allowedVideoExtension(ext string) bool {
	for _, allowed := range dataset.VideoExtensions {
		if ext == allowed {
			return true
		}
	}
	return false
}

func summarizeProbe(metadata media.Metadata) domain.MediaSummary {
	summary := domain.MediaSummary{}
	if duration, ok := media.Duration(metadata); ok {
		summary.DurationSeconds = duration.Seconds()
		summary.HasDuration = true
	}
	if size, ok := media.SizeBytes(metadata); ok {
		summary.SizeBytes = size
		summary.HasSize = true
	}
	if stream, ok := media.VideoStream(metadata); ok {
		summary.VideoCodec = stream.CodecName
		summary.Width = stream.Width
		summary.Height = stream.Height
		summary.FrameRate = media.FrameRate(stream)
	}
	if stream, ok := media.AudioStream(metadata); ok {
		summary.AudioCodec = stream.CodecName
		summary.HasAudio = true
	}
	return summary
}

func readAsset(metadataPath string) (UploadedAsset, error) {
	raw, err := os.ReadFile(metadataPath)
	if err != nil {
		return UploadedAsset{}, err
	}
	var upload UploadedVOD
	if err := json.Unmarshal(raw, &upload); err != nil {
		return UploadedAsset{}, fmt.Errorf("decode %s: %w", metadataPath, err)
	}
	path := filepath.Join(filepath.Dir(metadataPath), filepath.Base(upload.VideoFilename))
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		if err == nil {
			err = ErrUploadNotFound
		}
		return UploadedAsset{}, err
	}
	return UploadedAsset{Upload: upload, Path: path}, nil
}

func writeMetadata(path string, upload UploadedVOD) error {
	raw, err := json.MarshalIndent(upload, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), ".metadata-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func uploadLabel(now time.Time) (string, error) {
	random := make([]byte, 5)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "upload_" + now.UTC().Format("20060102t150405") + "_" + hex.EncodeToString(random), nil
}

func (s LocalStore) maxUploadBytes() int64 {
	if s.MaxUploadBytes > 0 {
		return s.MaxUploadBytes
	}
	return DefaultMaxUploadBytes
}

func (s LocalStore) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func (s LocalStore) isStagedPath(path string) bool {
	root, err := filepath.Abs(filepath.Join(s.Root, ".staging"))
	if err != nil {
		return false
	}
	target, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func (s LocalStore) isAssetDir(path string) bool {
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return false
	}
	target, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	return len(parts) == 2 && parts[0] != ".staging" && parts[0] != ".trash"
}

func safeSegment(value string) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' {
			builder.WriteRune(char)
		}
	}
	if builder.Len() == 0 {
		return "unknown"
	}
	return builder.String()
}

func cleanText(value string) string {
	return strings.TrimSpace(strings.Map(func(char rune) rune {
		if unicode.IsControl(char) {
			return -1
		}
		return char
	}, value))
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
