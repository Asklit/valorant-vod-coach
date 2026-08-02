package report

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

func TestPublishingStoreUploadsReferencedReportFiles(t *testing.T) {
	root := t.TempDir()
	framePath := filepath.Join(root, "vod_1", "frames", "sample", "frame_000001.jpg")
	if err := os.MkdirAll(filepath.Dir(framePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(framePath, []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	report := domain.AnalysisReport{
		SchemaVersion: domain.AnalysisReportSchemaVersion,
		RunID:         "run_01",
		Status:        "completed",
		GeneratedAt:   time.Now().UTC(),
		VOD:           domain.VOD{Label: "vod_1"},
		Sample: domain.FrameSampleSummary{
			FrameCount: 1,
			Frames:     []domain.Frame{{Index: 1, Path: framePath}},
		},
		Findings:  []domain.Finding{},
		Timeline:  []domain.TimelineEvent{},
		Artifacts: []domain.Artifact{},
	}
	objects := &recordingBlobStore{}
	store := PublishingStore{Local: LocalStore{ProcessedRoot: root}, Root: root, Objects: objects}

	saved, err := store.SaveReport(context.Background(), report, true)
	if err != nil {
		t.Fatalf("SaveReport() error = %v", err)
	}
	if len(objects.uploads) != 3 {
		t.Fatalf("uploads = %+v, want frame and two reports", objects.uploads)
	}
	for _, key := range []string{
		"artifacts/vod_1/frames/sample/frame_000001.jpg",
		"artifacts/vod_1/reports/run_01/report.json",
		"artifacts/vod_1/reports/run_01/report.md",
	} {
		if _, ok := objects.uploads[key]; !ok {
			t.Fatalf("missing object key %q in %+v", key, objects.uploads)
		}
	}
	if saved.JSONPath == "" || saved.MarkdownPath == "" {
		t.Fatalf("saved report paths = %+v", saved)
	}
}

func TestArtifactObjectKeyRejectsOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	if _, err := ArtifactObjectKey(root, "artifacts", outside); err == nil {
		t.Fatal("ArtifactObjectKey() unexpectedly accepted outside path")
	}
}

type recordingBlobStore struct {
	uploads map[string]string
	mu      sync.Mutex
}

func (s *recordingBlobStore) UploadFile(_ context.Context, key string, localPath string, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.uploads == nil {
		s.uploads = map[string]string{}
	}
	s.uploads[key] = localPath
	return nil
}

func (*recordingBlobStore) DownloadFile(context.Context, string, string) error { return nil }
func (*recordingBlobStore) DeleteObject(context.Context, string) error         { return nil }
func (*recordingBlobStore) DeletePrefix(context.Context, string) error         { return nil }

var _ app.BlobStore = (*recordingBlobStore)(nil)
