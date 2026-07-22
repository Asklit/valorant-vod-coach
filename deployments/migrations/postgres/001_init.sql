CREATE TABLE IF NOT EXISTS schema_migrations (
  version integer PRIMARY KEY,
  name text NOT NULL,
  applied_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS vods (
  label text PRIMARY KEY,
  video_id text NOT NULL DEFAULT '',
  rank text NOT NULL DEFAULT '',
  source_url text NOT NULL DEFAULT '',
  title text NOT NULL DEFAULT '',
  channel text NOT NULL DEFAULT '',
  manifest_duration_seconds double precision NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS analysis_reports (
  id text PRIMARY KEY,
  vod_label text NOT NULL REFERENCES vods(label) ON DELETE CASCADE,
  run_id text NOT NULL,
  status text NOT NULL,
  generated_at timestamptz NOT NULL,
  schema_version integer NOT NULL,
  analyzer text NOT NULL DEFAULT '',
  mode text NOT NULL DEFAULT '',
  media jsonb NOT NULL,
  sample jsonb NOT NULL,
  gameplay jsonb,
  findings jsonb NOT NULL,
  timeline jsonb NOT NULL,
  artifacts jsonb NOT NULL,
  report_json_path text NOT NULL,
  report_markdown_path text NOT NULL,
  frame_count integer NOT NULL DEFAULT 0,
  finding_count integer NOT NULL DEFAULT 0,
  review_window_count integer NOT NULL DEFAULT 0,
  round_segment_count integer NOT NULL DEFAULT 0,
  model_review_run_count integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (vod_label, run_id)
);

CREATE INDEX IF NOT EXISTS idx_analysis_reports_vod_generated
  ON analysis_reports (vod_label, generated_at DESC);

CREATE TABLE IF NOT EXISTS report_artifacts (
  id text PRIMARY KEY,
  report_id text NOT NULL REFERENCES analysis_reports(id) ON DELETE CASCADE,
  vod_label text NOT NULL REFERENCES vods(label) ON DELETE CASCADE,
  run_id text NOT NULL,
  artifact_type text NOT NULL,
  format text NOT NULL DEFAULT '',
  path text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (report_id, artifact_type, path)
);

CREATE INDEX IF NOT EXISTS idx_report_artifacts_report
  ON report_artifacts (report_id);

CREATE TABLE IF NOT EXISTS outbox_events (
  id text PRIMARY KEY,
  topic text NOT NULL,
  event_type text NOT NULL,
  event_version integer NOT NULL,
  aggregate_type text NOT NULL,
  aggregate_id text NOT NULL,
  occurred_at timestamptz NOT NULL,
  producer text NOT NULL,
  correlation_id text NOT NULL DEFAULT '',
  causation_id text NOT NULL DEFAULT '',
  trace_id text NOT NULL DEFAULT '',
  payload jsonb NOT NULL,
  envelope jsonb NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  attempts integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz,
  locked_at timestamptz,
  locked_by text NOT NULL DEFAULT '',
  published_at timestamptz,
  last_error text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_outbox_events_pending
  ON outbox_events (status, next_attempt_at, occurred_at);

CREATE INDEX IF NOT EXISTS idx_outbox_events_aggregate
  ON outbox_events (aggregate_type, aggregate_id, occurred_at);
