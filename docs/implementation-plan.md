# Valorant VOD Coach Implementation Plan

## Product Goal

Build a personal Valorant VOD analysis system that accepts full match recordings, extracts useful game context, and produces coach-style feedback with concrete timestamps.

This starts as a local-first learning project and should be designed so it can later become a hosted product.

## Core Workflow

1. Collect full game VODs from the curated manifest.
2. Probe and normalize videos into a consistent local dataset.
3. Extract frames, thumbnails, audio metadata, and low-level visual signals.
4. Detect match structure: map, agent, side, score, rounds, deaths, kills, buys, and key HUD states.
5. Build a round timeline from visual/OCR events.
6. Select important review windows around deaths, lost rounds, spikes, clutches, retakes, and obvious utility usage.
7. Ask a vision model to analyze only selected windows, not the entire video.
8. Merge model observations with deterministic timeline data.
9. Generate a report with mistakes, timestamps, severity, and practice recommendations.

The default product strategy should be hybrid: full-match coarse processing for structure and context, plus high-resolution AI review on selected clips. Full-match semantic analysis is kept as an experimental `deep` mode and must be evaluated against `clip_only` and `hybrid` before becoming default. See `docs/product-and-architecture-decisions.md`.

Architecture diagrams are tracked in `docs/system-diagrams.md`. Benchmarking rules are tracked in `docs/benchmarks.md`.

## MVP Functional Scope

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
- Generate contact sheets for manual inspection.
- Save all derived artifacts under `data/processed/<vod_id>/`.

### Detection

- Detect whether minimap and HUD are visible.
- Detect map name from loading screen or scoreboard when available.
- Detect selected agent from HUD/portrait/ability icons.
- Detect round boundaries from timer/score changes.
- OCR round timer, score, killfeed, combat report, and scoreboard.
- Capture player death windows and end-of-round windows.
- Mark confidence for every detection instead of pretending uncertain data is reliable.

### Analysis

- Produce a first report from heuristics before using a large vision model.
- Add VLM analysis only for selected review windows.
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
  vod-api/              # Go HTTP API for uploads, reports, assets
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
    storage/            # local FS, later S3-compatible storage
    kafka/              # event publishing, consuming, and outbox relay support
    temporal/           # Temporal workflow definitions and activities
    vision/             # OCR and ML service clients
  platform/             # config, logging, metrics, tracing, health checks

tests/
  integration/          # slow tests requiring real tools or services

ml/
  vision-service/       # Python FastAPI service for OCR/VLM inference
  prompts/              # prompt templates and expected outputs
  evals/                # small golden-set evaluation cases

web/
  app/                  # later UI

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
      -> Python vision-service: OCR, CV, Qwen/VLM inference

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
  - Steps: probe video, normalize, sample frames, detect HUD, run OCR, build timeline, select windows, extract clips, run VLM review, build report.
  - Gives retries, timeouts, cancellation, resume, and workflow visibility.
- Kafka is the durable event streaming layer.
  - Publish events like `vod.registered`, `vod.probed`, `frames.extracted`, `timeline.ready`, `report.ready`, `processing.failed`.
  - Use it for status projections, analytics fan-out, replayable delivery into ClickHouse, future billing, and evaluation datasets.
  - Do not use Kafka as the primary workflow engine; Temporal owns long-running process state.
- Use the PostgreSQL outbox pattern for reliable event publication.
  - API and worker write state changes and outbox rows in the same transaction.
  - A Go outbox relay publishes events from PostgreSQL to Kafka.
- Go workers execute deterministic media and orchestration tasks.
- Python workers/services execute CV, OCR, and VLM inference tasks.
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

### Python ML Service

Keep model inference behind a simple API boundary:

- `POST /ocr/frame`
- `POST /vision/analyze-window`
- `POST /vision/classify-hud`

The Go code should not depend on a specific model implementation. The Python service can run Qwen-VL-compatible models, OCR libraries, or local experiments.

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
  -> VLM analysis per selected clip
  -> report JSON
  -> Postgres report/finding records
  -> Kafka report.ready event
  -> UI rendering
```

The first useful version should not analyze every frame. It should sample broadly, detect candidate regions, then spend expensive model calls only on the most important windows.

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
- ML boundary: Python FastAPI service, called from Go.
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
- `vodctl analyze run --vod <label>` exists and runs the local MVP pipeline:
  - manifest lookup;
  - local video resolution;
  - ffprobe metadata extraction;
  - low-frequency frame sampling;
  - deterministic baseline observations;
  - JSON and Markdown report artifacts.
- `internal/domain` contains the first analysis/report schema.
- `internal/app` contains the first orchestration use case and ports.
- `internal/adapters/report` writes local report artifacts.
- `cmd/vod-web` exposes the local HTTP API used by the React MVP UI.
- `web/app` contains the React/TypeScript/Vite UI for browsing VODs and running baseline analysis.
- Postgres-backed status is not implemented yet.

### Milestone 1.5: Media Benchmarks

- Measure ffprobe latency on all downloaded VODs.
- Measure frame extraction throughput on a small rank-balanced sample.
- Record benchmark outputs under `data/processed/benchmarks/`.
- Use measured media timings to set realistic SLA targets for `fast`, `standard`, and `deep` modes.

### Milestone 1.6: Kafka Event Stream

- Add Kafka client wiring in Go.
- Define event envelope and topic names.
- Implement PostgreSQL outbox writer.
- Implement simple outbox relay.
- Publish first lifecycle events from dataset/probe commands.
- Add a ClickHouse sink consumer for pipeline timing events.

### Milestone 1.7: Local MVP Analysis Pipeline

- Add app-layer orchestration for a single VOD analysis run.
- Probe media metadata through the media adapter.
- Extract a configurable low-frequency frame sample.
- Generate a deterministic baseline report before using a VLM.
- Save `report.json` and `report.md` under `data/processed/<vod_label>/reports/<run_id>/`.
- Keep the analyzer behind a port so the Python Qwen/VLM service can replace or augment it.

Current status:

- Implemented in `vodctl analyze run`.
- Smoke-tested on `diamond_crazies_01` with `--duration 10s --fps 1`.
- Current report is intentionally baseline-only and marks AI gameplay analysis as not enabled.

### Milestone 2: Frame Extraction

- Extract frames at fixed intervals.
- Generate contact sheets.
- Save `frames.json`.
- Add integration tests with a tiny local fixture video.

### Milestone 2.5: Local Web UI

- Add a React/TypeScript/Vite frontend.
- Add a Go HTTP API server for local MVP interaction.
- Show VOD library, ranks, local download status, report readiness, and latest report.
- Run baseline analysis from the UI.
- List generated report runs for the selected VOD.
- Switch between existing reports without rerunning analysis.
- Show findings, timeline events, media stats, and sampled frame evidence.

Current status:

- Implemented through `cmd/vod-web` and `web/app`.
- The dev setup runs Vite on `127.0.0.1:5173` and calls the Go API on `127.0.0.1:8080` with local CORS.
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
- Add confidence levels and manual TODO markers.

### Milestone 5: VLM Clip Review

- Extract candidate clips around deaths and round ends.
- Send selected windows to the Python ML service.
- Merge VLM observations into the report schema.
- Add prompt/eval fixtures.

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

## Immediate Next Steps

1. Manually check the Platinum item marked `search_metadata`.
2. Add contact sheet generation for sampled frames so reports are easier to review manually.
3. Add contact sheet preview to the report evidence area.
4. Add local Docker Compose infrastructure.
5. Add Postgres migrations, typed DB access, and `outbox_events`.
6. Persist VOD/assets/report metadata in PostgreSQL from the analysis pipeline.
7. Publish the first `vod.probed`, `frames.extracted`, and `report.ready` events through the outbox-to-Kafka path.
8. Add the first OpenTelemetry traces and structured logs around `vodctl analyze run` and `vod-web`.
