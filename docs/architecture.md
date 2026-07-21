# Architecture Notes

The project is Go-first, service-oriented, and designed to look like a real production system while still being runnable locally.

## Service Map

```text
React/TypeScript web app
  -> Go API
      -> PostgreSQL
      -> MinIO/S3
      -> Temporal
      -> Kafka
      -> Redis
      -> ClickHouse
      -> Python vision-service

Go API / Go workers / Python vision-service
  -> OpenTelemetry Collector
      -> Prometheus
      -> Loki
      -> Tempo
      -> Grafana
```

Detailed Mermaid diagrams are available in [system-diagrams.md](system-diagrams.md).

## Language Boundaries

- Go owns product logic, API, CLI, workers, media orchestration, database access, and report assembly.
- Python owns OCR, computer vision, model experiments, and Qwen/VLM inference.
- TypeScript/React owns the browser UI.
- SQL owns durable schemas and analytical queries.

## Storage Roles

- PostgreSQL is the source of truth for VODs, assets, reports, findings, users, manual corrections, and workflow references.
- ClickHouse stores high-volume immutable analytics: frame detections, OCR observations, model-call telemetry, pipeline timings, and UI events.
- MinIO locally and S3-compatible storage in hosted environments store videos, frames, clips, contact sheets, and report artifacts.
- Redis stores cache, rate limits, short-lived locks, and temporary processing state.

## Async Processing

- Temporal runs durable VOD processing workflows.
- Kafka stores durable domain and pipeline events such as `vod.registered`, `vod.probed`, `frames.extracted`, `timeline.ready`, `report.ready`, and `processing.failed`.
- A Go outbox relay publishes PostgreSQL outbox rows into Kafka so database writes and event publication stay reliable.
- Kafka consumers project event data into ClickHouse and later support status projections, notifications, billing, and evaluation datasets.
- Go workers execute deterministic activities: ffprobe, ffmpeg, artifact registration, timeline/report assembly.
- Python service executes OCR/CV/VLM activities through a stable HTTP boundary.

## Agreed Deployment Direction

Start local-first with Docker Compose. The default local stack runs PostgreSQL, ClickHouse, Redis, Kafka in KRaft mode, Temporal, MinIO, and the observability stack locally.

For a hosted prototype, keep the same service boundaries:

- host Go API and Go workers as containers;
- keep PostgreSQL as the transactional source of truth;
- keep ClickHouse for append-only processing analytics;
- move artifacts to S3-compatible storage when local MinIO is no longer enough;
- run Qwen/VLM either inside the Python `vision-service` on a local GPU host or through an external GPU provider behind the same `vision-service` API;
- keep Temporal self-hosted at first unless managed Temporal cost becomes justified.
- keep Kafka self-hosted for the MVP; move to managed Kafka only if hosted traffic and operational needs justify it.

The important rule is that external GPU providers must remain implementation details of `vision-service`. The rest of the product should not care whether Qwen runs locally, on RunPod, Modal, or another provider.

## Proposed Layout

```text
cmd/
  vodctl/               # Go CLI
  vod-api/              # Go HTTP API
  vod-worker/           # Go Temporal worker
internal/
  dataset/              # manifest parsing, local dataset metadata
  db/                   # Postgres repositories
  analytics/            # Kafka consumers, ClickHouse writers, analytical queries
  storage/              # local FS and S3-compatible object storage
  video/                # ffmpeg probing, frame extraction, clip slicing
  workflows/            # Temporal workflow definitions
  events/               # Kafka event publishing, consuming, and outbox relay
  vision/               # Python vision-service client
  analysis/             # timeline, mistake taxonomy, report assembly
  observability/        # logging, metrics, tracing, health checks
ml/
  vision-service/       # Python/FastAPI OCR and VLM service
  prompts/              # prompt/eval fixtures
web/
  app/                  # React/TypeScript UI
deployments/
  compose/              # local Docker Compose stack
  migrations/           # Postgres and ClickHouse migrations
data/
  manifests/            # tracked source manifests
  raw/                  # ignored downloaded originals
  processed/            # ignored local frames, clips, OCR, timelines
scripts/
  download_vods.sh      # local dataset bootstrap
```

## Dataset Rules

- Keep source videos in a manifest, not hardcoded in application code.
- Use only full game VODs for baseline analysis.
- Avoid livestream archives during early MVP because they add menus, pauses, queue time, chat overlays, and inconsistent cuts.
- Keep raw videos immutable; write derived clips/frames to object storage or `data/processed/`.
- Store rank confidence explicitly. `title` means the rank appears in the video title; `search_metadata` means the rank came from search context and should be checked manually from the HUD.
