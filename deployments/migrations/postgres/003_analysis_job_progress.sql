ALTER TABLE analysis_jobs
  ADD COLUMN IF NOT EXISTS stage text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS progress_percent smallint NOT NULL DEFAULT 0;

ALTER TABLE analysis_jobs
  DROP CONSTRAINT IF EXISTS analysis_jobs_progress_percent_check;

ALTER TABLE analysis_jobs
  ADD CONSTRAINT analysis_jobs_progress_percent_check
  CHECK (progress_percent >= 0 AND progress_percent <= 100);
