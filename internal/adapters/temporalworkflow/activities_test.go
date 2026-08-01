package temporalworkflow

import (
	"context"
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"go.temporal.io/sdk/testsuite"
)

func TestRunAnalysisActivityPersistsAttemptAndProgress(t *testing.T) {
	now := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	input := testWorkflowInput()
	store := &dispatcherJobStore{jobs: map[string]app.AnalysisJob{
		input.JobID: {
			ID: input.JobID, OwnerID: input.Request.OwnerID, VODLabel: input.Request.VODLabel, RunID: input.Request.RunID,
			Status: app.AnalysisJobQueued, MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
		},
	}}
	executor := progressExecutor{}
	activities := Activities{Executor: executor, Jobs: store, Clock: func() time.Time { return now }}
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivity(activities.RunAnalysis)

	encoded, err := env.ExecuteActivity(activities.RunAnalysis, input)
	if err != nil {
		t.Fatalf("ExecuteActivity() error = %v", err)
	}
	var result app.AnalysisExecutionResult
	if err := encoded.Get(&result); err != nil || result.ReportJSONPath != "report.json" {
		t.Fatalf("activity result = %+v err=%v", result, err)
	}
	job := store.jobs[input.JobID]
	if job.Status != app.AnalysisJobRunning || job.Attempts != 1 || job.Stage != "analyzing" || job.ProgressPercent != 55 {
		t.Fatalf("persisted running job = %+v", job)
	}
}

type progressExecutor struct{}

func (progressExecutor) RunAnalysis(ctx context.Context, _ app.AnalysisJobRequest, progress app.AnalysisProgressReporter) (app.AnalysisExecutionResult, error) {
	progress.ReportAnalysisProgress(ctx, app.AnalysisProgress{Stage: "sampling", Message: "Sampling", Percent: 20})
	progress.ReportAnalysisProgress(ctx, app.AnalysisProgress{Stage: "analyzing", Message: "Analyzing", Percent: 55})
	return app.AnalysisExecutionResult{ReportJSONPath: "report.json", ReportMDPath: "report.md"}, nil
}
