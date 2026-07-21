package media

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

func TestRunReviewClipExtractionWithFakeFFmpeg(t *testing.T) {
	root := t.TempDir()
	ffmpegPath := filepath.Join(root, "fake-ffmpeg")
	ffmpegScript := `#!/bin/sh
last=""
for arg in "$@"; do
  last="$arg"
done
dir="$(dirname "$last")"
mkdir -p "$dir"
printf fake-clip > "$last"
`
	if err := os.WriteFile(ffmpegPath, []byte(ffmpegScript), 0o755); err != nil {
		t.Fatalf("write fake ffmpeg: %v", err)
	}

	inputPath := filepath.Join(root, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fake video"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	outputDir := filepath.Join(root, "clips")
	result, err := RunReviewClipExtraction(context.Background(), ReviewClipOptions{
		FFmpegPath: ffmpegPath,
		InputPath:  inputPath,
		OutputDir:  outputDir,
		RunID:      "clip_test",
		Windows: []domain.ReviewWindow{
			{
				ID:           "window 01",
				Kind:         "combat_spike",
				Severity:     domain.FindingSeverityMedium,
				StartSeconds: 15.25,
				EndSeconds:   27.75,
				PeakSeconds:  21,
			},
		},
		Overwrite: true,
	})
	if err != nil {
		t.Fatalf("RunReviewClipExtraction returned error: %v", err)
	}

	if len(result.Windows) != 1 {
		t.Fatalf("unexpected window count: %d", len(result.Windows))
	}
	if result.Windows[0].ClipDurationSeconds != 12.5 {
		t.Fatalf("unexpected clip duration: %.3f", result.Windows[0].ClipDurationSeconds)
	}
	if !strings.HasSuffix(result.Windows[0].ClipPath, "review_01_window_01.mp4") {
		t.Fatalf("unexpected clip path: %s", result.Windows[0].ClipPath)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].Type != "review_clip" {
		t.Fatalf("unexpected artifacts: %+v", result.Artifacts)
	}
	if _, err := os.Stat(result.Windows[0].ClipPath); err != nil {
		t.Fatalf("expected clip file: %v", err)
	}

	_, err = RunReviewClipExtraction(context.Background(), ReviewClipOptions{
		FFmpegPath: ffmpegPath,
		InputPath:  inputPath,
		OutputDir:  outputDir,
		RunID:      "clip_test",
		Windows: []domain.ReviewWindow{
			{ID: "window 01", StartSeconds: 15.25, EndSeconds: 27.75},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected existing clip error, got %v", err)
	}
}
