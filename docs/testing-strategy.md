# Testing Strategy

Date: 2026-07-21

Go tests should usually live next to the `.go` files they test. This is not a bad practice in Go; it is the standard toolchain convention.

## Why Tests Are Colocated

Colocated tests are preferred because:

- `go test ./...` discovers them naturally;
- the test is close to the behavior it protects;
- package-level tests can exercise unexported helpers when that keeps the public API smaller;
- reviewers can see code and tests together;
- no custom test runner or path convention is needed.

This is normal in mature Go repositories.

## Test Package Modes

Use same-package tests when testing internal behavior:

```go
package media
```

Use external-package tests when testing only the public contract:

```go
package media_test
```

The file still lives next to the package. The package name controls visibility.

## Project Test Layout

```text
internal/adapters/media/
  sample.go
  sample_test.go                 # fast unit tests for media adapter behavior

internal/adapters/dataset/
  manifest.go
  manifest_test.go               # fast unit tests for manifest parsing

internal/adapters/postgres/
  store_integration_test.go      # env-gated real PostgreSQL contract

internal/adapters/temporalworkflow/
  workflow_integration_test.go   # env-gated Temporal + PostgreSQL workflow

cmd/vodctl/
  main.go
  main_test.go                   # CLI behavior tests with fake ffmpeg/ffprobe

tests/integration/
  ...                            # optional black-box tests spanning many services
```

## Test Categories

### Unit Tests

Fast tests with no real external service.

Examples:

- parse manifest;
- validate duplicate labels;
- parse ffprobe JSON;
- build ffmpeg arguments;
- write `frames.json` from fake frame output.

### Integration Tests

Tests that need real binaries, Docker services, databases, Kafka, or object storage.

Adapter-level integration tests stay beside the adapter and skip unless their explicit test environment variables are present. This keeps `go test ./...` useful while avoiding accidental access to developer services.

Use `tests/integration/` for black-box scenarios that do not belong to one package. Build tags remain an option for unusually expensive suites:

```go
//go:build integration
```

Examples:

- real `ffmpeg` against a tiny fixture video;
- PostgreSQL migrations;
- Kafka producer/consumer contract;
- ClickHouse sink writes;
- Temporal workflow smoke test.

Current real-service commands:

```sh
TEST_DATABASE_URL='postgres://.../valorant_vod_coach_test?sslmode=disable' \
go test ./internal/adapters/postgres -run Integration -count=1 -v

TEST_DATABASE_URL='postgres://.../valorant_vod_coach_test?sslmode=disable' \
TEST_TEMPORAL_ADDRESS=localhost:7243 \
go test ./internal/adapters/temporalworkflow -run Integration -count=1 -v
```

The PostgreSQL integration suite rejects database names that do not contain `test` and truncates only that dedicated database.

### Contract Tests

Tests for boundaries between services.

Examples:

- Go client to Python vision-service;
- Kafka event envelope compatibility;
- report JSON schema compatibility;
- API response contracts.

## Rule Of Thumb

- Keep unit tests next to package code.
- Keep adapter-owned real-service tests beside the adapter and gate them with test-only environment variables.
- Put broad black-box scenarios under `tests/integration`.
- Prefer fake binaries/services for CLI unit tests.
- Add tiny fixtures only when needed; never commit real VODs.
