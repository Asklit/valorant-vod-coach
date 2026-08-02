package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/asklit/valorant-vod-coach/internal/adapters/clickhouse"
	kafkaadapter "github.com/asklit/valorant-vod-coach/internal/adapters/kafka"
	"github.com/asklit/valorant-vod-coach/internal/domain"
	"github.com/asklit/valorant-vod-coach/internal/platform/observability"
)

func main() {
	if err := run(); err != nil {
		slog.Error("vod-clickhouse-sink failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	brokersRaw := flag.String("brokers", envDefault("KAFKA_BROKERS", "localhost:9092"), "comma-separated Kafka brokers")
	topicsRaw := flag.String("topics", envDefault("KAFKA_SINK_TOPICS", domain.TopicVODProcessing+","+domain.TopicVODLifecycle), "comma-separated Kafka topics")
	groupID := flag.String("group-id", envDefault("CLICKHOUSE_SINK_GROUP_ID", "vod-clickhouse-sink"), "Kafka consumer group id")
	clickhouseURL := flag.String("clickhouse-url", envDefault("CLICKHOUSE_URL", "http://localhost:8123"), "ClickHouse HTTP endpoint")
	clickhouseDB := flag.String("clickhouse-db", envDefault("CLICKHOUSE_DB", "vodcoach"), "ClickHouse database")
	clickhouseUser := flag.String("clickhouse-user", os.Getenv("CLICKHOUSE_USER"), "ClickHouse user")
	clickhousePassword := flag.String("clickhouse-password", os.Getenv("CLICKHOUSE_PASSWORD"), "ClickHouse password")
	deadLetterTopic := flag.String("dead-letter-topic", envDefault("KAFKA_DEAD_LETTER_TOPIC", "vod.dead-letter.v1"), "Kafka topic for invalid event envelopes")
	migrationsDir := flag.String("migrations-dir", "deployments/migrations/clickhouse", "ClickHouse migrations directory")
	migrate := flag.Bool("migrate", true, "apply ClickHouse migrations before consuming")
	once := flag.Bool("once", false, "process one message and exit")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	obs, err := observability.Setup(ctx, observability.Config{ServiceName: "vod-clickhouse-sink"}, os.Stderr)
	if err != nil {
		return fmt.Errorf("setup observability: %w", err)
	}
	defer observability.Shutdown(context.Background(), obs.Shutdown, obs.Logger)
	telemetry, err := newSinkTelemetry(obs.Tracer, obs.Meter)
	if err != nil {
		return fmt.Errorf("configure sink telemetry: %w", err)
	}

	client := clickhouse.Client{
		Endpoint: *clickhouseURL,
		Database: *clickhouseDB,
		User:     *clickhouseUser,
		Password: *clickhousePassword,
	}
	if *migrate {
		applied, err := client.ApplyMigrations(ctx, *migrationsDir)
		if err != nil {
			return fmt.Errorf("apply clickhouse migrations: %w", err)
		}
		obs.Logger.InfoContext(ctx, "clickhouse migrations checked", "count", len(applied))
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
	dlqWriter := &kafkago.Writer{
		Addr:         kafkago.TCP(splitCSV(*brokersRaw)...),
		RequiredAcks: kafkago.RequireOne,
		Balancer:     &kafkago.Hash{},
	}
	defer dlqWriter.Close()

	obs.Logger.InfoContext(ctx, "vod-clickhouse-sink started", "brokers", *brokersRaw, "topics", *topicsRaw, "group_id", *groupID)
	for {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				obs.Logger.InfoContext(context.Background(), "vod-clickhouse-sink stopped")
				return nil
			}
			telemetry.fetchFailed(ctx)
			obs.Logger.ErrorContext(ctx, "fetch message failed", "error", err)
			continue
		}

		messageCtx, startedAt := telemetry.startMessage(kafkaadapter.ExtractTraceContext(ctx, message.Headers), message)
		if err := handleMessage(messageCtx, client, message); err != nil {
			if isPermanentMessageError(err) {
				if dlqErr := publishDeadLetter(messageCtx, dlqWriter, *deadLetterTopic, message, err); dlqErr != nil {
					telemetry.finishMessage(messageCtx, message, "dlq_failed", dlqErr, time.Since(startedAt))
					obs.Logger.ErrorContext(messageCtx, "publish dead letter failed", messageLogFields(message, dlqErr)...)
					continue
				}
				if commitErr := reader.CommitMessages(messageCtx, message); commitErr != nil {
					telemetry.finishMessage(messageCtx, message, "commit_failed", commitErr, time.Since(startedAt))
					obs.Logger.ErrorContext(messageCtx, "commit dead-lettered message failed", messageLogFields(message, commitErr)...)
					continue
				}
				telemetry.finishMessage(messageCtx, message, "dead_lettered", nil, time.Since(startedAt))
				obs.Logger.WarnContext(messageCtx, "invalid event sent to dead letter topic", messageLogFields(message, err)...)
				continue
			}
			telemetry.finishMessage(messageCtx, message, "insert_failed", err, time.Since(startedAt))
			obs.Logger.ErrorContext(messageCtx, "insert event failed", messageLogFields(message, err)...)
			continue
		}
		if err := reader.CommitMessages(messageCtx, message); err != nil {
			telemetry.finishMessage(messageCtx, message, "commit_failed", err, time.Since(startedAt))
			obs.Logger.ErrorContext(messageCtx, "commit message failed", messageLogFields(message, err)...)
			continue
		}
		telemetry.finishMessage(messageCtx, message, "stored", nil, time.Since(startedAt))
		obs.Logger.DebugContext(messageCtx, "event stored", "topic", message.Topic, "partition", message.Partition, "offset", message.Offset)
		if *once {
			return nil
		}
	}
}

func handleMessage(ctx context.Context, client clickhouse.Client, message kafkago.Message) error {
	var event domain.EventEnvelope
	if err := json.Unmarshal(message.Value, &event); err != nil {
		return permanentMessageError{err: fmt.Errorf("decode event envelope: %w", err)}
	}
	if event.EventID == "" {
		return permanentMessageError{err: fmt.Errorf("event envelope missing event_id")}
	}
	if event.EventVersion <= 0 || event.EventType == "" || event.OccurredAt.IsZero() {
		return permanentMessageError{err: fmt.Errorf("event envelope missing required metadata")}
	}
	return client.InsertEvent(ctx, message.Topic, event, message.Value)
}

type permanentMessageError struct{ err error }

func (e permanentMessageError) Error() string { return e.err.Error() }
func (e permanentMessageError) Unwrap() error { return e.err }

func isPermanentMessageError(err error) bool {
	var target permanentMessageError
	return errors.As(err, &target)
}

type kafkaMessageWriter interface {
	WriteMessages(context.Context, ...kafkago.Message) error
}

func publishDeadLetter(ctx context.Context, writer kafkaMessageWriter, topic string, message kafkago.Message, cause error) error {
	if writer == nil {
		return fmt.Errorf("dead letter writer is required")
	}
	if strings.TrimSpace(topic) == "" {
		return fmt.Errorf("dead letter topic is required")
	}
	headers := append([]kafkago.Header(nil), message.Headers...)
	headers = append(headers,
		kafkago.Header{Key: "dlq_error", Value: []byte(cause.Error())},
		kafkago.Header{Key: "dlq_original_topic", Value: []byte(message.Topic)},
		kafkago.Header{Key: "dlq_original_partition", Value: []byte(fmt.Sprint(message.Partition))},
		kafkago.Header{Key: "dlq_original_offset", Value: []byte(fmt.Sprint(message.Offset))},
	)
	return writer.WriteMessages(ctx, kafkago.Message{
		Topic: topic, Key: message.Key, Value: message.Value, Headers: headers, Time: time.Now().UTC(),
	})
}

func messageLogFields(message kafkago.Message, err error) []any {
	return []any{"topic", message.Topic, "partition", message.Partition, "offset", message.Offset, "error", err}
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
