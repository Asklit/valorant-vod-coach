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
	"time"
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
	if payload.VODs[0].VideoURL != "/api/vods/diamond_example/video" {
		t.Fatalf("unexpected video URL: %s", payload.VODs[0].VideoURL)
	}
}

func TestServerServesLocalVODVideo(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)

	request := httptest.NewRequest(http.MethodGet, "/api/vods/diamond_example/video", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); got != "fake video" {
		t.Fatalf("unexpected video response body: %q", got)
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

	if got := response.Body.String(); !strings.Contains(got, `"run_id": "api_test"`) ||
		!strings.Contains(got, `"frame_count": 2`) ||
		!strings.Contains(got, `"contact_sheet_path"`) {
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

	request = httptest.NewRequest(http.MethodGet, "/api/reports?vod_label=diamond_example", nil)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, `"run_id": "api_test"`) ||
		!strings.Contains(got, `"frame_count": 2`) ||
		!strings.Contains(got, `"schema_version": 7`) ||
		!strings.Contains(got, `"contact_sheet"`) {
		t.Fatalf("unexpected report list response:\n%s", got)
	}
}

func TestServerRunsAsyncAnalysisJob(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)

	body := bytes.NewBufferString(`{"vod_label":"diamond_example","run_id":"async_test","fps":"1","duration_seconds":5,"force":true,"async":true}`)
	request := httptest.NewRequest(http.MethodPost, "/api/analysis-runs", body)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", response.Code, response.Body.String())
	}

	var job AnalysisJobResponse
	if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode job response: %v", err)
	}
	if job.JobID == "" || job.RunID != "async_test" || job.Status == "" {
		t.Fatalf("unexpected initial job: %+v", job)
	}

	for attempts := 0; attempts < 40; attempts++ {
		request = httptest.NewRequest(http.MethodGet, "/api/analysis-runs/"+job.JobID, nil)
		response = httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
			t.Fatalf("decode polled job: %v", err)
		}
		if job.Status == "completed" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	if job.Status != "completed" {
		t.Fatalf("expected completed job, got %+v", job)
	}
	if job.Report == nil || job.Report.RunID != "async_test" || job.Report.Gameplay == nil {
		t.Fatalf("expected completed gameplay report: %+v", job)
	}
	if job.ReportJSON == "" || job.ReportMD == "" {
		t.Fatalf("expected report paths: %+v", job)
	}
}

func TestServerRejectsModelReviewWithoutVisionURL(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)

	body := bytes.NewBufferString(`{"vod_label":"diamond_example","run_id":"model_review_missing_url","fps":"1","duration_seconds":5,"force":true,"model_review":true}`)
	request := httptest.NewRequest(http.MethodPost, "/api/analysis-runs", body)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, "vision service URL is not configured") {
		t.Fatalf("unexpected response:\n%s", got)
	}
}

func TestServerHealthIncludesAnalyzerContract(t *testing.T) {
	server := NewServer(Config{})

	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, `"schema_version": 7`) ||
		!strings.Contains(got, `"analyzer": "visual-heuristic-gameplay"`) ||
		!strings.Contains(got, `"model_review_configured": false`) ||
		!strings.Contains(got, `"model_review_available": false`) ||
		!strings.Contains(got, `"configured": false`) {
		t.Fatalf("unexpected health response:\n%s", got)
	}
}

func TestDevCORSAllowsFallbackVitePorts(t *testing.T) {
	if !isAllowedDevOrigin("http://127.0.0.1:5174") {
		t.Fatalf("expected fallback Vite port to be allowed")
	}
	if !isAllowedDevOrigin("http://localhost:5179") {
		t.Fatalf("expected localhost Vite port to be allowed")
	}
	if isAllowedDevOrigin("https://127.0.0.1:5174") {
		t.Fatalf("expected https dev origin to be rejected")
	}
	if isAllowedDevOrigin("http://example.com:5174") {
		t.Fatalf("expected non-local dev origin to be rejected")
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
