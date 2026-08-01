package temporalworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
)

func TestDispatcherStartsQueuedJobsAndRejectsCorruptTenantBinding(t *testing.T) {
	validRequest := app.AnalysisJobRequest{
		SchemaVersion: app.AnalysisJobRequestSchemaVersion,
		VODLabel:      "vod_valid", OwnerID: "user_valid", RunID: "run_valid",
		FPS: "1", DurationSeconds: 180, ImageQuality: 3,
	}
	validRaw, _ := json.Marshal(validRequest)
	corruptRequest := validRequest
	corruptRequest.OwnerID = "user_attacker"
	corruptRaw, _ := json.Marshal(corruptRequest)
	now := time.Now().UTC()
	store := &dispatcherJobStore{jobs: map[string]app.AnalysisJob{
		"job_valid": {
			ID: "job_valid", OwnerID: validRequest.OwnerID, VODLabel: validRequest.VODLabel, RunID: validRequest.RunID,
			Status: app.AnalysisJobQueued, Request: validRaw, CreatedAt: now, UpdatedAt: now,
		},
		"job_corrupt": {
			ID: "job_corrupt", OwnerID: validRequest.OwnerID, VODLabel: validRequest.VODLabel, RunID: validRequest.RunID,
			Status: app.AnalysisJobQueued, Request: corruptRaw, CreatedAt: now.Add(-time.Second), UpdatedAt: now,
		},
	}}
	launcher := &dispatcherLauncher{}
	dispatcher := Dispatcher{Launcher: launcher, Jobs: store}

	if err := dispatcher.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(launcher.started) != 1 || launcher.started[0] != "job_valid" {
		t.Fatalf("started jobs = %+v", launcher.started)
	}
	if got := store.jobs["job_corrupt"]; got.Status != app.AnalysisJobFailed || got.FinishedAt == nil {
		t.Fatalf("corrupt job was not terminally failed: %+v", got)
	}
}

func TestDispatcherLeavesQueuedIntentForTransientTemporalFailure(t *testing.T) {
	request := app.AnalysisJobRequest{
		SchemaVersion: app.AnalysisJobRequestSchemaVersion,
		VODLabel:      "vod_1", OwnerID: "user_1", RunID: "run_1", FPS: "1", ImageQuality: 3,
	}
	raw, _ := json.Marshal(request)
	now := time.Now().UTC()
	store := &dispatcherJobStore{jobs: map[string]app.AnalysisJob{
		"job_1": {
			ID: "job_1", OwnerID: request.OwnerID, VODLabel: request.VODLabel, RunID: request.RunID,
			Status: app.AnalysisJobQueued, Request: raw, CreatedAt: now, UpdatedAt: now,
		},
	}}
	dispatcher := Dispatcher{
		Launcher: &dispatcherLauncher{err: errors.New("Temporal unavailable")},
		Jobs:     store,
	}

	if err := dispatcher.Reconcile(context.Background()); err == nil {
		t.Fatal("Reconcile() unexpectedly succeeded")
	}
	if got := store.jobs["job_1"].Status; got != app.AnalysisJobQueued {
		t.Fatalf("job status = %q, want queued", got)
	}
}

func TestDispatcherRedeliversPersistedCancellationIntent(t *testing.T) {
	now := time.Now().UTC()
	store := &dispatcherJobStore{jobs: map[string]app.AnalysisJob{
		"job_running": {
			ID: "job_running", OwnerID: "user_1", RunID: "run_1", VODLabel: "vod_1",
			Status: app.AnalysisJobRunning, CancellationRequested: true, CreatedAt: now, UpdatedAt: now,
		},
		"job_terminal": {
			ID: "job_terminal", OwnerID: "user_1", RunID: "run_2", VODLabel: "vod_1",
			Status: app.AnalysisJobCancelled, CancellationRequested: true, CreatedAt: now.Add(-time.Second), UpdatedAt: now,
		},
	}}
	launcher := &dispatcherLauncher{}
	dispatcher := Dispatcher{Launcher: launcher, Jobs: store}

	if err := dispatcher.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(launcher.cancelled) != 1 || launcher.cancelled[0] != "job_running" {
		t.Fatalf("cancelled jobs = %+v", launcher.cancelled)
	}
}

type dispatcherLauncher struct {
	started   []string
	cancelled []string
	err       error
}

func (l *dispatcherLauncher) StartAnalysisWorkflow(_ context.Context, jobID string, _ app.AnalysisJobRequest) error {
	if l.err != nil {
		return l.err
	}
	l.started = append(l.started, jobID)
	return nil
}

func (l *dispatcherLauncher) CancelAnalysisWorkflow(_ context.Context, jobID string) error {
	if l.err != nil {
		return l.err
	}
	l.cancelled = append(l.cancelled, jobID)
	return nil
}

type dispatcherJobStore struct {
	jobs map[string]app.AnalysisJob
}

func (s *dispatcherJobStore) CreateAnalysisJob(_ context.Context, job app.AnalysisJob) error {
	s.jobs[job.ID] = job
	return nil
}

func (s *dispatcherJobStore) UpdateAnalysisJob(_ context.Context, job app.AnalysisJob) error {
	if _, found := s.jobs[job.ID]; !found {
		return errors.New("not found")
	}
	s.jobs[job.ID] = job
	return nil
}

func (s *dispatcherJobStore) FindAnalysisJob(_ context.Context, id string, ownerID string, includeAll bool) (app.AnalysisJob, bool, error) {
	job, found := s.jobs[id]
	if !found || (!includeAll && job.OwnerID != ownerID) {
		return app.AnalysisJob{}, false, nil
	}
	return job, true, nil
}

func (s *dispatcherJobStore) ListAnalysisJobs(_ context.Context, filter app.AnalysisJobListFilter) ([]app.AnalysisJob, error) {
	jobs := make([]app.AnalysisJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		if filter.Status != "" && job.Status != filter.Status {
			continue
		}
		if filter.CancellationRequested != nil && job.CancellationRequested != *filter.CancellationRequested {
			continue
		}
		if filter.ActiveOnly && job.Status != app.AnalysisJobQueued && job.Status != app.AnalysisJobRunning {
			continue
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (s *dispatcherJobStore) CountAnalysisJobs(context.Context) (map[string]int, error) {
	return map[string]int{}, nil
}
