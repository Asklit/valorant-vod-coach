package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/asklit/valorant-vod-coach/internal/adapters/clickhouse"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

func main() {
	brokersRaw := flag.String("brokers", envDefault("KAFKA_BROKERS", "localhost:9092"), "comma-separated Kafka brokers")
	topicsRaw := flag.String("topics", envDefault("KAFKA_SINK_TOPICS", domain.TopicVODProcessing+","+domain.TopicVODLifecycle), "comma-separated Kafka topics")
	groupID := flag.String("group-id", envDefault("CLICKHOUSE_SINK_GROUP_ID", "vod-clickhouse-sink"), "Kafka consumer group id")
	clickhouseURL := flag.String("clickhouse-url", envDefault("CLICKHOUSE_URL", "http://localhost:8123"), "ClickHouse HTTP endpoint")
	clickhouseDB := flag.String("clickhouse-db", envDefault("CLICKHOUSE_DB", "vodcoach"), "ClickHouse database")
	clickhouseUser := flag.String("clickhouse-user", os.Getenv("CLICKHOUSE_USER"), "ClickHouse user")
	clickhousePassword := flag.String("clickhouse-password", os.Getenv("CLICKHOUSE_PASSWORD"), "ClickHouse password")
	migrationsDir := flag.String("migrations-dir", "deployments/migrations/clickhouse", "ClickHouse migrations directory")
	migrate := flag.Bool("migrate", true, "apply ClickHouse migrations before consuming")
	once := flag.Bool("once", false, "process one message and exit")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := clickhouse.Client{
		Endpoint: *clickhouseURL,
		Database: *clickhouseDB,
		User:     *clickhouseUser,
		Password: *clickhousePassword,
	}
	if *migrate {
		applied, err := client.ApplyMigrations(ctx, *migrationsDir)
		if err != nil {
			log.Fatalf("apply clickhouse migrations: %v", err)
		}
		log.Printf("clickhouse migrations checked count=%d", len(applied))
	}

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:     splitCSV(*brokersRaw),
		GroupID:     *groupID,
		GroupTopics: splitCSV(*topicsRaw),
		MinBytes:    1,
		MaxBytes:    10e6,
		MaxWait:     500 * time.Millisecond,
	})
	defer reader.Close()

	log.Printf("vod-clickhouse-sink started brokers=%s topics=%s group_id=%s", *brokersRaw, *topicsRaw, *groupID)
	for {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				log.Printf("vod-clickhouse-sink stopped")
				return
			}
			log.Printf("fetch message failed: %v", err)
			continue
		}

		if err := handleMessage(ctx, client, message); err != nil {
			log.Printf("insert event failed topic=%s partition=%d offset=%d error=%v", message.Topic, message.Partition, message.Offset, err)
			continue
		}
		if err := reader.CommitMessages(ctx, message); err != nil {
			log.Printf("commit message failed topic=%s partition=%d offset=%d error=%v", message.Topic, message.Partition, message.Offset, err)
			continue
		}
		log.Printf("event stored topic=%s partition=%d offset=%d", message.Topic, message.Partition, message.Offset)
		if *once {
			return
		}
	}
}

func handleMessage(ctx context.Context, client clickhouse.Client, message kafkago.Message) error {
	var event domain.EventEnvelope
	if err := json.Unmarshal(message.Value, &event); err != nil {
		return fmt.Errorf("decode event envelope: %w", err)
	}
	if event.EventID == "" {
		return fmt.Errorf("event envelope missing event_id")
	}
	return client.InsertEvent(ctx, message.Topic, event, message.Value)
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
