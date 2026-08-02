CREATE VIEW IF NOT EXISTS frame_extractions AS
SELECT
  event_id,
  aggregate_id AS vod_label,
  correlation_id AS run_id,
  occurred_at,
  ingested_at,
  JSONExtractString(payload, 'sample_name') AS sample_name,
  JSONExtractString(payload, 'fps') AS fps,
  JSONExtractFloat(payload, 'fps_value') AS fps_value,
  JSONExtractFloat(payload, 'start_seconds') AS start_seconds,
  JSONExtractFloat(payload, 'duration_seconds') AS duration_seconds,
  JSONExtractUInt(payload, 'frame_count') AS frame_count
FROM kafka_events_deduplicated
WHERE event_type = 'FramesExtracted';
