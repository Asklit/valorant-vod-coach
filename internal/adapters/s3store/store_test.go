package s3store

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
)

func TestNormalizeObjectKeys(t *testing.T) {
	for _, valid := range []string{"users/user_1/video.mp4", "artifacts/report.json", "prefix"} {
		if got, err := normalizeRequiredKey(valid); err != nil || got != valid {
			t.Fatalf("normalizeRequiredKey(%q) = %q, %v", valid, got, err)
		}
	}
	for _, invalid := range []string{"", "/absolute", "../escape", "a/../b", "a//b", `a\\b`, "."} {
		if _, err := normalizeRequiredKey(invalid); err == nil {
			t.Fatalf("normalizeRequiredKey(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestStoreAddsConfiguredPrefix(t *testing.T) {
	store := Store{Bucket: "bucket", Prefix: "environment/dev"}
	got, err := store.objectKey("uploads/user/video.mp4")
	if err != nil {
		t.Fatalf("objectKey() error = %v", err)
	}
	if got != "environment/dev/uploads/user/video.mp4" {
		t.Fatalf("objectKey() = %q", got)
	}
}

func TestStoreUploadsAndAtomicallyDownloadsFiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.mp4")
	if err := os.WriteFile(source, []byte("video bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	transfer := &memoryTransferClient{objects: map[string][]byte{}}
	store := Store{Bucket: "bucket", Prefix: "dev", transfers: transfer}
	if err := store.UploadFile(context.Background(), "uploads/user/video.mp4", source, "video/mp4"); err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	if got := string(transfer.objects["dev/uploads/user/video.mp4"]); got != "video bytes" {
		t.Fatalf("uploaded body = %q", got)
	}
	if transfer.contentType != "video/mp4" || transfer.contentLength != int64(len("video bytes")) {
		t.Fatalf("upload metadata: type=%q length=%d", transfer.contentType, transfer.contentLength)
	}

	destination := filepath.Join(root, "cache", "video.mp4")
	if err := store.DownloadFile(context.Background(), "uploads/user/video.mp4", destination); err != nil {
		t.Fatalf("DownloadFile() error = %v", err)
	}
	raw, err := os.ReadFile(destination)
	if err != nil || string(raw) != "video bytes" {
		t.Fatalf("downloaded body = %q, err=%v", raw, err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(destination), ".object-*.part"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary download files remain: %+v err=%v", matches, err)
	}
}

func TestStoreRemovesPartialDownloadAfterFailure(t *testing.T) {
	root := t.TempDir()
	transfer := &memoryTransferClient{downloadErr: errors.New("connection reset")}
	store := Store{Bucket: "bucket", transfers: transfer}
	destination := filepath.Join(root, "cache", "missing.mp4")
	if err := store.DownloadFile(context.Background(), "uploads/user/missing.mp4", destination); err == nil {
		t.Fatal("DownloadFile() unexpectedly succeeded")
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial destination exists: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(destination), ".object-*.part"))
	if len(matches) != 0 {
		t.Fatalf("temporary download files remain: %+v", matches)
	}
}

type memoryTransferClient struct {
	objects       map[string][]byte
	contentType   string
	contentLength int64
	downloadErr   error
}

func (c *memoryTransferClient) UploadObject(_ context.Context, input *transfermanager.UploadObjectInput, _ ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error) {
	raw, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	if c.objects == nil {
		c.objects = map[string][]byte{}
	}
	c.objects[aws.ToString(input.Key)] = raw
	c.contentType = aws.ToString(input.ContentType)
	c.contentLength = aws.ToInt64(input.ContentLength)
	return &transfermanager.UploadObjectOutput{}, nil
}

func (c *memoryTransferClient) DownloadObject(_ context.Context, input *transfermanager.DownloadObjectInput, _ ...func(*transfermanager.Options)) (*transfermanager.DownloadObjectOutput, error) {
	if c.downloadErr != nil {
		return nil, c.downloadErr
	}
	raw, found := c.objects[aws.ToString(input.Key)]
	if !found {
		return nil, errors.New("object not found")
	}
	if _, err := input.WriterAt.WriteAt(raw, 0); err != nil {
		return nil, err
	}
	return &transfermanager.DownloadObjectOutput{}, nil
}
