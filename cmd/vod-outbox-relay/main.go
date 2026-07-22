package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	kafkaproducer "github.com/asklit/valorant-vod-coach/internal/adapters/kafka"
	"github.com/asklit/valorant-vod-coach/internal/adapters/postgres"
)

func main() {
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "Postgres connection URL; can also be set through DATABASE_URL")
	brokersRaw := flag.String("brokers", envDefault("KAFKA_BROKERS", "localhost:9092"), "comma-separated Kafka brokers")
	workerID := flag.String("worker-id", envDefault("OUTBOX_WORKER_ID", hostnameWorkerID()), "relay worker id")
	batchSize := flag.Int("batch-size", 50, "maximum events to claim per poll")
	interval := flag.Duration("interval", time.Second, "poll interval when no events are available")
	once := flag.Bool("once", false, "process one batch and exit")
	flag.Parse()

	if strings.TrimSpace(*databaseURL) == "" {
		log.Fatal("--database-url or DATABASE_URL is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := postgres.Open(ctx, *databaseURL)
	if err != nil {
		log.Fatalf("open postgres: %v", err)
	}
	defer db.Close()

	producer, err := kafkaproducer.NewProducer(splitCSV(*brokersRaw))
	if err != nil {
		log.Fatalf("configure kafka producer: %v", err)
	}
	defer producer.Close()

	log.Printf("vod-outbox-relay started worker_id=%s brokers=%s batch_size=%d", *workerID, *brokersRaw, *batchSize)
	for {
		processed, err := processBatch(ctx, db, producer, *workerID, *batchSize)
		if err != nil {
			log.Printf("outbox batch failed: %v", err)
		}
		if *once {
			log.Printf("vod-outbox-relay processed=%d once=true", processed)
			return
		}
		if processed == 0 {
			select {
			case <-ctx.Done():
				log.Printf("vod-outbox-relay stopped")
				return
			case <-time.After(*interval):
			}
		}
	}
}

type outboxProducer interface {
	PublishOutboxEvent(ctx context.Context, event postgres.OutboxEvent) error
}

func processBatch(ctx context.Context, db *sql.DB, producer outboxProducer, workerID string, batchSize int) (int, error) {
	events, err := postgres.ClaimPendingOutboxEvents(ctx, db, batchSize, workerID)
	if err != nil {
		return 0, err
	}
	for _, event := range events {
		if err := producer.PublishOutboxEvent(ctx, event); err != nil {
			if markErr := postgres.MarkOutboxFailed(ctx, db, event.ID, err); markErr != nil {
				return 0, fmt.Errorf("publish %s: %v; mark failed: %w", event.ID, err, markErr)
			}
			continue
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
