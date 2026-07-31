# Evaluation Fixtures

Reviewed evaluation labels live here as versioned regression fixtures.

Run an evaluation against an existing analysis report:

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

The command writes:

- `data/processed/evaluations/<run_id>/evaluation.json`
- `data/processed/evaluations/<run_id>/evaluation.md`

Supported label `type` aliases:

- `combat`, `fight`, `kill`, `bad_fight`
- `death`, `death_review`, `death_state_confirmed`
- `rotation`, `rotate`, `bad_rotate`
- `tempo`, `low_activity`, `hold`, `pacing`
- `round`, `round_start`, `round_boundary`

The evaluator matches labels to `report.gameplay.gameplay_events` within the configured timestamp tolerance and reports precision, recall, F1, missed labels, and false positives. Use `evaluated_types` to measure false positives for classes that intentionally have no labels in a fixture.
