package domain

import (
	"encoding/json"
	"time"
)

const EventEnvelopeVersion = 1

const (
	TopicVODLifecycle  = "vod.lifecycle.v1"
	TopicVODProcessing = "vod.processing.v1"
)

const (
	EventTypeVODProbed       = "VodProbed"
	EventTypeFramesExtracted = "FramesExtracted"
	EventTypeReportReady     = "ReportReady"
)

type EventEnvelope struct {
	EventID       string          `json:"event_id"`
	Topic         string          `json:"-"`
	EventType     string          `json:"event_type"`
	EventVersion  int             `json:"event_version"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Producer      string          `json:"producer"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	TraceID       string          `json:"trace_id,omitempty"`
	CausationID   string          `json:"causation_id,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}
