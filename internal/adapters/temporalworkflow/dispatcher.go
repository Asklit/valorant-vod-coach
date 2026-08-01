package temporalworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
)

type Dispatcher struct {
	Launcher  app.AnalysisWorkflowLauncher
	Jobs      app.AnalysisJobStore
	Interval  time.Duration
	BatchSize int
	OnError   func(error)
}

func (d Dispatcher) Run(ctx context.Context) {
	interval := d.Interval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	d.reconcileAndReport(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.reconcileAndReport(ctx)
		}
	}
}

func (d Dispatcher) Reconcile(ctx context.Context) error {
	if d.Launcher == nil || d.Jobs == nil {
		return errors.New("workflow launcher and job store are required")
	}
	limit := d.BatchSize
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	jobs, err := d.Jobs.ListAnalysisJobs(ctx, app.AnalysisJobListFilter{
		IncludeAll: true,
		Status:     app.AnalysisJobQueued,
		Limit:      limit,
	})
	if err != nil {
		return fmt.Errorf("list queued analysis jobs: %w", err)
	}
	var dispatchErrors []error
	for _, job := range jobs {
		var request app.AnalysisJobRequest
		if err := json.Unmarshal(job.Request, &request); err != nil {
			d.failCorruptJob(ctx, job, fmt.Errorf("decode queued job request: %w", err))
			continue
		}
		if err := validateJobBinding(job, request); err != nil {
			d.failCorruptJob(ctx, job, err)
			continue
		}
		if err := d.Launcher.StartAnalysisWorkflow(ctx, job.ID, request); err != nil {
			dispatchErrors = append(dispatchErrors, fmt.Errorf("dispatch job %s: %w", job.ID, err))
		}
	}
	cancellationRequested := true
	cancellations, err := d.Jobs.ListAnalysisJobs(ctx, app.AnalysisJobListFilter{
		IncludeAll:            true,
		CancellationRequested: &cancellationRequested,
		ActiveOnly:            true,
		Limit:                 limit,
	})
	if err != nil {
		dispatchErrors = append(dispatchErrors, fmt.Errorf("list requested analysis cancellations: %w", err))
	} else {
		for _, job := range cancellations {
			if err := d.Launcher.CancelAnalysisWorkflow(ctx, job.ID); err != nil {
				dispatchErrors = append(dispatchErrors, fmt.Errorf("deliver cancellation for job %s: %w", job.ID, err))
			}
		}
	}
	return errors.Join(dispatchErrors...)
}

func (d Dispatcher) reconcileAndReport(ctx context.Context) {
	if err := d.Reconcile(ctx); err != nil && d.OnError != nil {
		d.OnError(err)
	}
}

func (d Dispatcher) failCorruptJob(ctx context.Context, job app.AnalysisJob, cause error) {
	now := time.Now().UTC()
	job.Status = app.AnalysisJobFailed
	job.Stage = "failed"
	job.Message = "Stored analysis request is invalid"
	job.Error = truncate(cause.Error(), 4096)
	job.FinishedAt = &now
	job.UpdatedAt = now
	if err := d.Jobs.UpdateAnalysisJob(ctx, job); err != nil && d.OnError != nil {
		d.OnError(fmt.Errorf("fail corrupt job %s: %w", job.ID, err))
	}
}

func validateJobBinding(job app.AnalysisJob, request app.AnalysisJobRequest) error {
	if request.SchemaVersion != app.AnalysisJobRequestSchemaVersion {
		return fmt.Errorf("unsupported stored request schema version %d", request.SchemaVersion)
	}
	if strings.TrimSpace(request.OwnerID) != strings.TrimSpace(job.OwnerID) ||
		strings.TrimSpace(request.VODLabel) != strings.TrimSpace(job.VODLabel) ||
		strings.TrimSpace(request.RunID) != strings.TrimSpace(job.RunID) {
		return errors.New("stored request tenant or resource binding does not match its job")
	}
	return nil
}
