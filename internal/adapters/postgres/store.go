package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

type Store struct {
	DB                 *sql.DB
	Producer           string
	Clock              func() time.Time
	NewID              app.EventIDGenerator
	AuthHashIterations int
}

func (s Store) Ping(ctx context.Context) error {
	if s.DB == nil {
		return errors.New("postgres store requires DB")
	}
	return s.DB.PingContext(ctx)
}

func (s Store) SaveAnalysisResult(ctx context.Context, request app.PersistAnalysisRequest) error {
	if s.DB == nil {
		return fmt.Errorf("postgres store requires DB")
	}
	if strings.TrimSpace(request.Report.VOD.Label) == "" {
		return fmt.Errorf("report VOD label is required")
	}
	if strings.TrimSpace(request.Report.RunID) == "" {
		return fmt.Errorf("report run ID is required")
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := upsertVOD(ctx, tx, request.Report.VOD); err != nil {
		return err
	}
	reportID := analysisReportID(request.Report.Metadata.OwnerID, request.Report.VOD.Label, request.Report.RunID)
	if err := upsertAnalysisReport(ctx, tx, reportID, request); err != nil {
		return err
	}
	if err := replaceReportArtifacts(ctx, tx, reportID, request.Report); err != nil {
		return err
	}
	if err := insertAnalysisEvents(ctx, tx, request, s.now(), s.producer(), s.eventID); err != nil {
		return err
	}

	return tx.Commit()
}

func (s Store) ListReportSummaries(ctx context.Context, ownerID string, vodLabel string, includeSystem bool) ([]app.ReportCatalogSummary, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("postgres store requires DB")
	}
	vodLabel = strings.TrimSpace(vodLabel)
	if vodLabel == "" {
		return nil, fmt.Errorf("vod label is required")
	}

	rows, err := s.DB.QueryContext(ctx, `
SELECT
  owner_id,
  vod_label,
  run_id,
  status,
  generated_at,
  schema_version,
  analyzer,
  mode,
  frame_count,
  finding_count,
  review_window_count,
  round_segment_count,
  COALESCE(NULLIF(gameplay->>'model_review_task_count', '')::integer, 0) AS model_review_task_count,
  model_review_run_count,
  COALESCE(sample->>'name', '') AS sample_name,
  COALESCE(sample->>'fps', '') AS sample_fps,
  COALESCE(NULLIF(sample->>'duration_seconds', '')::double precision, 0) AS sample_duration_seconds,
  COALESCE(sample->>'contact_sheet_path', '') AS contact_sheet_path,
  report_json_path,
  report_markdown_path
FROM analysis_reports
WHERE vod_label = $1
  AND (owner_id = $2 OR ($3 AND owner_id = ''))
ORDER BY (owner_id = $2) DESC, generated_at DESC, run_id DESC
`, vodLabel, strings.TrimSpace(ownerID), includeSystem)
	if err != nil {
		return nil, fmt.Errorf("list report summaries: %w", err)
	}
	defer rows.Close()

	var summaries []app.ReportCatalogSummary
	for rows.Next() {
		var summary app.ReportCatalogSummary
		if err := rows.Scan(
			&summary.OwnerID,
			&summary.VODLabel,
			&summary.RunID,
			&summary.Status,
			&summary.GeneratedAt,
			&summary.SchemaVersion,
			&summary.Analyzer,
			&summary.Mode,
			&summary.FrameCount,
			&summary.FindingCount,
			&summary.ReviewWindowCount,
			&summary.RoundSegmentCount,
			&summary.ModelReviewTaskCount,
			&summary.ModelReviewRunCount,
			&summary.SampleName,
			&summary.SampleFPS,
			&summary.SampleDuration,
			&summary.ContactSheetPath,
			&summary.JSONPath,
			&summary.MarkdownPath,
		); err != nil {
			return nil, fmt.Errorf("scan report summary: %w", err)
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate report summaries: %w", err)
	}
	return summaries, nil
}

func (s Store) FindReport(ctx context.Context, ownerID string, vodLabel string, runID string, includeSystem bool) (app.ReportCatalogRecord, bool, error) {
	if s.DB == nil {
		return app.ReportCatalogRecord{}, false, fmt.Errorf("postgres store requires DB")
	}
	ownerID = strings.TrimSpace(ownerID)
	vodLabel = strings.TrimSpace(vodLabel)
	runID = strings.TrimSpace(runID)
	if vodLabel == "" {
		return app.ReportCatalogRecord{}, false, fmt.Errorf("vod label is required")
	}
	var snapshot []byte
	var record app.ReportCatalogRecord
	err := s.DB.QueryRowContext(ctx, `
SELECT report_snapshot, report_json_path, report_markdown_path
FROM analysis_reports
WHERE vod_label = $1
  AND (owner_id = $2 OR ($4 AND owner_id = ''))
  AND ($3 = '' OR run_id = $3)
ORDER BY (owner_id = $2) DESC, generated_at DESC, run_id DESC
LIMIT 1
`, vodLabel, ownerID, runID, includeSystem).Scan(&snapshot, &record.JSONPath, &record.MarkdownPath)
	if errors.Is(err, sql.ErrNoRows) {
		return app.ReportCatalogRecord{}, false, nil
	}
	if err != nil {
		return app.ReportCatalogRecord{}, false, fmt.Errorf("find report: %w", err)
	}
	if len(snapshot) > 0 {
		if err := json.Unmarshal(snapshot, &record.Report); err != nil {
			return app.ReportCatalogRecord{}, false, fmt.Errorf("decode report snapshot: %w", err)
		}
	}
	return record, true, nil
}

func upsertVOD(ctx context.Context, tx *sql.Tx, vod domain.VOD) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO vods (
  label, video_id, rank, source_url, title, channel, manifest_duration_seconds,
  owner_id, source_type, original_filename, uploaded_at, map_name, agent, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, now())
ON CONFLICT (label) DO UPDATE SET
  video_id = EXCLUDED.video_id,
  rank = EXCLUDED.rank,
  source_url = EXCLUDED.source_url,
  title = EXCLUDED.title,
  channel = EXCLUDED.channel,
  manifest_duration_seconds = EXCLUDED.manifest_duration_seconds,
  owner_id = EXCLUDED.owner_id,
  source_type = EXCLUDED.source_type,
  original_filename = EXCLUDED.original_filename,
  uploaded_at = EXCLUDED.uploaded_at,
  map_name = EXCLUDED.map_name,
  agent = EXCLUDED.agent,
  updated_at = now()
`,
		vod.Label,
		vod.VideoID,
		string(vod.Rank),
		vod.SourceURL,
		vod.Title,
		vod.Channel,
		vod.ManifestDurationSeconds,
		vod.OwnerID,
		vod.SourceType,
		vod.OriginalFilename,
		nullableTime(vod.UploadedAt),
		vod.Map,
		vod.Agent,
	)
	if err != nil {
		return fmt.Errorf("upsert vod: %w", err)
	}
	return nil
}

func upsertAnalysisReport(ctx context.Context, tx *sql.Tx, reportID string, request app.PersistAnalysisRequest) error {
	report := request.Report
	media, err := jsonString(report.Media)
	if err != nil {
		return err
	}
	sample, err := jsonString(report.Sample)
	if err != nil {
		return err
	}
	gameplay, err := nullableJSONString(report.Gameplay)
	if err != nil {
		return err
	}
	findings, err := jsonString(report.Findings)
	if err != nil {
		return err
	}
	timeline, err := jsonString(report.Timeline)
	if err != nil {
		return err
	}
	artifacts, err := jsonString(report.Artifacts)
	if err != nil {
		return err
	}
	reportSnapshot, err := jsonString(report)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO analysis_reports (
  id, owner_id, vod_label, run_id, status, generated_at, schema_version, analyzer, mode,
  media, sample, gameplay, findings, timeline, artifacts, report_snapshot,
  report_json_path, report_markdown_path,
  frame_count, finding_count, review_window_count, round_segment_count, model_review_run_count, updated_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9,
  $10::jsonb, $11::jsonb, $12::jsonb, $13::jsonb, $14::jsonb, $15::jsonb, $16::jsonb,
  $17, $18,
  $19, $20, $21, $22, $23, now()
)
ON CONFLICT (owner_id, vod_label, run_id) DO UPDATE SET
  status = EXCLUDED.status,
  generated_at = EXCLUDED.generated_at,
  schema_version = EXCLUDED.schema_version,
  analyzer = EXCLUDED.analyzer,
  mode = EXCLUDED.mode,
  media = EXCLUDED.media,
  sample = EXCLUDED.sample,
  gameplay = EXCLUDED.gameplay,
  findings = EXCLUDED.findings,
  timeline = EXCLUDED.timeline,
  artifacts = EXCLUDED.artifacts,
  report_snapshot = EXCLUDED.report_snapshot,
  report_json_path = EXCLUDED.report_json_path,
  report_markdown_path = EXCLUDED.report_markdown_path,
  frame_count = EXCLUDED.frame_count,
  finding_count = EXCLUDED.finding_count,
  review_window_count = EXCLUDED.review_window_count,
  round_segment_count = EXCLUDED.round_segment_count,
  model_review_run_count = EXCLUDED.model_review_run_count,
  updated_at = now()
`,
		reportID,
		report.Metadata.OwnerID,
		report.VOD.Label,
		report.RunID,
		report.Status,
		report.GeneratedAt,
		report.SchemaVersion,
		report.Metadata.Analyzer,
		report.Metadata.Mode,
		media,
		sample,
		gameplay,
		findings,
		timeline,
		artifacts,
		reportSnapshot,
		request.Saved.JSONPath,
		request.Saved.MarkdownPath,
		report.Sample.FrameCount,
		len(report.Findings),
		reviewWindowCount(report.Gameplay),
		roundSegmentCount(report.Gameplay),
		modelReviewRunCount(report.Gameplay),
	)
	if err != nil {
		return fmt.Errorf("upsert analysis report: %w", err)
	}
	return nil
}

func replaceReportArtifacts(ctx context.Context, tx *sql.Tx, reportID string, report domain.AnalysisReport) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM report_artifacts WHERE report_id = $1", reportID); err != nil {
		return fmt.Errorf("delete report artifacts: %w", err)
	}
	for _, artifact := range report.Artifacts {
		if strings.TrimSpace(artifact.Path) == "" {
			continue
		}
		_, err := tx.ExecContext(ctx, `
INSERT INTO report_artifacts (
  id, report_id, owner_id, vod_label, run_id, artifact_type, format, path
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (report_id, artifact_type, path) DO UPDATE SET
  format = EXCLUDED.format
`,
			stableID("artifact", reportID, artifact.Type, artifact.Path),
			reportID,
			report.Metadata.OwnerID,
			report.VOD.Label,
			report.RunID,
			artifact.Type,
			artifact.Format,
			artifact.Path,
		)
		if err != nil {
			return fmt.Errorf("insert report artifact: %w", err)
		}
	}
	return nil
}

func insertAnalysisEvents(ctx context.Context, tx *sql.Tx, request app.PersistAnalysisRequest, occurredAt time.Time, producer string, newID app.EventIDGenerator) error {
	events, err := app.BuildAnalysisEvents(request, occurredAt, producer, newID)
	if err != nil {
		return err
	}
	for _, event := range events {
		envelope, err := json.Marshal(event)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO outbox_events (
  id, topic, event_type, event_version, aggregate_type, aggregate_id,
  occurred_at, producer, correlation_id, causation_id, trace_id, payload, envelope
) VALUES (
  $1, $2, $3, $4, $5, $6,
  $7, $8, $9, $10, $11, $12::jsonb, $13::jsonb
)
ON CONFLICT (id) DO NOTHING
`,
			event.EventID,
			event.Topic,
			event.EventType,
			event.EventVersion,
			event.AggregateType,
			event.AggregateID,
			event.OccurredAt,
			event.Producer,
			event.CorrelationID,
			event.CausationID,
			event.TraceID,
			string(event.Payload),
			string(envelope),
		)
		if err != nil {
			return fmt.Errorf("insert outbox event %s: %w", event.EventType, err)
		}
	}
	return nil
}

func (s Store) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func (s Store) producer() string {
	if strings.TrimSpace(s.Producer) != "" {
		return s.Producer
	}
	return "vodcoach-go"
}

func (s Store) eventID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	return randomEventID()
}

func jsonString(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func nullableJSONString(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	return jsonString(value)
}

func analysisReportID(ownerID string, vodLabel string, runID string) string {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return vodLabel + ":" + runID
	}
	return ownerID + ":" + vodLabel + ":" + runID
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func stableID(parts ...string) string {
	hash := sha1.New()
	for _, part := range parts {
		hash.Write([]byte(part))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func randomEventID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("event_%d", time.Now().UnixNano())
	}
	return "event_" + hex.EncodeToString(raw[:])
}

func reviewWindowCount(gameplay *domain.GameplaySummary) int {
	if gameplay == nil {
		return 0
	}
	return gameplay.ReviewWindowCount
}

func roundSegmentCount(gameplay *domain.GameplaySummary) int {
	if gameplay == nil {
		return 0
	}
	if gameplay.RoundSegmentCount == 0 && len(gameplay.RoundSegments) > 0 {
		return len(gameplay.RoundSegments)
	}
	return gameplay.RoundSegmentCount
}

func modelReviewRunCount(gameplay *domain.GameplaySummary) int {
	if gameplay == nil {
		return 0
	}
	if gameplay.ModelReviewRunCount == 0 && len(gameplay.ModelReviewRuns) > 0 {
		return len(gameplay.ModelReviewRuns)
	}
	return gameplay.ModelReviewRunCount
}
