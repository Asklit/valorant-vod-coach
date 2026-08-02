# Project Structure

Date: 2026-08-02

The project uses a Go-first modular monolith with ports/adapters boundaries and DDD-lite naming. This keeps the codebase simple while preserving the important dependency rule: product logic should not depend on infrastructure details.

## Architecture Choice

Clean Architecture is still useful as a set of principles:

- keep business rules independent from frameworks;
- keep infrastructure replaceable;
- test core behavior without external services;
- make dependencies point inward.

The folder-heavy version of Clean Architecture is not the best default for this Go project. It often creates too many packages such as `entities`, `usecases`, `repositories`, `controllers`, and `presenters` before the code needs them.

The chosen approach is:

```text
modular monolith
  + ports and adapters
  + DDD-lite domain language
  + vertical use cases when workflows appear
```

This is a better fit because the project has several external systems, but the product itself should remain easy to run locally and easy to reason about.

## Dependency Rule

```text
cmd/*
  -> internal/app
      -> internal/domain
      -> ports defined near use cases
  -> internal/adapters
      -> internal/domain
  -> internal/platform
```

Rules:

- `internal/domain` must not import adapters, platform, databases, Kafka, Temporal, HTTP, or ffmpeg.
- `internal/app` owns use-case orchestration and depends on domain plus small interfaces.
- `internal/adapters` owns real implementations for files, media tools, databases, Kafka, Temporal, object storage, and model clients.
- `cmd/*` wires dependencies and exposes executables.
- Interfaces should usually live where they are consumed, not in a global `ports` package.

## Current Layout

```text
cmd/
  vodctl/                         # CLI entrypoint for local operations
  vod-web/                        # authenticated Go HTTP API and static React SPA server
  vod-worker/                     # Temporal worker for durable full-VOD analysis
  vod-outbox-relay/               # PostgreSQL outbox to Kafka relay
  vod-clickhouse-sink/            # Kafka event sink into ClickHouse

internal/
  domain/                         # pure product concepts: VOD, media summary, findings, reports, corrections
  app/                            # use cases and ports; local analysis, evaluation, corrections, auth, persistence
  adapters/
    dataset/                      # TSV manifest parsing and local dataset inventory
    media/                        # ffprobe/ffmpeg probing and frame sampling
    postgres/                     # PostgreSQL migrations, metadata persistence, outbox access
    redislock/                    # Redis-backed analysis locks
    redissession/                 # Redis-backed secure browser sessions
    redisrate/                    # atomic Redis authentication rate limiter
    vodstore/                     # tenant-aware curated/uploaded VOD resolution
    s3store/                      # S3/MinIO object storage and artifact transfers
    kafka/                        # Kafka outbox event producer
    clickhouse/                   # ClickHouse HTTP migrations and event inserts
    report/                       # local JSON/Markdown report persistence
    vision/                       # local visual heuristic analyzer
    visionservice/                # HTTP client for Python model-review service
    localanalysis/                # owner-scoped local pipeline dependency composition
    temporalworkflow/             # Temporal workflow, activities, launcher, dispatcher
    operations/                   # bounded Prometheus/Loki admin aggregator
    webapi/                       # local HTTP API for React UI, auth sessions, and admin diagnostics
  platform/                       # config/logging/observability/runtime helpers

scripts/
  download_vods.sh                # dataset download helper
  benchmark_video.sh              # shell media benchmark helper
  check_git_index.sh              # pre-commit safety check for large/generated files
  run_vision_service.sh           # dependency-free local Python vision-service stub

docs/                             # architecture and project decisions
deployments/
  compose/                        # local Docker Compose infrastructure
  migrations/                     # Postgres and ClickHouse migration roots
data/
  manifests/                      # tracked curated dataset manifest
  raw/                            # ignored local videos
  processed/                      # ignored generated artifacts
tests/
  integration/                    # integration/e2e tests that need real services or tools
web/
  app/                            # React 19/TypeScript/Vite product UI
    src/app/                      # routing and application composition
    src/pages/                    # player and operator page features
    src/shared/                   # typed HTTP and shared UI utilities
ml/
  vision-service/                 # Python model-review service boundary
  evals/                          # manual quality-evaluation label fixtures
```

## Evolution Policy

The flat `internal/domain` and `internal/app` packages are intentional. Split them into domain-specific packages only when ownership, dependency cycles, or review cost demonstrate a real boundary. Do not duplicate `vod-web` with a second API binary until the API and SPA need independent scaling or release lifecycles. Keep optional model code behind `visionservice`; default media, CV, and OCR orchestration stays in Go adapters.

## When To Add A Layer

Do not create abstractions only because the diagram has a box.

Add `internal/app` use cases when one operation coordinates multiple adapters. Examples:

- register VOD in PostgreSQL and write an outbox event;
- probe video, write asset metadata, and publish lifecycle event;
- sample frames, persist artifact records, and emit processing telemetry.
- run the local analysis pipeline across dataset, media, analyzer, and report adapters.

Add a domain package when behavior or invariants appear. Examples:

- round boundary rules;
- finding severity rules;
- report reproducibility rules;
- manual correction rules.

Keep simple technical wrappers in adapters. Examples:

- running `ffprobe`;
- reading a TSV file;
- uploading an object to S3;
- publishing to Kafka.

The frontend follows the same dependency intent without mirroring Go packages. Route-level features own private state, `shared` does not import pages, and `app` composes features. See [Web Product Architecture](web-product.md).
