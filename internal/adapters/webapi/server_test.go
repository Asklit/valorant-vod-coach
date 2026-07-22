package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
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
		!strings.Contains(got, `"schema_version": 8`) ||
		!strings.Contains(got, `"contact_sheet"`) {
		t.Fatalf("unexpected report list response:\n%s", got)
	}
}

func TestServerUsesReportCatalogWhenConfigured(t *testing.T) {
	fixture := newFixture(t)
	reportDir := filepath.Join(fixture.outRoot, "diamond_example", "reports", "db_run")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatalf("mkdir report dir: %v", err)
	}
	reportPath := filepath.Join(reportDir, "report.json")
	if err := os.WriteFile(reportPath, []byte(`{
  "schema_version": 8,
  "run_id": "db_run",
  "status": "completed",
  "generated_at": "2026-07-22T12:30:00Z",
  "vod": {"label": "diamond_example"},
  "sample": {"name": "db_sample", "fps": "0.5", "frame_count": 12},
  "findings": [],
  "timeline": [],
  "artifacts": [],
  "metadata": {"analyzer": "db-backed"}
}`), 0o644); err != nil {
		t.Fatalf("write report json: %v", err)
	}

	generatedAt := time.Date(2026, 7, 22, 12, 30, 0, 0, time.UTC)
	catalog := &fakeReportCatalog{summaries: []app.ReportCatalogSummary{{
		SchemaVersion:        8,
		VODLabel:             "diamond_example",
		RunID:                "db_run",
		Status:               "completed",
		GeneratedAt:          generatedAt,
		FindingCount:         3,
		FrameCount:           12,
		ReviewWindowCount:    2,
		RoundSegmentCount:    1,
		ModelReviewTaskCount: 2,
		ModelReviewRunCount:  1,
		Analyzer:             "db-backed",
		SampleName:           "db_sample",
		SampleFPS:            "0.5",
		SampleDuration:       60,
		ContactSheetPath:     filepath.Join(fixture.outRoot, "diamond_example", "frames", "sheet.jpg"),
		JSONPath:             reportPath,
		MarkdownPath:         filepath.Join(reportDir, "report.md"),
	}}}
	fixture.config.ReportCatalog = catalog
	server := NewServer(fixture.config)

	request := httptest.NewRequest(http.MethodGet, "/api/reports?vod_label=diamond_example", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	got := response.Body.String()
	if !strings.Contains(got, `"run_id": "db_run"`) ||
		!strings.Contains(got, `"finding_count": 3`) ||
		!strings.Contains(got, `"model_review_task_count": 2`) ||
		!strings.Contains(got, reportPath) {
		t.Fatalf("unexpected report list response:\n%s", got)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/vods", nil)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	got = response.Body.String()
	if !strings.Contains(got, `"latest_report_id": "db_run"`) ||
		!strings.Contains(got, `"report_count": 1`) {
		t.Fatalf("unexpected VOD list response:\n%s", got)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/reports/latest?vod_label=diamond_example", nil)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, `"run_id": "db_run"`) ||
		!strings.Contains(got, `"analyzer": "db-backed"`) {
		t.Fatalf("unexpected latest report response:\n%s", got)
	}

	if len(catalog.labels) < 3 {
		t.Fatalf("expected catalog to be used by reports, VOD list, and latest report; labels=%v", catalog.labels)
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

func TestServerListsEvaluations(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	evaluationDir := filepath.Join(fixture.outRoot, "evaluations", "eval_01")
	if err := os.MkdirAll(evaluationDir, 0o755); err != nil {
		t.Fatalf("mkdir evaluation dir: %v", err)
	}
	evaluationJSON := `{
  "schema_version": 1,
  "run_id": "eval_01",
  "generated_at": "2026-07-22T12:00:00Z",
  "vod_label": "diamond_example",
  "report_run_id": "api_test",
  "tolerance_seconds": 6,
  "overall": {
    "label_count": 4,
    "prediction_count": 5,
    "match_count": 3,
    "precision": 0.6,
    "recall": 0.75,
    "f1": 0.6667
  }
}`
	if err := os.WriteFile(filepath.Join(evaluationDir, "evaluation.json"), []byte(evaluationJSON), 0o644); err != nil {
		t.Fatalf("write evaluation json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(evaluationDir, "evaluation.md"), []byte("# Eval\n"), 0o644); err != nil {
		t.Fatalf("write evaluation markdown: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/evaluations?vod_label=diamond_example", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	got := response.Body.String()
	if !strings.Contains(got, `"run_id": "eval_01"`) ||
		!strings.Contains(got, `"precision": 0.6`) ||
		!strings.Contains(got, `"markdown_path"`) {
		t.Fatalf("unexpected evaluations response:\n%s", got)
	}
}

func TestServerListsEvaluationAnnotations(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	annotationsPath := filepath.Join(fixture.config.EvaluationAnnotationsRoot, "diamond_example.json")
	if err := os.WriteFile(annotationsPath, []byte(`{
  "schema_version": 1,
  "vod_label": "diamond_example",
  "report_run_id": "api_test",
  "tolerance_seconds": 4,
  "labels": [
    {"id": "label_001", "type": "combat", "timestamp_seconds": 2}
  ]
}`), 0o644); err != nil {
		t.Fatalf("write annotations: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/evaluation-annotations?vod_label=diamond_example", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	got := response.Body.String()
	if !strings.Contains(got, `"vod_label": "diamond_example"`) ||
		!strings.Contains(got, `"label_count": 1`) ||
		!strings.Contains(got, `"path"`) {
		t.Fatalf("unexpected annotations response:\n%s", got)
	}
}

func TestServerRunsEvaluation(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	reportDir := filepath.Join(fixture.outRoot, "diamond_example", "reports", "api_test")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatalf("mkdir report dir: %v", err)
	}
	reportJSON := `{
  "schema_version": 8,
  "run_id": "api_test",
  "status": "completed",
  "generated_at": "2026-07-22T12:00:00Z",
  "vod": {"label": "diamond_example"},
  "sample": {"name": "sample", "fps": "1", "frame_count": 1},
  "gameplay": {
    "gameplay_events": [
      {
        "id": "event_combat_001",
        "type": "combat_candidate",
        "category": "combat",
        "severity": "medium",
        "title": "Combat candidate",
        "timestamp_seconds": 2
      }
    ]
  }
}`
	if err := os.WriteFile(filepath.Join(reportDir, "report.json"), []byte(reportJSON), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
	annotationsPath := filepath.Join(fixture.config.EvaluationAnnotationsRoot, "diamond_example.json")
	if err := os.WriteFile(annotationsPath, []byte(`{
  "schema_version": 1,
  "vod_label": "diamond_example",
  "labels": [
    {"id": "label_001", "type": "combat", "timestamp_seconds": 2.5}
  ]
}`), 0o644); err != nil {
		t.Fatalf("write annotations: %v", err)
	}

	body := bytes.NewBufferString(`{"vod_label":"diamond_example","report_run_id":"api_test","annotations_path":"` + annotationsPath + `","run_id":"eval_api","force":true}`)
	request := httptest.NewRequest(http.MethodPost, "/api/evaluation-runs", body)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	got := response.Body.String()
	if !strings.Contains(got, `"run_id": "eval_api"`) ||
		!strings.Contains(got, `"f1": 1`) ||
		!strings.Contains(got, `"evaluation_json"`) {
		t.Fatalf("unexpected evaluation response:\n%s", got)
	}

	if _, err := os.Stat(filepath.Join(fixture.outRoot, "evaluations", "eval_api", "evaluation.json")); err != nil {
		t.Fatalf("expected evaluation json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fixture.outRoot, "evaluations", "eval_api", "evaluation.md")); err != nil {
		t.Fatalf("expected evaluation markdown: %v", err)
	}
}

func TestServerCreatesAndListsManualCorrections(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)

	body := bytes.NewBufferString(`{
  "vod_label": "diamond_example",
  "report_run_id": "api_test",
  "type": "false_detection",
  "target_id": "event_001",
  "comment": "This event should be ignored.",
  "timestamp_seconds": 42.5
}`)
	request := httptest.NewRequest(http.MethodPost, "/api/corrections", body)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", response.Code, response.Body.String())
	}
	got := response.Body.String()
	if !strings.Contains(got, `"type": "false_detection"`) ||
		!strings.Contains(got, `"target_id": "event_001"`) ||
		!strings.Contains(got, `"json_path"`) {
		t.Fatalf("unexpected correction response:\n%s", got)
	}

	correctionsPath := filepath.Join(fixture.outRoot, "corrections", "diamond_example", "api_test", app.ManualCorrectionsJSONName)
	if _, err := os.Stat(correctionsPath); err != nil {
		t.Fatalf("expected corrections file: %v", err)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/corrections?vod_label=diamond_example&report_run_id=api_test", nil)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, `"comment": "This event should be ignored."`) {
		t.Fatalf("unexpected correction list response:\n%s", got)
	}
}

func TestServerAuthRegisterLoginAndAdminOverview(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)

	body := bytes.NewBufferString(`{"email":"coach@example.com","password":"secret-pass","display_name":"Coach"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", body)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", response.Code, response.Body.String())
	}
	var auth AuthResponse
	if err := json.Unmarshal(response.Body.Bytes(), &auth); err != nil {
		t.Fatalf("decode auth response: %v", err)
	}
	if auth.Token == "" || auth.User.Role != app.AuthRoleAdmin {
		t.Fatalf("unexpected auth response: %+v", auth)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
	request.Header.Set("Authorization", "Bearer "+auth.Token)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected admin overview 200, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, `"user_count": 1`) ||
		!strings.Contains(got, `"schema_version": 8`) {
		t.Fatalf("unexpected admin overview:\n%s", got)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	request.Header.Set("Authorization", "Bearer "+auth.Token)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected logout 200, got %d: %s", response.Code, response.Body.String())
	}

	body = bytes.NewBufferString(`{"email":"coach@example.com","password":"secret-pass"}`)
	request = httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected login 200, got %d: %s", response.Code, response.Body.String())
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
	if got := response.Body.String(); !strings.Contains(got, `"schema_version": 8`) ||
		!strings.Contains(got, `"analyzer": "visual-heuristic-gameplay"`) ||
		!strings.Contains(got, `"model_review_configured": false`) ||
		!strings.Contains(got, `"model_review_available": false`) ||
		!strings.Contains(got, `"configured": false`) {
		t.Fatalf("unexpected health response:\n%s", got)
	}
}

func TestServerDiagnosticsEndpoints(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected healthz 200, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, `"status": "ok"`) ||
		!strings.Contains(got, `"service": "vod-web"`) {
		t.Fatalf("unexpected healthz response:\n%s", got)
	}

	request = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected readyz 200, got %d: %s", response.Code, response.Body.String())
	}
	got := response.Body.String()
	if !strings.Contains(got, `"status": "ready"`) ||
		!strings.Contains(got, `"manifest"`) ||
		!strings.Contains(got, `"vision_service"`) {
		t.Fatalf("unexpected readyz response:\n%s", got)
	}

	request = httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected pprof 200, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, "profiles") {
		t.Fatalf("unexpected pprof response:\n%s", got)
	}
}

func TestServerMetricsEndpoint(t *testing.T) {
	server := NewServer(Config{VisionURL: "http://vision.invalid"})

	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	request = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	got := response.Body.String()
	for _, expected := range []string{
		`vodcoach_info{schema_version="8",analyzer="visual-heuristic-gameplay"} 1`,
		`vodcoach_model_review_configured 1`,
		`vodcoach_http_requests_total{method="GET",route="/api/health",status="200"} 1`,
		`vodcoach_analysis_jobs_total{status="completed"} 0`,
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, got)
		}
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
	annotationsRoot := filepath.Join(root, "evals")
	rankDir := filepath.Join(rawRoot, "diamond")
	if err := os.MkdirAll(rankDir, 0o755); err != nil {
		t.Fatalf("mkdir raw rank dir: %v", err)
	}
	if err := os.MkdirAll(annotationsRoot, 0o755); err != nil {
		t.Fatalf("mkdir annotations dir: %v", err)
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
			ManifestPath:              manifestPath,
			RawRoot:                   rawRoot,
			ProcessedRoot:             outRoot,
			EvaluationAnnotationsRoot: annotationsRoot,
			FFprobePath:               ffprobePath,
			FFmpegPath:                ffmpegPath,
			AuthHashIterations:        4,
		},
		outRoot: outRoot,
	}
}

type fakeReportCatalog struct {
	labels    []string
	summaries []app.ReportCatalogSummary
}

func (c *fakeReportCatalog) ListReportSummaries(_ context.Context, vodLabel string) ([]app.ReportCatalogSummary, error) {
	c.labels = append(c.labels, vodLabel)
	return c.summaries, nil
}
