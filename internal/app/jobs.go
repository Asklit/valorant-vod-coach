package app

import (
	"context"
	"encoding/json"
	"time"
)

const (
	AnalysisJobQueued    = "queued"
	AnalysisJobRunning   = "running"
	AnalysisJobCompleted = "completed"
	AnalysisJobFailed    = "failed"
)

type AnalysisJob struct {
	ID                    string
	OwnerID               string
	RunID                 string
	VODLabel              string
	Status                string
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

type AnalysisJobStore interface {
	CreateAnalysisJob(ctx context.Context, job AnalysisJob) error
	UpdateAnalysisJob(ctx context.Context, job AnalysisJob) error
	FindAnalysisJob(ctx context.Context, id string, ownerID string, includeAll bool) (AnalysisJob, bool, error)
	CountAnalysisJobs(ctx context.Context) (map[string]int, error)
}
