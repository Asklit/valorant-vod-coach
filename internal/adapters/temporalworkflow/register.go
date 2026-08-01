package temporalworkflow

import (
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

func Register(registry worker.Registry, activities Activities) {
	registry.RegisterWorkflowWithOptions(ProcessAnalysisWorkflow, workflow.RegisterOptions{Name: WorkflowName})
	registry.RegisterActivityWithOptions(activities.RunAnalysis, activity.RegisterOptions{Name: RunAnalysisActivityName})
	registry.RegisterActivityWithOptions(activities.Complete, activity.RegisterOptions{Name: CompleteActivityName})
	registry.RegisterActivityWithOptions(activities.Fail, activity.RegisterOptions{Name: FailActivityName})
	registry.RegisterActivityWithOptions(activities.Cancel, activity.RegisterOptions{Name: CancelActivityName})
}
