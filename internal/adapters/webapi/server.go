package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
)

type Config struct {
	ManifestPath  string
	RawRoot       string
	ProcessedRoot string
	FFprobePath   string
	FFmpegPath    string
	VisionURL     string
	StaticDir     string
}

type Server struct {
	config Config
	mux    *http.ServeMux
	jobs   *analysisJobStore
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
	if config.FFprobePath == "" {
		config.FFprobePath = "ffprobe"
	}
	if config.FFmpegPath == "" {
		config.FFmpegPath = "ffmpeg"
	}

	server := &Server{config: config, mux: http.NewServeMux(), jobs: &analysisJobStore{jobs: map[string]*analysisJob{}}}
	server.routes()
	return server
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); isAllowedDevOrigin(origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/vods", s.handleListVODs)
	s.mux.HandleFunc("GET /api/vods/", s.handleVODVideo)
	s.mux.HandleFunc("POST /api/analysis-runs", s.handleAnalyze)
	s.mux.HandleFunc("GET /api/analysis-runs/", s.handleAnalysisJob)
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
