# Evaluation Fixtures

Manual evaluation labels live here before they are promoted into a database-backed evaluation workflow.

Run an evaluation against an existing analysis report:

```sh
go run ./cmd/vodctl eval run \
  --report data/processed/iron_spudbud_01/reports/gameplay_events_smoke/report.json \
  --annotations ml/evals/gameplay_events.example.json \
  --run-id gameplay-events-example \
  --force
```

The command writes:

- `data/processed/evaluations/<run_id>/evaluation.json`
- `data/processed/evaluations/<run_id>/evaluation.md`

Supported label `type` aliases:

- `combat`, `fight`, `death`, `kill`, `bad_fight`
- `rotation`, `rotate`, `bad_rotate`
- `tempo`, `low_activity`, `hold`, `pacing`
- `round`, `round_start`, `round_boundary`

The evaluator matches labels to `report.gameplay.gameplay_events` within the configured timestamp tolerance and reports precision, recall, F1, missed labels, and false positives.
