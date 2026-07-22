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
case "$last" in
  *contact_sheet.jpg)
    printf fake > "$last"
    ;;
  *)
    printf fake > "$dir/frame_000001.jpg"
    printf fake > "$dir/frame_000002.jpg"
    ;;
esac
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

	contactSheetPath := filepath.Join(outRoot, "diamond_example", "frames", "test_sample", "contact_sheet.jpg")
	if _, err := os.Stat(contactSheetPath); err != nil {
		t.Fatalf("expected contact sheet: %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "contact_sheet.jpg") {
		t.Fatalf("expected contact sheet path in stdout:\n%s", got)
	}
}

func TestRunAnalyzeRunWritesReport(t *testing.T) {
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

	ffmpegPath := filepath.Join(root, "fake-ffmpeg")
	ffmpegScript := `#!/bin/sh
last=""
for arg in "$@"; do
  last="$arg"
done
dir="$(dirname "$last")"
mkdir -p "$dir"
case "$last" in
  *contact_sheet.jpg)
    printf fake > "$last"
    ;;
  *)
    printf fake > "$dir/frame_000001.jpg"
    printf fake > "$dir/frame_000002.jpg"
    ;;
esac
`
	if err := os.WriteFile(ffmpegPath, []byte(ffmpegScript), 0o755); err != nil {
		t.Fatalf("write fake ffmpeg: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"analyze", "run",
		"--manifest", manifestPath,
		"--raw-root", rawRoot,
		"--out-root", outRoot,
		"--ffprobe", ffprobePath,
		"--ffmpeg", ffmpegPath,
		"--vod", "diamond_example",
		"--run-id", "test_run",
		"--fps", "1",
		"--duration", "5s",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}

	if got := stdout.String(); !strings.Contains(got, "diamond_example") ||
		!strings.Contains(got, "test_run") ||
		!strings.Contains(got, "WINDOWS") ||
		!strings.Contains(got, "CLIPS") ||
		!strings.Contains(got, "MODEL_RUNS") {
		t.Fatalf("unexpected stdout:\n%s", got)
	}

	reportPath := filepath.Join(outRoot, "diamond_example", "reports", "test_run", "report.json")
	rawReport, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("expected JSON report: %v", err)
	}

	if got := string(rawReport); !strings.Contains(got, `"run_id": "test_run"`) ||
		!strings.Contains(got, `"analyzer": "visual-heuristic-gameplay"`) ||
		!strings.Contains(got, `"gameplay"`) ||
		!strings.Contains(got, `"gameplay_review"`) ||
		!strings.Contains(got, `"contact_sheet_path"`) {
		t.Fatalf("unexpected report:\n%s", got)
	}

	markdownPath := filepath.Join(outRoot, "diamond_example", "reports", "test_run", "report.md")
	if _, err := os.Stat(markdownPath); err != nil {
		t.Fatalf("expected markdown report: %v", err)
	}
}

func TestRunEvalRunWritesEvaluation(t *testing.T) {
	root := t.TempDir()
	reportPath := filepath.Join(root, "report.json")
	annotationsPath := filepath.Join(root, "annotations.json")
	outRoot := filepath.Join(root, "evaluations")

	report := `{
  "schema_version": 8,
  "run_id": "analysis_01",
  "status": "completed",
  "vod": {"label": "iron_example", "rank": "iron"},
  "media": {"has_duration": false, "has_size": false, "has_audio": false},
  "sample": {"name": "sample", "manifest_path": "frames.json", "fps": "1", "frame_count": 2},
  "gameplay": {
    "sampled_frames": 2,
    "analyzed_frames": 2,
    "review_window_count": 2,
    "gameplay_events": [
      {"id": "event_combat_001", "type": "combat_candidate", "category": "fight_selection", "severity": "medium", "title": "Combat", "detail": "Candidate", "timestamp_seconds": 10},
      {"id": "event_combat_002", "type": "combat_candidate", "category": "fight_selection", "severity": "medium", "title": "Combat", "detail": "Candidate", "timestamp_seconds": 35},
      {"id": "event_rotation_001", "type": "rotation_candidate", "category": "rotation_timing", "severity": "low", "title": "Rotation", "detail": "Candidate", "timestamp_seconds": 50}
    ]
  },
  "findings": [],
  "timeline": [],
  "artifacts": [],
  "metadata": {"analyzer": "visual-heuristic-gameplay", "mode": "local"}
}`
	annotations := `{
  "schema_version": 1,
  "vod_label": "iron_example",
  "tolerance_seconds": 6,
  "labels": [
    {"id": "label_fight_001", "type": "death", "timestamp_seconds": 12, "description": "Bad duel"},
    {"id": "label_tempo_001", "type": "tempo", "timestamp_seconds": 90, "description": "Lost tempo"}
  ]
}`
	if err := os.WriteFile(reportPath, []byte(report), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
	if err := os.WriteFile(annotationsPath, []byte(annotations), 0o644); err != nil {
		t.Fatalf("write annotations: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"eval", "run",
		"--report", reportPath,
		"--annotations", annotationsPath,
		"--out-root", outRoot,
		"--run-id", "eval_test",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "eval_test") ||
		!strings.Contains(got, "PRECISION") ||
		!strings.Contains(got, "0.50") {
		t.Fatalf("unexpected stdout:\n%s", got)
	}

	evaluationPath := filepath.Join(outRoot, "eval_test", evaluationJSONName)
	rawEvaluation, err := os.ReadFile(evaluationPath)
	if err != nil {
		t.Fatalf("expected evaluation JSON: %v", err)
	}
	if got := string(rawEvaluation); !strings.Contains(got, `"match_count": 1`) ||
		!strings.Contains(got, `"missed_labels"`) ||
		!strings.Contains(got, `"false_positives"`) {
		t.Fatalf("unexpected evaluation JSON:\n%s", got)
	}

	markdownPath := filepath.Join(outRoot, "eval_test", evaluationMarkdownName)
	rawMarkdown, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatalf("expected evaluation markdown: %v", err)
	}
	if got := string(rawMarkdown); !strings.Contains(got, "Gameplay Event Evaluation") ||
		!strings.Contains(got, "Missed Labels") ||
		!strings.Contains(got, "False Positives") {
		t.Fatalf("unexpected evaluation markdown:\n%s", got)
	}
}

func TestFlagHelpReturnsSuccess(t *testing.T) {
	tests := [][]string{
		{"analyze", "run", "--help"},
		{"dataset", "validate", "--help"},
		{"dataset", "list", "--help"},
		{"dataset", "status", "--help"},
		{"eval", "run", "--help"},
		{"video", "probe", "--help"},
		{"video", "sample", "--help"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			code := run(args, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("expected exit code 0, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
			}
		})
	}
}
