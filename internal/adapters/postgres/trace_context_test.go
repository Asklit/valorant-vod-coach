package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/asklit/valorant-vod-coach/internal/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestEnrichEventTraceContextPersistsW3CParent(t *testing.T) {
	original := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(original) })
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3},
		SpanID:     trace.SpanID{4, 5, 6},
		TraceFlags: trace.FlagsSampled,
	})
	event := domain.EventEnvelope{}

	enrichEventTraceContext(trace.ContextWithSpanContext(context.Background(), spanContext), &event)

	if event.TraceID != spanContext.TraceID().String() {
		t.Fatalf("trace_id = %q, want %q", event.TraceID, spanContext.TraceID())
	}
	if !strings.Contains(event.TraceParent, spanContext.TraceID().String()) || !strings.Contains(event.TraceParent, spanContext.SpanID().String()) {
		t.Fatalf("trace_parent = %q, want trace and span IDs", event.TraceParent)
	}
}
