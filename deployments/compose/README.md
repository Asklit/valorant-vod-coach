# Local Infrastructure

This compose stack is the local production-shaped product environment. It is intended for development and integration testing; production deployment uses independently managed stateful services or their supported Kubernetes operators/Helm charts.

## Services

- `vod-web`: React product UI, authenticated Go API, and workflow dispatcher.
- `vod-worker`: Temporal media-analysis worker with ffmpeg, ffprobe, and Tesseract.
- `vod-outbox-relay`: transactional PostgreSQL outbox publisher.
- `vod-clickhouse-sink`: Kafka consumer, dead-letter handling, and analytical projections.
- PostgreSQL: transactional source of truth.
- Redis: sessions, distributed locks, rate limits, and cache.
- Kafka in KRaft mode: durable domain and pipeline event stream.
- ClickHouse: analytical event and pipeline telemetry store.
- MinIO: local S3-compatible artifact storage.
- Temporal: durable VOD processing workflows.
- OpenTelemetry Collector, Prometheus, Alloy, Loki, Tempo, Grafana: correlated metrics, container logs, traces, alerts, and dashboards.

## Start

```sh
cp .env.example .env
docker compose --env-file .env -f deployments/compose/docker-compose.yml up -d --build --wait
```

Replace `VODCOACH_BOOTSTRAP_TOKEN` in `.env` before the first registration. The token creates only the first administrator and is not a user password.

Useful URLs:

- Product UI: http://localhost:8090
- Grafana: http://localhost:3000, login `admin` / `admin`
- Prometheus: http://localhost:9090
- Temporal UI: http://localhost:8233
- MinIO console: http://localhost:9001
- MinIO S3 API: http://localhost:9002
- ClickHouse HTTP: http://localhost:8123
- Alloy status: http://localhost:12345

Grafana provisions `Operations` and `Product analytics` in the `Valorant VOD Coach` folder. ClickHouse queries use the read-only local `grafana_reader` account. Prometheus loads `prometheus-alerts.yml`; no notification receiver is configured for local development.

Run the HTTP and authorization smoke after the stack becomes healthy:

```sh
set -a
source .env
set +a
./scripts/smoke_compose.sh
```

The smoke checks liveness, readiness, direct SPA routes, unauthenticated rejection, administrator authentication, tenant library access, telemetry, CSRF logout, and cleans up its temporary cookie files. On a reused volume, set `SMOKE_EMAIL` and `SMOKE_PASSWORD` to an existing administrator.

`vod-web` service diagnostics:

- `GET /healthz`: liveness;
- `GET /readyz`: manifest, local storage, and optional vision-service readiness;
- `GET /metrics`: Prometheus text metrics;
- `GET /debug/pprof/`: local Go profiling index.

## Runtime Behavior

PostgreSQL and ClickHouse migrations run under service-level locks during application startup. Kafka topics and the private MinIO bucket are created by one-shot init services. No manual ordering is required.

When `vodctl analyze run` or `vod-web` receives a `DATABASE_URL`, successful analysis runs are persisted into:

- `vods`
- `analysis_reports`
- `report_artifacts`
- `outbox_events`

With `DATABASE_URL`, `vod-web` also reads report history and latest report metadata from PostgreSQL. The report JSON/Markdown files remain artifact payloads referenced by the database rows.

When `REDIS_URL` is configured, analysis runs acquire a Redis-backed lock per VOD before ffprobe/ffmpeg work starts. Use the default `redis://localhost:6379/0` from `.env.example` for local duplicate-run protection.

To run a service on the host for debugging, stop its container and use the public addresses from `.env.example`. For example:

```sh
docker compose --env-file .env -f deployments/compose/docker-compose.yml stop vod-worker
go run ./cmd/vod-worker
```

Invalid event envelopes are copied to `vod.dead-letter.v1`. Valid events are queried through the deduplicated `kafka_events_deduplicated`, `analysis_runs`, and `frame_extractions` ClickHouse views.

Export worker, API, relay, and sink telemetry through the Collector:

```sh
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
export APP_ENV=local
export SERVICE_VERSION="$(git rev-parse --short HEAD)"
```

The generic OTLP endpoint is expanded to `/v1/traces` and `/v1/metrics`. Signal-specific `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` and `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` values override it when needed.

See [docs/observability.md](../../docs/observability.md) for metric cardinality, trace propagation, delivery semantics, and the Docker socket security boundary.

## Delivery Boundaries

Compose is the reproducible single-host development and demonstration environment. A hosted deployment must use TLS termination, secret management, backups, restricted console ports, Kafka/Redis/PostgreSQL authentication, and independently managed stateful services or supported Kubernetes operators. The image itself runs as UID/GID `10001` and contains no source VODs, generated artifacts, credentials, or build toolchains.

## Stop

```sh
docker compose --env-file .env -f deployments/compose/docker-compose.yml down
```

Remove local infrastructure data:

```sh
docker compose --env-file .env -f deployments/compose/docker-compose.yml down -v
```
