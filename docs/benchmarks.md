# Benchmarks

Last measured: 2026-08-02

The default product path uses local CPU processing only: ffmpeg for sampling,
VALORANT-specific temporal CV for HUD events, and staged Tesseract OCR for
semantic overlay confirmation. It needs neither a paid API nor a local GPU.

## Quality Method

Quality is measured against reviewed, versioned fixtures in `ml/evals`.
An annotation fixture binds itself to a VOD label, report run, sample range,
sampling FPS, timestamp tolerance, and the event types being evaluated.

`evaluated_types` is important for negative coverage. For example, declaring
`death` with no death labels means that any predicted death is still counted as
a false positive. Death and fight are separate classes.

The evaluator reports:

- overall and per-type precision, recall, and F1;
- matched labels and timestamp deltas;
- missed labels;
- false-positive events.

Generate the current Gold benchmark report:

```sh
go run ./cmd/vodctl analyze run \
  --vod gold_remortius_01 \
  --fps 1 \
  --duration 180s \
  --run-id cpu_cv_gold_180_v1 \
  --force
```

Run the regression gate:

```sh
go run ./cmd/vodctl eval run \
  --report data/processed/gold_remortius_01/reports/cpu_cv_gold_180_v1/report.json \
  --annotations ml/evals/gold_remortius_01.first_180s.v1.json \
  --run-id cpu_cv_gold_180_v1 \
  --min-precision 0.95 \
  --min-recall 0.95 \
  --min-f1 0.95 \
  --force
```

The command exits non-zero when a configured threshold is missed, so the same
command can be used in CI after benchmark media is provisioned.

## Current Quality Result

Environment: Apple Silicon development machine, Turkish HUD, first 180 seconds
of `gold_remortius_01`, 1 FPS, 3-second matching tolerance.

| Fixture | Labels | Predictions | Matches | Precision | Recall | F1 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `gold_remortius_01.first_180s.v1` | 4 | 4 | 4 | 1.00 | 1.00 | 1.00 |

The labels cover two buy-phase round starts and two visible fights. `death` and
`rotation` are explicitly evaluated zero-label classes; neither produced a
false positive.

This is a regression score for one short segment, not a population-quality
claim. Release-level quality requires reviewed fixtures across rank buckets,
maps, agents, HUD languages, resolutions, spectator states, and full matches.

## Short Fixture Latency

The same 180-second report completed in 24.3 seconds after analysis started:

| Metric | Result |
| --- | ---: |
| sampled/analyzed frames | 180 / 180 |
| staged OCR frames | 83 |
| selected review windows | 2 |
| detected round segments | 2 |
| frame artifacts | 26.2 MiB |
| report artifacts | 0.2 MiB |
| wall time | 24.3 s |
| processing speed | 7.4x realtime |

The number includes extraction, CV, OCR, clip generation, and report writing on
this machine. It is a preliminary local measurement; codec, resolution, disk
cache, and CPU materially affect it.

## Full-Match Product Result

The released CPU path was also run over the complete English-HUD
`iron_spudbud_01` recording:

```sh
go run ./cmd/vodctl analyze run \
  --vod iron_spudbud_01 \
  --fps 1 \
  --duration 0 \
  --timeout 20m \
  --run-id full_product_benchmark_full_v4 \
  --force
```

Environment: 8-core Apple M2 MacBook Air with 16 GB RAM, warm local filesystem
cache, 1920x1080 60 FPS source, 33:02.7 media duration.

| Metric | Result |
| --- | ---: |
| sampled/analyzed frames | 1,983 / 1,983 |
| staged OCR frames | 840 |
| round segments | 17, buy-phase visual anchors |
| review windows / clips | 18 / 18 |
| window mix | 7 death, 8 fight, 2 rotation, 1 tempo |
| wall time | 413.0 s |
| processing speed | 4.80x realtime |
| frame artifacts | 413.6 MiB |
| clip artifacts | 196.6 MiB |
| report artifacts | 2.0 MiB |

Release invariants checked against the structured report:

- every selected death contains an OCR-confirmed POV marker such as `KILLED BY`
  or `KILLED YOU`;
- death moments are unique per round and cover early, middle, and late match
  sections rather than only the highest-confidence early frames;
- all 18 automatic coach decisions remain `needs_confirmation`;
- automatic recommendations before guided assessment: zero;
- every selected moment has a generated clip and chronological frame evidence.

This run proves end-to-end completion, local resource cost, and safety
invariants for one full match. It is not a full-match precision/recall score;
that still requires the broader manually reviewed corpus below.

## Media Baseline

The reusable media benchmark remains available through
`scripts/benchmark_video.sh`:

```sh
./scripts/benchmark_video.sh --metadata-only
./scripts/benchmark_video.sh --rank diamond --limit 1 --sample-seconds 180 --fps 1
```

Initial 60-second, 1 FPS extraction measurements from 2026-07-21 were:

| Rank | Label | Wall Time | Frames | Artifact Size |
| --- | --- | ---: | ---: | ---: |
| iron | `iron_spudbud_01` | 5.56 s | 60 | 12.1 MB |
| diamond | `diamond_crazies_01` | 1.80 s | 60 | 10.8 MB |
| radiant | `radiant_valorantdaily_fade_01` | 4.88 s | 60 | 9.9 MB |

Raw benchmark outputs are intentionally ignored by Git and are written under
`data/processed/benchmarks` and `data/processed/evaluations`.

## Release Gate Expansion

The next quality corpus must add, in order:

1. one full VOD with round/fight/death/rotation labels;
2. one fixture for each low, middle, and high rank bucket;
3. English and Turkish HUD fixtures at minimum;
4. 720p, 1080p, and non-16:9 capture compatibility cases;
5. negative fixtures for menus, scoreboard usage, post-round reports, pauses,
   and edited videos.

No quality percentage should be presented to users until this broader corpus
passes a documented release threshold.
