# Phase 32 Cut `measureLineAnnotation` To Go

This document completes phase 32 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The default desktop app path now routes `measureLineAnnotation` through the Go sidecar even when the selected frontend runtime remains `legacy-rust`, so manual line measurement no longer depends on the live Rust backend path.

Primary implementation references:

- [frontend/src/lib/runtime.ts](frontend/src/lib/runtime.ts)
- [frontend/src-tauri/src/go_sidecar.rs](frontend/src-tauri/src/go_sidecar.rs)
- [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts)
- [go-backend/internal/httpapi/router.go](go-backend/internal/httpapi/router.go)
- [go-backend/internal/annotations/measurement.go](go-backend/internal/annotations/measurement.go)

## 1. The Legacy Desktop Runtime Now Sends Manual Line Measurement To Go

Before phase 32, the Go backend already implemented `measure_line_annotation`, but the default desktop runtime still called the in-process Rust bridge for manual line measurement.

Phase 32 closes that last gap for this command:

- `measureLineAnnotation` now resolves to the Go sidecar in the default desktop runtime
- `openStudy`, `renderStudy`, and `analyzeStudy` still remain on the Rust bridge for now
- the frontend measurement UX and contract shape stay unchanged

That removes another live Rust dependency without forcing a broader shell or study-open cutover ahead of schedule.

## 2. Study Identity Reuses The Existing Rust-To-Go Bridge

The legacy desktop runtime still treats Rust `openStudy` as the frontend-visible source of truth until phase 33.

To keep that stable while moving line measurement to Go:

- the runtime reuses the existing lazy study registration bridge that phase 31 added for Go-owned process jobs
- the first manual measurement on a Rust-opened study registers the same input path with the Go sidecar if needed
- the Go study ID stays internal to the adapter while the frontend continues using the Rust-facing study ID

Because the measurement response only returns the measured annotation, no extra study-ID remapping was needed in the UI state layer.

## 3. Shell And Runtime Logging Now Reflect The Broader Go Ownership

Phase 31 already made the default desktop shell launch the Go sidecar for `legacy-rust` mode so Go-owned process jobs would work.

Phase 32 keeps that startup rule and updates the logging to reflect the actual mixed ownership split more accurately:

- the frontend runtime description now reports both `processStudy` and `measureLineAnnotation` as Go-owned in `legacy-rust`
- the Tauri shell startup log now says the sidecar is enabled for process work and manual line measurement

That avoids hiding the command split during debugging.

## 4. Validation Coverage

Validated with:

```bash
npm --prefix frontend run build
cargo test --manifest-path frontend/src-tauri/Cargo.toml go_sidecar
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./internal/httpapi ./internal/annotations
```

This covers:

- TypeScript compilation of the hybrid Rust/Go runtime adapter
- Rust coverage for the shell-side sidecar runtime behavior
- Go measurement and HTTP endpoint regression coverage for the live command path

## 5. Exit Criteria Check

Phase 32 exit criteria are now met:

- the live desktop `measureLineAnnotation` path is Go-owned by default
- the frontend measurement workflow and DTO shape are unchanged
- the cutover stays reversible because the rest of the default desktop command surface remains Rust-first
