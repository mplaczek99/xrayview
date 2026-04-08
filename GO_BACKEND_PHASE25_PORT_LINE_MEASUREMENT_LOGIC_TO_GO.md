# Phase 25 Port Line Measurement Logic To Go

This document completes phase 25 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Go backend now owns the manual line-measurement helper path that the Rust backend previously handled: pixel length, calibrated millimeter length, and one-decimal rounding semantics exposed through `measure_line_annotation`.

Primary implementation references:

- [go-backend/internal/contracts/annotations.go](go-backend/internal/contracts/annotations.go)
- [go-backend/internal/annotations/measurement.go](go-backend/internal/annotations/measurement.go)
- [go-backend/internal/annotations/measurement_test.go](go-backend/internal/annotations/measurement_test.go)
- [go-backend/internal/httpapi/router.go](go-backend/internal/httpapi/router.go)
- [go-backend/internal/httpapi/router_test.go](go-backend/internal/httpapi/router_test.go)
- [backend/src/analysis/measurement_service.rs](backend/src/analysis/measurement_service.rs)

## 1. Line Measurement Has A Real Go Owner Now

Before this phase, the frontend could call `measure_line_annotation` against the Go sidecar transport, but the HTTP router still returned `501 not implemented` even though the command was advertised as supported.

Phase 25 closes that gap by adding the missing contract models and a dedicated Go measurement helper that owns:

- Euclidean pixel-length calculation from line start/end points
- calibrated millimeter length when study spacing metadata is available
- overwriting the annotation `measurement` payload with freshly computed values

That makes the line-measurement path an actual Go-served behavior instead of a declared-but-missing endpoint.

## 2. Rounding Semantics Match The Rust Helper

The Go implementation mirrors the Rust helper in `backend/src/analysis/measurement_service.rs`:

- pixel length uses `sqrt(dx^2 + dy^2)`
- calibrated length uses anisotropic row/column spacing from the study measurement scale
- both values are rounded to one decimal place
- midpoint rounding uses Go `math.Round`, which matches the Rust `round()` behavior this phase depended on

The helper works directly from the registered study metadata, so the frontend keeps the same contract shape it already used with the Rust backend.

## 3. Validation Coverage

Validated with:

```bash
gofmt -w go-backend/internal/contracts/annotations.go go-backend/internal/annotations/measurement.go go-backend/internal/annotations/measurement_test.go go-backend/internal/httpapi/router.go go-backend/internal/httpapi/router_test.go
cd go-backend
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./internal/annotations ./internal/httpapi
```

Coverage now includes:

- Rust-parity unit tests for pixel-only and calibrated line measurement
- an explicit rounding test for half-step one-decimal behavior
- HTTP integration coverage for pixel-only measurement responses
- HTTP integration coverage for calibrated millimeter responses
- HTTP error handling for unknown study IDs

## 4. Exit Criteria Check

Phase 25 exit criteria are now met:

- `measureLineAnnotation` is served by Go
- pixel and calibrated line measurements are Go-owned
- one-decimal rounding semantics are covered directly by tests
