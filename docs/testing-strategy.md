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

cmd/vodctl/
  main.go
  main_test.go                   # CLI behavior tests with fake ffmpeg/ffprobe

tests/integration/
  ...                            # slower tests that need Docker, Postgres, Kafka, etc.
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

These should live under `tests/integration/` or use build tags:

```go
//go:build integration
```

Examples:

- real `ffmpeg` against a tiny fixture video;
- PostgreSQL migrations;
- Kafka producer/consumer contract;
- ClickHouse sink writes;
- Temporal workflow smoke test.

### Contract Tests

Tests for boundaries between services.

Examples:

- Go client to Python vision-service;
- Kafka event envelope compatibility;
- report JSON schema compatibility;
- API response contracts.

## Rule Of Thumb

- Keep unit tests next to package code.
- Put slow cross-service tests under `tests/integration`.
- Prefer fake binaries/services for CLI unit tests.
- Add tiny fixtures only when needed; never commit real VODs.

