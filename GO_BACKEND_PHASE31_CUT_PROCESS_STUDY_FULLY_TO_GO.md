# Phase 31 Cut `processStudy` Fully To Go

This document completes phase 31 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The default desktop app path now routes `processStudy` through the Go sidecar even when the selected frontend runtime remains `legacy-rust`, so processed preview generation, output-path handling, artifact caching, and DICOM export handoff are Go-owned in the live workstation flow.

Primary implementation references:

- [frontend/src/lib/runtime.ts](frontend/src/lib/runtime.ts)
- [frontend/src-tauri/src/go_sidecar.rs](frontend/src-tauri/src/go_sidecar.rs)
- [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts)
- [go-backend/internal/jobs/service.go](go-backend/internal/jobs/service.go)
- [go-backend/internal/export/writer.go](go-backend/internal/export/writer.go)

## 1. The Default Desktop Runtime Now Treats `processStudy` As A Go-Owned Path

Before phase 31, the Go backend could already run `start_process_job`, but only when the whole frontend was built in `go-sidecar` mode. The default desktop runtime still sent processing through the in-process Rust backend.

Phase 31 changes that split:

- `openStudy`, `renderStudy`, `analyzeStudy`, and manual measurement stay on the Rust bridge in the default desktop runtime
- `processStudy` now resolves to the Go sidecar by default
- `getJob` and `cancelJob` follow the owning backend for each live job instead of assuming one global backend implementation

That makes the actual workstation processing flow Go-owned without forcing the whole desktop runtime to switch to Go at once.

## 2. Study Identity Is Shadowed Into Go Only When Processing Needs It

The frontend still treats Rust `openStudy` as the source of truth in the default desktop runtime, so phase 31 does not move the initial study-open path ahead of phase 33.

To make Go process jobs possible without changing the frozen contract:

- the runtime adapter records the Rust-opened study ID plus input path
- on the first processing request for that study, it registers the same input path with the Go backend
- it stores the Go-generated study ID privately and reuses it for later process runs
- Go-owned job snapshots are remapped back to the frontend-visible Rust study ID before the UI consumes them

This keeps the desktop state model stable while letting the Go backend own the real processing work.

## 3. The Tauri Shell Now Starts The Go Sidecar For The Default Desktop Runtime Too

Routing `processStudy` to Go by default only works if the sidecar is available in the standard desktop build.

Phase 31 therefore extends the shell startup rule:

- `mock` still does not start the sidecar
- `legacy-rust` now starts the sidecar because process jobs depend on it
- `go-sidecar` still starts the sidecar for the broader Go transport mode

The shell logs this mixed ownership explicitly so the production split is not hidden during debugging.

## 4. Validation Coverage

Validated with:

```bash
npm --prefix frontend run build
cargo test --manifest-path frontend/src-tauri/Cargo.toml go_sidecar
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./...
```

This covers:

- TypeScript compilation of the mixed Rust/Go runtime adapter
- Rust unit coverage for the sidecar runtime-mode decision
- full Go backend regression coverage for the process pipeline that now owns the default desktop processing path

## 5. Exit Criteria Check

Phase 31 exit criteria are now met:

- the live desktop `processStudy` path is Go-owned by default
- job polling and cancellation follow the correct backend owner
- Go owns preview processing, artifact cache reuse, output-path handling, and export handoff in the workstation flow
- the cutover stays reversible because the Rust-first desktop runtime remains intact for the rest of the command surface
