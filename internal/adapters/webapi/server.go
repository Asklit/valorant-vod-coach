package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/pprof"
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
	AuthStorePath             string
	AuthHashIterations        int
	Catalog                   app.AnalysisCatalog
	ReportCatalog             app.ReportCatalog
	Locks                     app.LockManager
	Logger                    *slog.Logger
	Tracer                    trace.Tracer
}

type Server struct {
	config  Config
	mux     *http.ServeMux
	jobs    *analysisJobStore
	metrics *serverMetrics
	auth    *authSessionStore
	users   *app.AuthStore
	logs    *requestLogStore
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

type AuthRegisterRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type AuthLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	User  app.PublicAuthUser `json:"user"`
	Token string             `json:"token"`
}

type AuthSessionResponse struct {
	Authenticated bool                `json:"authenticated"`
	User          *app.PublicAuthUser `json:"user,omitempty"`
}

type AdminOverviewResponse struct {
	GeneratedAt time.Time          `json:"generated_at"`
	User        app.PublicAuthUser `json:"user"`
	System      AdminSystemStatus  `json:"system"`
	Dataset     Counts             `json:"dataset"`
	Jobs        map[string]int     `json:"jobs"`
	Auth        AdminAuthStatus    `json:"auth"`
}

type AdminSystemStatus struct {
	SchemaVersion       int    `json:"schema_version"`
	Analyzer            string `json:"analyzer"`
	ModelReviewEnabled  bool   `json:"model_review_enabled"`
	ManifestPath        string `json:"manifest_path"`
	RawRoot             string `json:"raw_root"`
	ProcessedRoot       string `json:"processed_root"`
	EvaluationLabelRoot string `json:"evaluation_label_root"`
}

type AdminAuthStatus struct {
	UserCount int `json:"user_count"`
}

type AdminUsersResponse struct {
	Users []app.PublicAuthUser `json:"users"`
}

type AdminMetricsResponse struct {
	StartedAt time.Time            `json:"started_at"`
	Requests  []AdminRequestMetric `json:"requests"`
	Jobs      map[string]int       `json:"jobs"`
	Logs      []requestLogEntry    `json:"logs"`
	Routes    []string             `json:"routes"`
	User      app.PublicAuthUser   `json:"user"`
}

type AdminRequestMetric struct {
	Method          string  `json:"method"`
	Route           string  `json:"route"`
	Status          int     `json:"status"`
	Count           int64   `json:"count"`
	DurationSeconds float64 `json:"duration_seconds"`
}

type AdminLogsResponse struct {
	Logs []requestLogEntry `json:"logs"`
}

type ManualCorrectionRequest struct {
	VODLabel         string   `json:"vod_label"`
	ReportRunID      string   `json:"report_run_id"`
	Type             string   `json:"type"`
	TargetID         string   `json:"target_id,omitempty"`
	CorrectedValue   string   `json:"corrected_value,omitempty"`
	Comment          string   `json:"comment,omitempty"`
	TimestampSeconds *float64 `json:"timestamp_seconds,omitempty"`
	Author           string   `json:"author,omitempty"`
}

type ManualCorrectionResponse struct {
	VODLabel    string                    `json:"vod_label"`
	ReportRunID string                    `json:"report_run_id,omitempty"`
	Corrections []domain.ManualCorrection `json:"corrections"`
	JSONPath    string                    `json:"json_path"`
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
	if config.AuthStorePath == "" {
		config.AuthStorePath = filepath.Join(config.ProcessedRoot, "auth", "users.json")
	}

	server := &Server{
		config:  config,
		mux:     http.NewServeMux(),
		jobs:    &analysisJobStore{jobs: map[string]*analysisJob{}},
		metrics: newServerMetrics(time.Now().UTC()),
		auth:    newAuthSessionStore(24 * time.Hour),
		users:   &app.AuthStore{Path: config.AuthStorePath, Iterations: config.AuthHashIterations},
		logs:    newRequestLogStore(200),
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
		recorder.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	}
	if r.Method == http.MethodOptions {
		recorder.WriteHeader(http.StatusNoContent)
		status := recorder.statusCode()
		span.SetAttributes(attribute.Int("http.response.status_code", status))
		s.metrics.record(r.Method, r.URL.Path, status, time.Since(started))
		s.logRequest(ctx, r, route, status, time.Since(started))
		return
	}
	if s.requiresAPIAuth(r) {
		if _, ok := s.currentUser(r); !ok {
			writeError(recorder, http.StatusUnauthorized, errors.New("authentication required"))
			status := recorder.statusCode()
			span.SetAttributes(attribute.Int("http.response.status_code", status))
			s.metrics.record(r.Method, r.URL.Path, status, time.Since(started))
			s.logRequest(ctx, r, route, status, time.Since(started))
			return
		}
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
	if s.logs != nil {
		user, _ := s.currentUser(r)
		s.logs.append(requestLogEntry{
			Time:       time.Now().UTC(),
			Method:     r.Method,
			Path:       r.URL.Path,
			Route:      route,
			Status:     status,
			DurationMS: float64(duration.Microseconds()) / 1000,
			UserID:     user.ID,
			UserEmail:  user.Email,
			UserRole:   user.Role,
		})
	}
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
	s.mux.HandleFunc("GET /healthz", s.handleLiveness)
	s.mux.HandleFunc("GET /readyz", s.handleReadiness)
	s.mux.HandleFunc("GET /metrics", s.handleMetrics)
	s.registerPprofRoutes()
	s.mux.HandleFunc("POST /api/auth/register", s.handleAuthRegister)
	s.mux.HandleFunc("POST /api/auth/login", s.handleAuthLogin)
	s.mux.HandleFunc("POST /api/auth/logout", s.handleAuthLogout)
	s.mux.HandleFunc("GET /api/auth/session", s.handleAuthSession)
	s.mux.HandleFunc("GET /api/admin/overview", s.handleAdminOverview)
	s.mux.HandleFunc("GET /api/admin/metrics", s.handleAdminMetrics)
	s.mux.HandleFunc("GET /api/admin/logs", s.handleAdminLogs)
	s.mux.HandleFunc("GET /api/admin/users", s.handleAdminUsers)
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/vods", s.handleListVODs)
	s.mux.HandleFunc("GET /api/vods/", s.handleVODVideo)
	s.mux.HandleFunc("POST /api/analysis-runs", s.handleAnalyze)
	s.mux.HandleFunc("GET /api/analysis-runs/", s.handleAnalysisJob)
	s.mux.HandleFunc("GET /api/evaluation-annotations", s.handleEvaluationAnnotations)
	s.mux.HandleFunc("POST /api/evaluation-runs", s.handleRunEvaluation)
	s.mux.HandleFunc("GET /api/evaluations", s.handleEvaluations)
	s.mux.HandleFunc("GET /api/corrections", s.handleListCorrections)
	s.mux.HandleFunc("POST /api/corrections", s.handleCreateCorrection)
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

func (s *Server) registerPprofRoutes() {
	s.mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	s.mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	s.mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	s.mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	s.mux.HandleFunc("POST /debug/pprof/symbol", pprof.Symbol)
	s.mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	for _, name := range []string{"allocs", "block", "goroutine", "heap", "mutex", "threadcreate"} {
		s.mux.Handle("GET /debug/pprof/"+name, pprof.Handler(name))
	}
}

func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"schema_version": domain.AnalysisReportSchemaVersion,
		"service":        "vod-web",
	})
}

type readinessCheck struct {
	Status  string `json:"status"`
	Detail  string `json:"detail,omitempty"`
	Path    string `json:"path,omitempty"`
	Runtime string `json:"runtime,omitempty"`
	Model   string `json:"model,omitempty"`
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	checks := map[string]readinessCheck{
		"manifest":       checkExistingFile(s.config.ManifestPath),
		"raw_root":       checkExistingDir(s.config.RawRoot),
		"processed_root": checkWritableTargetDir(s.config.ProcessedRoot),
		"vision_service": s.checkVisionReadiness(r.Context()),
	}

	ready := true
	for _, check := range checks {
		if check.Status == "failed" {
			ready = false
			break
		}
	}

	status := http.StatusOK
	payloadStatus := "ready"
	if !ready {
		status = http.StatusServiceUnavailable
		payloadStatus = "not_ready"
	}

	writeJSON(w, status, map[string]any{
		"status":         payloadStatus,
		"schema_version": domain.AnalysisReportSchemaVersion,
		"service":        "vod-web",
		"checks":         checks,
	})
}

func (s *Server) checkVisionReadiness(ctx context.Context) readinessCheck {
	if strings.TrimSpace(s.config.VisionURL) == "" {
		return readinessCheck{Status: "skipped", Detail: "VISION_SERVICE_URL is not configured"}
	}

	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	status, err := (visionservice.Client{
		BaseURL:    s.config.VisionURL,
		HTTPClient: &http.Client{Timeout: time.Second},
	}).Health(ctx)
	if err != nil {
		return readinessCheck{Status: "failed", Detail: err.Error()}
	}
	if !strings.EqualFold(status.Status, "ok") {
		return readinessCheck{Status: "failed", Detail: "vision service returned " + status.Status, Runtime: status.Runtime, Model: status.Model}
	}
	return readinessCheck{Status: "ok", Runtime: status.Runtime, Model: status.Model}
}

func checkExistingFile(path string) readinessCheck {
	check := readinessCheck{Path: path}
	info, err := os.Stat(path)
	if err != nil {
		check.Status = "failed"
		check.Detail = err.Error()
		return check
	}
	if info.IsDir() {
		check.Status = "failed"
		check.Detail = "expected a file"
		return check
	}
	check.Status = "ok"
	return check
}

func checkExistingDir(path string) readinessCheck {
	check := readinessCheck{Path: path}
	info, err := os.Stat(path)
	if err != nil {
		check.Status = "failed"
		check.Detail = err.Error()
		return check
	}
	if !info.IsDir() {
		check.Status = "failed"
		check.Detail = "expected a directory"
		return check
	}
	check.Status = "ok"
	return check
}

func checkWritableTargetDir(path string) readinessCheck {
	check := readinessCheck{Path: path}
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			check.Status = "failed"
			check.Detail = "expected a directory"
			return check
		}
		check.Status = "ok"
		return check
	}
	if !os.IsNotExist(err) {
		check.Status = "failed"
		check.Detail = err.Error()
		return check
	}

	parent := filepath.Dir(path)
	parentCheck := checkExistingDir(parent)
	if parentCheck.Status != "ok" {
		check.Status = "failed"
		check.Detail = "parent is not ready: " + parentCheck.Detail
		return check
	}
	return readinessCheck{Status: "ok", Path: path, Detail: "directory will be created on first write"}
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

func (s *Server) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var request AuthRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	user, err := s.authStore().Register(r.Context(), app.AuthRegisterRequest{
		Email:       request.Email,
		Password:    request.Password,
		DisplayName: request.DisplayName,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	token, err := s.auth.create(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, AuthResponse{User: user, Token: token})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var request AuthLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	user, err := s.authStore().Authenticate(r.Context(), app.AuthLoginRequest{
		Email:    request.Email,
		Password: request.Password,
	})
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	token, err := s.auth.create(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, AuthResponse{User: user, Token: token})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if token := bearerToken(r); token != "" {
		s.auth.delete(token)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(r)
	if !ok {
		writeJSON(w, http.StatusOK, AuthSessionResponse{Authenticated: false})
		return
	}
	writeJSON(w, http.StatusOK, AuthSessionResponse{Authenticated: true, User: &user})
}

func (s *Server) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	vods, err := dataset.LoadManifest(s.config.ManifestPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("load manifest: %w", err))
		return
	}
	assets := dataset.ScanLocalAssets(s.config.RawRoot, dataset.Filter(vods, "", true))
	counts := Counts{Total: len(vods), Enabled: dataset.CountEnabled(vods)}
	for _, asset := range assets {
		if asset.Status == dataset.LocalStatusDownloaded {
			counts.Downloaded++
		}
		reports, err := s.listReportSummaries(r.Context(), asset.VOD.Label)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if len(reports) > 0 {
			counts.Reported++
		}
	}
	userCount, _ := s.authStore().UserCount(r.Context())
	writeJSON(w, http.StatusOK, AdminOverviewResponse{
		GeneratedAt: time.Now().UTC(),
		User:        user,
		System: AdminSystemStatus{
			SchemaVersion:       domain.AnalysisReportSchemaVersion,
			Analyzer:            "visual-heuristic-gameplay",
			ModelReviewEnabled:  strings.TrimSpace(s.config.VisionURL) != "",
			ManifestPath:        s.config.ManifestPath,
			RawRoot:             s.config.RawRoot,
			ProcessedRoot:       s.config.ProcessedRoot,
			EvaluationLabelRoot: s.config.EvaluationAnnotationsRoot,
		},
		Dataset: counts,
		Jobs:    s.jobs.countByStatus(),
		Auth:    AdminAuthStatus{UserCount: userCount},
	})
}

func (s *Server) handleAdminMetrics(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	startedAt, requestMetrics := s.metrics.snapshot()
	metrics := make([]AdminRequestMetric, 0, len(requestMetrics))
	for _, item := range requestMetrics {
		metrics = append(metrics, AdminRequestMetric{
			Method:          item.key.Method,
			Route:           item.key.Route,
			Status:          item.key.Status,
			Count:           item.value.Count,
			DurationSeconds: item.value.DurationSeconds,
		})
	}
	writeJSON(w, http.StatusOK, AdminMetricsResponse{
		StartedAt: startedAt,
		Requests:  metrics,
		Jobs:      s.jobs.countByStatus(),
		Logs:      s.logs.snapshot(20),
		Routes:    adminRouteList(),
		User:      user,
	})
}

func (s *Server) handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, AdminLogsResponse{Logs: s.logs.snapshot(100)})
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	users, err := s.authStore().ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, AdminUsersResponse{Users: users})
}

func (s *Server) authStore() *app.AuthStore {
	if s.users == nil {
		s.users = &app.AuthStore{Path: s.config.AuthStorePath, Iterations: s.config.AuthHashIterations}
	}
	return s.users
}

func (s *Server) currentUser(r *http.Request) (app.PublicAuthUser, bool) {
	token := bearerToken(r)
	if token == "" || s.auth == nil {
		return app.PublicAuthUser{}, false
	}
	return s.auth.get(token)
}

func (s *Server) requiresAPIAuth(r *http.Request) bool {
	path := r.URL.Path
	if !strings.HasPrefix(path, "/api/") {
		return false
	}
	if strings.HasPrefix(path, "/api/auth/") || path == "/api/health" {
		return false
	}
	if r.Method == http.MethodGet && isVODVideoPath(path) {
		return false
	}
	return true
}

func isVODVideoPath(path string) bool {
	return strings.HasPrefix(path, "/api/vods/") && strings.HasSuffix(path, "/video")
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) (app.PublicAuthUser, bool) {
	user, ok := s.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
		return app.PublicAuthUser{}, false
	}
	if user.Role != app.AuthRoleAdmin {
		writeError(w, http.StatusForbidden, errors.New("admin role required"))
		return app.PublicAuthUser{}, false
	}
	return user, true
}

func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return ""
	}
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
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

		reports, err := s.listReportSummaries(r.Context(), asset.VOD.Label)
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
			item.LatestReportPath = latest.JSONPath
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

	reportPath, err := s.resolveReportPath(r.Context(), label, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if reportPath == "" {
		writeError(w, http.StatusNotFound, fmt.Errorf("no reports for %s", label))
		return
	}

	report, err := readReport(reportPath)
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

	summaries, err := s.listReportSummaries(r.Context(), label)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
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

func (s *Server) handleListCorrections(w http.ResponseWriter, r *http.Request) {
	label := strings.TrimSpace(r.URL.Query().Get("vod_label"))
	if label == "" {
		writeError(w, http.StatusBadRequest, errors.New("vod_label is required"))
		return
	}
	reportRunID := strings.TrimSpace(r.URL.Query().Get("report_run_id"))

	set, saved, err := app.LoadManualCorrections(s.correctionsRoot(), label, reportRunID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("load corrections: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, ManualCorrectionResponse{
		VODLabel:    set.VODLabel,
		ReportRunID: set.ReportRunID,
		Corrections: set.Corrections,
		JSONPath:    saved.JSONPath,
	})
}

func (s *Server) handleCreateCorrection(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var request ManualCorrectionRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	if strings.TrimSpace(request.VODLabel) == "" {
		writeError(w, http.StatusBadRequest, errors.New("vod_label is required"))
		return
	}

	set, saved, err := app.AppendManualCorrection(r.Context(), s.correctionsRoot(), request.VODLabel, request.ReportRunID, domain.ManualCorrection{
		Type:             request.Type,
		TargetID:         request.TargetID,
		CorrectedValue:   request.CorrectedValue,
		Comment:          request.Comment,
		TimestampSeconds: request.TimestampSeconds,
		Author:           request.Author,
	}, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, ManualCorrectionResponse{
		VODLabel:    set.VODLabel,
		ReportRunID: set.ReportRunID,
		Corrections: set.Corrections,
		JSONPath:    saved.JSONPath,
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

	reportPath, err := s.resolveReportPath(r.Context(), parts[0], parts[1])
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if reportPath == "" {
		writeError(w, http.StatusNotFound, fmt.Errorf("no report %s for %s", parts[1], parts[0]))
		return
	}
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

func (s *Server) listReportSummaries(ctx context.Context, vodLabel string) ([]ReportSummary, error) {
	if s.config.ReportCatalog != nil {
		catalogSummaries, err := s.config.ReportCatalog.ListReportSummaries(ctx, vodLabel)
		if err != nil {
			return nil, err
		}
		summaries := make([]ReportSummary, 0, len(catalogSummaries))
		for _, summary := range catalogSummaries {
			summaries = append(summaries, reportCatalogSummaryToAPI(summary))
		}
		return summaries, nil
	}

	reports, err := s.listReports(vodLabel)
	if err != nil {
		return nil, err
	}
	summaries := make([]ReportSummary, 0, len(reports))
	for _, report := range reports {
		summaries = append(summaries, report.Summary)
	}
	return summaries, nil
}

func (s *Server) correctionsRoot() string {
	return filepath.Join(s.config.ProcessedRoot, "corrections")
}

func (s *Server) resolveReportPath(ctx context.Context, vodLabel string, reportRunID string) (string, error) {
	reportRunID = strings.TrimSpace(reportRunID)
	if reportRunID != "" && s.config.ReportCatalog == nil {
		return filepath.Join(s.config.ProcessedRoot, vodLabel, "reports", reportRunID, reportstore.JSONReportName), nil
	}

	summaries, err := s.listReportSummaries(ctx, vodLabel)
	if err != nil {
		return "", err
	}
	for _, summary := range summaries {
		if reportRunID == "" || summary.RunID == reportRunID {
			return summary.JSONPath, nil
		}
	}
	return "", nil
}

func reportCatalogSummaryToAPI(summary app.ReportCatalogSummary) ReportSummary {
	return ReportSummary{
		SchemaVersion:     summary.SchemaVersion,
		RunID:             summary.RunID,
		Status:            summary.Status,
		GeneratedAt:       summary.GeneratedAt,
		FindingCount:      summary.FindingCount,
		FrameCount:        summary.FrameCount,
		ReviewWindowCount: summary.ReviewWindowCount,
		RoundSegmentCount: summary.RoundSegmentCount,
		ModelTaskCount:    summary.ModelReviewTaskCount,
		ModelRunCount:     summary.ModelReviewRunCount,
		Analyzer:          summary.Analyzer,
		SampleName:        summary.SampleName,
		SampleFPS:         summary.SampleFPS,
		SampleDuration:    summary.SampleDuration,
		ContactSheet:      summary.ContactSheetPath,
		JSONPath:          summary.JSONPath,
		MarkdownPath:      summary.MarkdownPath,
	}
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

	reportPath, err := s.resolveEvaluationReportPath(ctx, vodLabel, request.ReportRunID)
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

func (s *Server) resolveEvaluationReportPath(ctx context.Context, vodLabel string, reportRunID string) (string, error) {
	reportPath, err := s.resolveReportPath(ctx, vodLabel, reportRunID)
	if err != nil {
		return "", err
	}
	if reportPath == "" {
		if strings.TrimSpace(reportRunID) != "" {
			return "", fmt.Errorf("no report %s for %s", reportRunID, vodLabel)
		}
		return "", fmt.Errorf("no reports for %s", vodLabel)
	}
	return reportPath, nil
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

type authSession struct {
	Token     string
	User      app.PublicAuthUser
	ExpiresAt time.Time
}

type authSessionStore struct {
	mu       sync.Mutex
	ttl      time.Duration
	sessions map[string]authSession
}

type requestLogEntry struct {
	Time       time.Time `json:"time"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Route      string    `json:"route"`
	Status     int       `json:"status"`
	DurationMS float64   `json:"duration_ms"`
	UserID     string    `json:"user_id,omitempty"`
	UserEmail  string    `json:"user_email,omitempty"`
	UserRole   string    `json:"user_role,omitempty"`
}

type requestLogStore struct {
	mu      sync.Mutex
	limit   int
	entries []requestLogEntry
}

func newAuthSessionStore(ttl time.Duration) *authSessionStore {
	return &authSessionStore{ttl: ttl, sessions: map[string]authSession{}}
}

func (s *authSessionStore) create(user app.PublicAuthUser) (string, error) {
	token, err := app.NewAuthToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = authSession{
		Token:     token,
		User:      user,
		ExpiresAt: time.Now().UTC().Add(s.ttl),
	}
	return token, nil
}

func (s *authSessionStore) get(token string) (app.PublicAuthUser, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[token]
	if !ok {
		return app.PublicAuthUser{}, false
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		delete(s.sessions, token)
		return app.PublicAuthUser{}, false
	}
	return session.User, true
}

func (s *authSessionStore) delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

func newRequestLogStore(limit int) *requestLogStore {
	if limit <= 0 {
		limit = 100
	}
	return &requestLogStore{limit: limit}
}

func (s *requestLogStore) append(entry requestLogEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry)
	if len(s.entries) > s.limit {
		s.entries = append([]requestLogEntry(nil), s.entries[len(s.entries)-s.limit:]...)
	}
}

func (s *requestLogStore) snapshot(limit int) []requestLogEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > len(s.entries) {
		limit = len(s.entries)
	}
	out := make([]requestLogEntry, 0, limit)
	for i := len(s.entries) - 1; i >= len(s.entries)-limit; i-- {
		out = append(out, s.entries[i])
	}
	return out
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
	case path == "/healthz":
		return "/healthz"
	case path == "/readyz":
		return "/readyz"
	case path == "/metrics":
		return "/metrics"
	case strings.HasPrefix(path, "/debug/pprof/"):
		return "/debug/pprof/*"
	case strings.HasPrefix(path, "/api/auth/"):
		return "/api/auth/*"
	case strings.HasPrefix(path, "/api/admin/"):
		return "/api/admin/*"
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
	case path == "/api/corrections":
		return "/api/corrections"
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

func adminRouteList() []string {
	return []string{
		"/healthz",
		"/readyz",
		"/metrics",
		"/api/auth/*",
		"/api/admin/*",
		"/api/health",
		"/api/vods",
		"/api/analysis-runs",
		"/api/reports",
		"/api/evaluations",
		"/api/corrections",
		"/artifacts/*",
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
