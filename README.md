# Valorant VOD Coach

Go-first Valorant VOD analysis project.

Current scope:

- keep a curated VOD manifest in `data/manifests/vods.tsv`;
- download only full game VODs, not stream archives;
- normalize downloads to mp4 through `yt-dlp` and `ffmpeg`;
- store raw videos outside git under `data/raw/youtube/<rank>/`.
- run a local MVP gameplay review pipeline that writes reproducible JSON and Markdown reports.

Planned product stack:

- Go API, CLI, and workers;
- Python/FastAPI vision service for OCR, CV, and Qwen/VLM inference;
- React/TypeScript web UI;
- PostgreSQL as the primary database;
- ClickHouse for high-volume pipeline analytics;
- Temporal for durable video-processing workflows;
- Kafka for durable domain events and analytics streaming;
- Redis for cache, locks, and rate limits;
- MinIO/S3-compatible object storage for videos and artifacts;
- OpenTelemetry, Prometheus, Grafana, Loki, and Tempo for diagnostics.

## Current Architecture

Kafka is the agreed MVP event streaming layer.

```mermaid
flowchart LR
  API[Go API]
  Worker[Go Temporal Worker]
  PG[(PostgreSQL)]
  Outbox[(PostgreSQL Outbox)]
  Relay[Go Outbox Relay]
  Kafka[(Kafka Event Stream)]
  Consumers[Go Kafka Consumers]
  CH[(ClickHouse)]
  Temporal[(Temporal)]
  Redis[(Redis)]
  S3[(MinIO / S3)]
  Vision[Python Vision Service]

  API --> PG
  API --> Outbox
  API --> Temporal
  API --> Redis
  API --> S3
  Temporal --> Worker
  Worker --> PG
  Worker --> Outbox
  Worker --> S3
  Worker --> Vision
  Outbox --> Relay
  Relay --> Kafka
  Kafka --> Consumers
  Consumers --> CH
  Consumers --> PG
```

## Prerequisites

```sh
brew install yt-dlp ffmpeg
```

Alternative:

```sh
pipx install yt-dlp
brew install ffmpeg
```

## Download

Preview selected videos:

```sh
./scripts/download_vods.sh --print-only
```

Download all enabled VODs:

```sh
./scripts/download_vods.sh
```

Download one rank:

```sh
./scripts/download_vods.sh --rank diamond
```

The downloader is intentionally not run automatically. Review `data/manifests/vods.tsv` before downloading.

## Planning

- [Architecture notes](docs/architecture.md)
- [System diagrams](docs/system-diagrams.md)
- [Project structure](docs/project-structure.md)
- [Testing strategy](docs/testing-strategy.md)
- [Implementation plan](docs/implementation-plan.md)
- [Product and architecture decisions](docs/product-and-architecture-decisions.md)
- [Kafka event streaming](docs/kafka-event-streaming.md)
- [Git workflow](docs/git-workflow.md)
- [Benchmarks](docs/benchmarks.md)

## Benchmarks

Preview a benchmark run:

```sh
./scripts/benchmark_video.sh --rank diamond --limit 1 --print-only
```

Run a quick media benchmark:

```sh
./scripts/benchmark_video.sh --rank diamond --limit 1 --sample-seconds 180 --fps 1
```

Run a named benchmark:

```sh
./scripts/benchmark_video.sh --run-id media-smoke --rank diamond --limit 1 --sample-seconds 60 --fps 1
```

## Go CLI

Build the CLI:

```sh
go build -o bin/vodctl ./cmd/vodctl
```

Validate the curated manifest:

```sh
go run ./cmd/vodctl dataset validate
```

List enabled VODs:

```sh
go run ./cmd/vodctl dataset list
```

Show local download status:

```sh
go run ./cmd/vodctl dataset status
```

Probe one downloaded VOD with `ffprobe`:

```sh
go run ./cmd/vodctl video probe --vod diamond_crazies_01
```

Extract a short frame sample:

```sh
go run ./cmd/vodctl video sample --vod diamond_crazies_01 --duration 30s --fps 1
```

Run the local MVP analysis pipeline:

```sh
go run ./cmd/vodctl analyze run --vod diamond_crazies_01
```

Fast smoke run:

```sh
go run ./cmd/vodctl analyze run --vod diamond_crazies_01 --run-id smoke_mvp --duration 10s --fps 1 --force
```

The command writes:

- `data/processed/<vod_label>/probe.ffprobe.json`
- `data/processed/<vod_label>/frames/<sample_name>/frames.json`
- `data/processed/<vod_label>/frames/<sample_name>/contact_sheet.jpg`
- `data/processed/<vod_label>/frames/<sample_name>/gameplay_review.json`
- `data/processed/<vod_label>/clips/<run_id>/review_*.mp4`
- `data/processed/<vod_label>/clips/<run_id>/review_clips.json`
- `data/processed/<vod_label>/reports/<run_id>/report.json`
- `data/processed/<vod_label>/reports/<run_id>/report.md`

The current analyzer is a local visual heuristic gameplay reviewer. It validates ingestion, media quality, and sample coverage, decodes sampled JPG frames, estimates motion/HUD/minimap/center-screen signals, builds estimated round segments for navigation, selects gameplay review windows, extracts short mp4 review clips for those windows, builds a coach summary with focus areas and a practice plan, generates evidence links, and writes reproducible reports with recommendations, confidence, timeline events, and review-window metadata.

This is already useful for local VOD review and benchmarking, but it is not the final Qwen/VLM coach. The next ML stage will replace or enrich the same `ObservationAnalyzer` port with OCR, round detection, kill/death windows, and model reasoning over selected clips.

After building, the same commands can be run through `bin/vodctl`.

## Web UI

The local MVP UI is a React/TypeScript/Vite app backed by a Go API server.

Start the Go API:

```sh
go run ./cmd/vod-web
```

Start the React dev server in another terminal:

```sh
cd web/app
npm install
npm run dev
```

Open:

```text
http://127.0.0.1:5173
```

The UI can:

- browse the curated VOD library;
- filter by rank and search text;
- show downloaded/report-ready status;
- play downloaded local VOD files through the Go API;
- run the local heuristic analysis pipeline against a sample window or the full VOD through async analysis jobs;
- switch between generated report runs for a selected VOD;
- render gameplay review windows, coach priorities, practice plan, phase profile, visual signal metrics, findings, recommendations, timeline events, media stats, contact sheets, and sampled frame evidence;
- render estimated round segments and attach review windows to those segments;
- jump from a selected review window to the matching VOD timestamp in the local video player;
- open generated review clips for selected gameplay windows.

Production-style local serving:

```sh
cd web/app
npm run build
cd ../..
go run ./cmd/vod-web --static-dir web/app/dist
```
