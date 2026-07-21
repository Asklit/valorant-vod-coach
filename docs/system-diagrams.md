# System Diagrams

Date: 2026-07-21

These diagrams describe the agreed target architecture. They are written in Mermaid so they render directly in GitHub, many IDEs, and documentation tools without requiring an external diagramming service.

Current decision: Kafka is the MVP event streaming layer.

## Implemented Local MVP

This flow is implemented by `vodctl analyze run`. It runs locally and writes artifacts under ignored `data/processed/`.

```mermaid
flowchart LR
  user[Developer]
  cli[vodctl analyze run]
  ui[React Vite UI]
  api[vod-web Go API]
  runner[app.AnalysisRunner]
  resolver[dataset.LocalVODResolver]
  media[media.LocalProcessor]
  ffprobe[ffprobe]
  frames[ffmpeg frames]
  sheet[ffmpeg contact sheet]
  baseline[BaselineObservationAnalyzer]
  report[report.LocalStore]
  raw[(data/raw/youtube)]
  processed[(data/processed)]

  user --> cli
  user --> ui
  ui --> api
  api --> raw
  cli --> runner
  api --> runner
  api --> processed
  runner --> resolver
  resolver --> raw
  runner --> media
  media --> ffprobe
  media --> frames
  media --> sheet
  media --> processed
  runner --> baseline
  runner --> report
  report --> processed
```

## Agreed Architecture

The system is a Go-first video analysis platform with a narrow Python ML boundary.

```mermaid
flowchart LR
  user[User]
  web[React Web UI]
  cli[Go CLI<br/>vodctl]

  subgraph go[Go Product Layer]
    api[Go API vod-api]
    worker[Go Temporal Worker vod-worker]
    relay[Go Outbox Relay]
    sink[Go Kafka Consumers]
    domain[Domain Logic<br/>timeline, reports, findings]
  end

  subgraph workflow[Workflow and Events]
    temporal[Temporal durable workflows]
    kafka[Kafka durable event stream]
  end

  subgraph data[Data Layer]
    pg[(PostgreSQL source of truth)]
    outbox[(PostgreSQL Outbox)]
    ch[(ClickHouse analytics)]
    redis[(Redis cache, locks, rate limits)]
    object[(MinIO / S3 artifacts)]
  end

  subgraph ml[ML Boundary]
    vision[Python FastAPI vision-service]
    ocr[OCR / CV]
    vlm[Qwen / VLM Runtime]
  end

  subgraph obs[Observability]
    otel[OpenTelemetry Collector]
    prom[(Prometheus)]
    loki[(Loki)]
    tempo[(Tempo)]
    grafana[Grafana]
  end

  user --> web
  user --> cli

  web --> api
  cli --> api
  cli --> worker

  api --> pg
  api --> redis
  api --> object
  api --> temporal
  api --> outbox

  temporal --> worker
  worker --> domain
  worker --> pg
  worker --> outbox
  worker --> redis
  worker --> object
  worker --> vision
  outbox --> relay
  relay --> kafka
  kafka --> sink
  sink --> ch
  sink --> pg

  vision --> ocr
  vision --> vlm

  api --> otel
  worker --> otel
  vision --> otel
  otel --> prom
  otel --> loki
  otel --> tempo
  prom --> grafana
  loki --> grafana
  tempo --> grafana
```

## Standard Processing Flow

This is the default product path. It processes the full match for structure and context, then spends model budget only on selected review windows.

```mermaid
sequenceDiagram
  autonumber
  actor User
  participant UI as React UI
  participant API as Go API
  participant PG as PostgreSQL
  participant S3 as MinIO/S3
  participant Outbox as PG Outbox
  participant Relay as Go Outbox Relay
  participant T as Temporal
  participant W as Go Worker
  participant VS as Python vision-service
  participant CH as ClickHouse
  participant Kafka as Kafka

  User->>UI: Upload or register VOD
  UI->>API: POST /vods
  API->>S3: Store raw video
  API->>PG: Create vod, asset, workflow records
  API->>Outbox: Write vod.lifecycle event
  API->>T: Start ProcessVodWorkflow
  Relay->>Outbox: Poll unpublished events
  Relay->>Kafka: Publish vod.lifecycle event

  T->>W: Run probe activity
  W->>S3: Read raw video
  W->>PG: Save ffprobe metadata
  W->>Outbox: Write vod.probed event
  Relay->>Kafka: Publish vod.probed

  T->>W: Run frame sampling activity
  W->>S3: Write sampled frames and contact sheets
  W->>Outbox: Write frames.extracted event
  Relay->>Kafka: Publish frames.extracted
  Kafka->>CH: ClickHouse sink stores frame extraction timings

  T->>W: Run detection activity
  W->>VS: OCR/HUD/minimap requests
  VS-->>W: Structured observations with confidence
  W->>Outbox: Write detection observation events
  Relay->>Kafka: Publish observations
  Kafka->>CH: ClickHouse sink stores OCR/detection observations

  T->>W: Build timeline and candidate windows
  W->>PG: Save rounds, events, candidate windows

  T->>W: Run selected VLM reviews
  W->>S3: Extract short clips
  W->>VS: Analyze selected windows
  VS-->>W: Findings and evidence

  T->>W: Assemble report
  W->>PG: Save report and findings
  W->>S3: Store report artifacts
  W->>Outbox: Write report.ready event
  Relay->>Kafka: Publish report.ready
  UI->>API: GET /vods/{id}/report
```

## Deployment Profiles

### Local Development

Use this first. It has the strongest learning value and the lowest cost.

```mermaid
flowchart TB
  dev[Developer machine]

  subgraph local[Docker Compose / local processes]
    api[Go API]
    worker[Go Worker]
    relay[Go Outbox Relay]
    consumers[Go Kafka Consumers]
    vision[Python vision-service]
    pg[(PostgreSQL)]
    ch[(ClickHouse)]
    redis[(Redis)]
    kafka[(Kafka<br/>KRaft mode)]
    temporal[(Temporal)]
    minio[(MinIO)]
    grafana[Grafana stack]
  end

  dev --> api
  dev --> worker
  dev --> vision
  api --> pg
  worker --> temporal
  worker --> pg
  worker --> minio
  worker --> vision
  relay --> pg
  relay --> kafka
  kafka --> consumers
  consumers --> ch
  api --> grafana
  worker --> grafana
  vision --> grafana
```

### Hosted Prototype

The external service boundary should be object storage and GPU inference, not the core product logic.

```mermaid
flowchart LR
  user[User]
  ui[Web UI]

  subgraph app[App Runtime<br/>VPS, containers, or managed containers]
    api[Go API]
    worker[Go Worker]
    relay[Outbox Relay]
    consumers[Kafka Consumers]
    temporal[Temporal<br/>self-hosted first]
    kafka[Kafka]
    redis[Redis]
    pg[PostgreSQL]
    ch[ClickHouse]
    otel[OpenTelemetry]
  end

  subgraph external[External Services]
    r2[Cloudflare R2 / S3<br/>object storage]
    gpu[RunPod / Modal / similar<br/>Qwen/VLM GPU runtime]
    grafana[Grafana Cloud<br/>optional]
    sentry[Sentry<br/>optional]
  end

  user --> ui
  ui --> api
  api --> pg
  api --> r2
  api --> temporal
  temporal --> worker
  worker --> pg
  worker --> r2
  worker --> gpu
  relay --> pg
  relay --> kafka
  kafka --> consumers
  consumers --> ch
  api --> redis
  worker --> redis
  api --> otel
  worker --> otel
  otel --> grafana
  api --> sentry
  ui --> sentry
```

## Why This Boundary

- Go remains the main product language: API, CLI, workers, workflows, persistence, reports, and business rules.
- Python is contained behind `vision-service` because OCR/CV/VLM libraries move faster there.
- Temporal handles long-running, retryable VOD processing. Kafka handles durable domain events, replayable event streams, and analytics fan-out.
- PostgreSQL owns truth. ClickHouse owns large append-only observations. Redis owns short-lived operational state.
- The Qwen/VLM runtime can move between local GPU, RunPod, Modal, or another GPU provider without changing the Go API, data model, or UI.
