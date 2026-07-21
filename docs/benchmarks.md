# Benchmarks

Date: 2026-07-21

The goal of benchmarking is to replace guesses with measured latency, throughput, artifact size, and model cost.

The first benchmark suite is intentionally simple: it measures the media pipeline on the already downloaded VODs. GPU and VLM benchmarks will be added after the Python `vision-service` exists.

## What We Measure

### Phase 0: Media Baseline

Available now through `scripts/benchmark_video.sh`.

- local file size;
- `ffprobe` wall time;
- video duration, codec, resolution, and stream metadata;
- frame extraction wall time;
- requested sampling FPS;
- number of extracted frames;
- derived frame artifact size.

This tells us how expensive the deterministic part of the pipeline is before OCR or AI.

### Phase 1: Detection Baseline

Added after frame extraction code exists.

- HUD/minimap visibility detection time;
- OCR time per frame crop;
- OCR confidence distribution;
- timer/score extraction accuracy;
- round boundary precision/recall;
- death window precision/recall.

### Phase 2: VLM Baseline

Added after the Python `vision-service` exists.

- model name and version;
- prompt version;
- selected windows per VOD;
- input frames or clip seconds per request;
- active GPU seconds;
- wall time;
- VRAM use;
- retry count;
- model output validity;
- estimated cost per VOD.

Cost formula:

```text
cost_per_vod = active_gpu_seconds * gpu_price_per_second
```

### Phase 3: End-to-End Workflow

Added after Temporal workflow exists.

- total processing wall time;
- per-stage timings;
- workflow retries;
- event publication latency;
- report generation latency;
- report reproducibility from saved artifacts;
- dashboard/tracing coverage.

## Benchmark Modes

Use the same VODs for all modes so the results are comparable.

| Mode | Purpose | Expected Cost | Expected Quality |
| --- | --- | --- | --- |
| `fast` | deterministic metadata, sampling, OCR, simple heuristics | lowest | useful but shallow |
| `standard` | full coarse scan plus selected VLM windows | medium | target product default |
| `deep` | denser full-match representation plus selected VLM review | highest | experimental quality ceiling |

The default architecture assumes `standard = hybrid` until benchmarks prove another strategy is better.

## Current Script

Preview what would run:

```sh
./scripts/benchmark_video.sh --rank diamond --limit 1 --print-only
```

Probe all downloaded VODs:

```sh
./scripts/benchmark_video.sh --metadata-only
```

Run a quick extraction benchmark on one VOD:

```sh
./scripts/benchmark_video.sh --rank diamond --limit 1 --sample-seconds 180 --fps 1
```

The Go CLI can also extract a reusable artifact for one VOD:

```sh
go run ./cmd/vodctl video sample --vod diamond_crazies_01 --duration 60s --fps 1
```

Run a named benchmark so multiple commands append to the same result folder:

```sh
./scripts/benchmark_video.sh --run-id media-smoke --label iron_spudbud_01 --sample-seconds 60 --fps 1
./scripts/benchmark_video.sh --run-id media-smoke --label diamond_crazies_01 --sample-seconds 60 --fps 1
./scripts/benchmark_video.sh --run-id media-smoke --label radiant_valorantdaily_fade_01 --sample-seconds 60 --fps 1
```

Run a heavier extraction benchmark on one VOD:

```sh
./scripts/benchmark_video.sh --label diamond_crazies_01 --sample-seconds 600 --fps 2
```

Results are written under:

```text
data/processed/benchmarks/<run_id>/
```

The generated `results.tsv` is intentionally simple so it can later be imported into PostgreSQL or ClickHouse.

## Result Columns

`results.tsv` currently contains:

- `run_id`
- `phase`
- `status`
- `rank`
- `label`
- `video_id`
- `file_path`
- `file_size_bytes`
- `manifest_duration`
- `sample_seconds`
- `sample_fps`
- `wall_seconds`
- `frame_count`
- `artifact_bytes`
- `notes`

## Interpretation Rules

- Do not compare VODs without checking resolution and codec.
- Do not use YouTube title rank as ground truth when `rank_source=search_metadata`.
- Keep failed benchmark rows; failure rate is part of the benchmark.
- Treat first-run results separately from warm-cache results.
- GPU estimates are not accepted as project facts until measured by Phase 2.

## Benchmark Matrix

Start with this small matrix:

| Rank Bucket | VOD Count | Media Baseline | Detection Baseline | VLM Baseline |
| --- | ---: | --- | --- | --- |
| Low: Iron/Bronze/Silver | 3 | yes | later | later |
| Mid: Gold/Platinum/Diamond | 3 | yes | later | later |
| High: Ascendant/Immortal/Radiant | 3 | yes | later | later |

Then expand to all 18 downloaded VODs after the pipeline is stable.

## Initial Local Measurements

Environment:

- date: 2026-07-21;
- dataset: 18 downloaded YouTube VODs;
- script: `scripts/benchmark_video.sh`;
- media tools: local `ffprobe` and `ffmpeg`;
- frame sampling: first 60 seconds, 1 fps, JPEG quality `-q:v 3`.

Probe baseline:

| Run ID | Scope | Status | Probe Wall Time |
| --- | --- | --- | --- |
| `media-probe-all-20260721` | all 18 VODs | 18/18 ok | 0.03-0.07s per VOD |

Frame sampling smoke:

| Rank | Label | Source Size | Sample | Wall Time | Frames | Artifact Size | Decode Speed |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| iron | `iron_spudbud_01` | 1.13 GB | 60s at 1 fps | 5.56s | 60 | 12.1 MB | 10.8x realtime |
| diamond | `diamond_crazies_01` | 1.30 GB | 60s at 1 fps | 1.80s | 60 | 10.8 MB | 33.3x realtime |
| radiant | `radiant_valorantdaily_fade_01` | 0.91 GB | 60s at 1 fps | 4.88s | 60 | 9.9 MB | 12.3x realtime |

Early interpretation:

- The media probe stage is negligible compared with extraction and future OCR/VLM work.
- Frame extraction speed varies materially by source encoding, resolution, and local decode path.
- Extrapolating from this tiny smoke sample, full-match 1 fps extraction for a 35 minute VOD might land around 1-3.5 minutes locally, but this is only a first approximation.
- These numbers do not estimate AI cost yet. GPU/VLM cost remains unproven until Phase 2 benchmarks exist.
