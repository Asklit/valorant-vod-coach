package app

import "context"

// BlobStore is the durable file boundary used for VODs and generated artifacts.
// Processing still happens in a local workspace because ffmpeg requires seekable files.
type BlobStore interface {
	UploadFile(ctx context.Context, key string, localPath string, contentType string) error
	DownloadFile(ctx context.Context, key string, localPath string) error
	DeleteObject(ctx context.Context, key string) error
	DeletePrefix(ctx context.Context, prefix string) error
}
