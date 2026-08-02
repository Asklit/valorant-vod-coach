package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	kafkaproducer "github.com/asklit/valorant-vod-coach/internal/adapters/kafka"
	"github.com/asklit/valorant-vod-coach/internal/adapters/postgres"
	"github.com/asklit/valorant-vod-coach/internal/platform/observability"
)

func main() {
	if err := run(); err != nil {
		slog.Error("vod-outbox-relay failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "Postgres connection URL; can also be set through DATABASE_URL")
	brokersRaw := flag.String("brokers", envDefault("KAFKA_BROKERS", "localhost:9092"), "comma-separated Kafka brokers")
	workerID := flag.String("worker-id", envDefault("OUTBOX_WORKER_ID", hostnameWorkerID()), "relay worker id")
	batchSize := flag.Int("batch-size", 50, "maximum events to claim per poll")
	interval := flag.Duration("interval", time.Second, "poll interval when no events are available")
	once := flag.Bool("once", false, "process one batch and exit")
	flag.Parse()

	if strings.TrimSpace(*databaseURL) == "" {
		return fmt.Errorf("--database-url or DATABASE_URL is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	obs, err := observability.Setup(ctx, observability.Config{ServiceName: "vod-outbox-relay"}, os.Stderr)
	if err != nil {
		return fmt.Errorf("setup observability: %w", err)
	}
	defer observability.Shutdown(context.Background(), obs.Shutdown, obs.Logger)
	telemetry, err := newRelayTelemetry(obs.Tracer, obs.Meter)
	if err != nil {
		return fmt.Errorf("configure relay telemetry: %w", err)
	}

	db, err := postgres.Open(ctx, *databaseURL)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer db.Close()

	producer, err := kafkaproducer.NewProducer(splitCSV(*brokersRaw))
	if err != nil {
		return fmt.Errorf("configure kafka producer: %w", err)
	}
	defer producer.Close()

	obs.Logger.InfoContext(ctx, "vod-outbox-relay started", "worker_id", *workerID, "brokers", *brokersRaw, "batch_size", *batchSize)
	for {
		batchCtx, startedAt := telemetry.startBatch(ctx)
		processed, err := processBatch(batchCtx, db, producer, *workerID, *batchSize, telemetry)
		telemetry.finishBatch(batchCtx, processed, err, time.Since(startedAt))
		if err != nil {
			obs.Logger.ErrorContext(batchCtx, "outbox batch failed", "error", err)
		}
		if *once {
			obs.Logger.InfoContext(ctx, "vod-outbox-relay stopped", "processed", processed, "once", true)
			return err
		}
		if processed == 0 {
			select {
			case <-ctx.Done():
				obs.Logger.InfoContext(context.Background(), "vod-outbox-relay stopped")
				return nil
			case <-time.After(*interval):
			}
		}
	}
}

type outboxProducer interface {
	PublishOutboxEvent(ctx context.Context, event postgres.OutboxEvent) error
}

func processBatch(ctx context.Context, db *sql.DB, producer outboxProducer, workerID string, batchSize int, telemetry *relayTelemetry) (int, error) {
	events, err := postgres.ClaimPendingOutboxEvents(ctx, db, batchSize, workerID)
	if err != nil {
		return 0, err
	}
	for _, event := range events {
		eventCtx := kafkaproducer.ContextFromOutboxEvent(ctx, event)
		startedAt := time.Now()
		if telemetry != nil {
			eventCtx = telemetry.startPublish(eventCtx, event)
		}
		if err := producer.PublishOutboxEvent(eventCtx, event); err != nil {
			if telemetry != nil {
				telemetry.finishPublish(eventCtx, event, err, time.Since(startedAt))
			}
			if markErr := postgres.MarkOutboxFailed(ctx, db, event.ID, err); markErr != nil {
				return 0, fmt.Errorf("publish %s: %v; mark failed: %w", event.ID, err, markErr)
			}
			continue
		}
		if telemetry != nil {
			telemetry.finishPublish(eventCtx, event, nil, time.Since(startedAt))
		}
		if err := postgres.MarkOutboxPublished(ctx, db, event.ID); err != nil {
			return 0, fmt.Errorf("mark published %s: %w", event.ID, err)
		}
	}
	return len(events), nil
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if cleaned := strings.TrimSpace(part); cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

func envDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func hostnameWorkerID() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "outbox-relay"
	}
	return "outbox-relay-" + host
}
