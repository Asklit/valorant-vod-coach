package kafka

import (
	"context"
	"fmt"
	"strings"
	"time"

	kafkago "github.com/segmentio/kafka-go"

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
	return p.Writer.WriteMessages(ctx, kafkago.Message{
		Topic: event.Topic,
		Key:   []byte(event.AggregateID),
		Value: event.Envelope,
		Headers: []kafkago.Header{
			{Key: "event_id", Value: []byte(event.ID)},
			{Key: "event_type", Value: []byte(event.EventType)},
			{Key: "correlation_id", Value: []byte(event.CorrelationID)},
		},
		Time: event.OccurredAt,
	})
}
