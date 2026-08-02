ALTER TABLE outbox_events
  ADD COLUMN IF NOT EXISTS trace_parent text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS trace_state text NOT NULL DEFAULT '';
