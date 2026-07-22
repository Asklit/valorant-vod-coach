CREATE TABLE IF NOT EXISTS kafka_events (
  event_id String,
  topic LowCardinality(String),
  event_type LowCardinality(String),
  event_version UInt16,
  aggregate_type LowCardinality(String),
  aggregate_id String,
  occurred_at DateTime64(3, 'UTC'),
  producer LowCardinality(String),
  correlation_id String,
  causation_id String,
  trace_id String,
  payload String,
  envelope String,
  ingested_at DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = MergeTree
ORDER BY (topic, event_type, occurred_at, event_id);
