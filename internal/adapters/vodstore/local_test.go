package vodstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalStoreStagesWithLimit(t *testing.T) {
	store := LocalStore{Root: t.TempDir(), MaxUploadBytes: 4}
	if _, err := store.Stage(context.Background(), strings.NewReader("12345")); err == nil {
		t.Fatal("expected upload limit error")
	}
	entries, err := os.ReadDir(filepath.Join(store.Root, ".staging"))
	if err != nil {
		t.Fatalf("read staging: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed stage must be removed: %+v", entries)
	}
}

func TestLocalStoreCreatesListsAndResolvesUpload(t *testing.T) {
	root := t.TempDir()
	ffprobe := writeFakeFFprobe(t, root)
	store := LocalStore{Root: filepath.Join(root, "uploads"), FFprobePath: ffprobe, Clock: func() time.Time {
		return time.Date(2026, 7, 31, 12, 30, 0, 0, time.UTC)
	}}
	staged, err := store.Stage(context.Background(), strings.NewReader("fake mp4 content"))
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	asset, err := store.Create(context.Background(), CreateUploadRequest{
		OwnerID: "user_01", Title: "My ranked game", Rank: "diamond", Map: "Bind", Agent: "Sova",
		OriginalFilename: "round.mp4", Staged: staged,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if asset.Upload.VOD.SourceType != "upload" || asset.Upload.VOD.OwnerID != "user_01" || asset.Upload.Media.Width != 1920 {
		t.Fatalf("unexpected upload: %+v", asset.Upload)
	}
	if _, err := os.Stat(asset.Path); err != nil {
		t.Fatalf("video path: %v", err)
	}
	assets, err := store.List(context.Background(), "user_01", false)
	if err != nil || len(assets) != 1 {
		t.Fatalf("list: %+v / %v", assets, err)
	}
	resolved, err := store.Resolve(context.Background(), asset.Upload.VOD.Label, "user_01", false)
	if err != nil || resolved.Path != asset.Path {
		t.Fatalf("resolve: %+v / %v", resolved, err)
	}
	if _, err := store.Resolve(context.Background(), asset.Upload.VOD.Label, "user_02", false); err == nil {
		t.Fatal("other owner must not resolve upload")
	}
	updated, err := store.Update(context.Background(), asset.Upload.VOD.Label, "user_01", false, UpdateUploadRequest{
		Title: "Updated match", Rank: "ascendant", Map: "Abyss", Agent: "Jett",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Upload.VOD.Title != "Updated match" || updated.Upload.VOD.Rank != "ascendant" || updated.Upload.SchemaVersion != MetadataSchemaVersion {
		t.Fatalf("unexpected update: %+v", updated.Upload)
	}
	if _, err := store.Update(context.Background(), asset.Upload.VOD.Label, "user_02", false, UpdateUploadRequest{Title: "No", Rank: "gold"}); !errors.Is(err, ErrUploadNotFound) {
		t.Fatalf("other owner update must be hidden, got %v", err)
	}
	deleted, err := store.Delete(context.Background(), asset.Upload.VOD.Label, "user_01", false)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleted.Upload.VOD.Label != asset.Upload.VOD.Label {
		t.Fatalf("unexpected deleted upload: %+v", deleted)
	}
	if _, err := os.Stat(asset.Path); !os.IsNotExist(err) {
		t.Fatalf("deleted video must be gone, got %v", err)
	}
}

func writeFakeFFprobe(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "fake-ffprobe")
	script := `#!/bin/sh
cat <<'JSON'
{"streams":[{"codec_name":"h264","codec_type":"video","width":1920,"height":1080,"avg_frame_rate":"60/1"},{"codec_name":"aac","codec_type":"audio"}],"format":{"duration":"1800","size":"16"}}
JSON
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write ffprobe: %v", err)
	}
	return path
}
