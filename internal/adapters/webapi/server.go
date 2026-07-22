package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/adapters/dataset"
	"github.com/asklit/valorant-vod-coach/internal/adapters/media"
	reportstore "github.com/asklit/valorant-vod-coach/internal/adapters/report"
	"github.com/asklit/valorant-vod-coach/internal/adapters/vision"
	"github.com/asklit/valorant-vod-coach/internal/adapters/visionservice"
	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Config struct {
	ManifestPath              string
	RawRoot                   string
	ProcessedRoot             string
	EvaluationAnnotationsRoot string
	FFprobePath               string
	FFmpegPath                string
	VisionURL                 string
	StaticDir                 string
	Catalog                   app.AnalysisCatalog
	Locks                     app.LockManager
	Logger                    *slog.Logger
	Tracer                    trace.Tracer
}

type Server struct {
	config  Config
	mux     *http.ServeMux
	jobs    *analysisJobStore
	metrics *serverMetrics
	logger  *slog.Logger
	tracer  trace.Tracer
}

type VODItem struct {
	Label            string  `json:"label"`
	Rank             string  `json:"rank"`
	Title            string  `json:"title"`
	Channel          string  `json:"channel"`
	VideoID          string  `json:"video_id"`
	SourceURL        string  `json:"source_url"`
	DurationText     string  `json:"duration_text"`
	DurationSeconds  float64 `json:"duration_seconds"`
	RankSource       string  `json:"rank_source"`
	Notes            string  `json:"notes"`
	Enabled          bool    `json:"enabled"`
	LocalStatus      string  `json:"local_status"`
	LocalSizeBytes   int64   `json:"local_size_bytes"`
	VideoURL         string  `json:"video_url,omitempty"`
	ReportCount      int     `json:"report_count"`
	LatestReportID   string  `json:"latest_report_id,omitempty"`
	LatestGenerated  string  `json:"latest_generated,omitempty"`
	LatestReportPath string  `json:"latest_report_path,omitempty"`
}

type VODListResponse struct {
	GeneratedAt time.Time `json:"generated_at"`
	Counts      Counts    `json:"counts"`
	VODs        []VODItem `json:"vods"`
}

type Counts struct {
	Total      int `json:"total"`
	Enabled    int `json:"enabled"`
	Downloaded int `json:"downloaded"`
	Reported   int `json:"reported"`
}

type AnalyzeRequest struct {
	VODLabel        string   `json:"vod_label"`
	RunID           string   `json:"run_id"`
	FPS             string   `json:"fps"`
	StartSeconds    float64  `json:"start_seconds"`
	DurationSeconds *float64 `json:"duration_seconds"`
	ImageQuality    int      `json:"image_quality"`
	Force           bool     `json:"force"`
	Async           bool     `json:"async,omitempty"`
	ModelReview     bool     `json:"model_review,omitempty"`
}

type AnalyzeResponse struct {
	Report       domain.AnalysisReport `json:"report"`
	ReportJSON   string                `json:"report_json"`
	ReportMD     string                `json:"report_md"`
	ArtifactBase string                `json:"artifact_base"`
}

type EvaluationRunRequest struct {
	VODLabel         string  `json:"vod_label"`
	ReportRunID      string  `json:"report_run_id"`
	AnnotationsPath  string  `json:"annotations_path"`
	RunID            string  `json:"run_id"`
	ToleranceSeconds float64 `json:"tolerance_seconds"`
	Force            bool    `json:"force"`
}

type EvaluationRunResponse struct {
	Evaluation     domain.GameplayEvaluationReport `json:"evaluation"`
	EvaluationJSON string                          `json:"evaluation_json"`
	EvaluationMD   string                          `json:"evaluation_md"`
	ArtifactBase   string                          `json:"artifact_base"`
}

type AnalysisJobResponse struct {
	JobID        string                 `json:"job_id"`
	RunID        string                 `json:"run_id"`
	VODLabel     string                 `json:"vod_label"`
	Status       string                 `json:"status"`
	Message      string                 `json:"message,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	StartedAt    *time.Time             `json:"started_at,omitempty"`
	FinishedAt   *time.Time             `json:"finished_at,omitempty"`
	Error        string                 `json:"error,omitempty"`
	Report       *domain.AnalysisReport `json:"report,omitempty"`
	ReportJSON   string                 `json:"report_json,omitempty"`
	ReportMD     string                 `json:"report_md,omitempty"`
	ArtifactBase string                 `json:"artifact_base"`
}

type analysisJob struct {
	AnalysisJobResponse
}

type analysisJobStore struct {
	mu   sync.RWMutex
	jobs map[string]*analysisJob
}

type ReportListResponse struct {
	VODLabel string          `json:"vod_label"`
	Reports  []ReportSummary `json:"reports"`
}

type EvaluationListResponse struct {
	VODLabel    string              `json:"vod_label,omitempty"`
	Evaluations []EvaluationSummary `json:"evaluations"`
}

type EvaluationAnnotationListResponse struct {
	VODLabel    string                        `json:"vod_label,omitempty"`
	Annotations []EvaluationAnnotationSummary `json:"annotations"`
}

type ReportSummary struct {
	SchemaVersion     int       `json:"schema_version"`
	RunID             string    `json:"run_id"`
	Status            string    `json:"status"`
	GeneratedAt       time.Time `json:"generated_at"`
	FindingCount      int       `json:"finding_count"`
	FrameCount        int       `json:"frame_count"`
	ReviewWindowCount int       `json:"review_window_count"`
	RoundSegmentCount int       `json:"round_segment_count"`
	ModelTaskCount    int       `json:"model_review_task_count"`
	ModelRunCount     int       `json:"model_review_run_count"`
	Analyzer          string    `json:"analyzer,omitempty"`
	SampleName        string    `json:"sample_name"`
	SampleFPS         string    `json:"sample_fps"`
	SampleDuration    float64   `json:"sample_duration_seconds,omitempty"`
	ContactSheet      string    `json:"contact_sheet,omitempty"`
	JSONPath          string    `json:"json_path"`
	MarkdownPath      string    `json:"markdown_path"`
}

type EvaluationSummary struct {
	SchemaVersion    int       `json:"schema_version"`
	RunID            string    `json:"run_id"`
	GeneratedAt      time.Time `json:"generated_at"`
	VODLabel         string    `json:"vod_label"`
	ReportRunID      string    `json:"report_run_id"`
	ToleranceSeconds float64   `json:"tolerance_seconds"`
	LabelCount       int       `json:"label_count"`
	PredictionCount  int       `json:"prediction_count"`
	MatchCount       int       `json:"match_count"`
	Precision        float64   `json:"precision"`
	Recall           float64   `json:"recall"`
	F1               float64   `json:"f1"`
	JSONPath         string    `json:"json_path"`
	MarkdownPath     string    `json:"markdown_path"`
}

type EvaluationAnnotationSummary struct {
	SchemaVersion    int     `json:"schema_version"`
	VODLabel         string  `json:"vod_label"`
	ReportRunID      string  `json:"report_run_id,omitempty"`
	ToleranceSeconds float64 `json:"tolerance_seconds,omitempty"`
	LabelCount       int     `json:"label_count"`
	Path             string  `json:"path"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func NewServer(config Config) *Server {
	if config.ManifestPath == "" {
		config.ManifestPath = "data/manifests/vods.tsv"
	}
	if config.RawRoot == "" {
		config.RawRoot = "data/raw/youtube"
	}
	if config.ProcessedRoot == "" {
		config.ProcessedRoot = "data/processed"
	}
	if config.EvaluationAnnotationsRoot == "" {
		config.EvaluationAnnotationsRoot = "ml/evals"
	}
	if config.FFprobePath == "" {
		config.FFprobePath = "ffprobe"
	}
	if config.FFmpegPath == "" {
		config.FFmpegPath = "ffmpeg"
	}

	server := &Server{
		config:  config,
		mux:     http.NewServeMux(),
		jobs:    &analysisJobStore{jobs: map[string]*analysisJob{}},
		metrics: newServerMetrics(time.Now().UTC()),
		logger:  config.Logger,
		tracer:  config.Tracer,
	}
	if server.logger == nil {
		server.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if server.tracer == nil {
		server.tracer = trace.NewNoopTracerProvider().Tracer("vod-web")
	}
	server.routes()
	return server
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	route := metricRoute(r.URL.Path)
	ctx, span := s.tracer.Start(r.Context(), "http "+r.Method+" "+route)
	span.SetAttributes(
		attribute.String("http.request.method", r.Method),
		attribute.String("url.path", r.URL.Path),
		attribute.String("http.route", route),
	)
	defer span.End()
	r = r.WithContext(ctx)

	recorder := &statusRecorder{ResponseWriter: w}
	if origin := r.Header.Get("Origin"); isAllowedDevOrigin(origin) {
		recorder.Header().Set("Access-Control-Allow-Origin", origin)
		recorder.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		recorder.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	}
	if r.Method == http.MethodOptions {
		recorder.WriteHeader(http.StatusNoContent)
		status := recorder.statusCode()
		span.SetAttributes(attribute.Int("http.response.status_code", status))
		s.metrics.record(r.Method, r.URL.Path, status, time.Since(started))
		s.logRequest(ctx, r, route, status, time.Since(started))
		return
	}
	s.mux.ServeHTTP(recorder, r)
	status := recorder.statusCode()
	span.SetAttributes(attribute.Int("http.response.status_code", status))
	if status >= http.StatusInternalServerError {
		span.SetStatus(codes.Error, http.StatusText(status))
	}
	s.metrics.record(r.Method, r.URL.Path, status, time.Since(started))
	s.logRequest(ctx, r, route, status, time.Since(started))
}

func (s *Server) logRequest(ctx context.Context, r *http.Request, route string, status int, duration time.Duration) {
	if s.logger == nil {
		return
	}
	level := slog.LevelInfo
	if status >= http.StatusInternalServerError {
		level = slog.LevelError
	} else if status >= http.StatusBadRequest {
		level = slog.LevelWarn
	}
	s.logger.LogAttrs(ctx, level, "http request completed",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("route", route),
		slog.Int("status", status),
		slog.Float64("duration_ms", float64(duration.Microseconds())/1000),
	)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /metrics", s.handleMetrics)
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/vods", s.handleListVODs)
	s.mux.HandleFunc("GET /api/vods/", s.handleVODVideo)
	s.mux.HandleFunc("POST /api/analysis-runs", s.handleAnalyze)
	s.mux.HandleFunc("GET /api/analysis-runs/", s.handleAnalysisJob)
	s.mux.HandleFunc("GET /api/evaluation-annotations", s.handleEvaluationAnnotations)
	s.mux.HandleFunc("POST /api/evaluation-runs", s.handleRunEvaluation)
	s.mux.HandleFunc("GET /api/evaluations", s.handleEvaluations)
	s.mux.HandleFunc("GET /api/reports", s.handleReports)
	s.mux.HandleFunc("GET /api/reports/latest", s.handleLatestReport)
	s.mux.HandleFunc("GET /api/reports/", s.handleReportByPath)
	s.mux.Handle("/artifacts/", http.StripPrefix("/artifacts/", http.FileServer(http.Dir(s.config.ProcessedRoot))))

	if s.config.StaticDir != "" {
		fileServer := http.FileServer(http.Dir(s.config.StaticDir))
		s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/artifacts/") {
				http.NotFound(w, r)
				return
			}
			path := filepath.Join(s.config.StaticDir, filepath.Clean(r.URL.Path))
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
			http.ServeFile(w, r, filepath.Join(s.config.StaticDir, "index.html"))
		})
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	configured := strings.TrimSpace(s.config.VisionURL) != ""
	available := false
	visionStatus := map[string]any{
		"configured": configured,
	}
	if configured {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		status, err := (visionservice.Client{
			BaseURL:    s.config.VisionURL,
			HTTPClient: &http.Client{Timeout: time.Second},
		}).Health(ctx)
		if err != nil {
			visionStatus["error"] = err.Error()
		} else {
			available = strings.EqualFold(status.Status, "ok")
			visionStatus["status"] = status.Status
			visionStatus["model"] = status.Model
			visionStatus["mode"] = status.Mode
			visionStatus["runtime"] = status.Runtime
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":                  "ok",
		"schema_version":          domain.AnalysisReportSchemaVersion,
		"analyzer":                "visual-heuristic-gameplay",
		"model_review_configured": configured,
		"model_review_available":  available,
		"vision_service":          visionStatus,
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	startedAt, requestMetrics := s.metrics.snapshot()
	jobCounts := s.jobs.countByStatus()
	uptime := time.Since(startedAt).Seconds()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP vodcoach_info Static application information.\n")
	fmt.Fprintf(w, "# TYPE vodcoach_info gauge\n")
	fmt.Fprintf(w, "vodcoach_info{schema_version=\"%d\",analyzer=\"visual-heuristic-gameplay\"} 1\n", domain.AnalysisReportSchemaVersion)

	fmt.Fprintf(w, "# HELP vodcoach_uptime_seconds Process uptime in seconds.\n")
	fmt.Fprintf(w, "# TYPE vodcoach_uptime_seconds gauge\n")
	fmt.Fprintf(w, "vodcoach_uptime_seconds %.3f\n", uptime)

	fmt.Fprintf(w, "# HELP vodcoach_model_review_configured Whether VISION_SERVICE_URL is configured.\n")
	fmt.Fprintf(w, "# TYPE vodcoach_model_review_configured gauge\n")
	fmt.Fprintf(w, "vodcoach_model_review_configured %d\n", boolGauge(strings.TrimSpace(s.config.VisionURL) != ""))

	fmt.Fprintf(w, "# HELP vodcoach_http_requests_total HTTP requests by method, route, and status.\n")
	fmt.Fprintf(w, "# TYPE vodcoach_http_requests_total counter\n")
	for _, item := range requestMetrics {
		fmt.Fprintf(w, "vodcoach_http_requests_total{method=\"%s\",route=\"%s\",status=\"%d\"} %d\n", promLabel(item.key.Method), promLabel(item.key.Route), item.key.Status, item.value.Count)
	}

	fmt.Fprintf(w, "# HELP vodcoach_http_request_duration_seconds HTTP request duration by method, route, and status.\n")
	fmt.Fprintf(w, "# TYPE vodcoach_http_request_duration_seconds summary\n")
	for _, item := range requestMetrics {
		labels := fmt.Sprintf("method=\"%s\",route=\"%s\",status=\"%d\"", promLabel(item.key.Method), promLabel(item.key.Route), item.key.Status)
		fmt.Fprintf(w, "vodcoach_http_request_duration_seconds_sum{%s} %.6f\n", labels, item.value.DurationSeconds)
		fmt.Fprintf(w, "vodcoach_http_request_duration_seconds_count{%s} %d\n", labels, item.value.Count)
	}

	fmt.Fprintf(w, "# HELP vodcoach_analysis_jobs_total In-memory analysis jobs by status.\n")
	fmt.Fprintf(w, "# TYPE vodcoach_analysis_jobs_total gauge\n")
	for _, status := range []string{"queued", "running", "completed", "failed"} {
		fmt.Fprintf(w, "vodcoach_analysis_jobs_total{status=\"%s\"} %d\n", status, jobCounts[status])
	}
}

func (s *Server) handleListVODs(w http.ResponseWriter, r *http.Request) {
	vods, err := dataset.LoadManifest(s.config.ManifestPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("load manifest: %w", err))
		return
	}

	rank := strings.TrimSpace(r.URL.Query().Get("rank"))
	if rank == "all" {
		rank = ""
	}
	search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	enabledOnly := r.URL.Query().Get("enabled_only") != "false"

	assets := dataset.ScanLocalAssets(s.config.RawRoot, dataset.Filter(vods, dataset.Rank(rank), enabledOnly))
	items := make([]VODItem, 0, len(assets))
	counts := Counts{Total: len(vods), Enabled: dataset.CountEnabled(vods)}

	for _, asset := range assets {
		if search != "" && !matchesSearch(asset.VOD, search) {
			continue
		}

		reports, err := s.listReports(asset.VOD.Label)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		item := VODItem{
			Label:           asset.VOD.Label,
			Rank:            string(asset.VOD.Rank),
			Title:           asset.VOD.Title,
			Channel:         asset.VOD.Channel,
			VideoID:         asset.VOD.VideoID,
			SourceURL:       asset.VOD.URL,
			DurationText:    asset.VOD.DurationRaw,
			DurationSeconds: asset.VOD.Duration.Seconds(),
			RankSource:      string(asset.VOD.RankSource),
			Notes:           asset.VOD.Notes,
			Enabled:         asset.VOD.Enabled,
			LocalStatus:     string(asset.Status),
			LocalSizeBytes:  asset.SizeBytes,
			ReportCount:     len(reports),
		}
		if asset.Status == dataset.LocalStatusDownloaded {
			counts.Downloaded++
			item.VideoURL = "/api/vods/" + url.PathEscape(asset.VOD.Label) + "/video"
		}
		if len(reports) > 0 {
			counts.Reported++
			latest := reports[0]
			item.LatestReportID = latest.RunID
			item.LatestGenerated = latest.GeneratedAt.Format(time.RFC3339)
			item.LatestReportPath = latest.Path
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, VODListResponse{
		GeneratedAt: time.Now().UTC(),
		Counts:      counts,
		VODs:        items,
	})
}

func (s *Server) handleVODVideo(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/vods/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "video" {
		writeError(w, http.StatusBadRequest, errors.New("expected /api/vods/{label}/video"))
		return
	}

	label, err := url.PathUnescape(parts[0])
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode VOD label: %w", err))
		return
	}

	vods, err := dataset.LoadManifest(s.config.ManifestPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("load manifest: %w", err))
		return
	}

	vod, ok := dataset.FindByLabel(vods, label)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown VOD label %q", label))
		return
	}

	videoPath, _, ok := dataset.FindLocalVideo(s.config.RawRoot, vod)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("video file not found: %s", videoPath))
		return
	}

	http.ServeFile(w, r, videoPath)
}

func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var request AnalyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	durationSeconds, err := normalizeAnalyzeRequest(&request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if request.Async {
		now := time.Now().UTC()
		if strings.TrimSpace(request.RunID) == "" {
			request.RunID = app.DefaultRunID(now)
		}
		jobID := newAnalysisJobID(request.RunID, now)
		job := &analysisJob{AnalysisJobResponse: AnalysisJobResponse{
			JobID:        jobID,
			RunID:        request.RunID,
			VODLabel:     request.VODLabel,
			Status:       "queued",
			Message:      "Analysis job queued",
			CreatedAt:    now,
			ArtifactBase: "/artifacts/",
		}}
		s.jobs.put(job)
		go s.runAnalysisJob(jobID, request, durationSeconds)
		writeJSON(w, http.StatusAccepted, s.jobs.snapshot(jobID))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()

	result, err := s.runLocalAnalysis(ctx, request, durationSeconds)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, AnalyzeResponse{
		Report:       result.Report,
		ReportJSON:   result.Saved.JSONPath,
		ReportMD:     result.Saved.MarkdownPath,
		ArtifactBase: "/artifacts/",
	})
}

func (s *Server) handleAnalysisJob(w http.ResponseWriter, r *http.Request) {
	jobID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/analysis-runs/"), "/")
	if jobID == "" {
		writeError(w, http.StatusBadRequest, errors.New("job id is required"))
		return
	}

	job, ok := s.jobs.get(jobID)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("analysis job not found: %s", jobID))
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) runAnalysisJob(jobID string, request AnalyzeRequest, durationSeconds float64) {
	startedAt := time.Now().UTC()
	s.jobs.update(jobID, func(job *analysisJob) {
		job.Status = "running"
		job.Message = "Analyzing VOD"
		job.StartedAt = &startedAt
	})

	overallTimeout, _ := analysisTimeouts(durationSeconds)
	ctx, cancel := context.WithTimeout(context.Background(), overallTimeout)
	defer cancel()

	result, err := s.runLocalAnalysis(ctx, request, durationSeconds)
	finishedAt := time.Now().UTC()
	s.jobs.update(jobID, func(job *analysisJob) {
		job.FinishedAt = &finishedAt
		if err != nil {
			job.Status = "failed"
			job.Message = "Analysis failed"
			job.Error = err.Error()
			return
		}
		job.Status = "completed"
		job.Message = "Analysis completed"
		job.Report = &result.Report
		job.ReportJSON = result.Saved.JSONPath
		job.ReportMD = result.Saved.MarkdownPath
	})
}

func (s *Server) runLocalAnalysis(ctx context.Context, request AnalyzeRequest, durationSeconds float64) (app.RunAnalysisResult, error) {
	_, sampleTimeout := analysisTimeouts(durationSeconds)
	runner := app.AnalysisRunner{
		Resolver: dataset.LocalVODResolver{
			ManifestPath: s.config.ManifestPath,
			RawRoot:      s.config.RawRoot,
		},
		Media: media.LocalProcessor{
			FFprobePath:   s.config.FFprobePath,
			FFmpegPath:    s.config.FFmpegPath,
			ProcessedRoot: s.config.ProcessedRoot,
			ProbeTimeout:  30 * time.Second,
			SampleTimeout: sampleTimeout,
		},
		Analyzer: vision.LocalGameplayAnalyzer{},
		Reports: reportstore.LocalStore{
			ProcessedRoot: s.config.ProcessedRoot,
		},
		Catalog: s.config.Catalog,
		Locks:   s.config.Locks,
	}
	if request.ModelReview {
		if strings.TrimSpace(s.config.VisionURL) == "" {
			return app.RunAnalysisResult{}, errors.New("model_review requested but vision service URL is not configured")
		}
		runner.Reviewer = visionservice.Client{BaseURL: s.config.VisionURL}
	}

	return runner.Run(ctx, app.RunAnalysisRequest{
		VODLabel:     request.VODLabel,
		RunID:        request.RunID,
		FPS:          request.FPS,
		Start:        secondsDuration(request.StartSeconds),
		Duration:     secondsDuration(durationSeconds),
		ImageQuality: request.ImageQuality,
		Overwrite:    request.Force,
		ModelReview:  request.ModelReview,
	})
}

func (s *Server) handleLatestReport(w http.ResponseWriter, r *http.Request) {
	label := strings.TrimSpace(r.URL.Query().Get("vod_label"))
	if label == "" {
		writeError(w, http.StatusBadRequest, errors.New("vod_label is required"))
		return
	}

	reports, err := s.listReports(label)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(reports) == 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("no reports for %s", label))
		return
	}

	report, err := readReport(reports[0].Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleReports(w http.ResponseWriter, r *http.Request) {
	label := strings.TrimSpace(r.URL.Query().Get("vod_label"))
	if label == "" {
		writeError(w, http.StatusBadRequest, errors.New("vod_label is required"))
		return
	}

	reports, err := s.listReports(label)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	summaries := make([]ReportSummary, 0, len(reports))
	for _, report := range reports {
		summaries = append(summaries, report.Summary)
	}

	writeJSON(w, http.StatusOK, ReportListResponse{
		VODLabel: label,
		Reports:  summaries,
	})
}

func (s *Server) handleEvaluations(w http.ResponseWriter, r *http.Request) {
	label := strings.TrimSpace(r.URL.Query().Get("vod_label"))
	evaluations, err := s.listEvaluations(label)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	summaries := make([]EvaluationSummary, 0, len(evaluations))
	for _, evaluation := range evaluations {
		summaries = append(summaries, evaluation.Summary)
	}
	writeJSON(w, http.StatusOK, EvaluationListResponse{
		VODLabel:    label,
		Evaluations: summaries,
	})
}

func (s *Server) handleEvaluationAnnotations(w http.ResponseWriter, r *http.Request) {
	label := strings.TrimSpace(r.URL.Query().Get("vod_label"))
	annotations, err := s.listEvaluationAnnotations(label)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	summaries := make([]EvaluationAnnotationSummary, 0, len(annotations))
	for _, annotation := range annotations {
		summaries = append(summaries, annotation.Summary)
	}
	writeJSON(w, http.StatusOK, EvaluationAnnotationListResponse{
		VODLabel:    label,
		Annotations: summaries,
	})
}

func (s *Server) handleRunEvaluation(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var request EvaluationRunRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}

	evaluation, saved, err := s.runEvaluation(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, EvaluationRunResponse{
		Evaluation:     evaluation,
		EvaluationJSON: saved.JSONPath,
		EvaluationMD:   saved.MarkdownPath,
		ArtifactBase:   "/artifacts/",
	})
}

func (s *Server) handleReportByPath(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/reports/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		writeError(w, http.StatusBadRequest, errors.New("expected /api/reports/{vod_label}/{run_id}"))
		return
	}

	reportPath := filepath.Join(s.config.ProcessedRoot, parts[0], "reports", parts[1], reportstore.JSONReportName)
	report, err := readReport(reportPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

type reportIndexItem struct {
	RunID       string
	Path        string
	GeneratedAt time.Time
	Summary     ReportSummary
}

type evaluationIndexItem struct {
	RunID       string
	Path        string
	GeneratedAt time.Time
	Summary     EvaluationSummary
}

type annotationIndexItem struct {
	Path        string
	Annotations domain.EvaluationAnnotationSet
	Summary     EvaluationAnnotationSummary
}

func (s *Server) listReports(vodLabel string) ([]reportIndexItem, error) {
	root := filepath.Join(s.config.ProcessedRoot, vodLabel, "reports")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var reports []reportIndexItem
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), reportstore.JSONReportName)
		report, err := readReport(path)
		if err != nil {
			continue
		}
		reports = append(reports, reportIndexItem{
			RunID:       report.RunID,
			Path:        path,
			GeneratedAt: report.GeneratedAt,
			Summary: ReportSummary{
				SchemaVersion:     report.SchemaVersion,
				RunID:             report.RunID,
				Status:            report.Status,
				GeneratedAt:       report.GeneratedAt,
				FindingCount:      len(report.Findings),
				FrameCount:        report.Sample.FrameCount,
				ReviewWindowCount: reviewWindowCount(report.Gameplay),
				RoundSegmentCount: roundSegmentCount(report.Gameplay),
				ModelTaskCount:    modelReviewTaskCount(report.Gameplay),
				ModelRunCount:     modelReviewRunCount(report.Gameplay),
				Analyzer:          report.Metadata.Analyzer,
				SampleName:        report.Sample.Name,
				SampleFPS:         report.Sample.FPS,
				SampleDuration:    report.Sample.DurationSeconds,
				ContactSheet:      report.Sample.ContactSheetPath,
				JSONPath:          path,
				MarkdownPath:      filepath.Join(root, entry.Name(), reportstore.MarkdownReportName),
			},
		})
	}

	sort.Slice(reports, func(i, j int) bool {
		return reports[i].GeneratedAt.After(reports[j].GeneratedAt)
	})
	return reports, nil
}

func (s *Server) listEvaluations(vodLabel string) ([]evaluationIndexItem, error) {
	root := filepath.Join(s.config.ProcessedRoot, "evaluations")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var evaluations []evaluationIndexItem
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), app.EvaluationJSONName)
		evaluation, err := readEvaluation(path)
		if err != nil {
			continue
		}
		if vodLabel != "" && evaluation.VODLabel != vodLabel {
			continue
		}
		evaluations = append(evaluations, evaluationIndexItem{
			RunID:       evaluation.RunID,
			Path:        path,
			GeneratedAt: evaluation.GeneratedAt,
			Summary: EvaluationSummary{
				SchemaVersion:    evaluation.SchemaVersion,
				RunID:            evaluation.RunID,
				GeneratedAt:      evaluation.GeneratedAt,
				VODLabel:         evaluation.VODLabel,
				ReportRunID:      evaluation.ReportRunID,
				ToleranceSeconds: evaluation.ToleranceSeconds,
				LabelCount:       evaluation.Overall.LabelCount,
				PredictionCount:  evaluation.Overall.PredictionCount,
				MatchCount:       evaluation.Overall.MatchCount,
				Precision:        evaluation.Overall.Precision,
				Recall:           evaluation.Overall.Recall,
				F1:               evaluation.Overall.F1,
				JSONPath:         path,
				MarkdownPath:     filepath.Join(root, entry.Name(), app.EvaluationMarkdownName),
			},
		})
	}

	sort.Slice(evaluations, func(i, j int) bool {
		return evaluations[i].GeneratedAt.After(evaluations[j].GeneratedAt)
	})
	return evaluations, nil
}

func (s *Server) listEvaluationAnnotations(vodLabel string) ([]annotationIndexItem, error) {
	root := s.config.EvaluationAnnotationsRoot
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var annotations []annotationIndexItem
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		annotationSet, err := readEvaluationAnnotations(path)
		if err != nil {
			continue
		}
		if vodLabel != "" && annotationSet.VODLabel != vodLabel {
			continue
		}
		annotations = append(annotations, annotationIndexItem{
			Path:        path,
			Annotations: annotationSet,
			Summary: EvaluationAnnotationSummary{
				SchemaVersion:    annotationSet.SchemaVersion,
				VODLabel:         annotationSet.VODLabel,
				ReportRunID:      annotationSet.ReportRunID,
				ToleranceSeconds: annotationSet.ToleranceSeconds,
				LabelCount:       len(annotationSet.Labels),
				Path:             path,
			},
		})
	}

	sort.Slice(annotations, func(i, j int) bool {
		if annotations[i].Summary.VODLabel == annotations[j].Summary.VODLabel {
			return annotations[i].Path < annotations[j].Path
		}
		return annotations[i].Summary.VODLabel < annotations[j].Summary.VODLabel
	})
	return annotations, nil
}

func (s *Server) runEvaluation(ctx context.Context, request EvaluationRunRequest) (domain.GameplayEvaluationReport, app.SavedEvaluation, error) {
	vodLabel := strings.TrimSpace(request.VODLabel)
	if vodLabel == "" {
		return domain.GameplayEvaluationReport{}, app.SavedEvaluation{}, errors.New("vod_label is required")
	}

	reportPath, err := s.resolveEvaluationReportPath(vodLabel, request.ReportRunID)
	if err != nil {
		return domain.GameplayEvaluationReport{}, app.SavedEvaluation{}, err
	}
	report, err := readReport(reportPath)
	if err != nil {
		return domain.GameplayEvaluationReport{}, app.SavedEvaluation{}, fmt.Errorf("read report: %w", err)
	}

	annotationsPath, err := s.resolveEvaluationAnnotationsPath(request.AnnotationsPath, vodLabel, report.RunID)
	if err != nil {
		return domain.GameplayEvaluationReport{}, app.SavedEvaluation{}, err
	}
	annotations, err := readEvaluationAnnotations(annotationsPath)
	if err != nil {
		return domain.GameplayEvaluationReport{}, app.SavedEvaluation{}, fmt.Errorf("read annotations: %w", err)
	}

	var tolerance time.Duration
	if request.ToleranceSeconds > 0 {
		tolerance = time.Duration(request.ToleranceSeconds * float64(time.Second))
	}
	evaluation, err := app.EvaluateGameplayEvents(app.GameplayEvaluationRequest{
		RunID:       strings.TrimSpace(request.RunID),
		GeneratedAt: time.Now().UTC(),
		Report:      report,
		Annotations: annotations,
		Tolerance:   tolerance,
	})
	if err != nil {
		return domain.GameplayEvaluationReport{}, app.SavedEvaluation{}, err
	}

	saved, err := app.WriteEvaluationArtifacts(ctx, filepath.Join(s.config.ProcessedRoot, "evaluations"), evaluation, request.Force)
	if err != nil {
		return domain.GameplayEvaluationReport{}, app.SavedEvaluation{}, fmt.Errorf("write evaluation artifacts: %w", err)
	}
	return evaluation, saved, nil
}

func (s *Server) resolveEvaluationReportPath(vodLabel string, reportRunID string) (string, error) {
	reportRunID = strings.TrimSpace(reportRunID)
	if reportRunID != "" {
		return filepath.Join(s.config.ProcessedRoot, vodLabel, "reports", reportRunID, reportstore.JSONReportName), nil
	}

	reports, err := s.listReports(vodLabel)
	if err != nil {
		return "", err
	}
	if len(reports) == 0 {
		return "", fmt.Errorf("no reports for %s", vodLabel)
	}
	return reports[0].Path, nil
}

func (s *Server) resolveEvaluationAnnotationsPath(rawPath string, vodLabel string, reportRunID string) (string, error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath != "" {
		return s.cleanAnnotationPath(rawPath)
	}

	annotations, err := s.listEvaluationAnnotations(vodLabel)
	if err != nil {
		return "", err
	}
	if len(annotations) == 0 {
		return "", fmt.Errorf("no evaluation annotations for %s", vodLabel)
	}
	for _, item := range annotations {
		if item.Annotations.ReportRunID == reportRunID {
			return item.Path, nil
		}
	}
	return annotations[0].Path, nil
}

func (s *Server) cleanAnnotationPath(rawPath string) (string, error) {
	path := filepath.Clean(rawPath)
	if !filepath.IsAbs(path) {
		if _, err := os.Stat(path); err != nil {
			path = filepath.Join(s.config.EvaluationAnnotationsRoot, path)
		}
	}

	rootAbs, err := filepath.Abs(s.config.EvaluationAnnotationsRoot)
	if err != nil {
		return "", err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if pathAbs != rootAbs && !strings.HasPrefix(pathAbs, rootAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("annotations_path must be inside %s", s.config.EvaluationAnnotationsRoot)
	}
	return path, nil
}

func reviewWindowCount(gameplay *domain.GameplaySummary) int {
	if gameplay == nil {
		return 0
	}
	return gameplay.ReviewWindowCount
}

func roundSegmentCount(gameplay *domain.GameplaySummary) int {
	if gameplay == nil {
		return 0
	}
	if gameplay.RoundSegmentCount == 0 && len(gameplay.RoundSegments) > 0 {
		return len(gameplay.RoundSegments)
	}
	return gameplay.RoundSegmentCount
}

func modelReviewTaskCount(gameplay *domain.GameplaySummary) int {
	if gameplay == nil {
		return 0
	}
	if gameplay.ModelReviewTaskCount == 0 && len(gameplay.ModelReviewTasks) > 0 {
		return len(gameplay.ModelReviewTasks)
	}
	return gameplay.ModelReviewTaskCount
}

func modelReviewRunCount(gameplay *domain.GameplaySummary) int {
	if gameplay == nil {
		return 0
	}
	if gameplay.ModelReviewRunCount == 0 && len(gameplay.ModelReviewRuns) > 0 {
		return len(gameplay.ModelReviewRuns)
	}
	return gameplay.ModelReviewRunCount
}

func readReport(path string) (domain.AnalysisReport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return domain.AnalysisReport{}, err
	}
	var report domain.AnalysisReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return domain.AnalysisReport{}, err
	}
	return report, nil
}

func readEvaluation(path string) (domain.GameplayEvaluationReport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return domain.GameplayEvaluationReport{}, err
	}
	var report domain.GameplayEvaluationReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return domain.GameplayEvaluationReport{}, err
	}
	return report, nil
}

func readEvaluationAnnotations(path string) (domain.EvaluationAnnotationSet, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return domain.EvaluationAnnotationSet{}, err
	}
	var annotations domain.EvaluationAnnotationSet
	if err := json.Unmarshal(raw, &annotations); err != nil {
		return domain.EvaluationAnnotationSet{}, err
	}
	return annotations, nil
}

func normalizeAnalyzeRequest(request *AnalyzeRequest) (float64, error) {
	if strings.TrimSpace(request.VODLabel) == "" {
		return 0, errors.New("vod_label is required")
	}
	if request.FPS == "" {
		request.FPS = "1"
	}
	durationSeconds := 180.0
	if request.DurationSeconds != nil {
		durationSeconds = *request.DurationSeconds
	}
	if durationSeconds < 0 {
		return 0, errors.New("duration_seconds must be non-negative")
	}
	if request.ImageQuality <= 0 {
		request.ImageQuality = 3
	}
	return durationSeconds, nil
}

func analysisTimeouts(durationSeconds float64) (time.Duration, time.Duration) {
	if durationSeconds == 0 || durationSeconds > 10*60 {
		return 50 * time.Minute, 45 * time.Minute
	}
	return 15 * time.Minute, 10 * time.Minute
}

func newAnalysisJobID(runID string, now time.Time) string {
	value := strings.TrimSpace(runID)
	if value == "" {
		value = "analysis"
	}
	value = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, value)
	value = strings.Trim(value, "_-")
	if value == "" {
		value = "analysis"
	}
	return fmt.Sprintf("job_%s_%s", value, strconv.FormatInt(now.UnixNano(), 36))
}

func (s *analysisJobStore) put(job *analysisJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.JobID] = job
}

func (s *analysisJobStore) get(jobID string) (AnalysisJobResponse, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return AnalysisJobResponse{}, false
	}
	return job.AnalysisJobResponse, true
}

func (s *analysisJobStore) snapshot(jobID string) AnalysisJobResponse {
	job, ok := s.get(jobID)
	if !ok {
		return AnalysisJobResponse{}
	}
	return job
}

func (s *analysisJobStore) update(jobID string, mutate func(*analysisJob)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.jobs[jobID]; ok {
		mutate(job)
	}
}

func (s *analysisJobStore) countByStatus() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	counts := map[string]int{}
	for _, job := range s.jobs {
		counts[job.Status]++
	}
	return counts
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func (w *statusRecorder) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

type serverMetrics struct {
	mu        sync.Mutex
	startedAt time.Time
	requests  map[requestMetricKey]requestMetricValue
}

type requestMetricKey struct {
	Method string
	Route  string
	Status int
}

type requestMetricValue struct {
	Count           int64
	DurationSeconds float64
}

type requestMetricSnapshot struct {
	key   requestMetricKey
	value requestMetricValue
}

func newServerMetrics(startedAt time.Time) *serverMetrics {
	return &serverMetrics{
		startedAt: startedAt,
		requests:  map[requestMetricKey]requestMetricValue{},
	}
}

func (m *serverMetrics) record(method, path string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	key := requestMetricKey{
		Method: method,
		Route:  metricRoute(path),
		Status: status,
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	value := m.requests[key]
	value.Count++
	value.DurationSeconds += duration.Seconds()
	m.requests[key] = value
}

func (m *serverMetrics) snapshot() (time.Time, []requestMetricSnapshot) {
	if m == nil {
		return time.Now().UTC(), nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]requestMetricSnapshot, 0, len(m.requests))
	for key, value := range m.requests {
		items = append(items, requestMetricSnapshot{key: key, value: value})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].key.Route == items[j].key.Route {
			if items[i].key.Method == items[j].key.Method {
				return items[i].key.Status < items[j].key.Status
			}
			return items[i].key.Method < items[j].key.Method
		}
		return items[i].key.Route < items[j].key.Route
	})
	return m.startedAt, items
}

func metricRoute(path string) string {
	switch {
	case path == "":
		return "/"
	case path == "/metrics":
		return "/metrics"
	case path == "/api/health":
		return "/api/health"
	case path == "/api/vods":
		return "/api/vods"
	case strings.HasPrefix(path, "/api/vods/"):
		return "/api/vods/{label}/video"
	case path == "/api/analysis-runs":
		return "/api/analysis-runs"
	case strings.HasPrefix(path, "/api/analysis-runs/"):
		return "/api/analysis-runs/{job_id}"
	case path == "/api/evaluation-annotations":
		return "/api/evaluation-annotations"
	case path == "/api/evaluation-runs":
		return "/api/evaluation-runs"
	case path == "/api/evaluations":
		return "/api/evaluations"
	case path == "/api/reports":
		return "/api/reports"
	case path == "/api/reports/latest":
		return "/api/reports/latest"
	case strings.HasPrefix(path, "/api/reports/"):
		return "/api/reports/{vod_label}/{run_id}"
	case strings.HasPrefix(path, "/artifacts/"):
		return "/artifacts/*"
	case strings.HasPrefix(path, "/api/"):
		return "/api/unknown"
	default:
		return "static"
	}
}

func promLabel(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return value
}

func boolGauge(value bool) int {
	if value {
		return 1
	}
	return 0
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, ErrorResponse{Error: err.Error()})
}

func matchesSearch(vod dataset.VOD, search string) bool {
	values := []string{vod.Label, string(vod.Rank), vod.Title, vod.Channel, vod.VideoID}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), search) {
			return true
		}
	}
	return false
}

func isAllowedDevOrigin(origin string) bool {
	switch origin {
	case "http://127.0.0.1:5173", "http://localhost:5173":
		return true
	default:
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme != "http" {
			return false
		}
		host := parsed.Hostname()
		port := parsed.Port()
		return (host == "127.0.0.1" || host == "localhost") && strings.HasPrefix(port, "517")
	}
}

func secondsDuration(seconds float64) time.Duration {
	return time.Duration(seconds * float64(time.Second))
}

func parsePort(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 {
		return fallback
	}
	return port
}

func AddrFromEnv(defaultPort int) string {
	port := parsePort(os.Getenv("PORT"), defaultPort)
	return fmt.Sprintf(":%d", port)
}
