package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

func TestServerListsVODs(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	token := registerTestAdmin(t, server)

	request := httptest.NewRequest(http.MethodGet, "/api/vods", nil)
	authorize(request, token)
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

func TestServerUploadsStreamsAndAnalyzesOwnedVOD(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	token := registerTestAdmin(t, server)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range map[string]string{"title": "My Bind review", "rank": "diamond", "map": "Bind", "agent": "Sova"} {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	file, err := writer.CreateFormFile("file", "ranked.mp4")
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := file.Write([]byte("fake uploaded video")); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/vods/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	authorize(request, token)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("upload expected 201, got %d: %s", response.Code, response.Body.String())
	}
	var uploaded UploadVODResponse
	if err := json.Unmarshal(response.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	if uploaded.VOD.SourceType != "upload" || uploaded.VOD.Map != "Bind" || uploaded.VOD.Agent != "Sova" || uploaded.VOD.Label == "" {
		t.Fatalf("unexpected upload: %+v", uploaded.VOD)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/vods", nil)
	authorize(request, token)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"source_type": "upload"`) {
		t.Fatalf("uploaded VOD missing from library %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, uploaded.VOD.VideoURL, nil)
	authorize(request, token)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "fake uploaded video" {
		t.Fatalf("stream expected uploaded content, got %d: %q", response.Code, response.Body.String())
	}

	analysisBody := bytes.NewBufferString(fmt.Sprintf(`{"vod_label":%q,"run_id":"upload_analysis","fps":"1","duration_seconds":5,"force":true}`, uploaded.VOD.Label))
	request = httptest.NewRequest(http.MethodPost, "/api/analysis-runs", analysisBody)
	authorize(request, token)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"run_id": "upload_analysis"`) {
		t.Fatalf("analysis expected 200, got %d: %s", response.Code, response.Body.String())
	}
	processedUploadRoot := filepath.Join(fixture.outRoot, "users", token.user.ID, "analyses", uploaded.VOD.Label)
	if _, err := os.Stat(processedUploadRoot); err != nil {
		t.Fatalf("processed upload artifacts must exist before deletion: %v", err)
	}

	request = httptest.NewRequest(http.MethodPatch, "/api/vods/"+uploaded.VOD.Label, bytes.NewBufferString(`{"title":"Updated upload","rank":"ascendant","map":"Abyss","agent":"Jett"}`))
	authorize(request, token)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"title": "Updated upload"`) || !strings.Contains(response.Body.String(), `"rank": "ascendant"`) {
		t.Fatalf("update expected 200, got %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/vods/"+uploaded.VOD.Label, nil)
	authorize(request, token)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"deleted": true`) {
		t.Fatalf("delete expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(processedUploadRoot); !os.IsNotExist(err) {
		t.Fatalf("processed upload artifacts must be removed, got %v", err)
	}
}

func TestServerRequiresAuthForProductAPI(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)

	request := httptest.NewRequest(http.MethodGet, "/api/vods", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/vods/diamond_example/video", nil)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected private video 401, got %d: %s", response.Code, response.Body.String())
	}
}

func TestServerServesLocalVODVideo(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	token := registerTestAdmin(t, server)

	request := httptest.NewRequest(http.MethodGet, "/api/vods/diamond_example/video", nil)
	authorize(request, token)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); got != "fake video" {
		t.Fatalf("unexpected video response body: %q", got)
	}
}

func TestServerPreventsAccessToAnotherUsersUpload(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	registerTestAdmin(t, server)
	ownerToken := registerTestAccount(t, server, "owner@example.com")
	otherToken := registerTestAccount(t, server, "other@example.com")
	uploaded := uploadTestVOD(t, server, ownerToken)

	request := httptest.NewRequest(http.MethodGet, uploaded.VOD.VideoURL, nil)
	authorize(request, otherToken)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("other user stream expected 404, got %d: %s", response.Code, response.Body.String())
	}

	body := bytes.NewBufferString(fmt.Sprintf(`{"vod_label":%q,"run_id":"forbidden_upload","fps":"1","duration_seconds":5}`, uploaded.VOD.Label))
	request = httptest.NewRequest(http.MethodPost, "/api/analysis-runs", body)
	authorize(request, otherToken)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("other user analysis expected 404, got %d: %s", response.Code, response.Body.String())
	}
}

func TestServerRunsAnalysisAndReturnsLatestReport(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	token := registerTestAdmin(t, server)

	body := bytes.NewBufferString(`{"vod_label":"diamond_example","run_id":"api_test","fps":"1","duration_seconds":5,"force":true}`)
	request := httptest.NewRequest(http.MethodPost, "/api/analysis-runs", body)
	authorize(request, token)
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

	reportPath := filepath.Join(fixture.outRoot, "users", token.user.ID, "analyses", "diamond_example", "reports", "api_test", "report.json")
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("expected report file: %v", err)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/reports/latest?vod_label=diamond_example", nil)
	authorize(request, token)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, `"run_id": "api_test"`) {
		t.Fatalf("unexpected latest report response:\n%s", got)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/reports?vod_label=diamond_example", nil)
	authorize(request, token)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, `"run_id": "api_test"`) ||
		!strings.Contains(got, `"frame_count": 2`) ||
		!strings.Contains(got, `"schema_version": 11`) ||
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
	token := registerTestAdmin(t, server)

	request := httptest.NewRequest(http.MethodGet, "/api/reports?vod_label=diamond_example", nil)
	authorize(request, token)
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
	authorize(request, token)
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
	authorize(request, token)
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

func TestServerReadsStructuredReportFromCatalogSnapshot(t *testing.T) {
	fixture := newFixture(t)
	generatedAt := time.Date(2026, 7, 31, 18, 30, 0, 0, time.UTC)
	report := domain.AnalysisReport{
		SchemaVersion: domain.AnalysisReportSchemaVersion, RunID: "snapshot_run", Status: "completed", GeneratedAt: generatedAt,
		VOD:      domain.VOD{Label: "diamond_example", Rank: "diamond", Title: "Snapshot fixture"},
		Sample:   domain.FrameSampleSummary{Name: "full", FPS: "1", FrameCount: 20},
		Gameplay: &domain.GameplaySummary{Analyzer: "postgres-snapshot", SampledFrames: 20, AnalyzedFrames: 20},
		Findings: []domain.Finding{}, Timeline: []domain.TimelineEvent{}, Artifacts: []domain.Artifact{},
		Metadata: domain.AnalysisRunMetadata{Analyzer: "postgres-snapshot", Mode: "full_vod"},
	}
	catalog := &fakeReportCatalog{
		summaries: []app.ReportCatalogSummary{{
			VODLabel: "diamond_example", RunID: report.RunID, Status: report.Status, GeneratedAt: generatedAt,
			SchemaVersion: report.SchemaVersion, Analyzer: report.Metadata.Analyzer, FrameCount: report.Sample.FrameCount,
			JSONPath: "/missing/report.json", MarkdownPath: "/missing/report.md",
		}},
		record: &app.ReportCatalogRecord{Report: report, JSONPath: "/missing/report.json", MarkdownPath: "/missing/report.md"},
	}
	fixture.config.ReportCatalog = catalog
	server := NewServer(fixture.config)
	auth := registerTestAdmin(t, server)

	request := httptest.NewRequest(http.MethodGet, "/api/reports/latest?vod_label=diamond_example", nil)
	authorize(request, auth)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"analyzer": "postgres-snapshot"`) {
		t.Fatalf("catalog snapshot must be returned without report file, got %d: %s", response.Code, response.Body.String())
	}
}

func TestServerRunsAsyncAnalysisJob(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	token := registerTestAdmin(t, server)

	body := bytes.NewBufferString(`{"vod_label":"diamond_example","run_id":"async_test","fps":"1","duration_seconds":5,"force":true,"async":true}`)
	request := httptest.NewRequest(http.MethodPost, "/api/analysis-runs", body)
	authorize(request, token)
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
		authorize(request, token)
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
	if job.ReportJSON == "" || job.ReportMD == "" {
		t.Fatalf("expected report paths: %+v", job)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/reports/diamond_example/async_test", nil)
	authorize(request, token)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	var report domain.AnalysisReport
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &report) != nil || report.Gameplay == nil {
		t.Fatalf("expected completed gameplay report from report resource, got %d: %s", response.Code, response.Body.String())
	}
}

func TestServerDispatchesVersionedJobToTemporalAndCancelsIt(t *testing.T) {
	fixture := newFixture(t)
	launcher := &fakeWorkflowLauncher{}
	fixture.config.WorkflowLauncher = launcher
	server := NewServer(fixture.config)
	auth := registerTestAdmin(t, server)

	body := bytes.NewBufferString(`{"vod_label":"diamond_example","run_id":"temporal_test","fps":"2","duration_seconds":0,"force":true,"async":true}`)
	request := httptest.NewRequest(http.MethodPost, "/api/analysis-runs", body)
	authorize(request, auth)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("dispatch expected 202, got %d: %s", response.Code, response.Body.String())
	}

	var job AnalysisJobResponse
	if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if launcher.startedJobID != job.JobID {
		t.Fatalf("started workflow %q, want %q", launcher.startedJobID, job.JobID)
	}
	if launcher.request.SchemaVersion != app.AnalysisJobRequestSchemaVersion || launcher.request.OwnerID != auth.user.ID || launcher.request.DurationSeconds != 0 {
		t.Fatalf("unexpected workflow request: %+v", launcher.request)
	}
	persisted, found, err := server.jobs.FindAnalysisJob(context.Background(), job.JobID, auth.user.ID, false)
	if err != nil || !found {
		t.Fatalf("find persisted job: found=%t err=%v", found, err)
	}
	var persistedRequest app.AnalysisJobRequest
	if err := json.Unmarshal(persisted.Request, &persistedRequest); err != nil || persistedRequest.OwnerID != auth.user.ID {
		t.Fatalf("persisted job request must retain tenant context: %+v err=%v", persistedRequest, err)
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/analysis-runs/"+job.JobID, nil)
	authorize(request, auth)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("cancel expected 202, got %d: %s", response.Code, response.Body.String())
	}
	if launcher.cancelledJobID != job.JobID {
		t.Fatalf("cancelled workflow %q, want %q", launcher.cancelledJobID, job.JobID)
	}
}

func TestServerPersistsCancellationIntentWhenTemporalIsUnavailable(t *testing.T) {
	fixture := newFixture(t)
	launcher := &fakeWorkflowLauncher{cancelErr: errors.New("Temporal unavailable")}
	fixture.config.WorkflowLauncher = launcher
	server := NewServer(fixture.config)
	auth := registerTestAdmin(t, server)

	request := httptest.NewRequest(http.MethodPost, "/api/analysis-runs", bytes.NewBufferString(`{"vod_label":"diamond_example","run_id":"cancel_retry","async":true}`))
	authorize(request, auth)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	var job AnalysisJobResponse
	if response.Code != http.StatusAccepted || json.Unmarshal(response.Body.Bytes(), &job) != nil {
		t.Fatalf("dispatch expected 202, got %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/analysis-runs/"+job.JobID, nil)
	authorize(request, auth)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"cancellation_requested": true`) || !strings.Contains(response.Body.String(), "Cancellation queued for delivery") {
		t.Fatalf("cancellation intent expected 202, got %d: %s", response.Code, response.Body.String())
	}
	persisted, found, err := server.jobs.FindAnalysisJob(context.Background(), job.JobID, auth.user.ID, false)
	if err != nil || !found || !persisted.CancellationRequested {
		t.Fatalf("cancellation intent was not persisted: found=%t job=%+v err=%v", found, persisted, err)
	}
}

func TestServerListsEvaluations(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	token := registerTestAdmin(t, server)
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
	authorize(request, token)
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
	token := registerTestAdmin(t, server)
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
	authorize(request, token)
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
	token := registerTestAdmin(t, server)
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
	authorize(request, token)
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
	token := registerTestAdmin(t, server)
	reportDir := filepath.Join(fixture.outRoot, "diamond_example", "reports", "api_test")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatalf("mkdir report: %v", err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "report.json"), []byte(`{"run_id":"api_test","vod":{"label":"diamond_example"}}`), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}

	body := bytes.NewBufferString(`{
  "vod_label": "diamond_example",
  "report_run_id": "api_test",
  "type": "false_detection",
  "target_id": "event_001",
  "comment": "This event should be ignored.",
  "timestamp_seconds": 42.5
}`)
	request := httptest.NewRequest(http.MethodPost, "/api/corrections", body)
	authorize(request, token)
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

	correctionsPath := filepath.Join(fixture.outRoot, "users", token.user.ID, "data", "corrections", "diamond_example", "api_test", app.ManualCorrectionsJSONName)
	if _, err := os.Stat(correctionsPath); err != nil {
		t.Fatalf("expected corrections file: %v", err)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/corrections?vod_label=diamond_example&report_run_id=api_test", nil)
	authorize(request, token)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, `"comment": "This event should be ignored."`) {
		t.Fatalf("unexpected correction list response:\n%s", got)
	}
}

func TestServerCreatesCoachAssessmentAndFeedback(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	token := registerTestAdmin(t, server)

	gameplay := domain.GameplaySummary{
		SampledFrames: 10, AnalyzedFrames: 10, AverageHUDSignal: .3, AverageMinimapSignal: .3,
		ReviewWindows: []domain.ReviewWindow{{ID: "combat_001", Kind: "combat_spike", Score: .8, PeakSeconds: 42}},
	}
	review, err := (app.EvidenceCoachEngine{}).BuildReview(context.Background(), app.CoachReviewRequest{
		Media: domain.MediaSummary{HasAudio: true}, Sample: domain.FrameSampleSummary{FPSValue: 1}, Gameplay: gameplay,
	})
	if err != nil {
		t.Fatalf("build coach review: %v", err)
	}
	gameplay.CoachReview = review
	report := domain.AnalysisReport{
		SchemaVersion: domain.AnalysisReportSchemaVersion, RunID: "coach_run", VOD: domain.VOD{Label: "diamond_example"}, Gameplay: &gameplay,
	}
	reportDir := filepath.Join(fixture.outRoot, "diamond_example", "reports", "coach_run")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatalf("mkdir report: %v", err)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "report.json"), raw, 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}

	body := bytes.NewBufferString(`{
  "vod_label":"diamond_example", "report_run_id":"coach_run", "window_id":"combat_001",
  "answers":{"fight_occurred":"yes","outcome":"death","tradeable":"no","utility_available":"yes","utility_used":"no","crosshair_ready":"yes","escape_route":"no"}
}`)
	request := httptest.NewRequest(http.MethodPost, "/api/coach-assessments", body)
	authorize(request, token)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, `"rule_id": "combat_untradeable_contact"`) ||
		!strings.Contains(got, `"actionable": 1`) || !strings.Contains(got, `"author": "coach@example.com"`) {
		t.Fatalf("unexpected assessment response:\n%s", got)
	}

	body = bytes.NewBufferString(`{"vod_label":"diamond_example","report_run_id":"coach_run","window_id":"combat_001","verdict":"useful"}`)
	request = httptest.NewRequest(http.MethodPost, "/api/coach-feedback", body)
	authorize(request, token)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"verdict": "useful"`) {
		t.Fatalf("unexpected feedback response %d: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/coach-assessments?vod_label=diamond_example&report_run_id=coach_run", nil)
	authorize(request, token)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"completed": 1`) {
		t.Fatalf("unexpected assessment list %d: %s", response.Code, response.Body.String())
	}
}

func TestServerAuthRegisterLoginAndAdminOverview(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)

	body := bytes.NewBufferString(`{"email":"coach@example.com","password":"secret-pass","display_name":"Coach","setup_token":"test-bootstrap-token"}`)
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
	if auth.CSRFToken == "" || auth.User.Role != app.AuthRoleAdmin {
		t.Fatalf("unexpected auth response: %+v", auth)
	}
	sessionCookie := response.Result().Cookies()[0]
	if sessionCookie.Name != sessionCookieName || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteLaxMode || sessionCookie.MaxAge <= 0 {
		t.Fatalf("unexpected session cookie: %+v", sessionCookie)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
	request.AddCookie(sessionCookie)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected admin overview 200, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, `"user_count": 1`) ||
		!strings.Contains(got, `"schema_version": 11`) ||
		!strings.Contains(got, `"readiness"`) ||
		!strings.Contains(got, `"vision_service"`) {
		t.Fatalf("unexpected admin overview:\n%s", got)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	request.AddCookie(sessionCookie)
	request.Header.Set(csrfHeaderName, auth.CSRFToken)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected logout 200, got %d: %s", response.Code, response.Body.String())
	}
	cleared := response.Result().Cookies()[0]
	if cleared.Name != sessionCookieName || cleared.MaxAge >= 0 {
		t.Fatalf("expected cleared session cookie: %+v", cleared)
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
	token := registerTestAdmin(t, server)

	body := bytes.NewBufferString(`{"vod_label":"diamond_example","run_id":"model_review_missing_url","fps":"1","duration_seconds":5,"force":true,"model_review":true}`)
	request := httptest.NewRequest(http.MethodPost, "/api/analysis-runs", body)
	authorize(request, token)
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
	if got := response.Body.String(); !strings.Contains(got, `"schema_version": 11`) ||
		!strings.Contains(got, `"analyzer": "valorant-hud-cv-v2"`) ||
		!strings.Contains(got, `"model_review_configured": false`) ||
		!strings.Contains(got, `"model_review_available": false`) ||
		!strings.Contains(got, `"configured": false`) {
		t.Fatalf("unexpected health response:\n%s", got)
	}
}

func TestServerDiagnosticsEndpoints(t *testing.T) {
	fixture := newFixture(t)
	server := NewServer(fixture.config)
	token := registerTestAdmin(t, server)

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
	authorize(request, token)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected pprof 200, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, "profiles") {
		t.Fatalf("unexpected pprof response:\n%s", got)
	}
}

type fakeWorkflowLauncher struct {
	startedJobID   string
	cancelledJobID string
	request        app.AnalysisJobRequest
	startErr       error
	cancelErr      error
}

func (f *fakeWorkflowLauncher) StartAnalysisWorkflow(_ context.Context, jobID string, request app.AnalysisJobRequest) error {
	f.startedJobID = jobID
	f.request = request
	return f.startErr
}

func (f *fakeWorkflowLauncher) CancelAnalysisWorkflow(_ context.Context, jobID string) error {
	f.cancelledJobID = jobID
	return f.cancelErr
}

func TestReadinessChecksConfiguredDependenciesWithoutLeakingDetails(t *testing.T) {
	fixture := newFixture(t)
	fixture.config.Dependencies = map[string]app.HealthChecker{
		"postgres": staticHealthChecker{err: fmt.Errorf("dial failed for database-password-secret")},
	}
	server := NewServer(fixture.config)
	auth := registerTestAdmin(t, server)

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"postgres": "failed"`) {
		t.Fatalf("failed dependency must make service unready, got %d: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "database-password-secret") {
		t.Fatalf("public readiness response leaked dependency details: %s", response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
	authorize(request, auth)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "database-password-secret") {
		t.Fatalf("administrator must receive diagnostic detail, got %d: %s", response.Code, response.Body.String())
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
		`vodcoach_info{schema_version="11",analyzer="valorant-hud-cv-v2"} 1`,
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

type staticHealthChecker struct {
	err error
}

func (c staticHealthChecker) Ping(context.Context) error {
	return c.err
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
			BootstrapAdminToken:       "test-bootstrap-token",
		},
		outRoot: outRoot,
	}
}

type fakeReportCatalog struct {
	labels    []string
	summaries []app.ReportCatalogSummary
	record    *app.ReportCatalogRecord
}

func (c *fakeReportCatalog) ListReportSummaries(_ context.Context, _ string, vodLabel string, _ bool) ([]app.ReportCatalogSummary, error) {
	c.labels = append(c.labels, vodLabel)
	return c.summaries, nil
}

func (c *fakeReportCatalog) FindReport(_ context.Context, _ string, _ string, runID string, _ bool) (app.ReportCatalogRecord, bool, error) {
	if c.record == nil || (runID != "" && c.record.Report.RunID != runID) {
		return app.ReportCatalogRecord{}, false, nil
	}
	return *c.record, true, nil
}

type testAuth struct {
	cookie    *http.Cookie
	csrfToken string
	user      app.PublicAuthUser
}

func registerTestAdmin(t *testing.T, server *Server) testAuth {
	t.Helper()
	return registerTestAccount(t, server, "coach@example.com")
}

func registerTestAccount(t *testing.T, server *Server, email string) testAuth {
	t.Helper()

	body := bytes.NewBufferString(fmt.Sprintf(`{"email":%q,"password":"secret-pass","display_name":"Coach","setup_token":"test-bootstrap-token"}`, email))
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", body)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected auth register 201, got %d: %s", response.Code, response.Body.String())
	}
	var auth AuthResponse
	if err := json.Unmarshal(response.Body.Bytes(), &auth); err != nil {
		t.Fatalf("decode auth response: %v", err)
	}
	if auth.CSRFToken == "" {
		t.Fatalf("expected CSRF token: %+v", auth)
	}
	cookies := response.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected session cookie")
	}
	return testAuth{cookie: cookies[0], csrfToken: auth.CSRFToken, user: auth.User}
}

func uploadTestVOD(t *testing.T, server *Server, token testAuth) UploadVODResponse {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range map[string]string{"title": "Private VOD", "rank": "diamond", "map": "Bind", "agent": "Sova"} {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	file, err := writer.CreateFormFile("file", "private.mp4")
	if err != nil {
		t.Fatalf("create upload file: %v", err)
	}
	if _, err := file.Write([]byte("private uploaded video")); err != nil {
		t.Fatalf("write upload file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close upload: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/vods/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	authorize(request, token)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("upload expected 201, got %d: %s", response.Code, response.Body.String())
	}
	var uploaded UploadVODResponse
	if err := json.Unmarshal(response.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	return uploaded
}

func authorize(request *http.Request, auth testAuth) {
	request.AddCookie(auth.cookie)
	if isUnsafeMethod(request.Method) {
		request.Header.Set(csrfHeaderName, auth.csrfToken)
	}
}
