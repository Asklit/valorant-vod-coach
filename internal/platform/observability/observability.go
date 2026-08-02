package observability

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"go.opentelemetry.io/otel/trace"
)

type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	InstanceID     string
	LogLevel       string
	LogFormat      string
	OTLPEndpoint   string
	MetricInterval time.Duration
}

type Runtime struct {
	Logger   *slog.Logger
	Tracer   trace.Tracer
	Meter    metric.Meter
	Shutdown func(context.Context) error
}

func Setup(ctx context.Context, config Config, output io.Writer) (Runtime, error) {
	if output == nil {
		output = os.Stderr
	}
	if strings.TrimSpace(config.ServiceName) == "" {
		config.ServiceName = "valorant-vod-coach"
	}
	if config.LogLevel == "" {
		config.LogLevel = os.Getenv("LOG_LEVEL")
	}
	if config.LogFormat == "" {
		config.LogFormat = os.Getenv("LOG_FORMAT")
	}
	if config.OTLPEndpoint == "" {
		config.OTLPEndpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	}
	if config.ServiceVersion == "" {
		config.ServiceVersion = os.Getenv("SERVICE_VERSION")
	}
	if config.Environment == "" {
		config.Environment = envDefault("APP_ENV", "local")
	}
	if config.InstanceID == "" {
		config.InstanceID = os.Getenv("HOSTNAME")
	}
	if config.MetricInterval <= 0 {
		config.MetricInterval = 10 * time.Second
	}

	logger := slog.New(traceLogHandler{next: logHandler(output, config.LogLevel, config.LogFormat)})
	slog.SetDefault(logger)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdown := func(context.Context) error { return nil }
	if strings.TrimSpace(config.OTLPEndpoint) != "" {
		telemetryResource, err := resource.New(ctx,
			resource.WithFromEnv(),
			resource.WithTelemetrySDK(),
			resource.WithAttributes(
				semconv.ServiceName(config.ServiceName),
				semconv.ServiceVersion(config.ServiceVersion),
				semconv.ServiceInstanceID(config.InstanceID),
				semconv.DeploymentEnvironmentName(config.Environment),
			),
		)
		if err != nil {
			return Runtime{}, err
		}
		traceExporter, err := otlptracehttp.New(ctx,
			otlptracehttp.WithEndpointURL(config.OTLPEndpoint),
		)
		if err != nil {
			return Runtime{}, err
		}
		traceProvider := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(traceExporter),
			sdktrace.WithResource(telemetryResource),
		)
		metricExporter, err := otlpmetrichttp.New(ctx,
			otlpmetrichttp.WithEndpointURL(config.OTLPEndpoint),
		)
		if err != nil {
			_ = traceProvider.Shutdown(ctx)
			return Runtime{}, err
		}
		metricReader := sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(config.MetricInterval))
		metricProvider := sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(metricReader),
			sdkmetric.WithResource(telemetryResource),
		)
		otel.SetTracerProvider(traceProvider)
		otel.SetMeterProvider(metricProvider)
		shutdown = func(shutdownCtx context.Context) error {
			return errors.Join(metricProvider.Shutdown(shutdownCtx), traceProvider.Shutdown(shutdownCtx))
		}
		logger.InfoContext(ctx, "opentelemetry export enabled",
			"service", config.ServiceName,
			"environment", config.Environment,
			"endpoint", config.OTLPEndpoint,
		)
	} else {
		logger.DebugContext(ctx, "opentelemetry export disabled", "service", config.ServiceName)
	}

	return Runtime{
		Logger:   logger,
		Tracer:   otel.Tracer(config.ServiceName),
		Meter:    otel.Meter(config.ServiceName),
		Shutdown: shutdown,
	}, nil
}

type traceLogHandler struct {
	next slog.Handler
}

func (h traceLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h traceLogHandler) Handle(ctx context.Context, record slog.Record) error {
	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", spanContext.TraceID().String()),
			slog.String("span_id", spanContext.SpanID().String()),
		)
	}
	return h.next.Handle(ctx, record)
}

func (h traceLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceLogHandler{next: h.next.WithAttrs(attrs)}
}

func (h traceLogHandler) WithGroup(name string) slog.Handler {
	return traceLogHandler{next: h.next.WithGroup(name)}
}

func logHandler(output io.Writer, levelRaw string, formatRaw string) slog.Handler {
	level := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(levelRaw)) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	options := &slog.HandlerOptions{Level: level}
	if strings.EqualFold(strings.TrimSpace(formatRaw), "text") {
		return slog.NewTextHandler(output, options)
	}
	return slog.NewJSONHandler(output, options)
}

func Shutdown(ctx context.Context, shutdown func(context.Context) error, logger *slog.Logger) {
	if shutdown == nil {
		return
	}
	if err := shutdown(ctx); err != nil && !errors.Is(err, context.Canceled) {
		if logger != nil {
			logger.WarnContext(ctx, "observability shutdown failed", "error", err)
		}
	}
}

func envDefault(name string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
