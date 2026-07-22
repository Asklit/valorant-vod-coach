package clickhouse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

type Client struct {
	Endpoint   string
	Database   string
	User       string
	Password   string
	HTTPClient *http.Client
}

type Migration struct {
	Version int
	Name    string
	SQL     string
}

type EventRow struct {
	EventID       string `json:"event_id"`
	Topic         string `json:"topic"`
	EventType     string `json:"event_type"`
	EventVersion  int    `json:"event_version"`
	AggregateType string `json:"aggregate_type"`
	AggregateID   string `json:"aggregate_id"`
	OccurredAt    string `json:"occurred_at"`
	Producer      string `json:"producer"`
	CorrelationID string `json:"correlation_id"`
	CausationID   string `json:"causation_id"`
	TraceID       string `json:"trace_id"`
	Payload       string `json:"payload"`
	Envelope      string `json:"envelope"`
}

func (c Client) ApplyMigrations(ctx context.Context, dir string) ([]Migration, error) {
	migrations, err := LoadMigrations(dir)
	if err != nil {
		return nil, err
	}
	for _, migration := range migrations {
		if err := c.Execute(ctx, migration.SQL); err != nil {
			return nil, fmt.Errorf("apply migration %s: %w", migration.Name, err)
		}
	}
	return migrations, nil
}

func LoadMigrations(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	migrations := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := migrationVersion(entry.Name())
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, Migration{
			Version: version,
			Name:    entry.Name(),
			SQL:     string(raw),
		})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	return migrations, nil
}

func (c Client) Execute(ctx context.Context, query string) error {
	return c.post(ctx, strings.NewReader(query))
}

func (c Client) InsertEvent(ctx context.Context, topic string, event domain.EventEnvelope, envelope []byte) error {
	row := EventRow{
		EventID:       event.EventID,
		Topic:         topic,
		EventType:     event.EventType,
		EventVersion:  event.EventVersion,
		AggregateType: event.AggregateType,
		AggregateID:   event.AggregateID,
		OccurredAt:    event.OccurredAt.UTC().Format("2006-01-02 15:04:05.000"),
		Producer:      event.Producer,
		CorrelationID: event.CorrelationID,
		CausationID:   event.CausationID,
		TraceID:       event.TraceID,
		Payload:       string(event.Payload),
		Envelope:      string(envelope),
	}
	raw, err := json.Marshal(row)
	if err != nil {
		return err
	}
	var body bytes.Buffer
	body.WriteString("INSERT INTO kafka_events FORMAT JSONEachRow\n")
	body.Write(raw)
	body.WriteByte('\n')
	return c.post(ctx, &body)
}

func (c Client) post(ctx context.Context, body io.Reader) error {
	endpoint := strings.TrimRight(c.Endpoint, "/")
	if endpoint == "" {
		endpoint = "http://localhost:8123"
	}
	requestURL, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	query := requestURL.Query()
	if c.Database != "" {
		query.Set("database", c.Database)
	}
	requestURL.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL.String(), body)
	if err != nil {
		return err
	}
	if c.User != "" || c.Password != "" {
		request.SetBasicAuth(c.User, c.Password)
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusBadRequest {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("clickhouse status %d: %s", response.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func migrationVersion(name string) (int, error) {
	head, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0, fmt.Errorf("migration %q must start with <version>_", name)
	}
	version, err := strconv.Atoi(head)
	if err != nil {
		return 0, fmt.Errorf("parse migration version %q: %w", name, err)
	}
	return version, nil
}
