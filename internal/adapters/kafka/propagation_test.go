package kafka

import (
	"context"
	"testing"

	kafkago "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestKafkaTraceContextRoundTrip(t *testing.T) {
	original := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(original) })

	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3},
		SpanID:     trace.SpanID{4, 5, 6},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)
	headers := InjectTraceContext(ctx, []kafkago.Header{{Key: "event_id", Value: []byte("event-1")}})

	extracted := trace.SpanContextFromContext(ExtractTraceContext(context.Background(), headers))
	if extracted.TraceID() != spanContext.TraceID() || extracted.SpanID() != spanContext.SpanID() {
		t.Fatalf("extracted context = %s/%s, want %s/%s", extracted.TraceID(), extracted.SpanID(), spanContext.TraceID(), spanContext.SpanID())
	}
	if !extracted.IsRemote() {
		t.Fatal("extracted context should be remote")
	}
}

func TestInjectTraceContextReplacesExistingHeader(t *testing.T) {
	original := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(original) })

	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{9},
		SpanID:  trace.SpanID{8},
	})
	headers := InjectTraceContext(
		trace.ContextWithSpanContext(context.Background(), spanContext),
		[]kafkago.Header{{Key: "Traceparent", Value: []byte("stale")}},
	)

	count := 0
	for _, header := range headers {
		if header.Key == "traceparent" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("traceparent header count = %d, want 1", count)
	}
}
