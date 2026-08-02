package main

import (
	"context"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type sinkTelemetry struct {
	tracer        trace.Tracer
	messages      metric.Int64Counter
	fetchFailures metric.Int64Counter
	duration      metric.Float64Histogram
	consumerLag   metric.Int64Histogram
}

func newSinkTelemetry(tracer trace.Tracer, meter metric.Meter) (*sinkTelemetry, error) {
	messages, err := meter.Int64Counter("vodcoach.clickhouse_sink.messages", metric.WithDescription("ClickHouse sink message outcomes"))
	if err != nil {
		return nil, err
	}
	fetchFailures, err := meter.Int64Counter("vodcoach.clickhouse_sink.fetch.failures")
	if err != nil {
		return nil, err
	}
	duration, err := meter.Float64Histogram("vodcoach.clickhouse_sink.processing.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	consumerLag, err := meter.Int64Histogram("vodcoach.clickhouse_sink.consumer.lag", metric.WithUnit("{message}"))
	if err != nil {
		return nil, err
	}
	return &sinkTelemetry{tracer: tracer, messages: messages, fetchFailures: fetchFailures, duration: duration, consumerLag: consumerLag}, nil
}

func (t *sinkTelemetry) fetchFailed(ctx context.Context) {
	t.fetchFailures.Add(ctx, 1)
}

func (t *sinkTelemetry) startMessage(ctx context.Context, message kafkago.Message) (context.Context, time.Time) {
	attributes := messageAttributes(message)
	ctx, _ = t.tracer.Start(ctx, "clickhouse.consume",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attributes...),
	)
	lag := message.HighWaterMark - message.Offset - 1
	if lag < 0 {
		lag = 0
	}
	t.consumerLag.Record(ctx, lag, metric.WithAttributes(attribute.String("messaging.destination.name", message.Topic)))
	return ctx, time.Now()
}

func (t *sinkTelemetry) finishMessage(ctx context.Context, message kafkago.Message, outcome string, err error, elapsed time.Duration) {
	attributes := append(messageAttributes(message), attribute.String("outcome", outcome))
	options := metric.WithAttributes(attributes...)
	t.messages.Add(ctx, 1, options)
	t.duration.Record(ctx, elapsed.Seconds(), options)
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("sink.outcome", outcome))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, outcome)
	}
	span.End()
}

func messageAttributes(message kafkago.Message) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("messaging.destination.name", message.Topic),
		attribute.Int("messaging.destination.partition.id", message.Partition),
	}
}
