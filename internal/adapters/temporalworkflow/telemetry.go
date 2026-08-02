package temporalworkflow

import (
	"context"
	"log/slog"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type ActivityTelemetry struct {
	logger           *slog.Logger
	tracer           trace.Tracer
	attempts         metric.Int64Counter
	running          metric.Int64UpDownCounter
	duration         metric.Float64Histogram
	progressFailures metric.Int64Counter
	finalized        metric.Int64Counter
}

func NewActivityTelemetry(logger *slog.Logger, tracer trace.Tracer, meter metric.Meter) (*ActivityTelemetry, error) {
	attempts, err := meter.Int64Counter("vodcoach.analysis.activity.attempts", metric.WithDescription("Temporal analysis activity attempts"))
	if err != nil {
		return nil, err
	}
	running, err := meter.Int64UpDownCounter("vodcoach.analysis.activity.running", metric.WithDescription("Currently running analysis activities"))
	if err != nil {
		return nil, err
	}
	duration, err := meter.Float64Histogram("vodcoach.analysis.activity.duration", metric.WithUnit("s"), metric.WithDescription("Analysis activity duration"))
	if err != nil {
		return nil, err
	}
	progressFailures, err := meter.Int64Counter("vodcoach.analysis.progress.failures", metric.WithDescription("Analysis progress persistence failures"))
	if err != nil {
		return nil, err
	}
	finalized, err := meter.Int64Counter("vodcoach.analysis.jobs.finalized", metric.WithDescription("Finalized analysis jobs"))
	if err != nil {
		return nil, err
	}
	return &ActivityTelemetry{
		logger: logger, tracer: tracer, attempts: attempts, running: running,
		duration: duration, progressFailures: progressFailures, finalized: finalized,
	}, nil
}

func (t *ActivityTelemetry) startRun(ctx context.Context, request app.AnalysisJobRequest) context.Context {
	ctx, span := t.tracer.Start(ctx, "analysis.run",
		trace.WithAttributes(analysisAttributes(request)...),
		trace.WithSpanKind(trace.SpanKindConsumer),
	)
	_ = span
	options := metric.WithAttributes(analysisAttributes(request)...)
	t.attempts.Add(ctx, 1, options)
	t.running.Add(ctx, 1, options)
	return ctx
}

func (t *ActivityTelemetry) finishRun(ctx context.Context, request app.AnalysisJobRequest, outcome string, elapsed time.Duration) {
	attributes := append(analysisAttributes(request), attribute.String("outcome", outcome))
	options := metric.WithAttributes(attributes...)
	t.running.Add(ctx, -1, metric.WithAttributes(analysisAttributes(request)...))
	t.duration.Record(ctx, elapsed.Seconds(), options)
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("analysis.outcome", outcome))
	if outcome != "completed" {
		span.SetStatus(codes.Error, outcome)
	}
	span.End()
}

func (t *ActivityTelemetry) progressFailure(ctx context.Context, err error) {
	t.progressFailures.Add(ctx, 1)
	if t.logger != nil {
		t.logger.WarnContext(ctx, "persist analysis progress failed", "error", err)
	}
}

func (t *ActivityTelemetry) jobFinalized(ctx context.Context, status string) {
	t.finalized.Add(ctx, 1, metric.WithAttributes(attribute.String("status", status)))
}

func analysisAttributes(request app.AnalysisJobRequest) []attribute.KeyValue {
	scope := "segment"
	if request.DurationSeconds <= 0 {
		scope = "full_vod"
	}
	return []attribute.KeyValue{
		attribute.String("analysis.scope", scope),
		attribute.Bool("analysis.model_review", request.ModelReview),
	}
}
