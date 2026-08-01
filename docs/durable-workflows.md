# Durable Analysis Workflows

## Responsibility Split

- PostgreSQL stores the user-facing job intent and status read-model.
- Temporal stores executable workflow history, retry timers, and cancellation state.
- `vod-worker` polls the Temporal task queue and runs the local CPU analysis activity.
- Redis prevents concurrent analysis of the same owner/VOD pair.
- The PostgreSQL outbox and Kafka distribute immutable report and processing events after product state is committed.

Temporal and Kafka are deliberately not interchangeable. Temporal controls one long-running process; Kafka supports replayable events and independent consumers.

## Submission And Recovery

1. The API authenticates the owner and resolves the requested VOD within that tenant boundary.
2. It normalizes input into `app.AnalysisJobRequest` with an explicit schema version.
3. It commits a `queued` job and the complete request to PostgreSQL.
4. It starts `ProcessValorantVOD` with a workflow ID equal to the job ID.
5. A background dispatcher scans only queued jobs and repeats step 4 every 15 seconds.

Temporal start is idempotent: a running workflow with the same ID is reused and a completed ID is not duplicated. This closes the crash window between the PostgreSQL commit and Temporal start without treating Kafka as a task queue.

## Execution Lifecycle

The activity records its Temporal attempt and writes these coarse stages to the PostgreSQL read-model:

```text
queued -> starting -> locking -> resolving -> probing -> sampling
       -> analyzing -> clips -> coaching -> model_review (optional)
       -> saving -> persisting -> completed
```

Progress updates also record Temporal activity heartbeats. The activity has a 55 minute start-to-close timeout, a 30 second heartbeat timeout, and three attempts with exponential backoff. Finalization activities retry independently so a temporary PostgreSQL outage does not immediately leave the UI stale.

All generated paths are deterministic for an owner, VOD, and run ID. Redis locking prevents overlapping attempts while an old process is still winding down.

## Cancellation

`DELETE /api/analysis-runs/{job_id}` is owner-scoped (administrators may operate across tenants). It records `cancellation_requested` before calling Temporal. Temporal cancels the activity context, which propagates to ffprobe/ffmpeg and analysis code. A disconnected finalization activity writes the terminal `cancelled` state even though the workflow context is cancelled.

## Local Verification

Unit tests use Temporal's time-skipping workflow test environment to verify success, three retries, terminal failure, and cancellation. A gated integration test verifies the real path through Temporal and PostgreSQL:

```sh
temporal server start-dev --ip 127.0.0.1 --port 7243 --headless

TEST_DATABASE_URL='postgres://user@127.0.0.1:5432/valorant_vod_coach_test?sslmode=disable' \
TEST_TEMPORAL_ADDRESS=127.0.0.1:7243 \
go test -count=1 -run TestWorkflowPostgresIntegration \
  ./internal/adapters/temporalworkflow
```

The database name guard requires `test` and prevents the integration test from truncating a non-test database.
