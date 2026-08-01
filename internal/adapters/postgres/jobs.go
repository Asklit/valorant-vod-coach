package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/asklit/valorant-vod-coach/internal/app"
)

func (s Store) CreateAnalysisJob(ctx context.Context, job app.AnalysisJob) error {
	if s.DB == nil {
		return errors.New("postgres store requires DB")
	}
	if err := validateAnalysisJob(job); err != nil {
		return err
	}
	request := job.Request
	if len(request) == 0 {
		request = json.RawMessage(`{}`)
	}
	maxAttempts := job.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO analysis_jobs (
  id, owner_id, run_id, vod_label, status, message, error, request,
  report_json_path, report_markdown_path, attempts, max_attempts,
  lease_owner, lease_expires_at, cancellation_requested,
  created_at, started_at, finished_at, updated_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8::jsonb,
  $9, $10, $11, $12,
  $13, $14, $15,
  $16, $17, $18, now()
)
`,
		job.ID,
		job.OwnerID,
		job.RunID,
		job.VODLabel,
		job.Status,
		job.Message,
		job.Error,
		string(request),
		job.ReportJSONPath,
		job.ReportMDPath,
		job.Attempts,
		maxAttempts,
		job.LeaseOwner,
		job.LeaseExpiresAt,
		job.CancellationRequested,
		job.CreatedAt.UTC(),
		job.StartedAt,
		job.FinishedAt,
	)
	if err != nil {
		return fmt.Errorf("insert analysis job: %w", err)
	}
	return nil
}

func (s Store) UpdateAnalysisJob(ctx context.Context, job app.AnalysisJob) error {
	if s.DB == nil {
		return errors.New("postgres store requires DB")
	}
	if err := validateAnalysisJob(job); err != nil {
		return err
	}
	result, err := s.DB.ExecContext(ctx, `
UPDATE analysis_jobs SET
  status = $3,
  message = $4,
  error = $5,
  report_json_path = $6,
  report_markdown_path = $7,
  attempts = $8,
  max_attempts = $9,
  lease_owner = $10,
  lease_expires_at = $11,
  cancellation_requested = $12,
  started_at = $13,
  finished_at = $14,
  updated_at = now()
WHERE id = $1 AND owner_id = $2
`,
		job.ID,
		job.OwnerID,
		job.Status,
		job.Message,
		job.Error,
		job.ReportJSONPath,
		job.ReportMDPath,
		job.Attempts,
		job.MaxAttempts,
		job.LeaseOwner,
		job.LeaseExpiresAt,
		job.CancellationRequested,
		job.StartedAt,
		job.FinishedAt,
	)
	if err != nil {
		return fmt.Errorf("update analysis job: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated == 0 {
		return errors.New("analysis job not found")
	}
	return nil
}

func (s Store) FindAnalysisJob(ctx context.Context, id string, ownerID string, includeAll bool) (app.AnalysisJob, bool, error) {
	if s.DB == nil {
		return app.AnalysisJob{}, false, errors.New("postgres store requires DB")
	}
	row := s.DB.QueryRowContext(ctx, `
SELECT
  id, owner_id, run_id, vod_label, status, message, error, request,
  report_json_path, report_markdown_path, attempts, max_attempts,
  lease_owner, lease_expires_at, cancellation_requested,
  created_at, started_at, finished_at, updated_at
FROM analysis_jobs
WHERE id = $1 AND ($3 OR owner_id = $2)
`, strings.TrimSpace(id), strings.TrimSpace(ownerID), includeAll)
	job, err := scanAnalysisJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return app.AnalysisJob{}, false, nil
	}
	if err != nil {
		return app.AnalysisJob{}, false, fmt.Errorf("find analysis job: %w", err)
	}
	return job, true, nil
}

func (s Store) CountAnalysisJobs(ctx context.Context) (map[string]int, error) {
	if s.DB == nil {
		return nil, errors.New("postgres store requires DB")
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT status, count(*) FROM analysis_jobs GROUP BY status")
	if err != nil {
		return nil, fmt.Errorf("count analysis jobs: %w", err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

func validateAnalysisJob(job app.AnalysisJob) error {
	if strings.TrimSpace(job.ID) == "" || strings.TrimSpace(job.OwnerID) == "" || strings.TrimSpace(job.VODLabel) == "" {
		return errors.New("analysis job ID, owner ID, and VOD label are required")
	}
	switch job.Status {
	case app.AnalysisJobQueued, app.AnalysisJobRunning, app.AnalysisJobCompleted, app.AnalysisJobFailed, "cancelled":
	default:
		return fmt.Errorf("invalid analysis job status %q", job.Status)
	}
	if job.CreatedAt.IsZero() {
		return errors.New("analysis job creation time is required")
	}
	if len(job.Request) > 0 && !json.Valid(job.Request) {
		return errors.New("analysis job request must be valid JSON")
	}
	return nil
}

func scanAnalysisJob(scanner rowScanner) (app.AnalysisJob, error) {
	var job app.AnalysisJob
	err := scanner.Scan(
		&job.ID,
		&job.OwnerID,
		&job.RunID,
		&job.VODLabel,
		&job.Status,
		&job.Message,
		&job.Error,
		&job.Request,
		&job.ReportJSONPath,
		&job.ReportMDPath,
		&job.Attempts,
		&job.MaxAttempts,
		&job.LeaseOwner,
		&job.LeaseExpiresAt,
		&job.CancellationRequested,
		&job.CreatedAt,
		&job.StartedAt,
		&job.FinishedAt,
		&job.UpdatedAt,
	)
	return job, err
}

var _ app.AnalysisJobStore = Store{}
