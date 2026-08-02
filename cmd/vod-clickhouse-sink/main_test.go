package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/asklit/valorant-vod-coach/internal/adapters/clickhouse"
	kafkago "github.com/segmentio/kafka-go"
)

func TestHandleMessageClassifiesInvalidEnvelopeAsPermanent(t *testing.T) {
	tests := []string{
		`not-json`,
		`{"event_id":"event-1"}`,
	}
	for _, value := range tests {
		err := handleMessage(context.Background(), clickhouse.Client{}, kafkago.Message{Value: []byte(value)})
		if !isPermanentMessageError(err) {
			t.Fatalf("handleMessage(%q) error = %v, want permanent", value, err)
		}
	}
}

func TestPublishDeadLetterPreservesPayloadAndCoordinates(t *testing.T) {
	writer := &captureKafkaWriter{}
	source := kafkago.Message{
		Topic: "vod.processing.v1", Partition: 2, Offset: 42,
		Key: []byte("vod-1"), Value: []byte("invalid"),
		Headers: []kafkago.Header{{Key: "traceparent", Value: []byte("parent")}},
	}

	if err := publishDeadLetter(context.Background(), writer, "vod.dead-letter.v1", source, io.ErrUnexpectedEOF); err != nil {
		t.Fatalf("publishDeadLetter: %v", err)
	}
	if len(writer.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(writer.messages))
	}
	message := writer.messages[0]
	if message.Topic != "vod.dead-letter.v1" || string(message.Key) != "vod-1" || string(message.Value) != "invalid" {
		t.Fatalf("unexpected dead letter message: %+v", message)
	}
	headers := map[string]string{}
	for _, header := range message.Headers {
		headers[header.Key] = string(header.Value)
	}
	if headers["dlq_original_topic"] != source.Topic || headers["dlq_original_partition"] != "2" || headers["dlq_original_offset"] != "42" {
		t.Fatalf("source coordinates were not preserved: %+v", headers)
	}
	if headers["dlq_error"] == "" || headers["traceparent"] != "parent" {
		t.Fatalf("diagnostic headers were not preserved: %+v", headers)
	}
}

func TestHandleMessageInsertsValidEnvelope(t *testing.T) {
	transport := &okTransport{}
	client := clickhouse.Client{Endpoint: "http://clickhouse.test", HTTPClient: &http.Client{Transport: transport}}
	value := `{"event_id":"event-1","event_type":"ReportReady","event_version":1,"occurred_at":"2026-08-02T12:00:00Z","producer":"test","aggregate_type":"vod","aggregate_id":"vod-1","payload":{}}`

	err := handleMessage(context.Background(), client, kafkago.Message{Topic: "vod.lifecycle.v1", Value: []byte(value)})
	if err != nil {
		t.Fatalf("handleMessage: %v", err)
	}
	if !strings.Contains(transport.body, `"event_id":"event-1"`) {
		t.Fatalf("ClickHouse insert body = %q", transport.body)
	}
}

type captureKafkaWriter struct{ messages []kafkago.Message }

func (w *captureKafkaWriter) WriteMessages(_ context.Context, messages ...kafkago.Message) error {
	w.messages = append(w.messages, messages...)
	return nil
}

type okTransport struct{ body string }

func (t *okTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	t.body = string(raw)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
		Request:    request,
	}, nil
}
