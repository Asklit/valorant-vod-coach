package vodstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asklit/valorant-vod-coach/internal/app"
)

type memoryUploadCatalog struct {
	records map[string]app.UploadRecord
	saveErr error
}

func (c *memoryUploadCatalog) SaveUpload(_ context.Context, record app.UploadRecord) error {
	if c.saveErr != nil {
		return c.saveErr
	}
	if c.records == nil {
		c.records = map[string]app.UploadRecord{}
	}
	c.records[record.VOD.Label] = record
	return nil
}

func (c *memoryUploadCatalog) ListUploads(_ context.Context, ownerID string, includeAll bool) ([]app.UploadRecord, error) {
	records := make([]app.UploadRecord, 0)
	for _, record := range c.records {
		if includeAll || record.VOD.OwnerID == ownerID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (c *memoryUploadCatalog) FindUpload(_ context.Context, label string, ownerID string, includeAll bool) (app.UploadRecord, bool, error) {
	record, found := c.records[label]
	if !found || (!includeAll && record.VOD.OwnerID != ownerID) {
		return app.UploadRecord{}, false, nil
	}
	return record, true, nil
}

func (c *memoryUploadCatalog) DeleteUpload(_ context.Context, label string, ownerID string, includeAll bool) error {
	record, found := c.records[label]
	if !found || (!includeAll && record.VOD.OwnerID != ownerID) {
		return ErrUploadNotFound
	}
	delete(c.records, label)
	return nil
}

func TestCatalogStoreUsesCatalogForVisibleMetadata(t *testing.T) {
	root := t.TempDir()
	catalog := &memoryUploadCatalog{records: map[string]app.UploadRecord{}}
	store := CatalogStore{
		Files:   LocalStore{Root: filepath.Join(root, "uploads"), FFprobePath: writeFakeFFprobe(t, root)},
		Catalog: catalog,
	}
	staged, err := store.Stage(context.Background(), strings.NewReader("fake mp4 content"))
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	asset, err := store.Create(context.Background(), CreateUploadRequest{
		OwnerID: "user_01", Title: "Local title", Rank: "diamond", OriginalFilename: "round.mp4", Staged: staged,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	record := catalog.records[asset.Upload.VOD.Label]
	record.VOD.Title = "Database title"
	catalog.records[asset.Upload.VOD.Label] = record

	assets, err := store.List(context.Background(), "user_01", false)
	if err != nil || len(assets) != 1 || assets[0].Upload.VOD.Title != "Database title" {
		t.Fatalf("catalog metadata must be authoritative: %+v / %v", assets, err)
	}
	if _, err := store.Resolve(context.Background(), asset.Upload.VOD.Label, "other_user", false); !errors.Is(err, ErrUploadNotFound) {
		t.Fatalf("catalog must enforce owner isolation, got %v", err)
	}
}

func TestCatalogStoreRollsBackFileWhenCatalogSaveFails(t *testing.T) {
	root := t.TempDir()
	store := CatalogStore{
		Files:   LocalStore{Root: filepath.Join(root, "uploads"), FFprobePath: writeFakeFFprobe(t, root)},
		Catalog: &memoryUploadCatalog{saveErr: errors.New("database unavailable")},
	}
	staged, err := store.Stage(context.Background(), strings.NewReader("fake mp4 content"))
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	_, err = store.Create(context.Background(), CreateUploadRequest{
		OwnerID: "user_01", Title: "My VOD", Rank: "gold", OriginalFilename: "round.mp4", Staged: staged,
	})
	if err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("expected catalog failure, got %v", err)
	}
	assets, listErr := store.Files.List(context.Background(), "user_01", false)
	if listErr != nil || len(assets) != 0 {
		t.Fatalf("failed catalog save must not leave a visible file upload: %+v / %v", assets, listErr)
	}
}

func TestCatalogStorePublishesAndMaterializesVODFromObjectStorage(t *testing.T) {
	root := t.TempDir()
	catalog := &memoryUploadCatalog{records: map[string]app.UploadRecord{}}
	objects := &recordingVODBlobStore{objects: map[string][]byte{}}
	store := CatalogStore{
		Files:   LocalStore{Root: filepath.Join(root, "uploads"), FFprobePath: writeFakeFFprobe(t, root)},
		Catalog: catalog,
		Objects: objects,
	}
	staged, err := store.Stage(context.Background(), strings.NewReader("durable mp4 content"))
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	asset, err := store.Create(context.Background(), CreateUploadRequest{
		OwnerID: "user_01", Title: "Object VOD", Rank: "diamond", OriginalFilename: "round.mp4", Staged: staged,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	expectedKey := filepath.ToSlash(filepath.Join("uploads", "user_01", asset.Upload.VOD.Label, "video.mp4"))
	if asset.ObjectKey != expectedKey || catalog.records[asset.Upload.VOD.Label].VideoObjectKey != expectedKey {
		t.Fatalf("object key was not persisted: asset=%+v record=%+v", asset, catalog.records[asset.Upload.VOD.Label])
	}
	if err := os.RemoveAll(filepath.Dir(asset.Path)); err != nil {
		t.Fatal(err)
	}

	materialized, err := store.Resolve(context.Background(), asset.Upload.VOD.Label, "user_01", false)
	if err != nil {
		t.Fatalf("resolve cold cache: %v", err)
	}
	raw, err := os.ReadFile(materialized.Path)
	if err != nil || string(raw) != "durable mp4 content" {
		t.Fatalf("materialized content = %q, err=%v", raw, err)
	}
	if objects.downloads != 1 {
		t.Fatalf("download count = %d, want 1", objects.downloads)
	}
	if _, err := store.Resolve(context.Background(), asset.Upload.VOD.Label, "user_01", false); err != nil || objects.downloads != 1 {
		t.Fatalf("warm cache should not redownload: downloads=%d err=%v", objects.downloads, err)
	}
}

func TestCatalogStoreRevokesCatalogAccessBeforeCleanupFailure(t *testing.T) {
	root := t.TempDir()
	catalog := &memoryUploadCatalog{records: map[string]app.UploadRecord{}}
	objects := &recordingVODBlobStore{objects: map[string][]byte{}, deleteErr: errors.New("S3 unavailable")}
	store := CatalogStore{
		Files:   LocalStore{Root: filepath.Join(root, "uploads"), FFprobePath: writeFakeFFprobe(t, root)},
		Catalog: catalog,
		Objects: objects,
	}
	staged, _ := store.Stage(context.Background(), strings.NewReader("video"))
	asset, err := store.Create(context.Background(), CreateUploadRequest{
		OwnerID: "user_01", Title: "Delete VOD", Rank: "gold", OriginalFilename: "round.mp4", Staged: staged,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = store.Delete(context.Background(), asset.Upload.VOD.Label, "user_01", false)
	var cleanupErr PostDeleteCleanupError
	if !errors.As(err, &cleanupErr) {
		t.Fatalf("delete error = %v, want PostDeleteCleanupError", err)
	}
	if _, found, _ := catalog.FindUpload(context.Background(), asset.Upload.VOD.Label, "user_01", false); found {
		t.Fatal("catalog access was not revoked after delete")
	}
}

func TestUploadObjectKeyRejectsUnsafeSegments(t *testing.T) {
	if _, err := UploadObjectKey("uploads", "../user", "upload_01", "video.mp4"); err == nil {
		t.Fatal("UploadObjectKey() accepted unsafe owner")
	}
	if _, err := UploadObjectKey("uploads", "user_01", "upload_01", "../video.mp4"); err == nil {
		t.Fatal("UploadObjectKey() accepted unsafe filename")
	}
}

type recordingVODBlobStore struct {
	objects   map[string][]byte
	downloads int
	deleteErr error
}

func (s *recordingVODBlobStore) UploadFile(_ context.Context, key string, localPath string, _ string) error {
	raw, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	s.objects[key] = raw
	return nil
}

func (s *recordingVODBlobStore) DownloadFile(_ context.Context, key string, localPath string) error {
	raw, found := s.objects[key]
	if !found {
		return errors.New("object not found")
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
		return err
	}
	s.downloads++
	return os.WriteFile(localPath, raw, 0o600)
}

func (s *recordingVODBlobStore) DeleteObject(_ context.Context, key string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.objects, key)
	return nil
}

func (*recordingVODBlobStore) DeletePrefix(context.Context, string) error { return nil }

var _ app.BlobStore = (*recordingVODBlobStore)(nil)
