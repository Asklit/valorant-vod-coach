# Observability

The product uses one correlated diagnostic path rather than independent logging and metrics islands.

```text
HTTP request span
  -> durable AnalysisJobRequest W3C context
  -> Temporal workflow headers
  -> analysis activity span and metrics
  -> PostgreSQL transactional outbox W3C context
  -> Kafka headers
  -> ClickHouse sink span
  -> deduplicated ClickHouse analytical views
```

## Responsibilities

- OpenTelemetry SDK: creates application metrics and traces and adds trace IDs to structured logs.
- OpenTelemetry Collector: receives OTLP, batches telemetry, exposes metrics for Prometheus, and forwards traces to Tempo.
- Prometheus: stores numeric time series and evaluates pipeline alert rules.
- Tempo: stores distributed traces without using trace fields as high-cardinality metric labels.
- Alloy: discovers explicitly labelled Docker containers and sends their logs to Loki.
- Loki: stores and searches logs. A derived `trace_id` field links a log line to Tempo.
- ClickHouse: stores immutable product events and supports product-quality and throughput analytics.
- Grafana: provides the `Operations` and `Product analytics` dashboards over all four stores.

Temporal still owns workflow state and retries. Kafka still owns replayable domain-event delivery. Telemetry describes those systems; it does not replace either one.

## Metrics

Application metrics intentionally avoid user IDs, VOD labels, job IDs, run IDs, trace IDs, paths, and error messages as labels.

| Metric prefix | Signals |
| --- | --- |
| `vodcoach_http_*` | API request rate, status, and latency |
| `vodcoach_analysis_activity_*` | activity attempts, active work, and execution duration |
| `vodcoach_analysis_jobs_finalized_*` | completed, failed, and cancelled workflows |
| `vodcoach_analysis_progress_failures_*` | failed progress read-model writes |
| `vodcoach_outbox_*` | claimed events, publish outcomes, and relay latency |
| `vodcoach_clickhouse_sink_*` | storage outcomes, fetch failures, processing latency, and consumer lag |

OpenTelemetry names containing dots are normalized to underscores by the Collector Prometheus exporter. Counters receive the conventional `_total` suffix.

## Dashboards And Alerts

Open `http://localhost:3000` and use the provisioned `Valorant VOD Coach` folder:

- `Operations`: API health, active jobs, worker throughput, outbox delivery, sink outcomes, and service logs.
- `Product analytics`: completed analyses, findings, detected rounds, review windows, extraction profile, analyzer versions, and recent runs.

Prometheus evaluates alerts for API/Collector availability, elevated HTTP error rate, failed workflows, outbox publication failures, and sink persistence failures. Local alert rules intentionally have no paging receiver.

## Delivery And Failure Handling

The outbox relay reclaims a `publishing` row after a two-minute stale lock. This can publish an event more than once, which is required for crash-safe at-least-once delivery.

The sink inserts before committing the Kafka offset. A commit failure can therefore create duplicate ClickHouse rows. Dashboards query `kafka_events_deduplicated`, `analysis_runs`, and `frame_extractions`, which collapse rows by `event_id` using the latest ingestion timestamp.

Malformed envelopes are permanent failures. The sink publishes the original bytes and source coordinates to `vod.dead-letter.v1`, then commits the source offset only after DLQ publication succeeds. ClickHouse/network errors are transient and leave the source offset uncommitted.

## Local Security Note

Alloy mounts the Docker socket read-only for local container discovery. Read-only socket access still exposes broad daemon metadata and must not be copied into a shared production cluster. Production should use Kubernetes discovery with namespace-scoped RBAC or a node-level logging agent.

The local ClickHouse datasource uses a dedicated `grafana_reader` account with `SELECT` grants rather than the ClickHouse administrative account. Local passwords in Compose are development credentials only.

## Runtime Configuration

Set these variables on Go services:

```text
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
APP_ENV=local
SERVICE_VERSION=<git-sha-or-release>
LOG_FORMAT=json
LOG_LEVEL=info
```

When Go processes run directly on the host, their JSON logs remain on stderr; Alloy discovers Docker containers only. Containerized product services carry the `vodcoach.logs=enabled` label and are collected automatically.
