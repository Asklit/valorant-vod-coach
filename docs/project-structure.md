# Project Structure

Date: 2026-07-21

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
  vod-web/                        # local Go HTTP API and optional static UI server

internal/
  domain/                         # pure product concepts: VOD, media summary, findings, reports
  app/                            # use cases and ports; currently local analysis orchestration
  adapters/
    dataset/                      # TSV manifest parsing and local dataset inventory
    media/                        # ffprobe/ffmpeg probing and frame sampling
    report/                       # local JSON/Markdown report persistence
    vision/                       # local visual heuristic analyzer
    visionservice/                # HTTP client for Python model-review service
    webapi/                       # local HTTP API for React UI
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
  app/                            # React/TypeScript/Vite MVP UI
ml/
  vision-service/                 # Python model-review service boundary
  evals/                          # manual quality-evaluation label fixtures
```

## Target Layout

The project should grow into this shape:

```text
cmd/
  vodctl/                         # local CLI
  vod-api/                        # HTTP API
  vod-worker/                     # Temporal worker
  vod-outbox-relay/               # PostgreSQL outbox to Kafka relay
  vod-clickhouse-sink/            # Kafka consumer for ClickHouse projections

internal/
  domain/
    vod/                          # VOD identity, source metadata, ownership rules
    timeline/                     # rounds, events, candidate windows
    report/                       # findings, recommendations, report schema
    evaluation/                   # golden labels, scoring concepts

  app/
    dataset/                      # validate/import dataset use cases
    processing/                   # process VOD use cases
    reporting/                    # build report use cases
    corrections/                  # manual correction use cases

  adapters/
    dataset/                      # TSV/YouTube manifest adapter
    media/                        # ffprobe/ffmpeg adapter
    postgres/                     # transactional persistence
    clickhouse/                   # analytical persistence
    kafka/                        # producers, consumers, event envelopes
    temporal/                     # workflows and activities
    storage/                      # local filesystem and S3-compatible object storage
    vision/                       # local visual analyzer and future Python vision-service client
    http/                         # HTTP handlers once vod-api exists

  platform/
    config/
    logging/
    observability/
    health/

ml/
  vision-service/                 # Python OCR/CV/VLM service boundary
  prompts/
  evals/

web/
  app/                            # React UI

deployments/
  compose/                        # local infrastructure
  migrations/
    postgres/
    clickhouse/
```

## When To Add A Layer

Do not create abstractions only because the diagram has a box.

Add `internal/app` use cases when one operation coordinates multiple adapters. Examples:

- register VOD in PostgreSQL and write an outbox event;
- probe video, write asset metadata, and publish lifecycle event;
- sample frames, persist artifact records, and emit processing telemetry.
- run the local MVP analysis pipeline across dataset, media, analyzer, and report adapters.

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
