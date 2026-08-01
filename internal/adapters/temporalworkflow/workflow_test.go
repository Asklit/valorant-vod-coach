package temporalworkflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestProcessAnalysisWorkflowCompletesReadModel(t *testing.T) {
	env := newWorkflowEnvironment(t)
	input := testWorkflowInput()
	want := app.AnalysisExecutionResult{ReportJSONPath: "report.json", ReportMDPath: "report.md"}
	completed := false

	env.RegisterActivityWithOptions(func(context.Context, WorkflowInput) (app.AnalysisExecutionResult, error) {
		return want, nil
	}, activity.RegisterOptions{Name: RunAnalysisActivityName})
	env.RegisterActivityWithOptions(func(_ context.Context, got CompleteInput) error {
		completed = got.JobID == input.JobID && got.Result == want
		return nil
	}, activity.RegisterOptions{Name: CompleteActivityName})
	registerUnusedFinalizers(env)

	env.ExecuteWorkflow(ProcessAnalysisWorkflow, input)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if !completed {
		t.Fatal("completion activity did not receive the execution result")
	}
}

func TestProcessAnalysisWorkflowRetriesAndRecordsTerminalFailure(t *testing.T) {
	env := newWorkflowEnvironment(t)
	input := testWorkflowInput()
	attempts := 0
	recordedFailure := ""

	env.RegisterActivityWithOptions(func(context.Context, WorkflowInput) (app.AnalysisExecutionResult, error) {
		attempts++
		return app.AnalysisExecutionResult{}, errors.New("decoder unavailable")
	}, activity.RegisterOptions{Name: RunAnalysisActivityName})
	env.RegisterActivityWithOptions(func(_ context.Context, got FailureInput) error {
		recordedFailure = got.Error
		return nil
	}, activity.RegisterOptions{Name: FailActivityName})
	env.RegisterActivityWithOptions(func(context.Context, CompleteInput) error { return nil }, activity.RegisterOptions{Name: CompleteActivityName})
	env.RegisterActivityWithOptions(func(context.Context, CancelInput) error { return nil }, activity.RegisterOptions{Name: CancelActivityName})

	env.ExecuteWorkflow(ProcessAnalysisWorkflow, input)
	if err := env.GetWorkflowError(); err == nil {
		t.Fatal("workflow unexpectedly succeeded")
	}
	if attempts != 3 {
		t.Fatalf("activity attempts = %d, want 3", attempts)
	}
	if !strings.Contains(recordedFailure, "decoder unavailable") {
		t.Fatalf("recorded failure = %q", recordedFailure)
	}
}

func TestProcessAnalysisWorkflowFinalizesCancellation(t *testing.T) {
	env := newWorkflowEnvironment(t)
	input := testWorkflowInput()
	cancelled := false

	env.RegisterActivityWithOptions(func(context.Context, WorkflowInput) (app.AnalysisExecutionResult, error) {
		return app.AnalysisExecutionResult{}, temporal.NewCanceledError("cancelled by user")
	}, activity.RegisterOptions{Name: RunAnalysisActivityName})
	env.RegisterActivityWithOptions(func(_ context.Context, got CancelInput) error {
		cancelled = got.JobID == input.JobID
		return nil
	}, activity.RegisterOptions{Name: CancelActivityName})
	env.RegisterActivityWithOptions(func(context.Context, CompleteInput) error { return nil }, activity.RegisterOptions{Name: CompleteActivityName})
	env.RegisterActivityWithOptions(func(context.Context, FailureInput) error { return nil }, activity.RegisterOptions{Name: FailActivityName})

	env.ExecuteWorkflow(ProcessAnalysisWorkflow, input)
	if err := env.GetWorkflowError(); err == nil {
		t.Fatal("cancelled workflow unexpectedly succeeded")
	}
	if !cancelled {
		t.Fatal("cancellation finalizer was not called")
	}
}

func TestProcessAnalysisWorkflowHonorsCancellationDuringCompletion(t *testing.T) {
	env := newWorkflowEnvironment(t)
	input := testWorkflowInput()
	cancelled := false

	env.RegisterActivityWithOptions(func(context.Context, WorkflowInput) (app.AnalysisExecutionResult, error) {
		return app.AnalysisExecutionResult{ReportJSONPath: "report.json"}, nil
	}, activity.RegisterOptions{Name: RunAnalysisActivityName})
	env.RegisterActivityWithOptions(func(context.Context, CompleteInput) error {
		return temporal.NewCanceledError("cancelled before finalization")
	}, activity.RegisterOptions{Name: CompleteActivityName})
	env.RegisterActivityWithOptions(func(_ context.Context, got CancelInput) error {
		cancelled = got.JobID == input.JobID
		return nil
	}, activity.RegisterOptions{Name: CancelActivityName})
	env.RegisterActivityWithOptions(func(context.Context, FailureInput) error { return nil }, activity.RegisterOptions{Name: FailActivityName})

	env.ExecuteWorkflow(ProcessAnalysisWorkflow, input)
	if err := env.GetWorkflowError(); err == nil {
		t.Fatal("cancelled workflow unexpectedly succeeded")
	}
	if !cancelled {
		t.Fatal("completion cancellation was not finalized")
	}
}

func TestProcessAnalysisWorkflowForcesOverwriteOnRetriedExecution(t *testing.T) {
	env := newWorkflowEnvironment(t)
	input := testWorkflowInput()
	input.Request.Force = false
	now := time.Now().UTC()
	store := &dispatcherJobStore{jobs: map[string]app.AnalysisJob{
		input.JobID: {
			ID: input.JobID, OwnerID: input.Request.OwnerID, VODLabel: input.Request.VODLabel, RunID: input.Request.RunID,
			Status: app.AnalysisJobQueued, MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
		},
	}}
	executor := &retryForceExecutor{}
	activities := Activities{Executor: executor, Jobs: store}
	env.RegisterActivityWithOptions(activities.RunAnalysis, activity.RegisterOptions{Name: RunAnalysisActivityName})
	env.RegisterActivityWithOptions(activities.Complete, activity.RegisterOptions{Name: CompleteActivityName})
	env.RegisterActivityWithOptions(activities.Fail, activity.RegisterOptions{Name: FailActivityName})
	env.RegisterActivityWithOptions(activities.Cancel, activity.RegisterOptions{Name: CancelActivityName})

	env.ExecuteWorkflow(ProcessAnalysisWorkflow, input)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if executor.attempts != 2 || !executor.retryForced {
		t.Fatalf("executor attempts=%d retryForced=%t", executor.attempts, executor.retryForced)
	}
}

func newWorkflowEnvironment(t *testing.T) *testsuite.TestWorkflowEnvironment {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(ProcessAnalysisWorkflow, workflow.RegisterOptions{Name: WorkflowName})
	return env
}

func registerUnusedFinalizers(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivityWithOptions(func(context.Context, FailureInput) error { return nil }, activity.RegisterOptions{Name: FailActivityName})
	env.RegisterActivityWithOptions(func(context.Context, CancelInput) error { return nil }, activity.RegisterOptions{Name: CancelActivityName})
}

func testWorkflowInput() WorkflowInput {
	return WorkflowInput{
		JobID: "job_123",
		Request: app.AnalysisJobRequest{
			SchemaVersion: app.AnalysisJobRequestSchemaVersion,
			VODLabel:      "vod_123", OwnerID: "user_123", RunID: "run_123",
			FPS: "1", DurationSeconds: 180, ImageQuality: 3,
		},
	}
}

type retryForceExecutor struct {
	attempts    int
	retryForced bool
}

func (e *retryForceExecutor) RunAnalysis(_ context.Context, request app.AnalysisJobRequest, _ app.AnalysisProgressReporter) (app.AnalysisExecutionResult, error) {
	e.attempts++
	if e.attempts == 1 {
		return app.AnalysisExecutionResult{}, errors.New("fail after partially writing artifacts")
	}
	e.retryForced = request.Force
	return app.AnalysisExecutionResult{ReportJSONPath: "report.json", ReportMDPath: "report.md"}, nil
}
