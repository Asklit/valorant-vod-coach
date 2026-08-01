package temporalworkflow

import (
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	DefaultTaskQueue         = "valorant-vod-analysis"
	WorkflowName             = "ProcessValorantVOD"
	RunAnalysisActivityName  = "RunValorantVODAnalysis"
	CompleteActivityName     = "CompleteValorantVODAnalysisJob"
	FailActivityName         = "FailValorantVODAnalysisJob"
	CancelActivityName       = "CancelValorantVODAnalysisJob"
	activityExecutionTimeout = 55 * time.Minute
)

type WorkflowInput struct {
	JobID   string                 `json:"job_id"`
	Request app.AnalysisJobRequest `json:"request"`
}

type CompleteInput struct {
	JobID  string                      `json:"job_id"`
	Result app.AnalysisExecutionResult `json:"result"`
}

type FailureInput struct {
	JobID string `json:"job_id"`
	Error string `json:"error"`
}

type CancelInput struct {
	JobID string `json:"job_id"`
}

func ProcessAnalysisWorkflow(ctx workflow.Context, input WorkflowInput) error {
	runOptions := workflow.ActivityOptions{
		StartToCloseTimeout: activityExecutionTimeout,
		HeartbeatTimeout:    30 * time.Second,
		WaitForCancellation: true,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    2 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	runCtx := workflow.WithActivityOptions(ctx, runOptions)

	var result app.AnalysisExecutionResult
	err := workflow.ExecuteActivity(runCtx, RunAnalysisActivityName, input).Get(runCtx, &result)
	if err == nil {
		finalizeCtx := workflow.WithActivityOptions(ctx, finalizeActivityOptions())
		finalizeErr := workflow.ExecuteActivity(finalizeCtx, CompleteActivityName, CompleteInput{
			JobID:  input.JobID,
			Result: result,
		}).Get(finalizeCtx, nil)
		if temporal.IsCanceledError(finalizeErr) {
			return finalizeCancellation(ctx, input.JobID, finalizeErr)
		}
		return finalizeErr
	}

	if temporal.IsCanceledError(err) {
		return finalizeCancellation(ctx, input.JobID, err)
	}
	disconnectedCtx, _ := workflow.NewDisconnectedContext(ctx)
	disconnectedCtx = workflow.WithActivityOptions(disconnectedCtx, finalizeActivityOptions())
	_ = workflow.ExecuteActivity(disconnectedCtx, FailActivityName, FailureInput{
		JobID: input.JobID,
		Error: err.Error(),
	}).Get(disconnectedCtx, nil)
	return err
}

func finalizeCancellation(ctx workflow.Context, jobID string, cancellationErr error) error {
	disconnectedCtx, _ := workflow.NewDisconnectedContext(ctx)
	disconnectedCtx = workflow.WithActivityOptions(disconnectedCtx, finalizeActivityOptions())
	_ = workflow.ExecuteActivity(disconnectedCtx, CancelActivityName, CancelInput{JobID: jobID}).Get(disconnectedCtx, nil)
	return cancellationErr
}

func finalizeActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    5 * time.Second,
			MaximumAttempts:    5,
		},
	}
}
