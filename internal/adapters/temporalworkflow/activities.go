package temporalworkflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
)

type Activities struct {
	Executor  app.AnalysisExecutor
	Jobs      app.AnalysisJobStore
	Clock     func() time.Time
	Telemetry *ActivityTelemetry
}

func (a Activities) RunAnalysis(ctx context.Context, input WorkflowInput) (app.AnalysisExecutionResult, error) {
	startedAt := time.Now()
	outcome := "failed"
	if a.Telemetry != nil {
		ctx = a.Telemetry.startRun(ctx, input.Request)
		defer func() { a.Telemetry.finishRun(ctx, input.Request, outcome, time.Since(startedAt)) }()
	}
	if a.Executor == nil || a.Jobs == nil {
		return app.AnalysisExecutionResult{}, errors.New("analysis executor and job store are required")
	}
	job, err := a.loadJob(ctx, input.JobID)
	if err != nil {
		return app.AnalysisExecutionResult{}, err
	}
	if job.CancellationRequested {
		outcome = "cancelled"
		return app.AnalysisExecutionResult{}, temporal.NewCanceledError("analysis cancellation requested")
	}

	now := a.now()
	if job.StartedAt == nil {
		job.StartedAt = &now
	}
	job.Status = app.AnalysisJobRunning
	job.Stage = "starting"
	job.ProgressPercent = 1
	job.Message = "Analyzing VOD"
	job.Error = ""
	attempt := int(activity.GetInfo(ctx).Attempt)
	job.Attempts = attempt
	job.UpdatedAt = now
	if err := a.Jobs.UpdateAnalysisJob(ctx, job); err != nil {
		return app.AnalysisExecutionResult{}, fmt.Errorf("mark analysis job running: %w", err)
	}

	activity.RecordHeartbeat(ctx, "analysis started")
	stopHeartbeat := make(chan struct{})
	defer close(stopHeartbeat)
	go heartbeat(ctx, stopHeartbeat)

	reporter := app.AnalysisProgressReporterFunc(func(progressCtx context.Context, progress app.AnalysisProgress) {
		activity.RecordHeartbeat(progressCtx, progress.Stage, progress.Percent)
		current, found, err := a.Jobs.FindAnalysisJob(progressCtx, input.JobID, "", true)
		if err != nil || !found || current.CancellationRequested {
			if err != nil && a.Telemetry != nil {
				a.Telemetry.progressFailure(progressCtx, err)
			}
			return
		}
		current.Stage = truncate(strings.TrimSpace(progress.Stage), 64)
		current.ProgressPercent = clampPercent(progress.Percent)
		current.Message = truncate(strings.TrimSpace(progress.Message), 512)
		current.UpdatedAt = a.now()
		if err := a.Jobs.UpdateAnalysisJob(progressCtx, current); err != nil && a.Telemetry != nil {
			a.Telemetry.progressFailure(progressCtx, err)
		}
	})
	request := input.Request
	if attempt > 1 {
		request.Force = true
	}
	result, err := a.Executor.RunAnalysis(ctx, request, reporter)
	if err != nil {
		return app.AnalysisExecutionResult{}, err
	}
	outcome = "completed"
	return result, nil
}

func (a Activities) Complete(ctx context.Context, input CompleteInput) error {
	job, err := a.loadJob(ctx, input.JobID)
	if err != nil {
		return err
	}
	if job.Status == app.AnalysisJobCancelled {
		return nil
	}
	if job.CancellationRequested {
		return temporal.NewCanceledError("analysis cancellation requested before completion")
	}
	now := a.now()
	job.Status = app.AnalysisJobCompleted
	job.Stage = "completed"
	job.ProgressPercent = 100
	job.Message = "Analysis completed"
	job.Error = ""
	job.ReportJSONPath = strings.TrimSpace(input.Result.ReportJSONPath)
	job.ReportMDPath = strings.TrimSpace(input.Result.ReportMDPath)
	job.FinishedAt = &now
	job.UpdatedAt = now
	err = a.Jobs.UpdateAnalysisJob(ctx, job)
	if err == nil && a.Telemetry != nil {
		a.Telemetry.jobFinalized(ctx, app.AnalysisJobCompleted)
	}
	return err
}

func (a Activities) Fail(ctx context.Context, input FailureInput) error {
	job, err := a.loadJob(ctx, input.JobID)
	if err != nil {
		return err
	}
	if job.Status == app.AnalysisJobCompleted || job.Status == app.AnalysisJobCancelled {
		return nil
	}
	now := a.now()
	job.Status = app.AnalysisJobFailed
	job.Stage = "failed"
	job.Message = "Analysis failed"
	job.Error = truncate(strings.TrimSpace(input.Error), 4096)
	job.FinishedAt = &now
	job.UpdatedAt = now
	err = a.Jobs.UpdateAnalysisJob(ctx, job)
	if err == nil && a.Telemetry != nil {
		a.Telemetry.jobFinalized(ctx, app.AnalysisJobFailed)
	}
	return err
}

func (a Activities) Cancel(ctx context.Context, input CancelInput) error {
	job, err := a.loadJob(ctx, input.JobID)
	if err != nil {
		return err
	}
	if job.Status == app.AnalysisJobCompleted || job.Status == app.AnalysisJobFailed {
		return nil
	}
	now := a.now()
	job.Status = app.AnalysisJobCancelled
	job.Stage = "cancelled"
	job.Message = "Analysis cancelled"
	job.Error = ""
	job.CancellationRequested = true
	job.FinishedAt = &now
	job.UpdatedAt = now
	err = a.Jobs.UpdateAnalysisJob(ctx, job)
	if err == nil && a.Telemetry != nil {
		a.Telemetry.jobFinalized(ctx, app.AnalysisJobCancelled)
	}
	return err
}

func (a Activities) loadJob(ctx context.Context, jobID string) (app.AnalysisJob, error) {
	job, found, err := a.Jobs.FindAnalysisJob(ctx, strings.TrimSpace(jobID), "", true)
	if err != nil {
		return app.AnalysisJob{}, err
	}
	if !found {
		return app.AnalysisJob{}, fmt.Errorf("analysis job not found: %s", jobID)
	}
	return job, nil
}

func (a Activities) now() time.Time {
	if a.Clock != nil {
		return a.Clock().UTC()
	}
	return time.Now().UTC()
}

func heartbeat(ctx context.Context, stop <-chan struct{}) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			activity.RecordHeartbeat(ctx, "analysis running")
		}
	}
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func clampPercent(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
