CREATE VIEW IF NOT EXISTS kafka_events_deduplicated AS
SELECT
  event_id,
  argMax(topic, ingested_at) AS topic,
  argMax(event_type, ingested_at) AS event_type,
  argMax(event_version, ingested_at) AS event_version,
  argMax(aggregate_type, ingested_at) AS aggregate_type,
  argMax(aggregate_id, ingested_at) AS aggregate_id,
  argMax(occurred_at, ingested_at) AS occurred_at,
  argMax(producer, ingested_at) AS producer,
  argMax(correlation_id, ingested_at) AS correlation_id,
  argMax(causation_id, ingested_at) AS causation_id,
  argMax(trace_id, ingested_at) AS trace_id,
  argMax(payload, ingested_at) AS payload,
  argMax(envelope, ingested_at) AS envelope,
  max(ingested_at) AS ingested_at
FROM kafka_events
GROUP BY event_id;
