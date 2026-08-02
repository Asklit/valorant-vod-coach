package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
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
	OTLPTraceURL   string
	OTLPMetricURL  string
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
	if config.OTLPTraceURL == "" {
		config.OTLPTraceURL = os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	}
	if config.OTLPMetricURL == "" {
		config.OTLPMetricURL = os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT")
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

	traceEndpoint, err := resolveSignalEndpoint(config.OTLPEndpoint, config.OTLPTraceURL, "traces")
	if err != nil {
		return Runtime{}, err
	}
	metricEndpoint, err := resolveSignalEndpoint(config.OTLPEndpoint, config.OTLPMetricURL, "metrics")
	if err != nil {
		return Runtime{}, err
	}
	shutdown := func(context.Context) error { return nil }
	if traceEndpoint != "" || metricEndpoint != "" {
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
		var shutdownFunctions []func(context.Context) error
		if traceEndpoint != "" {
			traceExporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(traceEndpoint))
			if err != nil {
				return Runtime{}, err
			}
			traceProvider := sdktrace.NewTracerProvider(
				sdktrace.WithBatcher(traceExporter),
				sdktrace.WithResource(telemetryResource),
			)
			otel.SetTracerProvider(traceProvider)
			shutdownFunctions = append(shutdownFunctions, traceProvider.Shutdown)
		}
		if metricEndpoint != "" {
			metricExporter, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(metricEndpoint))
			if err != nil {
				shutdownAll(ctx, shutdownFunctions)
				return Runtime{}, err
			}
			metricReader := sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(config.MetricInterval))
			metricProvider := sdkmetric.NewMeterProvider(
				sdkmetric.WithReader(metricReader),
				sdkmetric.WithResource(telemetryResource),
			)
			otel.SetMeterProvider(metricProvider)
			shutdownFunctions = append(shutdownFunctions, metricProvider.Shutdown)
		}
		shutdown = func(shutdownCtx context.Context) error { return shutdownAll(shutdownCtx, shutdownFunctions) }
		logger.InfoContext(ctx, "opentelemetry export enabled",
			"service", config.ServiceName,
			"environment", config.Environment,
			"trace_endpoint", traceEndpoint,
			"metric_endpoint", metricEndpoint,
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

func resolveSignalEndpoint(base, override, signal string) (string, error) {
	raw := strings.TrimSpace(override)
	appendSignalPath := false
	if raw == "" {
		raw = strings.TrimSpace(base)
		appendSignalPath = raw != ""
	}
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse OTLP %s endpoint: %w", signal, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("OTLP %s endpoint must be an absolute HTTP URL", signal)
	}
	if appendSignalPath {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/" + signal
	}
	return parsed.String(), nil
}

func shutdownAll(ctx context.Context, functions []func(context.Context) error) error {
	errs := make([]error, 0, len(functions))
	for index := len(functions) - 1; index >= 0; index-- {
		errs = append(errs, functions[index](ctx))
	}
	return errors.Join(errs...)
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
