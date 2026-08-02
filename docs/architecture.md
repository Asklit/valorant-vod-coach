# Architecture Notes

The project is Go-first, service-oriented, and designed to look like a real production system while still being runnable locally.

## Service Map

```text
React/TypeScript web app
  -> Go API
      -> PostgreSQL
      -> MinIO/S3
      -> Temporal
      -> Redis
      -> Python vision-service (optional extension)

PostgreSQL outbox -> Go relay -> Kafka -> Go sink -> ClickHouse

Go API / Go background processes
  -> OpenTelemetry Collector -> Prometheus / Tempo -> Grafana
  -> structured container stdout -> Alloy -> Loki -> Grafana
```

Detailed Mermaid diagrams are available in [system-diagrams.md](system-diagrams.md). The repository layout and testing policy are documented in [project-structure.md](project-structure.md) and [testing-strategy.md](testing-strategy.md).

The `vod-web` process exposes Prometheus text metrics at `/metrics`. Compose Prometheus scrapes it through service DNS at `vod-web:8090`; all Go processes also export OTLP metrics and traces through the Collector.

## Delivered Product Path

The browser path uses durable state and workflows:

```text
React route -> vod-web
  -> secure cookie session + CSRF + tenant authorization
  -> PostgreSQL job intent + outbox event
  -> Temporal workflow
      -> vod-worker activities
          -> S3/local source materialization
          -> ffprobe media metadata
          -> ffmpeg full-match sampling
          -> VALORANT HUD compatibility CV
          -> staged Tesseract overlay OCR
          -> round/event timeline
          -> evidence-window selection and deduplication
          -> ffmpeg review clips
          -> evidence-first coach candidates
          -> JSON/Markdown reports
          -> concurrent S3 artifact publication
      -> PostgreSQL terminal state + outbox event
  -> guided visible-context assessment
  -> versioned coaching rule
  -> validated recommendation and practice drill

PostgreSQL outbox
  -> vod-outbox-relay
  -> Kafka domain streams
  -> vod-clickhouse-sink
  -> deduplicated ClickHouse projections
```

The CLI `vodctl analyze run` reuses `app.AnalysisRunner` and the same media, analyzer, report, persistence, lock, and object-storage ports without Temporal. It is the fast debugging and benchmark surface, not a separate implementation.

Important dependency boundaries:

- report, event, coaching, correction, and evaluation schemas live in `internal/domain`;
- use-case orchestration and consumed ports live in `internal/app`;
- ffmpeg, Tesseract, PostgreSQL, Redis, S3, Kafka, ClickHouse, Temporal, HTTP, and optional model clients stay in adapters;
- review clips and evidence are artifact references, so the coaching engine does not depend on ffmpeg or storage;
- temporal candidates are not tactical mistakes; `EvidenceCoachEngine` emits advice only from context confirmed in a guided assessment;
- model review remains behind `ModelReviewer`, but the deterministic Python contract double is excluded from product runs and Compose.

The React client has stable Dashboard, Library, Review, Reports, and Operations routes. In development Vite proxies to `vod-web`; the released non-root image serves the built SPA and Go API from one origin.

## Language Boundaries

- Go owns product logic, API, CLI, workers, media orchestration, database access, and report assembly.
- Python owns optional model experiments behind the `vision-service` contract. The default OCR/CV path is implemented in Go and invokes local Tesseract/ffmpeg tools.
- TypeScript/React owns the browser UI.
- SQL owns durable schemas and analytical queries.

## Storage Roles

- PostgreSQL is the source of truth for VODs, assets, reports, findings, users, manual corrections, and workflow references.
- ClickHouse stores immutable lifecycle/processing events and derived analysis-run and frame-extraction projections. Reserved event contracts can later add observation or model telemetry without changing the transactional store.
- MinIO locally and S3-compatible storage in hosted environments store videos, frames, clips, contact sheets, and report artifacts.
- Redis stores cache, rate limits, short-lived locks, and temporary processing state.

## Async Processing

- Temporal runs durable VOD processing workflows.
- Kafka stores the currently emitted `VodProbed`, `FramesExtracted`, and `ReportReady` domain events with versioned envelopes.
- A Go outbox relay publishes PostgreSQL outbox rows into Kafka so database writes and event publication stay reliable.
- Kafka consumers project event data into ClickHouse and later support status projections, notifications, billing, and evaluation datasets.
- Go workers execute deterministic activities: ffprobe, ffmpeg, artifact registration, timeline/report assembly.
- The optional Python service is a stable boundary for explicitly enabled, evaluated model experiments; it is not used by the default workflow.

## Agreed Deployment Direction

Run the complete local-first product with Docker Compose. The default stack includes PostgreSQL, ClickHouse, Redis, Kafka in KRaft mode, Temporal, MinIO, and the observability stack.

For a hosted prototype, keep the same service boundaries:

- host Go API and Go workers as containers;
- keep PostgreSQL as the transactional source of truth;
- keep ClickHouse for append-only processing analytics;
- move artifacts to S3-compatible storage when local MinIO is no longer enough;
- keep any future evaluated VLM implementation behind the Python `vision-service`; it is not required by the zero-cost product path;
- keep Temporal self-hosted at first unless managed Temporal cost becomes justified.
- keep Kafka self-hosted initially; move to managed Kafka only if hosted traffic and operational needs justify it.

Any future inference runtime remains an implementation detail of `vision-service`. The rest of the product does not depend on a model vendor or execution provider.

## Repository Layout

The Go code follows a modular monolith with ports/adapters boundaries. See [project-structure.md](project-structure.md) for the full rationale.

```text
cmd/
  vodctl/               # Go CLI
  vod-web/              # authenticated Go HTTP API and static SPA server
  vod-worker/           # Go Temporal worker
  vod-outbox-relay/     # Go PostgreSQL outbox to Kafka relay
  vod-clickhouse-sink/  # Go Kafka consumer for ClickHouse projections
internal/
  domain/               # pure product concepts and rules
  app/                  # application use cases and consumed ports
  adapters/
    dataset/            # manifest parsing, local dataset metadata
    media/              # ffmpeg probing, frame extraction, clip slicing
    report/             # local JSON/Markdown report writer
    webapi/             # local HTTP API for the React UI
    postgres/           # Postgres repositories
    clickhouse/         # ClickHouse writers and analytical queries
    kafka/              # event publishing, consuming, outbox relay support
    temporalworkflow/   # Temporal workflow definitions and activities
    vodstore/           # local curated/uploaded VOD resolution
    s3store/            # S3-compatible object storage
    redissession/       # server-side web sessions
    redislock/          # distributed analysis locks
    redisrate/          # atomic authentication rate limits
    localanalysis/      # worker/CLI analysis execution service
    vision/             # local visual heuristic analyzer
    visionservice/      # Python vision-service HTTP client
  platform/             # config, logging, metrics, tracing, health checks
ml/
  vision-service/       # Python OCR and VLM service boundary
  evals/                # reviewed quality fixtures
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
  run_vision_service.sh # local dependency-free vision-service stub
```

## Dataset Rules

- Keep source videos in a manifest, not hardcoded in application code.
- Use only full game VODs for baseline analysis.
- Avoid livestream archives in the baseline corpus because they add menus, pauses, queue time, chat overlays, and inconsistent cuts.
- Keep raw videos immutable; write derived clips/frames to object storage or `data/processed/`.
- Store rank confidence explicitly. `title` means the rank appears in the video title; `search_metadata` means the rank came from search context and should be checked manually from the HUD.
