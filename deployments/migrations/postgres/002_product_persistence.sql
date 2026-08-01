CREATE TABLE IF NOT EXISTS auth_users (
  id text PRIMARY KEY,
  email text NOT NULL,
  display_name text NOT NULL,
  role text NOT NULL CHECK (role IN ('admin', 'user')),
  password_hash text NOT NULL,
  created_at timestamptz NOT NULL,
  last_login_at timestamptz,
  disabled_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_auth_users_email_lower
  ON auth_users (lower(email));

ALTER TABLE vods
  ADD COLUMN IF NOT EXISTS owner_id text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS source_type text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS original_filename text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS uploaded_at timestamptz,
  ADD COLUMN IF NOT EXISTS map_name text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS agent text NOT NULL DEFAULT '';

ALTER TABLE analysis_reports
  ADD COLUMN IF NOT EXISTS owner_id text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS report_snapshot jsonb;

ALTER TABLE analysis_reports
  DROP CONSTRAINT IF EXISTS analysis_reports_vod_label_run_id_key;

CREATE UNIQUE INDEX IF NOT EXISTS idx_analysis_reports_owner_vod_run
  ON analysis_reports (owner_id, vod_label, run_id);

CREATE INDEX IF NOT EXISTS idx_analysis_reports_owner_vod_generated
  ON analysis_reports (owner_id, vod_label, generated_at DESC);

ALTER TABLE report_artifacts
  ADD COLUMN IF NOT EXISTS owner_id text NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS uploaded_vods (
  label text PRIMARY KEY REFERENCES vods(label) ON DELETE CASCADE,
  owner_id text NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
  video_path text NOT NULL,
  video_filename text NOT NULL,
  size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
  media jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_uploaded_vods_owner_uploaded
  ON uploaded_vods (owner_id, created_at DESC, label);

CREATE TABLE IF NOT EXISTS analysis_jobs (
  id text PRIMARY KEY,
  owner_id text NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
  run_id text NOT NULL,
  vod_label text NOT NULL,
  status text NOT NULL CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled')),
  message text NOT NULL DEFAULT '',
  error text NOT NULL DEFAULT '',
  request jsonb NOT NULL DEFAULT '{}'::jsonb,
  report_json_path text NOT NULL DEFAULT '',
  report_markdown_path text NOT NULL DEFAULT '',
  attempts integer NOT NULL DEFAULT 0,
  max_attempts integer NOT NULL DEFAULT 3,
  lease_owner text NOT NULL DEFAULT '',
  lease_expires_at timestamptz,
  cancellation_requested boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL,
  started_at timestamptz,
  finished_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_analysis_jobs_owner_created
  ON analysis_jobs (owner_id, created_at DESC, id);

CREATE INDEX IF NOT EXISTS idx_analysis_jobs_claim
  ON analysis_jobs (status, lease_expires_at, created_at)
  WHERE status IN ('queued', 'running');

CREATE TABLE IF NOT EXISTS user_documents (
  owner_id text NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
  kind text NOT NULL,
  vod_label text NOT NULL,
  report_run_id text NOT NULL DEFAULT '',
  schema_version integer NOT NULL DEFAULT 1,
  body jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (owner_id, kind, vod_label, report_run_id)
);
