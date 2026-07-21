package video

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunSampleWithFakeFFmpeg(t *testing.T) {
	root := t.TempDir()
	ffmpegPath := filepath.Join(root, "fake-ffmpeg")
	ffmpegScript := `#!/bin/sh
last=""
for arg in "$@"; do
  last="$arg"
done
dir="$(dirname "$last")"
mkdir -p "$dir"
printf fake > "$dir/frame_000001.jpg"
printf fake > "$dir/frame_000002.jpg"
`
	if err := os.WriteFile(ffmpegPath, []byte(ffmpegScript), 0o755); err != nil {
		t.Fatalf("write fake ffmpeg: %v", err)
	}

	inputPath := filepath.Join(root, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fake video"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	outputDir := filepath.Join(root, "frames")
	result, err := RunSample(context.Background(), SampleOptions{
		FFmpegPath:   ffmpegPath,
		InputPath:    inputPath,
		OutputDir:    outputDir,
		FPS:          "2",
		Start:        10 * time.Second,
		Duration:     5 * time.Second,
		ImageQuality: 3,
	})
	if err != nil {
		t.Fatalf("RunSample returned error: %v", err)
	}

	if result.FrameCount != 2 {
		t.Fatalf("unexpected frame count: %d", result.FrameCount)
	}
	if result.Frames[0].TimestampSeconds != 10 {
		t.Fatalf("unexpected first timestamp: %v", result.Frames[0].TimestampSeconds)
	}
	if result.Frames[1].TimestampSeconds != 10.5 {
		t.Fatalf("unexpected second timestamp: %v", result.Frames[1].TimestampSeconds)
	}
}

func TestDefaultSampleName(t *testing.T) {
	got := DefaultSampleName("0.5", 30*time.Second, 2*time.Minute)
	want := "sample_0.5fps_from_30s_120s"
	if got != want {
		t.Fatalf("unexpected sample name: got %q want %q", got, want)
	}
}

func TestParseFPSRejectsInvalidValue(t *testing.T) {
	if _, err := ParseFPS("0"); err == nil {
		t.Fatal("expected fps=0 to fail")
	}
}
