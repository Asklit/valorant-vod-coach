package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestResolveSignalEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		override string
		signal   string
		want     string
		wantErr  bool
	}{
		{name: "generic root", base: "http://collector:4318", signal: "traces", want: "http://collector:4318/v1/traces"},
		{name: "generic base path", base: "https://telemetry.example/otlp/", signal: "metrics", want: "https://telemetry.example/otlp/v1/metrics"},
		{name: "signal override", base: "http://collector:4318", override: "https://traces.example/custom", signal: "traces", want: "https://traces.example/custom"},
		{name: "disabled", signal: "metrics", want: ""},
		{name: "relative rejected", base: "collector:4318", signal: "traces", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveSignalEndpoint(test.base, test.override, test.signal)
			if (err != nil) != test.wantErr {
				t.Fatalf("resolveSignalEndpoint() error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("resolveSignalEndpoint() = %q, want %q", got, test.want)
			}
		})
	}
}

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
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
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
