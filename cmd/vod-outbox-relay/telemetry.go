package main

import (
	"context"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/adapters/postgres"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type relayTelemetry struct {
	tracer          trace.Tracer
	batches         metric.Int64Counter
	claimed         metric.Int64Counter
	published       metric.Int64Counter
	publishDuration metric.Float64Histogram
	batchDuration   metric.Float64Histogram
}

func newRelayTelemetry(tracer trace.Tracer, meter metric.Meter) (*relayTelemetry, error) {
	batches, err := meter.Int64Counter("vodcoach.outbox.batches", metric.WithDescription("Outbox polling batches"))
	if err != nil {
		return nil, err
	}
	claimed, err := meter.Int64Counter("vodcoach.outbox.claimed", metric.WithDescription("Claimed outbox events"))
	if err != nil {
		return nil, err
	}
	published, err := meter.Int64Counter("vodcoach.outbox.publish", metric.WithDescription("Outbox publish outcomes"))
	if err != nil {
		return nil, err
	}
	publishDuration, err := meter.Float64Histogram("vodcoach.outbox.publish.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	batchDuration, err := meter.Float64Histogram("vodcoach.outbox.batch.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	return &relayTelemetry{
		tracer: tracer, batches: batches, claimed: claimed, published: published,
		publishDuration: publishDuration, batchDuration: batchDuration,
	}, nil
}

func (t *relayTelemetry) startBatch(ctx context.Context) (context.Context, time.Time) {
	ctx, _ = t.tracer.Start(ctx, "outbox.claim", trace.WithSpanKind(trace.SpanKindProducer))
	return ctx, time.Now()
}

func (t *relayTelemetry) finishBatch(ctx context.Context, processed int, err error, elapsed time.Duration) {
	outcome := "success"
	span := trace.SpanFromContext(ctx)
	if err != nil {
		outcome = "failed"
		span.RecordError(err)
		span.SetStatus(codes.Error, "outbox batch failed")
	}
	attributes := metric.WithAttributes(attribute.String("outcome", outcome))
	t.batches.Add(ctx, 1, attributes)
	t.claimed.Add(ctx, int64(processed))
	t.batchDuration.Record(ctx, elapsed.Seconds(), attributes)
	span.SetAttributes(attribute.Int("outbox.claimed", processed))
	span.End()
}

func (t *relayTelemetry) startPublish(ctx context.Context, event postgres.OutboxEvent) context.Context {
	ctx, _ = t.tracer.Start(ctx, "kafka.publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(eventAttributes(event)...),
	)
	return ctx
}

func (t *relayTelemetry) finishPublish(ctx context.Context, event postgres.OutboxEvent, err error, elapsed time.Duration) {
	outcome := "success"
	span := trace.SpanFromContext(ctx)
	if err != nil {
		outcome = "failed"
		span.RecordError(err)
		span.SetStatus(codes.Error, "Kafka publish failed")
	}
	attributes := append(eventAttributes(event), attribute.String("outcome", outcome))
	options := metric.WithAttributes(attributes...)
	t.published.Add(ctx, 1, options)
	t.publishDuration.Record(ctx, elapsed.Seconds(), options)
	span.End()
}

func eventAttributes(event postgres.OutboxEvent) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("messaging.destination.name", event.Topic),
		attribute.String("event.type", event.EventType),
	}
}
