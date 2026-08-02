# Valorant VOD Coach Implementation Plan

## Product Goal

Build a personal Valorant VOD analysis system that accepts full match recordings, extracts useful game context, and produces coach-style feedback with concrete timestamps.

The delivered product is local-first and self-hosted, with boundaries that can later be deployed against managed stateful services.

## Delivery Status

As of 2026-08-02, the self-hosted product path is implemented end to end: multi-user upload/library management, durable full-match Temporal processing, CPU CV/OCR evidence selection, guided tactical assessment, coaching/practice reports, PostgreSQL/Redis/S3 persistence, Kafka/ClickHouse analytics, correlated observability, role-protected Operations UI, OCI packaging, CI, and product smoke automation.

The zero-cost default deliberately does not claim unattended AI understanding. Tactical advice is emitted only after the player confirms visible context in an automatically selected clip. Model-assisted review and replay-source adapters are optional future extensions, not unfinished requirements of the guided product.

## Core Workflow

1. Accept a full-match upload or resolve a curated local VOD.
2. Probe the media and sample the requested range at 1 FPS.
3. Validate VALORANT HUD compatibility and compute low-level visual signals.
4. Use staged OCR to confirm buy phase, scoreboard, combat report, and round-end overlays.
5. Build an approximate round timeline from visual/OCR anchors.
6. Select and deduplicate death, fight, rotation, and tempo review moments across the match.
7. Generate short clips and chronological evidence frames for each moment.
8. Ask the player to confirm tactical facts that pixels alone cannot establish.
9. Evaluate only confirmed facts with versioned coaching rules and generate findings, better actions, drills, and checkpoints.

The default strategy is full-match coarse discovery followed by high-resolution human review of selected evidence. An optional VLM may later replace part of the confirmation step only after it passes the same versioned evaluation contract. See `docs/product-and-architecture-decisions.md`.

Architecture diagrams are tracked in `docs/system-diagrams.md`. Benchmarking rules are tracked in `docs/benchmarks.md`.

## Product Functional Scope

### Dataset

- Read `data/manifests/vods.tsv`.
- Validate URLs, ranks, labels, durations, and duplicate video IDs.
- Download enabled videos through `yt-dlp`.
- Store raw videos under `data/raw/youtube/<rank>/`.
- Probe files with `ffprobe` and save metadata as JSON.
- Track local asset status: missing, downloaded, probed, processed, failed.

### Video Processing

- Normalize file naming and metadata.
- Extract low-frequency frames for global analysis.
- Extract higher-frequency clips around candidate events.
- Generate contact sheets for manual inspection; this is implemented in the current local CLI/API pipeline.
- Extract short mp4 review clips for selected gameplay windows; this is implemented in the current local CLI/API pipeline.
- Save all derived artifacts under `data/processed/<vod_id>/`.

### Detection

- Validate whether the VALORANT HUD and minimap regions are usable.
- Compute motion, center activity, killfeed, damage, overlay, and minimap signals.
- Confirm buy phase, scoreboard, combat report, and round-end overlays with staged OCR.
- Build approximate round boundaries from buy-phase visual anchors, with cadence fallback.
- Capture and deduplicate death, fight, rotation, and tempo windows.
- Attach confidence and provenance to detections instead of presenting uncertain data as fact.
- Treat map, agent, rank, score, economy, and hidden tactical intent as user-supplied or unconfirmed unless a future evaluated detector proves them.

### Analysis

- Produce a deterministic CPU report from visual signals and selected gameplay review windows.
- Build estimated round segments from buy-phase anchors or `estimated_from_visual_timeline` fallback segments.
- Generate an evidence-first guided review contract for every selected window.
- Keep optional VLM tasks behind an explicit, disabled-by-default model-review boundary.
- Output findings in a consistent schema:
  - timestamp;
  - round number;
  - category;
  - severity;
  - evidence;
  - recommendation;
  - confidence.
- Keep generated reports as JSON first, then render them in UI.

### UI

- Show a VOD library by rank, map, agent, status, and report readiness.
- Show video player with timeline markers.
- Show per-round review.
- Show coach findings grouped by category and severity.
- Allow manual correction of rank, agent, map, round boundaries, and false detections.
- Export a report as JSON/Markdown.

Implemented product UI:

- browse local VODs and report history;
- play downloaded VOD files through the Go API;
- run sample or full-VOD analysis from the browser;
- show gameplay review windows, timeline markers, clips, and before/event/after evidence;
- guide the player through visible-context questions and show only validated coaching findings;
- show coaching report history and a deduplicated practice plan;
- jump from a review window to the matching timestamp in the local video player;
- open generated mp4 clips for selected gameplay windows.
- create manual corrections for false detections, map/agent/rank/round metadata, findings, and events;
- separate player-facing flows into Dashboard, Library, Review, and Reports pages, with developer/operations data moved into an Admin page.
- provide registration/login with server-side secure cookie sessions. The bootstrap-token holder creates the first `admin`; later users become `user`.
- protect product API, video, artifact, and profiling routes with secure cookie sessions, CSRF commands, roles, and tenant ownership checks.

## Mistake Taxonomy

Start with categories that are visible from first-person VOD:

- positioning;
- crosshair placement;
- peeking and angle isolation;
- timing;
- utility value;
- trading and spacing;
- post-plant decisions;
- retake decisions;
- reload/weapon handling;
- economy and buy decisions;
- minimap awareness;
- unnecessary risk after advantage;
- slow rotation or over-rotation.

Avoid pretending to know hidden team comms or enemy plans unless they are visible from HUD/minimap.

## Architecture

Use Go for durable product code and Python only where the ML ecosystem is clearly better.

```text
cmd/
  vodctl/               # Go CLI: dataset validate, probe, process, report
  vod-web/              # authenticated Go HTTP API and React SPA server
  vod-worker/           # Go background worker for video jobs
  vod-outbox-relay/     # PostgreSQL outbox to Kafka relay
  vod-clickhouse-sink/  # Kafka consumer for analytical projections

internal/
  domain/               # pure product concepts and rules
  app/                  # application use cases and consumed ports
  adapters/
    dataset/            # manifest parsing and dataset inventory
    media/              # ffmpeg/ffprobe wrappers and media primitives
    postgres/           # Postgres repositories and migrations wiring
    clickhouse/         # Kafka consumers, ClickHouse writers, analytical queries
    vodstore/           # tenant-aware local VOD resolution
    s3store/            # S3-compatible object storage
    kafka/              # event publishing, consuming, and outbox relay support
    temporalworkflow/   # Temporal workflow definitions and activities
    localanalysis/      # shared CLI/worker analysis execution
    redissession/       # server-side web sessions
    redislock/          # distributed analysis locks
    redisrate/          # atomic authentication rate limits
    vision/             # local visual heuristic analyzer
    visionservice/      # Python vision-service HTTP client
  platform/             # config, logging, metrics, tracing, health checks

tests/
  integration/          # slow tests requiring real tools or services

ml/
  vision-service/       # Python OCR/VLM service boundary
  prompts/              # prompt templates and expected outputs
  evals/                # small golden-set evaluation cases

web/
  app/                  # React product and Operations UI

deployments/
  compose/              # local Docker Compose infrastructure
  migrations/           # Postgres and ClickHouse schema migrations

data/
  manifests/            # tracked source manifests
  raw/                  # ignored source videos
  processed/            # ignored frames, clips, JSON outputs
```

## Runtime Services and Infrastructure

Use a realistic service stack from the start, but keep the first version runnable locally through Docker Compose.

```text
React/TypeScript UI
  -> Go HTTP API
      -> PostgreSQL: source of truth
      -> MinIO/S3: videos, frames, clips, reports
      -> Kafka: durable domain events and analytics streams
      -> Temporal: durable video-processing workflows
      -> Redis: cache, rate limits, short-lived locks
      -> ClickHouse: high-volume analytical events
      -> Python vision-service: optional evaluated model experiments

Go/Python services
  -> OpenTelemetry Collector
      -> Prometheus: metrics
      -> Loki: logs
      -> Tempo: traces
      -> Grafana: dashboards
```

### Storage Stack

- PostgreSQL is the primary transactional database.
  - Stores VODs, users, uploads, assets, jobs, workflow state references, rounds, findings, reports, manual corrections, model run metadata.
  - Use JSONB only for inspectable intermediate payloads that are small enough to query occasionally.
  - Add `pgvector` later for semantic search over findings, recommendations, and similar mistakes.
- ClickHouse is the analytical/event database.
  - Stores append-only high-volume data: frame sample detections, OCR observations, model-call telemetry, pipeline timings, UI events, report-quality metrics.
  - Do not make ClickHouse the source of truth for user-visible state.
- MinIO locally and S3-compatible storage when hosted.
  - Stores raw VODs, normalized videos, extracted frames, clips, contact sheets, and generated report artifacts.
- Redis.
  - Stores short-lived cache, API rate limits, distributed locks, and temporary processing metadata.
  - It should not be the durable job store.

### Jobs, Queues, and Workflows

- Temporal owns long-running workflows.
  - Example workflow: `ProcessVodWorkflow`.
  - Steps: materialize/probe video, sample frames, detect HUD, run OCR, build timeline, select windows, extract clips, build report, and publish artifacts.
  - Gives retries, timeouts, cancellation, resume, and workflow visibility.
- Kafka is the durable event streaming layer.
  - Publish events like `vod.registered`, `vod.probed`, `frames.extracted`, `timeline.ready`, `report.ready`, `processing.failed`.
  - Use it for status projections, analytics fan-out, replayable delivery into ClickHouse, future billing, and evaluation datasets.
  - Do not use Kafka as the primary workflow engine; Temporal owns long-running process state.
- Use the PostgreSQL outbox pattern for reliable event publication.
  - API and worker write state changes and outbox rows in the same transaction.
  - A Go outbox relay publishes events from PostgreSQL to Kafka.
- Go workers execute deterministic media and orchestration tasks.
- The optional Python service executes only explicitly enabled model-review experiments; default CV/OCR runs in the Go worker with local tools.
- Every job must be idempotent.
  - Re-running a job should reuse existing artifacts when inputs and versions match.
  - Store tool/model/prompt versions on every derived artifact.

### Observability and Diagnostics

- Use structured JSON logs in Go and Python.
- Add OpenTelemetry tracing across API, worker, Temporal activities, and Python service calls.
- Export Prometheus metrics:
  - processing duration per stage;
  - queue/workflow latency;
  - model request count, latency, failures, token/video cost;
  - ffmpeg failures;
  - OCR confidence distributions;
  - report generation success rate.
- Use Grafana dashboards for pipeline health.
- Use Loki for logs and Tempo for distributed traces.
- Add `/healthz`, `/readyz`, and `/metrics` endpoints to every service.
- Add Go `pprof` endpoints for local performance debugging.
- Add Sentry or a similar error tracker later for hosted UI/API exceptions.

## Main Components

### Go CLI

`vodctl` should be built first because it gives fast feedback without needing a server.

Initial commands:

- `vodctl dataset validate`
- `vodctl dataset list`
- `vodctl video probe --vod <label>`
- `vodctl video sample --vod <label>`
- `vodctl analyze run --vod <label>`

### Go API

Start after the local pipeline works.

Responsibilities:

- upload/register VODs;
- show processing status;
- serve video artifacts and reports;
- accept manual corrections;
- expose report JSON.

### Go Worker

The worker owns long-running jobs:

- probe video;
- extract frames;
- run OCR;
- run VLM review;
- build report.

In the target architecture the worker is a Temporal worker with activity implementations in Go. Kafka is used for durable domain events and analytics streaming, not as the workflow engine.

### Optional Python ML Service

Keep model inference behind a simple API boundary:

- `POST /ocr/frame`
- `POST /vision/analyze-window`
- `POST /vision/classify-hud`

The delivered dependency-free server is a contract double and is excluded from product runs. The Go code does not depend on a model implementation; a future service may be added only with reviewed fixtures and an explicit inference budget.

## Data Model

Core entities:

- `Vod`: source URL, rank, label, duration, local asset paths.
- `Asset`: raw video, normalized video, frame, clip, contact sheet.
- `ProcessingJob`: type, status, attempts, error, timestamps.
- `WorkflowRun`: Temporal workflow ID, run ID, status, timestamps.
- `FrameSample`: timestamp, image path, extraction mode.
- `Detection`: kind, timestamp, value, confidence, source.
- `Round`: number, start, end, side, score before/after.
- `Event`: kill, death, spike plant, defuse, buy phase, round end.
- `Finding`: timestamp, category, severity, evidence, recommendation.
- `Report`: VOD summary, round summaries, findings, model versions.
- `ManualCorrection`: user correction for map, agent, rounds, detections, or findings.
- `ModelRun`: model name, prompt version, input artifact hashes, latency, cost, output path.

Store all intermediate outputs as JSON so they can be inspected and replayed.

## Processing Pipeline

```text
Raw MP4
  -> ffprobe metadata
  -> Postgres asset/job records
  -> Temporal ProcessVodWorkflow
  -> normalized asset record
  -> low-frequency frame sampling
  -> derived artifacts in MinIO/S3 or local object storage
  -> HUD/minimap visibility check
  -> OCR and template detection
  -> outbox events
  -> Kafka topics
  -> ClickHouse sink stores high-volume observations
  -> round segmentation
  -> candidate review windows
  -> clip extraction
  -> report JSON
  -> Postgres report/finding records
  -> Kafka report.ready event
  -> UI rendering
```

The product samples broadly, selects candidate regions, and spends human attention only on the most important windows. It incurs no inference charge by default.

## Early Technical Choices

- Go version: current stable local Go.
- CLI: standard library `flag` first, upgrade later only if needed.
- HTTP API: `chi` or standard `net/http`; keep handlers thin.
- Primary database: PostgreSQL from the start.
- Analytical database: ClickHouse for pipeline/event analytics once the first pipeline stages exist.
- Migrations: Goose or Atlas.
- SQL access: `pgx` plus SQLC for typed queries.
- Workflow engine: Temporal for durable long-running VOD processing.
- Event streaming: Kafka in KRaft mode for domain events, pipeline telemetry, status fan-out, and ClickHouse delivery.
- Cache/locks/rate limits: Redis.
- Storage: local filesystem through an object-store interface first, MinIO/S3-compatible storage for local infra and hosted use.
- Video tools: `ffmpeg` and `ffprobe` through thin Go wrappers.
- ML boundary: optional Python HTTP service called from Go. The repository includes a dependency-free contract double, disabled in product runs.
- Observability: OpenTelemetry, Prometheus, Grafana, Loki, Tempo.
- Report format: JSON first, Markdown/HTML later.

## Milestones

### Milestone 0: Local Infrastructure

- Add Docker Compose for PostgreSQL, ClickHouse, Kafka in KRaft mode, Temporal, Redis, MinIO, OpenTelemetry Collector, Prometheus, Grafana, Loki, and Tempo.
- Add `.env.example` for service URLs and credentials.
- Add database migrations for the first Postgres schema.
- Add the initial PostgreSQL `outbox_events` table.
- Add health checks for infra containers.
- Document local startup and reset commands.

Current status:

- `deployments/compose/docker-compose.yml` defines PostgreSQL, Redis, ClickHouse, Kafka in KRaft mode, MinIO, Temporal, OpenTelemetry Collector, Prometheus, Loki, Tempo, and Grafana.
- `.env.example` documents local service URLs and credentials.
- Grafana datasources are provisioned for Prometheus, Loki, and Tempo.
- `vod-web` exposes a Prometheus text metrics endpoint at `/metrics`.
- `vod-web` exposes `/healthz`, `/readyz`, and stdlib Go pprof endpoints under `/debug/pprof/`.
- PostgreSQL migrations define `vods`, `analysis_reports`, `report_artifacts`, and `outbox_events`.
- ClickHouse migrations define the Kafka-engine event table and durable event projection table.

### Milestone 1: Dataset CLI

- Add Go module.
- Implement TSV manifest parser.
- Validate ranks, URLs, labels, and durations.
- List local download status.
- Probe downloaded files with `ffprobe`.
- Store VOD and asset status in PostgreSQL.
- Add unit tests for manifest parsing.

Current status:

- Go module exists.
- `vodctl dataset validate/list/status` exists.
- `vodctl video probe --vod <label>` exists and writes `probe.ffprobe.json`.
- `vodctl video sample --vod <label>` exists and writes sampled frames plus `frames.json`.
- `vodctl analyze run --vod <label>` runs the same production analysis use case without Temporal:
  - manifest lookup;
  - local video resolution;
  - ffprobe metadata extraction;
  - low-frequency frame sampling;
  - CPU HUD/CV observations and staged Tesseract OCR;
  - approximate round segmentation and deduplicated review windows;
  - evidence clip generation and evidence-first coaching candidates;
  - JSON and Markdown report artifacts.
- `internal/domain` contains the first analysis/report schema.
- `internal/app` contains the first orchestration use case and ports.
- `internal/adapters/report` writes local report artifacts.
- `cmd/vod-web` exposes the authenticated HTTP API and built React SPA.
- `web/app` contains the routed React/TypeScript/Vite player and Operations UI.
- Postgres-backed VOD/report/artifact metadata persistence is implemented when `DATABASE_URL` is configured.
- `vod-web` can read report history and latest report metadata from PostgreSQL through the report catalog. Local video availability is still scan-based so the UI reflects files actually present on disk.
- Local manual corrections are implemented through `GET/POST /api/corrections` and JSON artifacts under `data/processed/corrections/`.

### Milestone 1.5: Media Benchmarks

- Measure ffprobe latency on all downloaded VODs.
- Measure frame extraction throughput on a small rank-balanced sample.
- Record benchmark outputs under `data/processed/benchmarks/`.
- Use measured media timings to set realistic SLA targets for `fast`, `standard`, and `deep` modes.
- Evaluate gameplay event precision/recall/F1 against manual labels.

Current status:

- Media benchmark script exists at `scripts/benchmark_video.sh`.
- Gameplay event quality evaluation exists at `vodctl eval run`.
- Example manual labels live under `ml/evals/`.

### Milestone 1.6: Kafka Event Stream

- Add Kafka client wiring in Go.
- Define event envelope and topic names.
- Implement PostgreSQL outbox writer.
- Implement simple outbox relay.
- Publish first lifecycle events from dataset/probe commands.
- Add a ClickHouse sink consumer for pipeline timing events.

### Milestone 1.7: Local Analysis Pipeline

- Add app-layer orchestration for a single VOD analysis run.
- Probe media metadata through the media adapter.
- Extract a configurable low-frequency frame sample.
- Generate a deterministic visual heuristic gameplay report before using a VLM.
- Decode sampled frames, compute visual signals, and select gameplay review windows.
- Build a coach summary with focus areas, phase profile, and practice plan.
- Save `report.json` and `report.md` under `data/processed/<vod_label>/reports/<run_id>/`.
- Keep the analyzer behind a port so evaluated extensions can augment it without changing the use case.

Current status:

- Implemented in `vodctl analyze run` and reused by the Temporal worker.
- Regression-tested on short manually annotated media and benchmarked on a complete 33-minute VOD at 1 FPS; see `docs/benchmarks.md`.
- Current report schema v8 includes gameplay understanding, phase profile, round segments, events, review windows, guided coach decisions, optional model-review records, clips, timeline events, confidence, and frame evidence.
- Tactical recommendations are separate tenant-owned guided assessments and are never inferred from an unconfirmed temporal spike.

### Milestone 2: Frame Extraction

- Extract frames at fixed intervals.
- Generate contact sheets.
- Save `frames.json`.
- Add integration tests with a tiny local fixture video.

### Milestone 2.5: Product Web UI

- Add a React/TypeScript/Vite frontend.
- Add a Go HTTP API server for product interaction.
- Show VOD library, ranks, local download status, report readiness, and latest report.
- Run visual gameplay analysis from the UI.
- Run long analysis through async API jobs and poll job status.
- List generated report runs for the selected VOD.
- Switch between existing reports without rerunning analysis.
- Show gameplay review windows, coach priorities, practice plan, phase profile, signal metrics, findings, timeline events, media stats, contact sheet, and sampled frame evidence.
- Jump from a review window to the matching VOD timestamp.

Current status:

- Implemented through `cmd/vod-web` and `web/app`.
- The default dev setup runs Vite on `127.0.0.1:5173` and calls the Go API on `127.0.0.1:8080` with local CORS. If those ports are occupied, run `vod-web` with `PORT=<free_port>` and start Vite with `VITE_API_BASE=http://localhost:<free_port>`.
- The production-style local setup serves `web/app/dist` from `vod-web`.
- Report history selection is implemented through `GET /api/reports?vod_label=<label>`.

### Milestone 3: First Detection Layer

- Detect HUD/minimap presence.
- Run OCR on timer/score areas.
- Build approximate round boundaries.
- Persist `detections.json` and `rounds.json`.

### Milestone 4: Heuristic Report

- Generate a report without VLM.
- Include deaths, round losses, economy mistakes if visible, and suspicious timings.
- Add confidence levels and explicit guided-review requirements.

### Optional Extension: VLM Clip Review

- Extract candidate clips around deaths and round ends.
- Generate versioned prompt/evaluation fixtures for selected windows.
- Use the runnable dependency-free `vision-service` contract double only for adapter tests.
- Enable a real implementation only after golden fixtures show a quality gain and an inference budget is explicitly approved.

### Milestone 6: Web Review UI

- Show VOD list and processing status.
- Show video timeline with markers.
- Show findings with timestamp jumps.
- Allow manual corrections.

### Milestone 7: Hosted Prototype

- Add API auth.
- Move storage to S3-compatible backend.
- Run Temporal workers separately from API containers.
- Keep Kafka for durable event streaming, ClickHouse delivery, and future integrations.
- Add ClickHouse dashboards for pipeline quality and cost.
- Add observability, rate limits, and cost controls.

## Evaluation Strategy

- Keep a small golden set across ranks.
- Manually label:
  - map;
  - agent;
  - round starts/ends;
  - deaths;
  - 3-5 obvious mistakes per VOD.
- Measure detection precision before adding more model complexity.
- Track false positives separately from missed findings.

## Cost Control

- Do deterministic extraction first.
- Use VLM only on short windows.
- Cache every model response by video ID, timestamp window, model, and prompt version.
- Keep reports reproducible from saved intermediate JSON.

## Current Product Work

Delivered product capabilities:

- Platinum `platinum_sanctifyed_01` search metadata was checked against the downloaded yt-dlp metadata: the uploader description says `Currently Platinum 1`.
- Contact sheet generation for sampled frames.
- Contact sheet preview in the UI evidence area.
- Local Docker Compose infrastructure.
- PostgreSQL migrations, typed DB access, and `outbox_events`.
- Analysis pipeline persistence of VOD/report/artifact metadata in PostgreSQL when `DATABASE_URL` is configured.
- Outbox-to-Kafka relay for `VodProbed`, `FramesExtracted`, and `ReportReady`.
- OpenTelemetry trace setup and structured logs around `vodctl analyze run` and `vod-web`.
- ClickHouse `kafka_events` migration plus `vod-clickhouse-sink` Kafka consumer for `vod.processing.v1` and `vod.lifecycle.v1`.
- Redis-backed analysis locks for repeated local CLI/API requests when `REDIS_URL` is configured.
- PostgreSQL-backed report history reads in `vod-web` as an alternative to filesystem scans.
- Service diagnostics for `vod-web`: liveness, readiness, metrics, and pprof.
- Manual correction capture in the Go API and React UI, saved as reproducible local JSON artifacts.
- Page-based React product UI, local auth endpoints, and admin console for readiness checks, request metrics, logs, users, and service diagnostics.
- Secure cookie authentication, CSRF protection, admin-only profiling/evaluation, and tenant-scoped media/artifact routes.
- PostgreSQL-backed users, uploads, report snapshots, analysis jobs, corrections, and guided reviews with tenant isolation.
- Redis-backed sessions and atomic authentication rate limiting.
- Temporal workflow, independent `vod-worker`, activity retries/heartbeats/cancellation, queued-intent reconciliation, and persisted stage progress.
- S3-compatible uploaded-VOD storage, worker cold-cache materialization, concurrent evidence publication, and authorized artifact cold reads.
- Kafka/ClickHouse operational dashboards plus cross-service OpenTelemetry metrics, logs, and traces.
- Routed React product architecture, job/report history, guided coaching workspace, and live administrator Operations console.
- Non-root multi-stage container image, production-shaped full Compose stack, GitHub CI, and repeatable product smoke automation.

Optional future extensions are a real evaluated clip VLM, a Riot-supported replay source, a broader manually reviewed quality corpus, hosted TLS/secret management/backups, and Kubernetes deployment. They do not change the current guided product contract.
