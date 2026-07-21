# Product and Architecture Decisions

Date: 2026-07-21

This document fixes the project direction at a middle+/senior engineering level. The goal is not to build a thin AI wrapper, but to build a production-like VOD analysis system with clear boundaries, reproducible processing, observability, and evaluation.

## Target Outcome

The project outcome is a local-first Valorant VOD coach that can later become a hosted product.

The system should:

- register or upload a match recording;
- process it through a durable workflow;
- extract technical media metadata, frames, clips, OCR observations, and match timeline;
- identify candidate review windows;
- run AI analysis only where it adds value;
- produce a structured report with timestamps, evidence, confidence, and recommendations;
- show the report in a web UI next to a video player;
- allow manual corrections that improve future evaluation.

The interview demo should show a complete flow:

```text
Open UI
  -> choose VOD
  -> start processing
  -> watch live workflow status
  -> open generated timeline
  -> click a mistake marker
  -> inspect clip, evidence, confidence, recommendation
  -> submit a manual correction
  -> see metrics/traces for the processing workflow
```

## Product Scope

### MVP

The MVP is not "perfect AI coach". The MVP is a reliable processing platform with a first useful report.

MVP must include:

- Go CLI for dataset validation and probing;
- Go API for VOD registration, processing status, reports, and artifacts;
- Go Temporal worker for VOD processing;
- Python/FastAPI vision service for OCR/CV/VLM calls;
- PostgreSQL schema and migrations;
- MinIO/S3-compatible artifact storage;
- Redis for cache, locks, and rate limits;
- Kafka for durable domain events, pipeline telemetry, status projections, and analytics streaming;
- OpenTelemetry traces, Prometheus metrics, Loki logs, Grafana dashboards;
- React/TypeScript review UI;
- tests for manifest parsing, media probing, timeline building, report schema, and API contracts.

### Non-goals for MVP

- Do not train a custom gameplay model from scratch.
- Do not rely on hidden team comms, enemy intent, or invisible information.
- Do not pretend rank can be reliably inferred from gameplay style.
- Do not parse private Riot replay internals unless Riot exposes a stable public format/API.
- Do not send an entire raw 30-40 minute video to a VLM as the default path.

## User Workflow

The user-facing system has three processing modes.

### Fast Mode

Fast mode is deterministic and cheap.

```text
VOD
  -> ffprobe
  -> frame sampling
  -> OCR/HUD/minimap checks
  -> approximate round timeline
  -> heuristic report
```

Use it for local iteration, smoke tests, and cheap feedback.

### Standard Mode

Standard mode is the default product mode.

```text
VOD
  -> full coarse scan
  -> timeline and candidate event detection
  -> short clip extraction
  -> VLM review on selected windows
  -> structured report
```

This is the expected best production tradeoff. It gives the model enough context while controlling cost, latency, and hallucinations.

### Deep Mode

Deep mode is an experimental mode for quality comparison.

```text
VOD or replay capture
  -> full coarse semantic pass over sampled frames
  -> per-round summaries
  -> selected high-resolution clip review
  -> report with higher context budget
```

Deep mode does not mean blindly uploading one huge raw video to the model. It means the whole match is represented through sampled frames, OCR, per-round summaries, and selected clips. It is allowed to be slower and more expensive, but it must be measurable.

## Full Demo/VOD vs Selected Clips

We should test both approaches and keep the best one based on data.

The candidate strategies are:

- `clip_only`: detect key windows, send only those clips to VLM.
- `full_coarse`: analyze the full match through sampled frames and OCR, no high-resolution clip deep dive.
- `hybrid`: full coarse pass plus VLM deep review on selected windows.

The default should be `hybrid` unless evaluation proves otherwise.

Why not raw full-VOD VLM as default:

- a 30-40 minute video is expensive to process;
- latency is too high for normal usage;
- context compression hides evidence;
- model output is harder to audit;
- retrying a failed run is costly;
- small timestamp mistakes become hard to debug;
- the UI needs concrete events and clips, not only a narrative summary.

What full-match analysis is useful for:

- global context;
- economy and side-switch context;
- repeated mistakes across rounds;
- macro patterns like late rotations, over-aggression after advantage, poor retake timing;
- ranking report findings by frequency and severity.

Therefore the professional design is not "full VOD or clips". It is:

```text
full match for structure and context
selected windows for high-resolution reasoning
```

## Replay System Strategy

As of 2026-07-21, VALORANT has an official Replay system. Riot's replay overview says replays provide all 10 first-person perspectives, third-person free cam, round skipping, time jumping, kill/death/ultimate timeline icons, HUD/minimap toggles, and combat report access. Riot also announced replays for Custom games in Patch 12.00 and friend replay sharing in Patch 12.10.

Sources:

- https://playvalorant.com/en-us/news/dev/replays-everything-you-need-to-know/
- https://playvalorant.com/en-us/news/game-updates/valorant-patch-notes-12-00/
- https://playvalorant.com/en-us/news/game-updates/valorant-patch-notes-12-10/

Project decision:

- Support uploaded MP4/MKV/WebM recordings first because they are stable and easy to process.
- Add a `ReplayCaptureSource` later if the user can record or export footage from the in-game replay viewer.
- Do not depend on private replay file parsing in MVP.
- If Riot later exposes a stable export/API, add it behind a `SourceAdapter` without changing the rest of the pipeline.

Source adapter interface:

```text
SourceAdapter
  -> Register(ctx, source)
  -> Probe(ctx, vodID)
  -> OpenVideo(ctx, vodID)
  -> ExtractMetadata(ctx, vodID)
```

Initial adapters:

- `LocalVideoSource`: uploaded/local MP4/MKV/WebM.
- `YouTubeSource`: curated dataset bootstrap through `yt-dlp`.
- `ReplayCaptureSource`: future adapter for replay-derived captures.

## Evaluation Plan

The analysis strategy must be chosen by evaluation, not taste.

Use a golden set:

- 2 VODs per rank from Iron to Radiant;
- at least 1 manually annotated VOD per low/mid/high rank bucket;
- tiny fixture video for CI;
- manually labeled map, agent, death windows, round starts/ends, and 3-5 obvious findings.

Measure:

- round boundary precision/recall;
- death window precision/recall;
- OCR accuracy for timer/score;
- finding usefulness score from manual review;
- false positive rate;
- hallucination rate;
- processing latency;
- estimated model cost;
- retry/failure rate;
- report reproducibility.

Run all three analysis strategies on the same VODs:

```text
clip_only
full_coarse
hybrid
```

Keep `hybrid` as default if it wins on quality/cost. Keep `deep` as an opt-in mode for experiments and difficult VODs.

## Architecture Decisions

### Languages

- Go is the core product language.
- Python is allowed only for ML/CV/OCR where the ecosystem is materially better.
- TypeScript/React is used for the UI.
- SQL is first-class and versioned through migrations.

### Data Stores

- PostgreSQL is the source of truth.
- ClickHouse is for analytics and high-volume immutable observations.
- Redis is for transient cache, locks, and rate limits.
- MinIO/S3-compatible storage is for large artifacts.

### Async

- Temporal owns long-running processing workflows.
- Kafka owns durable domain events, pipeline telemetry, and replayable analytics streams.
- PostgreSQL outbox is used so state changes and event publication stay reliable.
- Redis is not used as a durable queue.

### Observability

Every service must expose:

- `/healthz`;
- `/readyz`;
- `/metrics`;
- structured JSON logs;
- OpenTelemetry traces;
- service/version labels.

The demo should include at least one Grafana dashboard and one distributed trace for `ProcessVodWorkflow`.

## Quality Bar

The project should include:

- ADR-style documentation for major decisions;
- Docker Compose local environment;
- migrations for PostgreSQL and ClickHouse;
- typed DB access with `pgx` and SQLC;
- idempotent jobs and artifact versioning;
- deterministic replay of reports from saved intermediate JSON;
- unit tests for domain logic;
- integration tests for infrastructure boundaries;
- contract tests between Go and Python;
- CI with lint, tests, and build;
- explicit failure states in UI and API;
- manual correction flow;
- model/prompt versioning;
- cost and latency tracking for model calls.

## Security and Compliance

The first hosted version must define:

- upload size limits;
- accepted file formats;
- file retention policy;
- user data deletion path;
- rate limits;
- auth boundary;
- private-by-default VOD storage;
- no public redistribution of third-party YouTube VODs;
- clear separation between personal dataset assets and product uploads.

## Interview Narrative

The project should be presented as:

> I built a production-like video analysis platform in Go. The hard part is not calling an AI model; the hard part is turning an unstructured 30-minute game recording into reliable, inspectable domain data, then using AI only where it improves the result.

The strongest technical discussion points:

- why full raw video is not the default model input;
- how the hybrid strategy is evaluated;
- why Temporal and Kafka have separate roles;
- why Postgres and ClickHouse are both used;
- how the outbox pattern avoids losing Kafka events after database commits;
- how idempotency and artifact versioning make the pipeline reproducible;
- how observability makes long-running media workflows debuggable;
- how manual corrections feed evaluation instead of being a throwaway UI feature.
