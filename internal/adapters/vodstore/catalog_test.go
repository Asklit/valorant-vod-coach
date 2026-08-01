package vodstore

import (
	"context"
	"errors"
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
