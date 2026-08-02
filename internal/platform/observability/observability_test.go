package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestTraceLogHandlerAddsActiveTraceContext(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(traceLogHandler{next: slog.NewJSONHandler(&output, nil)})
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1, 2, 3},
		SpanID:  trace.SpanID{4, 5, 6},
		Remote:  true,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

	logger.InfoContext(ctx, "processed", "count", 3)

	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("decode JSON log: %v", err)
	}
	if got := entry["trace_id"]; got != spanContext.TraceID().String() {
		t.Fatalf("trace_id = %v, want %s", got, spanContext.TraceID())
	}
	if got := entry["span_id"]; got != spanContext.SpanID().String() {
		t.Fatalf("span_id = %v, want %s", got, spanContext.SpanID())
	}
}

func TestSetupWithoutExporterReturnsUsableRuntime(t *testing.T) {
	var output bytes.Buffer
	runtime, err := Setup(context.Background(), Config{
		ServiceName:  "test-service",
		LogLevel:     "debug",
		OTLPEndpoint: "",
	}, &output)
	if err != nil {
		t.Fatalf("setup observability: %v", err)
	}
	if runtime.Logger == nil || runtime.Tracer == nil || runtime.Meter == nil || runtime.Shutdown == nil {
		t.Fatal("runtime is not fully initialized")
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
