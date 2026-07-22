package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type OutboxEvent struct {
	ID            string
	Topic         string
	EventType     string
	EventVersion  int
	AggregateType string
	AggregateID   string
	OccurredAt    time.Time
	Producer      string
	CorrelationID string
	CausationID   string
	TraceID       string
	Payload       json.RawMessage
	Envelope      json.RawMessage
}

func ClaimPendingOutboxEvents(ctx context.Context, db *sql.DB, limit int, workerID string) ([]OutboxEvent, error) {
	if db == nil {
		return nil, fmt.Errorf("DB is required")
	}
	if limit <= 0 {
		limit = 50
	}
	if workerID == "" {
		workerID = "outbox-relay"
	}

	rows, err := db.QueryContext(ctx, `
WITH next_events AS (
  SELECT id
  FROM outbox_events
  WHERE status IN ('pending', 'failed')
    AND (next_attempt_at IS NULL OR next_attempt_at <= now())
  ORDER BY occurred_at, id
  LIMIT $1
  FOR UPDATE SKIP LOCKED
)
UPDATE outbox_events AS outbox
SET
  status = 'publishing',
  locked_at = now(),
  locked_by = $2,
  attempts = attempts + 1,
  updated_at = now()
FROM next_events
WHERE outbox.id = next_events.id
RETURNING
  outbox.id,
  outbox.topic,
  outbox.event_type,
  outbox.event_version,
  outbox.aggregate_type,
  outbox.aggregate_id,
  outbox.occurred_at,
  outbox.producer,
  outbox.correlation_id,
  outbox.causation_id,
  outbox.trace_id,
  outbox.payload,
  outbox.envelope
`, limit, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []OutboxEvent
	for rows.Next() {
		var event OutboxEvent
		if err := rows.Scan(
			&event.ID,
			&event.Topic,
			&event.EventType,
			&event.EventVersion,
			&event.AggregateType,
			&event.AggregateID,
			&event.OccurredAt,
			&event.Producer,
			&event.CorrelationID,
			&event.CausationID,
			&event.TraceID,
			&event.Payload,
			&event.Envelope,
		); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func MarkOutboxPublished(ctx context.Context, db *sql.DB, eventID string) error {
	_, err := db.ExecContext(ctx, `
UPDATE outbox_events
SET
  status = 'published',
  published_at = now(),
  locked_at = NULL,
  locked_by = '',
  last_error = '',
  updated_at = now()
WHERE id = $1
`, eventID)
	return err
}

func MarkOutboxFailed(ctx context.Context, db *sql.DB, eventID string, publishErr error) error {
	message := ""
	if publishErr != nil {
		message = publishErr.Error()
	}
	_, err := db.ExecContext(ctx, `
UPDATE outbox_events
SET
  status = 'failed',
  locked_at = NULL,
  locked_by = '',
  last_error = $2,
  next_attempt_at = now() + (LEAST(attempts, 10) * interval '5 seconds'),
  updated_at = now()
WHERE id = $1
`, eventID, message)
	return err
}
