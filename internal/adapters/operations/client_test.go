package operations

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSnapshotAggregatesFixedMetricsAndStructuredLogs(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	client := Client{
		PrometheusURL: "http://operations.test", LokiURL: "http://operations.test",
		HTTPClient: &http.Client{Transport: operationsTransport{now: now}},
		Clock:      func() time.Time { return now },
	}
	snapshot := client.Snapshot(context.Background(), time.Hour)

	if len(snapshot.Errors) != 0 {
		t.Fatalf("snapshot errors = %+v", snapshot.Errors)
	}
	if len(snapshot.Series) != len(dashboardQueries) {
		t.Fatalf("series = %d, want %d", len(snapshot.Series), len(dashboardQueries))
	}
	for _, series := range snapshot.Series {
		if series.ID == "" || len(series.Points) != 1 || series.Points[0].Value != 2.5 {
			t.Fatalf("unexpected series: %+v", series)
		}
	}
	if len(snapshot.Logs) != 1 || snapshot.Logs[0].Service != "vod-worker" || snapshot.Logs[0].Message != "analysis failed" || snapshot.Logs[0].TraceID != "abc" {
		t.Fatalf("unexpected logs: %+v", snapshot.Logs)
	}
}

type operationsTransport struct{ now time.Time }

func (t operationsTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var body string
	switch request.URL.Path {
	case "/api/v1/query_range":
		if request.URL.Query().Get("query") == "" || request.URL.Query().Get("step") == "" {
			return nil, fmt.Errorf("Prometheus request is missing fixed query parameters")
		}
		body = fmt.Sprintf(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[%d,"2.5"]]}]}}`, t.now.Unix())
	case "/loki/api/v1/query_range":
		if query := request.URL.Query().Get("query"); query != `{compose_project="valorant-vod-coach"}` {
			return nil, fmt.Errorf("unexpected Loki query %q", query)
		}
		body = fmt.Sprintf(`{"status":"success","data":{"resultType":"streams","result":[{"stream":{"service_name":"vod-worker"},"values":[["%d","{\"level\":\"error\",\"msg\":\"analysis failed\",\"trace_id\":\"abc\"}"]]}]}}`, t.now.UnixNano())
	default:
		return nil, fmt.Errorf("unexpected operations path %q", request.URL.Path)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    request,
	}, nil
}

func TestSnapshotDoesNotAcceptNonHTTPBackends(t *testing.T) {
	client := Client{PrometheusURL: "file:///etc/passwd", LokiURL: "gopher://localhost"}
	snapshot := client.Snapshot(context.Background(), time.Hour)
	if len(snapshot.Errors) != len(dashboardQueries)+1 {
		t.Fatalf("errors = %d, want %d: %+v", len(snapshot.Errors), len(dashboardQueries)+1, snapshot.Errors)
	}
	for _, message := range snapshot.Errors {
		if !strings.Contains(message, "unsupported operations backend scheme") {
			t.Fatalf("unexpected error: %s", message)
		}
	}
}
