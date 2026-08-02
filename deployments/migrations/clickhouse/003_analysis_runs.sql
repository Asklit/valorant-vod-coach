CREATE VIEW IF NOT EXISTS analysis_runs AS
SELECT
  event_id,
  aggregate_id AS vod_label,
  correlation_id AS run_id,
  occurred_at,
  ingested_at,
  producer,
  trace_id,
  JSONExtractString(payload, 'status') AS status,
  JSONExtractString(payload, 'analyzer') AS analyzer,
  JSONExtractString(payload, 'mode') AS mode,
  JSONExtractUInt(payload, 'finding_count') AS finding_count,
  JSONExtractUInt(payload, 'artifact_count') AS artifact_count,
  JSONExtractUInt(payload, 'review_window_count') AS review_window_count,
  JSONExtractUInt(payload, 'round_segment_count') AS round_segment_count,
  JSONExtractUInt(payload, 'model_review_run_count') AS model_review_run_count
FROM kafka_events_deduplicated
WHERE event_type = 'ReportReady';
