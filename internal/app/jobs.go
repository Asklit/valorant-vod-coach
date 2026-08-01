package app

import (
	"context"
	"encoding/json"
	"time"
)

const (
	AnalysisJobRequestSchemaVersion = 1

	AnalysisJobQueued    = "queued"
	AnalysisJobRunning   = "running"
	AnalysisJobCompleted = "completed"
	AnalysisJobFailed    = "failed"
	AnalysisJobCancelled = "cancelled"
)

type AnalysisJobRequest struct {
	SchemaVersion   int     `json:"schema_version"`
	VODLabel        string  `json:"vod_label"`
	OwnerID         string  `json:"owner_id"`
	RunID           string  `json:"run_id"`
	FPS             string  `json:"fps"`
	StartSeconds    float64 `json:"start_seconds"`
	DurationSeconds float64 `json:"duration_seconds"`
	ImageQuality    int     `json:"image_quality"`
	Force           bool    `json:"force"`
	ModelReview     bool    `json:"model_review"`
	IncludeAllVODs  bool    `json:"include_all_vods"`
}

type AnalysisExecutionResult struct {
	ReportJSONPath string `json:"report_json_path"`
	ReportMDPath   string `json:"report_markdown_path"`
}

type AnalysisProgress struct {
	Stage   string
	Message string
	Percent int
}

type AnalysisProgressReporter interface {
	ReportAnalysisProgress(ctx context.Context, progress AnalysisProgress)
}

type AnalysisProgressReporterFunc func(ctx context.Context, progress AnalysisProgress)

func (f AnalysisProgressReporterFunc) ReportAnalysisProgress(ctx context.Context, progress AnalysisProgress) {
	f(ctx, progress)
}

type AnalysisExecutor interface {
	RunAnalysis(ctx context.Context, request AnalysisJobRequest, progress AnalysisProgressReporter) (AnalysisExecutionResult, error)
}

type AnalysisWorkflowLauncher interface {
	StartAnalysisWorkflow(ctx context.Context, jobID string, request AnalysisJobRequest) error
	CancelAnalysisWorkflow(ctx context.Context, jobID string) error
}

type AnalysisJob struct {
	ID                    string
	OwnerID               string
	RunID                 string
	VODLabel              string
	Status                string
	Stage                 string
	ProgressPercent       int
	Message               string
	Error                 string
	ReportJSONPath        string
	ReportMDPath          string
	Request               json.RawMessage
	Attempts              int
	MaxAttempts           int
	LeaseOwner            string
	LeaseExpiresAt        *time.Time
	CancellationRequested bool
	CreatedAt             time.Time
	StartedAt             *time.Time
	FinishedAt            *time.Time
	UpdatedAt             time.Time
}

type AnalysisJobListFilter struct {
	OwnerID               string
	VODLabel              string
	Status                string
	IncludeAll            bool
	Limit                 int
	Before                *time.Time
	BeforeID              string
	CancellationRequested *bool
	ActiveOnly            bool
}

type AnalysisJobStore interface {
	CreateAnalysisJob(ctx context.Context, job AnalysisJob) error
	UpdateAnalysisJob(ctx context.Context, job AnalysisJob) error
	FindAnalysisJob(ctx context.Context, id string, ownerID string, includeAll bool) (AnalysisJob, bool, error)
	ListAnalysisJobs(ctx context.Context, filter AnalysisJobListFilter) ([]AnalysisJob, error)
	CountAnalysisJobs(ctx context.Context) (map[string]int, error)
}
