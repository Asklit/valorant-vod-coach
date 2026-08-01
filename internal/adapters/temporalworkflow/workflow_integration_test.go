package temporalworkflow

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/adapters/postgres"
	"github.com/asklit/valorant-vod-coach/internal/app"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func TestWorkflowPostgresIntegration(t *testing.T) {
	temporalAddress := strings.TrimSpace(os.Getenv("TEST_TEMPORAL_ADDRESS"))
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if temporalAddress == "" || databaseURL == "" {
		t.Skip("TEST_TEMPORAL_ADDRESS and TEST_DATABASE_URL are required")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || !strings.Contains(strings.ToLower(strings.Trim(parsed.Path, "/")), "test") {
		t.Fatalf("integration tests require a dedicated database with 'test' in its name")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	db, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	defer db.Close()
	if _, err := postgres.ApplyMigrations(ctx, db, "../../../deployments/migrations/postgres"); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if _, err := db.ExecContext(ctx, "TRUNCATE TABLE auth_users, vods, analysis_jobs, outbox_events CASCADE"); err != nil {
		t.Fatalf("reset test database: %v", err)
	}
	store := postgres.Store{DB: db, Producer: "temporal-integration", AuthHashIterations: 4}
	owner, err := store.Register(ctx, app.AuthRegisterRequest{
		Email: "temporal@example.com", Password: "secret-pass", BootstrapAdmin: true,
	})
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}

	request := app.AnalysisJobRequest{
		SchemaVersion: app.AnalysisJobRequestSchemaVersion,
		VODLabel:      "integration_vod", OwnerID: owner.ID, RunID: "integration_run",
		FPS: "1", DurationSeconds: 180, ImageQuality: 3,
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	now := time.Now().UTC()
	job := app.AnalysisJob{
		ID:      "temporal_integration_" + now.Format("150405000000000"),
		OwnerID: owner.ID, VODLabel: request.VODLabel, RunID: request.RunID,
		Status: app.AnalysisJobQueued, Stage: "queued", Message: "Queued", Request: raw,
		MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateAnalysisJob(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	temporalClient, err := client.Dial(client.Options{HostPort: temporalAddress})
	if err != nil {
		t.Fatalf("connect to Temporal: %v", err)
	}
	defer temporalClient.Close()
	taskQueue := "integration-" + job.ID
	temporalWorker := worker.New(temporalClient, taskQueue, worker.Options{})
	Register(temporalWorker, Activities{Executor: integrationExecutor{}, Jobs: store})
	if err := temporalWorker.Start(); err != nil {
		t.Fatalf("start worker: %v", err)
	}
	defer temporalWorker.Stop()

	launcher := Launcher{Client: temporalClient, TaskQueue: taskQueue}
	if err := launcher.StartAnalysisWorkflow(ctx, job.ID, request); err != nil {
		t.Fatalf("start workflow: %v", err)
	}
	run := temporalClient.GetWorkflow(ctx, job.ID, "")
	if err := run.Get(ctx, nil); err != nil {
		t.Fatalf("wait for workflow: %v", err)
	}
	persisted, found, err := store.FindAnalysisJob(ctx, job.ID, owner.ID, false)
	if err != nil || !found {
		t.Fatalf("find completed job: found=%t err=%v", found, err)
	}
	if persisted.Status != app.AnalysisJobCompleted || persisted.Stage != "completed" || persisted.ProgressPercent != 100 || persisted.Attempts != 1 {
		t.Fatalf("completed read model = %+v", persisted)
	}
	if persisted.ReportJSONPath != "integration-report.json" || persisted.ReportMDPath != "integration-report.md" {
		t.Fatalf("report paths = %+v", persisted)
	}
}

type integrationExecutor struct{}

func (integrationExecutor) RunAnalysis(ctx context.Context, _ app.AnalysisJobRequest, progress app.AnalysisProgressReporter) (app.AnalysisExecutionResult, error) {
	progress.ReportAnalysisProgress(ctx, app.AnalysisProgress{Stage: "analyzing", Message: "Analyzing", Percent: 55})
	return app.AnalysisExecutionResult{
		ReportJSONPath: "integration-report.json",
		ReportMDPath:   "integration-report.md",
	}, nil
}
