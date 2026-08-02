package kafka

import (
	"context"
	"fmt"
	"strings"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/asklit/valorant-vod-coach/internal/adapters/postgres"
)

type Producer struct {
	Brokers []string
	Writer  *kafkago.Writer
}

func NewProducer(brokers []string) (*Producer, error) {
	cleaned := make([]string, 0, len(brokers))
	for _, broker := range brokers {
		if value := strings.TrimSpace(broker); value != "" {
			cleaned = append(cleaned, value)
		}
	}
	if len(cleaned) == 0 {
		return nil, fmt.Errorf("at least one Kafka broker is required")
	}
	return &Producer{
		Brokers: cleaned,
		Writer: &kafkago.Writer{
			Addr:         kafkago.TCP(cleaned...),
			Balancer:     &kafkago.Hash{},
			RequiredAcks: kafkago.RequireOne,
			BatchTimeout: 50 * time.Millisecond,
		},
	}, nil
}

func (p *Producer) Close() error {
	if p == nil || p.Writer == nil {
		return nil
	}
	return p.Writer.Close()
}

func (p *Producer) PublishOutboxEvent(ctx context.Context, event postgres.OutboxEvent) error {
	if p == nil || p.Writer == nil {
		return fmt.Errorf("Kafka producer is not configured")
	}
	if strings.TrimSpace(event.Topic) == "" {
		return fmt.Errorf("outbox event %s has no topic", event.ID)
	}
	if len(event.Envelope) == 0 {
		return fmt.Errorf("outbox event %s has empty envelope", event.ID)
	}
	headers := []kafkago.Header{
		{Key: "event_id", Value: []byte(event.ID)},
		{Key: "event_type", Value: []byte(event.EventType)},
		{Key: "correlation_id", Value: []byte(event.CorrelationID)},
	}
	headers = InjectTraceContext(ctx, headers)
	return p.Writer.WriteMessages(ctx, kafkago.Message{
		Topic:   event.Topic,
		Key:     []byte(event.AggregateID),
		Value:   event.Envelope,
		Headers: headers,
		Time:    event.OccurredAt,
	})
}

// InjectTraceContext adds the configured OpenTelemetry propagation headers to a Kafka message.
func InjectTraceContext(ctx context.Context, headers []kafkago.Header) []kafkago.Header {
	carrier := headerCarrier{headers: &headers}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return headers
}

// ExtractTraceContext restores an upstream OpenTelemetry context from Kafka headers.
func ExtractTraceContext(ctx context.Context, headers []kafkago.Header) context.Context {
	carrier := headerCarrier{headers: &headers}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

func ContextFromOutboxEvent(ctx context.Context, event postgres.OutboxEvent) context.Context {
	headers := make([]kafkago.Header, 0, 2)
	if event.TraceParent != "" {
		headers = append(headers, kafkago.Header{Key: "traceparent", Value: []byte(event.TraceParent)})
	}
	if event.TraceState != "" {
		headers = append(headers, kafkago.Header{Key: "tracestate", Value: []byte(event.TraceState)})
	}
	return ExtractTraceContext(ctx, headers)
}

type headerCarrier struct {
	headers *[]kafkago.Header
}

var _ propagation.TextMapCarrier = headerCarrier{}

func (c headerCarrier) Get(key string) string {
	for i := len(*c.headers) - 1; i >= 0; i-- {
		if strings.EqualFold((*c.headers)[i].Key, key) {
			return string((*c.headers)[i].Value)
		}
	}
	return ""
}

func (c headerCarrier) Set(key string, value string) {
	for i := range *c.headers {
		if strings.EqualFold((*c.headers)[i].Key, key) {
			(*c.headers)[i].Key = key
			(*c.headers)[i].Value = []byte(value)
			return
		}
	}
	*c.headers = append(*c.headers, kafkago.Header{Key: key, Value: []byte(value)})
}

func (c headerCarrier) Keys() []string {
	keys := make([]string, 0, len(*c.headers))
	for _, header := range *c.headers {
		keys = append(keys, header.Key)
	}
	return keys
}
