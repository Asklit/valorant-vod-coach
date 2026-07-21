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

## Stop

```sh
docker compose --env-file .env -f deployments/compose/docker-compose.yml down
```

Remove local infrastructure data:

```sh
docker compose --env-file .env -f deployments/compose/docker-compose.yml down -v
```
