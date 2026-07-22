# Local Infrastructure

This compose stack is the local production-shaped environment for the MVP.

## Services

- PostgreSQL: transactional source of truth.
- Redis: cache, locks, rate limits, temporary job state.
- Kafka in KRaft mode: durable domain and pipeline event stream.
- ClickHouse: analytical event and pipeline telemetry store.
- MinIO: local S3-compatible artifact storage.
- Temporal: durable VOD processing workflows.
- OpenTelemetry Collector, Prometheus, Loki, Tempo, Grafana: metrics, logs, traces, dashboards.

## Start

```sh
cp .env.example .env
docker compose --env-file .env -f deployments/compose/docker-compose.yml up -d
```

Useful URLs:

- Grafana: http://localhost:3000, login `admin` / `admin`
- Prometheus: http://localhost:9090
- Temporal UI: http://localhost:8233
- MinIO console: http://localhost:9001
- MinIO S3 API: http://localhost:9002
- ClickHouse HTTP: http://localhost:8123

`vod-web` service diagnostics:

- `GET /healthz`: liveness;
- `GET /readyz`: manifest, local storage, and optional vision-service readiness;
- `GET /metrics`: Prometheus text metrics;
- `GET /debug/pprof/`: local Go profiling index.

## Database and Outbox

Apply the PostgreSQL schema after the stack is healthy:

```sh
go run ./cmd/vodctl db migrate \
  --database-url "${DATABASE_URL:-postgres://vodcoach:vodcoach@localhost:5432/vodcoach?sslmode=disable}"
```

When `vodctl analyze run` or `vod-web` receives a `DATABASE_URL`, successful analysis runs are persisted into:

- `vods`
- `analysis_reports`
- `report_artifacts`
- `outbox_events`

With `DATABASE_URL`, `vod-web` also reads report history and latest report metadata from PostgreSQL. The report JSON/Markdown files remain artifact payloads referenced by the database rows.

When `REDIS_URL` is configured, analysis runs acquire a Redis-backed lock per VOD before ffprobe/ffmpeg work starts. Use the default `redis://localhost:6379/0` from `.env.example` for local duplicate-run protection.

Publish pending outbox rows to Kafka:

```sh
go run ./cmd/vod-outbox-relay \
  --database-url "${DATABASE_URL:-postgres://vodcoach:vodcoach@localhost:5432/vodcoach?sslmode=disable}" \
  --brokers "${KAFKA_BROKERS:-localhost:9092}"
```

Sink Kafka events into ClickHouse:

```sh
go run ./cmd/vod-clickhouse-sink \
  --brokers "${KAFKA_BROKERS:-localhost:9092}" \
  --clickhouse-url "${CLICKHOUSE_URL:-http://localhost:8123}" \
  --clickhouse-db "${CLICKHOUSE_DB:-vodcoach}"
```

## Stop

```sh
docker compose --env-file .env -f deployments/compose/docker-compose.yml down
```

Remove local infrastructure data:

```sh
docker compose --env-file .env -f deployments/compose/docker-compose.yml down -v
```
