package observability

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"go.opentelemetry.io/otel/trace"
)

type Config struct {
	ServiceName  string
	LogLevel     string
	LogFormat    string
	OTLPEndpoint string
}

type Runtime struct {
	Logger   *slog.Logger
	Tracer   trace.Tracer
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

	logger := slog.New(logHandler(output, config.LogLevel, config.LogFormat))
	slog.SetDefault(logger)

	shutdown := func(context.Context) error { return nil }
	if strings.TrimSpace(config.OTLPEndpoint) != "" {
		exporter, err := otlptracehttp.New(ctx,
			otlptracehttp.WithEndpointURL(config.OTLPEndpoint),
		)
		if err != nil {
			return Runtime{}, err
		}
		provider := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(resource.NewWithAttributes(
				semconv.SchemaURL,
				semconv.ServiceName(config.ServiceName),
			)),
		)
		otel.SetTracerProvider(provider)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
		shutdown = provider.Shutdown
		logger.InfoContext(ctx, "opentelemetry tracing enabled", "service", config.ServiceName, "endpoint", config.OTLPEndpoint)
	} else {
		logger.DebugContext(ctx, "opentelemetry tracing disabled", "service", config.ServiceName)
	}

	return Runtime{
		Logger:   logger,
		Tracer:   otel.Tracer(config.ServiceName),
		Shutdown: shutdown,
	}, nil
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
