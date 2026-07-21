package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVideoProbeWritesArtifact(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "vods.tsv")
	rawRoot := filepath.Join(root, "raw")
	outRoot := filepath.Join(root, "processed")
	rankDir := filepath.Join(rawRoot, "diamond")

	if err := os.MkdirAll(rankDir, 0o755); err != nil {
		t.Fatalf("mkdir raw rank dir: %v", err)
	}

	manifest := "1\tdiamond\tdiamond_example\tabc123\thttps://www.youtube.com/watch?v=abc123\t37:04\tDiamond VOD\tChannel\ttitle\tgame_vod_20_40\n"
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	videoPath := filepath.Join(rankDir, "diamond_example__abc123.mp4")
	if err := os.WriteFile(videoPath, []byte("fake video"), 0o644); err != nil {
		t.Fatalf("write fake video: %v", err)
	}

	ffprobePath := filepath.Join(root, "fake-ffprobe")
	ffprobeScript := `#!/bin/sh
cat <<'JSON'
{
  "streams": [
    {
      "index": 0,
      "codec_name": "h264",
      "codec_type": "video",
      "width": 1920,
      "height": 1080,
      "avg_frame_rate": "60/1"
    },
    {
      "index": 1,
      "codec_name": "aac",
      "codec_type": "audio"
    }
  ],
  "format": {
    "filename": "fake.mp4",
    "nb_streams": 2,
    "format_name": "mov,mp4",
    "duration": "2224.000000",
    "size": "1301252227",
    "bit_rate": "4680312"
  }
}
JSON
`
	if err := os.WriteFile(ffprobePath, []byte(ffprobeScript), 0o755); err != nil {
		t.Fatalf("write fake ffprobe: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"video", "probe",
		"--manifest", manifestPath,
		"--raw-root", rawRoot,
		"--out-root", outRoot,
		"--ffprobe", ffprobePath,
		"--vod", "diamond_example",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}

	if got := stdout.String(); !strings.Contains(got, "diamond_example") || !strings.Contains(got, "h264 1920x1080") {
		t.Fatalf("unexpected stdout:\n%s", got)
	}

	artifactPath := filepath.Join(outRoot, "diamond_example", "probe.ffprobe.json")
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("expected probe artifact: %v", err)
	}
}

func TestRunVideoSampleWritesFramesManifest(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "vods.tsv")
	rawRoot := filepath.Join(root, "raw")
	outRoot := filepath.Join(root, "processed")
	rankDir := filepath.Join(rawRoot, "diamond")

	if err := os.MkdirAll(rankDir, 0o755); err != nil {
		t.Fatalf("mkdir raw rank dir: %v", err)
	}

	manifest := "1\tdiamond\tdiamond_example\tabc123\thttps://www.youtube.com/watch?v=abc123\t37:04\tDiamond VOD\tChannel\ttitle\tgame_vod_20_40\n"
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	videoPath := filepath.Join(rankDir, "diamond_example__abc123.mp4")
	if err := os.WriteFile(videoPath, []byte("fake video"), 0o644); err != nil {
		t.Fatalf("write fake video: %v", err)
	}

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

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"video", "sample",
		"--manifest", manifestPath,
		"--raw-root", rawRoot,
		"--out-root", outRoot,
		"--ffmpeg", ffmpegPath,
		"--vod", "diamond_example",
		"--fps", "2",
		"--start", "10s",
		"--duration", "5s",
		"--name", "test_sample",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}

	if got := stdout.String(); !strings.Contains(got, "diamond_example") || !strings.Contains(got, "test_sample") {
		t.Fatalf("unexpected stdout:\n%s", got)
	}

	manifestArtifact := filepath.Join(outRoot, "diamond_example", "frames", "test_sample", "frames.json")
	raw, err := os.ReadFile(manifestArtifact)
	if err != nil {
		t.Fatalf("expected frames manifest: %v", err)
	}

	if got := string(raw); !strings.Contains(got, `"frame_count": 2`) || !strings.Contains(got, `"timestamp_seconds": 10.5`) {
		t.Fatalf("unexpected frames manifest:\n%s", got)
	}
}
