package clickhouse

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

func TestLoadMigrations(t *testing.T) {
	migrations, err := LoadMigrations("../../../deployments/migrations/clickhouse")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if len(migrations) != 1 || migrations[0].Version != 1 || migrations[0].Name != "001_kafka_events.sql" {
		t.Fatalf("unexpected migrations: %+v", migrations)
	}
	if !strings.Contains(migrations[0].SQL, "CREATE TABLE IF NOT EXISTS kafka_events") {
		t.Fatalf("migration missing kafka_events table")
	}
}

func TestInsertEventUsesJSONEachRow(t *testing.T) {
	transport := &captureTransport{}

	event := domain.EventEnvelope{
		EventID:       "event_01",
		EventType:     domain.EventTypeReportReady,
		EventVersion:  domain.EventEnvelopeVersion,
		OccurredAt:    time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC),
		Producer:      "test",
		AggregateType: "vod",
		AggregateID:   "iron_example",
		CorrelationID: "run_01",
		Payload:       json.RawMessage(`{"run_id":"run_01"}`),
	}
	envelope, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	client := Client{
		Endpoint:   "http://clickhouse.local:8123",
		Database:   "vodcoach",
		HTTPClient: &http.Client{Transport: transport},
	}
	if err := client.InsertEvent(context.Background(), domain.TopicVODLifecycle, event, envelope); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	if got := transport.database; got != "vodcoach" {
		t.Fatalf("unexpected database query: %q", got)
	}
	if !strings.HasPrefix(transport.body, "INSERT INTO kafka_events FORMAT JSONEachRow\n") {
		t.Fatalf("unexpected insert query:\n%s", transport.body)
	}
	if !strings.Contains(transport.body, `"event_id":"event_01"`) ||
		!strings.Contains(transport.body, `"topic":"vod.lifecycle.v1"`) ||
		!strings.Contains(transport.body, `"payload":"{\"run_id\":\"run_01\"}"`) {
		t.Fatalf("unexpected JSONEachRow body:\n%s", transport.body)
	}
}

type captureTransport struct {
	body     string
	database string
}

func (t *captureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	t.body = string(raw)
	t.database = request.URL.Query().Get("database")
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
		Request:    request,
	}, nil
}
