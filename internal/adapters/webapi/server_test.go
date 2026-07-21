package webapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerListsVODs(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)

	request := httptest.NewRequest(http.MethodGet, "/api/vods", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}

	var payload VODListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.Counts.Enabled != 1 || payload.Counts.Downloaded != 1 {
		t.Fatalf("unexpected counts: %+v", payload.Counts)
	}
	if len(payload.VODs) != 1 || payload.VODs[0].Label != "diamond_example" {
		t.Fatalf("unexpected VODs: %+v", payload.VODs)
	}
}

func TestServerRunsAnalysisAndReturnsLatestReport(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)

	body := bytes.NewBufferString(`{"vod_label":"diamond_example","run_id":"api_test","fps":"1","duration_seconds":5,"force":true}`)
	request := httptest.NewRequest(http.MethodPost, "/api/analysis-runs", body)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}

	if got := response.Body.String(); !strings.Contains(got, `"run_id": "api_test"`) || !strings.Contains(got, `"frame_count": 2`) {
		t.Fatalf("unexpected analysis response:\n%s", got)
	}

	reportPath := filepath.Join(fixture.outRoot, "diamond_example", "reports", "api_test", "report.json")
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("expected report file: %v", err)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/reports/latest?vod_label=diamond_example", nil)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, `"run_id": "api_test"`) {
		t.Fatalf("unexpected latest report response:\n%s", got)
	}
}

type fixture struct {
	config  Config
	outRoot string
}

func newFixture(t *testing.T) fixture {
	t.Helper()

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
printf fake > "$dir/frame_000001.jpg"
printf fake > "$dir/frame_000002.jpg"
`
	if err := os.WriteFile(ffmpegPath, []byte(ffmpegScript), 0o755); err != nil {
		t.Fatalf("write fake ffmpeg: %v", err)
	}

	return fixture{
		config: Config{
			ManifestPath:  manifestPath,
			RawRoot:       rawRoot,
			ProcessedRoot: outRoot,
			FFprobePath:   ffprobePath,
			FFmpegPath:    ffmpegPath,
		},
		outRoot: outRoot,
	}
}
