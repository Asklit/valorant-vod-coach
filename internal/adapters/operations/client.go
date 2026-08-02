package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Client struct {
	PrometheusURL string
	LokiURL       string
	HTTPClient    *http.Client
	Clock         func() time.Time
}

type Snapshot struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Window      string            `json:"window"`
	Series      []MetricSeries    `json:"series"`
	Logs        []LogEntry        `json:"logs"`
	Errors      map[string]string `json:"errors,omitempty"`
}

type MetricSeries struct {
	ID     string        `json:"id"`
	Label  string        `json:"label"`
	Unit   string        `json:"unit"`
	Points []MetricPoint `json:"points"`
}

type MetricPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

type LogEntry struct {
	Timestamp time.Time         `json:"timestamp"`
	Service   string            `json:"service"`
	Level     string            `json:"level,omitempty"`
	Message   string            `json:"message"`
	TraceID   string            `json:"trace_id,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

type metricQuery struct {
	ID    string
	Label string
	Unit  string
	Query string
}

var dashboardQueries = []metricQuery{
	{ID: "api_request_rate", Label: "API requests", Unit: "req/s", Query: `sum(rate(vodcoach_http_requests_total[5m]))`},
	{ID: "api_error_ratio", Label: "API error ratio", Unit: "ratio", Query: `sum(rate(vodcoach_http_requests_total{status=~"5.."}[5m])) / clamp_min(sum(rate(vodcoach_http_requests_total[5m])), 0.001)`},
	{ID: "active_jobs", Label: "Active jobs", Unit: "jobs", Query: `sum(vodcoach_analysis_jobs_total{status=~"queued|running"})`},
	{ID: "analysis_p95", Label: "Analysis p95", Unit: "s", Query: `histogram_quantile(0.95, sum by (le) (rate(vodcoach_analysis_activity_duration_bucket[15m])))`},
	{ID: "outbox_failure_rate", Label: "Outbox failures", Unit: "events/s", Query: `sum(rate(vodcoach_outbox_publish_total{outcome="failed"}[5m]))`},
	{ID: "sink_failure_rate", Label: "Sink failures", Unit: "events/s", Query: `sum(rate(vodcoach_clickhouse_sink_messages_total{outcome=~"insert_failed|commit_failed|dlq_failed"}[5m]))`},
}

func (c Client) Snapshot(ctx context.Context, window time.Duration) Snapshot {
	window = normalizeWindow(window)
	now := c.now()
	snapshot := Snapshot{
		GeneratedAt: now,
		Window:      window.String(),
		Series:      make([]MetricSeries, len(dashboardQueries)),
		Errors:      map[string]string{},
	}

	var wait sync.WaitGroup
	var resultMu sync.Mutex
	for index, query := range dashboardQueries {
		wait.Add(1)
		go func() {
			defer wait.Done()
			series, err := c.queryRange(ctx, query, now.Add(-window), now, rangeStep(window))
			resultMu.Lock()
			defer resultMu.Unlock()
			if err != nil {
				snapshot.Errors["prometheus."+query.ID] = err.Error()
				return
			}
			snapshot.Series[index] = series
		}()
	}
	wait.Add(1)
	go func() {
		defer wait.Done()
		logs, err := c.queryLogs(ctx, now.Add(-window), now, 200)
		resultMu.Lock()
		defer resultMu.Unlock()
		if err != nil {
			snapshot.Errors["loki"] = err.Error()
			return
		}
		snapshot.Logs = logs
	}()
	wait.Wait()

	if len(snapshot.Errors) == 0 {
		snapshot.Errors = nil
	}
	return snapshot
}

func (c Client) queryRange(ctx context.Context, query metricQuery, start, end time.Time, step time.Duration) (MetricSeries, error) {
	series := MetricSeries{ID: query.ID, Label: query.Label, Unit: query.Unit}
	if strings.TrimSpace(c.PrometheusURL) == "" {
		return series, fmt.Errorf("Prometheus URL is not configured")
	}
	endpoint, err := endpointURL(c.PrometheusURL, "/api/v1/query_range", url.Values{
		"query": []string{query.Query},
		"start": []string{strconv.FormatInt(start.Unix(), 10)},
		"end":   []string{strconv.FormatInt(end.Unix(), 10)},
		"step":  []string{strconv.FormatInt(int64(step.Seconds()), 10)},
	})
	if err != nil {
		return series, err
	}
	var response prometheusResponse
	if err := c.getJSON(ctx, endpoint, &response); err != nil {
		return series, err
	}
	if response.Status != "success" {
		return series, fmt.Errorf("Prometheus query failed: %s", response.Error)
	}
	if len(response.Data.Result) == 0 {
		return series, nil
	}
	for _, rawPoint := range response.Data.Result[0].Values {
		point, ok := parseMetricPoint(rawPoint)
		if ok {
			series.Points = append(series.Points, point)
		}
	}
	return series, nil
}

func (c Client) queryLogs(ctx context.Context, start, end time.Time, limit int) ([]LogEntry, error) {
	if strings.TrimSpace(c.LokiURL) == "" {
		return nil, fmt.Errorf("Loki URL is not configured")
	}
	endpoint, err := endpointURL(c.LokiURL, "/loki/api/v1/query_range", url.Values{
		"query":     []string{`{compose_project="valorant-vod-coach"}`},
		"start":     []string{strconv.FormatInt(start.UnixNano(), 10)},
		"end":       []string{strconv.FormatInt(end.UnixNano(), 10)},
		"limit":     []string{strconv.Itoa(limit)},
		"direction": []string{"backward"},
	})
	if err != nil {
		return nil, err
	}
	var response lokiResponse
	if err := c.getJSON(ctx, endpoint, &response); err != nil {
		return nil, err
	}
	if response.Status != "success" {
		return nil, fmt.Errorf("Loki query failed")
	}
	logs := make([]LogEntry, 0, limit)
	for _, stream := range response.Data.Result {
		for _, raw := range stream.Values {
			if len(raw) != 2 {
				continue
			}
			nanoseconds, err := strconv.ParseInt(raw[0], 10, 64)
			if err != nil {
				continue
			}
			entry := parseLogLine(raw[1])
			entry.Timestamp = time.Unix(0, nanoseconds).UTC()
			entry.Labels = stream.Stream
			entry.Service = firstNonEmpty(entry.Service, stream.Stream["service_name"], stream.Stream["container"])
			logs = append(logs, entry)
		}
	}
	sort.Slice(logs, func(i, j int) bool { return logs[i].Timestamp.After(logs[j].Timestamp) })
	if len(logs) > limit {
		logs = logs[:limit]
	}
	return logs, nil
}

func (c Client) getJSON(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusBadRequest {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return fmt.Errorf("status %d: %s", response.StatusCode, strings.TrimSpace(string(raw)))
	}
	return json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(target)
}

type prometheusResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		Result []struct {
			Values [][]json.RawMessage `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

type lokiResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func parseMetricPoint(raw []json.RawMessage) (MetricPoint, bool) {
	if len(raw) != 2 {
		return MetricPoint{}, false
	}
	var timestamp float64
	var valueRaw string
	if err := json.Unmarshal(raw[0], &timestamp); err != nil {
		return MetricPoint{}, false
	}
	if err := json.Unmarshal(raw[1], &valueRaw); err != nil {
		return MetricPoint{}, false
	}
	value, err := strconv.ParseFloat(valueRaw, 64)
	if err != nil {
		return MetricPoint{}, false
	}
	return MetricPoint{Timestamp: time.UnixMilli(int64(timestamp * 1000)).UTC(), Value: value}, true
}

func parseLogLine(line string) LogEntry {
	entry := LogEntry{Message: line}
	var fields map[string]any
	if json.Unmarshal([]byte(line), &fields) != nil {
		return entry
	}
	entry.Message = stringField(fields, "msg", "message")
	entry.Level = stringField(fields, "level", "severity")
	entry.TraceID = stringField(fields, "trace_id")
	entry.Service = stringField(fields, "service", "service_name")
	if entry.Message == "" {
		entry.Message = line
	}
	return entry
}

func endpointURL(base, path string, query url.Values) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(base, "/") + path)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported operations backend scheme %q", parsed.Scheme)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (c Client) now() time.Time {
	if c.Clock != nil {
		return c.Clock().UTC()
	}
	return time.Now().UTC()
}

func normalizeWindow(window time.Duration) time.Duration {
	if window < 15*time.Minute {
		return time.Hour
	}
	if window > 7*24*time.Hour {
		return 7 * 24 * time.Hour
	}
	return window
}

func rangeStep(window time.Duration) time.Duration {
	step := window / 240
	if step < 15*time.Second {
		return 15 * time.Second
	}
	if step > 15*time.Minute {
		return 15 * time.Minute
	}
	return step
}

func stringField(fields map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := fields[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "unknown"
}
