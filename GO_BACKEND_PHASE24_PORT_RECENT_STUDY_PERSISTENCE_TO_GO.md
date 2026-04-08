# Phase 24 Port Recent-Study Persistence To Go

This document completes phase 24 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Go backend now owns the recent-study catalog behavior that the Rust backend previously handled: catalog JSON shape, reopen ordering, duplicate-path collapse, 10-entry truncation, corrupted-catalog recovery, and the live `open_study` persistence path.

Primary implementation references:

- [go-backend/internal/persistence/catalog.go](go-backend/internal/persistence/catalog.go)
- [go-backend/internal/persistence/catalog_test.go](go-backend/internal/persistence/catalog_test.go)
- [go-backend/internal/httpapi/router.go](go-backend/internal/httpapi/router.go)
- [go-backend/internal/httpapi/router_test.go](go-backend/internal/httpapi/router_test.go)
- [backend/src/persistence/catalog.rs](backend/src/persistence/catalog.rs)
- [backend/tests/fixtures/parity/sample-dental-radiograph/recent-study-catalog.json](backend/tests/fixtures/parity/sample-dental-radiograph/recent-study-catalog.json)

## 1. The Go Catalog Is Now A Real Rust-Parity Persistence Boundary

Phase 10 already attached a Go-side `record opened study` hook to `open_study`, but phase 24 closes the remaining gap by making the catalog behavior itself match the Rust store more closely.

The Go persistence layer now owns:

- `state/catalog.json` as the canonical recent-study file under the Go-managed disk layout
- JSON entries with the same key names Rust writes, including `measurementScale: null` when no calibration is present
- empty-catalog normalization so `recentStudies` stays an array shape instead of drifting to `null`
- duplicate-path collapse plus newest-first ordering
- truncation to the most recent 10 studies

That keeps the sidecar path aligned with the visible persistence behavior the desktop app already depended on from Rust.

## 2. Corrupted Catalogs Are Recovered The Same Way The Rust Backend Did

The Go catalog loader now mirrors the Rust corruption policy:

- invalid JSON is treated as cache corruption
- the bad file is renamed aside as `catalog.corrupt.json`
- `open_study` continues and rewrites a fresh catalog instead of failing the study-open workflow

This matters because recent-study persistence is intentionally best-effort. A damaged catalog should not block opening a DICOM study.

## 3. Validation Coverage Now Matches The Phase 24 Plan

Validated with:

```bash
cd go-backend
gofmt -w internal/persistence/catalog.go internal/persistence/catalog_test.go internal/httpapi/router_test.go
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./internal/persistence ./internal/httpapi
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./...
```

Coverage now includes:

- reopen ordering tests for repeated study opens on the same `inputPath`
- truncation tests proving the catalog keeps only the newest 10 entries
- invalid JSON tests at the persistence layer
- live `open_study` integration coverage for corrupted-catalog recovery
- parity-fixture validation against the committed phase 1 recent-study catalog fixture

## 4. Exit Criteria Check

Phase 24 exit criteria are now met:

- recent-study persistence works through the Go backend
- catalog JSON semantics are Go-owned
- corruption handling is exercised and matches the intended migration behavior
- reopen ordering and truncation are covered directly by tests
