package postgres

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

func TestProductPersistenceIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || !strings.Contains(strings.ToLower(strings.Trim(parsed.Path, "/")), "test") {
		t.Fatalf("integration tests require a dedicated database with 'test' in its name")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer db.Close()
	if _, err := ApplyMigrations(ctx, db, "../../../deployments/migrations/postgres"); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
TRUNCATE TABLE auth_users, vods, analysis_jobs, user_documents, outbox_events CASCADE
`); err != nil {
		t.Fatalf("reset test database: %v", err)
	}

	now := time.Date(2026, 7, 31, 18, 0, 0, 0, time.UTC)
	store := Store{DB: db, Producer: "integration-test", AuthHashIterations: 4, Clock: func() time.Time { return now }}
	if _, err := store.Register(ctx, app.AuthRegisterRequest{Email: "admin@example.com", Password: "secret-pass"}); err == nil {
		t.Fatal("implicit first administrator must be rejected")
	}
	admin, err := store.Register(ctx, app.AuthRegisterRequest{
		Email: "admin@example.com", Password: "secret-pass", DisplayName: "Admin", BootstrapAdmin: true,
	})
	if err != nil || admin.Role != app.AuthRoleAdmin {
		t.Fatalf("register administrator: %+v / %v", admin, err)
	}
	player, err := store.Register(ctx, app.AuthRegisterRequest{Email: "player@example.com", Password: "secret-pass"})
	if err != nil || player.Role != app.AuthRoleUser {
		t.Fatalf("register player: %+v / %v", player, err)
	}
	authenticated, err := store.Authenticate(ctx, app.AuthLoginRequest{Email: "PLAYER@example.com", Password: "secret-pass"})
	if err != nil || authenticated.ID != player.ID || authenticated.LastLoginAt == nil {
		t.Fatalf("authenticate player: %+v / %v", authenticated, err)
	}

	upload := app.UploadRecord{
		VOD: domain.VOD{
			Label: "upload_integration", VideoID: "upload_integration", Rank: "diamond", Title: "Integration upload",
			Channel: "Uploaded VOD", OwnerID: player.ID, SourceType: "upload", OriginalFilename: "match.mp4", UploadedAt: now,
		},
		VideoPath: "/tmp/upload_integration/video.mp4", VideoFilename: "video.mp4", SizeBytes: 1234,
		Media: domain.MediaSummary{DurationSeconds: 1800, HasDuration: true, Width: 1920, Height: 1080}, UpdatedAt: now,
	}
	if err := store.SaveUpload(ctx, upload); err != nil {
		t.Fatalf("save upload: %v", err)
	}
	if _, found, err := store.FindUpload(ctx, upload.VOD.Label, admin.ID, false); err != nil || found {
		t.Fatalf("another user must not find upload: found=%v err=%v", found, err)
	}
	if foundUpload, found, err := store.FindUpload(ctx, upload.VOD.Label, player.ID, false); err != nil || !found || foundUpload.Media.Width != 1920 {
		t.Fatalf("owner must find upload: %+v found=%v err=%v", foundUpload, found, err)
	}

	vod := domain.VOD{Label: "integration_curated", VideoID: "fixture", Rank: "gold", Title: "Fixture"}
	for _, owner := range []app.PublicAuthUser{admin, player} {
		report := domain.AnalysisReport{
			SchemaVersion: domain.AnalysisReportSchemaVersion, RunID: "same_run", Status: "completed", GeneratedAt: now,
			VOD: vod, Sample: domain.FrameSampleSummary{Name: "full", FPS: "1", FrameCount: 10},
			Gameplay: &domain.GameplaySummary{Analyzer: "integration", SampledFrames: 10, AnalyzedFrames: 10},
			Findings: []domain.Finding{}, Timeline: []domain.TimelineEvent{}, Artifacts: []domain.Artifact{},
			Metadata: domain.AnalysisRunMetadata{Analyzer: "integration", Mode: "full_vod", OwnerID: owner.ID},
		}
		if err := store.SaveAnalysisResult(ctx, app.PersistAnalysisRequest{
			Report: report, Saved: app.SavedReport{JSONPath: "/reports/" + owner.ID + ".json", MarkdownPath: "/reports/" + owner.ID + ".md"},
		}); err != nil {
			t.Fatalf("save report for %s: %v", owner.ID, err)
		}
	}
	for _, owner := range []app.PublicAuthUser{admin, player} {
		record, found, err := store.FindReport(ctx, owner.ID, vod.Label, "same_run", true)
		if err != nil || !found || record.Report.Metadata.OwnerID != owner.ID {
			t.Fatalf("find isolated report for %s: %+v found=%v err=%v", owner.ID, record.Report.Metadata, found, err)
		}
		summaries, err := store.ListReportSummaries(ctx, owner.ID, vod.Label, true)
		if err != nil || len(summaries) != 1 || summaries[0].OwnerID != owner.ID {
			t.Fatalf("list isolated reports for %s: %+v / %v", owner.ID, summaries, err)
		}
	}

	job := app.AnalysisJob{
		ID: "job_integration", OwnerID: player.ID, RunID: "same_run", VODLabel: vod.Label,
		Status: app.AnalysisJobQueued, Request: []byte(`{"fps":"1"}`), MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateAnalysisJob(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	job.Status = app.AnalysisJobRunning
	job.Attempts = 1
	job.StartedAt = &now
	if err := store.UpdateAnalysisJob(ctx, job); err != nil {
		t.Fatalf("update job: %v", err)
	}
	if foundJob, found, err := store.FindAnalysisJob(ctx, job.ID, player.ID, false); err != nil || !found || foundJob.Attempts != 1 {
		t.Fatalf("find job: %+v found=%v err=%v", foundJob, found, err)
	}

	corrections := domain.ManualCorrectionSet{
		SchemaVersion: domain.ManualCorrectionSetSchemaVersion, VODLabel: vod.Label, ReportRunID: "same_run", UpdatedAt: now,
		Corrections: []domain.ManualCorrection{{ID: "correction_1", Type: "event_note", Comment: "integration note", Author: player.Email, CreatedAt: now}},
	}
	if err := store.SaveManualCorrections(ctx, player.ID, corrections); err != nil {
		t.Fatalf("save corrections: %v", err)
	}
	if loaded, found, err := store.LoadManualCorrections(ctx, player.ID, vod.Label, "same_run"); err != nil || !found || len(loaded.Corrections) != 1 {
		t.Fatalf("load corrections: %+v found=%v err=%v", loaded, found, err)
	}
	if _, found, err := store.LoadManualCorrections(ctx, admin.ID, vod.Label, "same_run"); err != nil || found {
		t.Fatalf("another user must not load corrections: found=%v err=%v", found, err)
	}
	guided := domain.GuidedReviewSet{
		SchemaVersion: domain.GuidedReviewSetSchemaVersion, VODLabel: vod.Label, ReportRunID: "same_run", UpdatedAt: now,
		Assessments: []domain.GuidedReviewAssessment{},
	}
	if err := store.SaveGuidedReviews(ctx, player.ID, guided); err != nil {
		t.Fatalf("save guided reviews: %v", err)
	}
	if _, found, err := store.LoadGuidedReviews(ctx, player.ID, vod.Label, "same_run"); err != nil || !found {
		t.Fatalf("load guided reviews: found=%v err=%v", found, err)
	}
}
