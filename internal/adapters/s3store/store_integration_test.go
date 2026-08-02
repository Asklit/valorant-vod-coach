package s3store

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestS3StoreIntegration(t *testing.T) {
	bucket := strings.TrimSpace(os.Getenv("TEST_S3_BUCKET"))
	prefix := strings.Trim(strings.TrimSpace(os.Getenv("TEST_S3_PREFIX")), "/")
	if bucket == "" || prefix == "" {
		t.Skip("TEST_S3_BUCKET and TEST_S3_PREFIX are required")
	}
	if !strings.Contains(strings.ToLower(prefix), "test") {
		t.Fatal("TEST_S3_PREFIX must contain 'test'")
	}
	pathStyle, _ := strconv.ParseBool(os.Getenv("TEST_S3_USE_PATH_STYLE"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := New(ctx, Config{
		Endpoint:        os.Getenv("TEST_S3_ENDPOINT"),
		Region:          valueOrDefault(os.Getenv("TEST_S3_REGION"), "us-east-1"),
		Bucket:          bucket,
		AccessKeyID:     os.Getenv("TEST_S3_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("TEST_S3_SECRET_ACCESS_KEY"),
		Prefix:          prefix,
		UsePathStyle:    pathStyle,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := store.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	destination := filepath.Join(root, "cache", "destination.txt")
	if err := os.WriteFile(source, []byte("s3 integration"), 0o600); err != nil {
		t.Fatal(err)
	}
	key := "integration/" + time.Now().UTC().Format("20060102t150405.000000000") + ".txt"
	if err := store.UploadFile(ctx, key, source, "text/plain"); err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	defer store.DeleteObject(context.Background(), key)
	if err := store.DownloadFile(ctx, key, destination); err != nil {
		t.Fatalf("DownloadFile() error = %v", err)
	}
	raw, err := os.ReadFile(destination)
	if err != nil || string(raw) != "s3 integration" {
		t.Fatalf("downloaded data = %q, err=%v", raw, err)
	}
	if err := store.DeleteObject(ctx, key); err != nil {
		t.Fatalf("DeleteObject() error = %v", err)
	}
}

func valueOrDefault(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
