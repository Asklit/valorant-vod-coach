package temporalworkflow

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/asklit/valorant-vod-coach/internal/app"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
)

type Launcher struct {
	Client    client.Client
	TaskQueue string
}

func (l Launcher) StartAnalysisWorkflow(ctx context.Context, jobID string, request app.AnalysisJobRequest) error {
	if l.Client == nil {
		return errors.New("Temporal client is required")
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return errors.New("analysis job ID is required")
	}
	ctx = RestoreRequestTraceContext(ctx, request)
	_, err := l.Client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       jobID,
		TaskQueue:                defaultString(l.TaskQueue, DefaultTaskQueue),
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowIDReusePolicy:    enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	}, WorkflowName, WorkflowInput{JobID: jobID, Request: request})
	if err != nil {
		return fmt.Errorf("start Temporal analysis workflow: %w", err)
	}
	return nil
}

func (l Launcher) CancelAnalysisWorkflow(ctx context.Context, jobID string) error {
	if l.Client == nil {
		return errors.New("Temporal client is required")
	}
	if err := l.Client.CancelWorkflow(ctx, strings.TrimSpace(jobID), ""); err != nil {
		return fmt.Errorf("cancel Temporal analysis workflow: %w", err)
	}
	return nil
}

type HealthChecker struct {
	Client client.Client
}

func (h HealthChecker) Ping(ctx context.Context) error {
	if h.Client == nil {
		return errors.New("Temporal client is required")
	}
	_, err := h.Client.CheckHealth(ctx, &client.CheckHealthRequest{})
	return err
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

var _ app.AnalysisWorkflowLauncher = Launcher{}
var _ app.HealthChecker = HealthChecker{}
