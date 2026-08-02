package app

import (
	"context"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

type UploadRecord struct {
	VOD            domain.VOD
	VideoPath      string
	VideoObjectKey string
	VideoFilename  string
	SizeBytes      int64
	Media          domain.MediaSummary
	UpdatedAt      time.Time
}

type UploadCatalog interface {
	SaveUpload(ctx context.Context, record UploadRecord) error
	ListUploads(ctx context.Context, ownerID string, includeAll bool) ([]UploadRecord, error)
	FindUpload(ctx context.Context, label string, ownerID string, includeAll bool) (UploadRecord, bool, error)
	DeleteUpload(ctx context.Context, label string, ownerID string, includeAll bool) error
}
