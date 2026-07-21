package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunContactSheetWritesOutput(t *testing.T) {
	root := t.TempDir()
	framesDir := filepath.Join(root, "frames")
	if err := os.MkdirAll(framesDir, 0o755); err != nil {
		t.Fatalf("mkdir frames dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(framesDir, "frame_000001.jpg"), []byte("fake frame"), 0o644); err != nil {
		t.Fatalf("write fake frame: %v", err)
	}

	ffmpegPath := filepath.Join(root, "fake-ffmpeg")
	ffmpegScript := `#!/bin/sh
last=""
for arg in "$@"; do
  last="$arg"
done
printf fake-contact-sheet > "$last"
`
	if err := os.WriteFile(ffmpegPath, []byte(ffmpegScript), 0o755); err != nil {
		t.Fatalf("write fake ffmpeg: %v", err)
	}

	result, err := RunContactSheet(context.Background(), ContactSheetOptions{
		FFmpegPath: ffmpegPath,
		FramesDir:  framesDir,
		Overwrite:  true,
	})
	if err != nil {
		t.Fatalf("run contact sheet: %v", err)
	}

	if result.Path != filepath.ToSlash(filepath.Join(framesDir, ContactSheetName)) {
		t.Fatalf("unexpected contact sheet path: %s", result.Path)
	}
	if _, err := os.Stat(filepath.Join(framesDir, ContactSheetName)); err != nil {
		t.Fatalf("expected contact sheet file: %v", err)
	}
}
