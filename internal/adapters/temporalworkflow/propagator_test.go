package temporalworkflow

import (
	"context"
	"testing"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	commonpb "go.temporal.io/api/common/v1"
)

func TestDurableRequestTraceContextRoundTrip(t *testing.T) {
	setTestPropagator(t)
	spanContext := testSpanContext()
	request := app.AnalysisJobRequest{}

	CaptureRequestTraceContext(trace.ContextWithSpanContext(context.Background(), spanContext), &request)
	extracted := trace.SpanContextFromContext(RestoreRequestTraceContext(context.Background(), request))

	assertSpanContext(t, extracted, spanContext)
}

func TestTemporalTraceContextPropagatorRoundTrip(t *testing.T) {
	setTestPropagator(t)
	spanContext := testSpanContext()
	headers := temporalHeaders{}
	propagator := TraceContextPropagator{}

	if err := propagator.Inject(trace.ContextWithSpanContext(context.Background(), spanContext), headers); err != nil {
		t.Fatalf("inject: %v", err)
	}
	extractedContext, err := propagator.Extract(context.Background(), headers)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	assertSpanContext(t, trace.SpanContextFromContext(extractedContext), spanContext)
}

type temporalHeaders map[string]*commonpb.Payload

func (h temporalHeaders) Set(key string, payload *commonpb.Payload) { h[key] = payload }
func (h temporalHeaders) Get(key string) (*commonpb.Payload, bool) {
	payload, found := h[key]
	return payload, found
}
func (h temporalHeaders) ForEachKey(handler func(string, *commonpb.Payload) error) error {
	for key, payload := range h {
		if err := handler(key, payload); err != nil {
			return err
		}
	}
	return nil
}

func setTestPropagator(t *testing.T) {
	t.Helper()
	original := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	t.Cleanup(func() { otel.SetTextMapPropagator(original) })
}

func testSpanContext() trace.SpanContext {
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3},
		SpanID:     trace.SpanID{4, 5, 6},
		TraceFlags: trace.FlagsSampled,
	})
}

func assertSpanContext(t *testing.T, got trace.SpanContext, want trace.SpanContext) {
	t.Helper()
	if got.TraceID() != want.TraceID() || got.SpanID() != want.SpanID() {
		t.Fatalf("span context = %s/%s, want %s/%s", got.TraceID(), got.SpanID(), want.TraceID(), want.SpanID())
	}
	if !got.IsRemote() {
		t.Fatal("extracted span context should be remote")
	}
}
