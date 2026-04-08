# Phase 33 Cut `openStudy` To Go In Live Desktop Flow

This document completes phase 33 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The default desktop app path now routes `openStudy` through the Go sidecar even when the selected frontend runtime remains `legacy-rust`, so the live study-open path no longer depends on the in-process Rust backend for metadata extraction, study registration, or recent-study persistence.

Primary implementation references:

- [frontend/src/lib/runtime.ts](frontend/src/lib/runtime.ts)
- [frontend/src-tauri/src/go_sidecar.rs](frontend/src-tauri/src/go_sidecar.rs)
- [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts)
- [go-backend/internal/httpapi/router.go](go-backend/internal/httpapi/router.go)
- [go-backend/internal/persistence/catalog.go](go-backend/internal/persistence/catalog.go)

## 1. The Default Desktop Runtime Now Opens Studies Through Go First

Before phase 33, the default `legacy-rust` desktop runtime still called the Rust bridge for `openStudy`, even though the Go backend already implemented the same command and owned recent-study persistence.

Phase 33 flips that default:

- `openStudy` now resolves to the Go sidecar in the default desktop runtime
- `processStudy` and `measureLineAnnotation` remain Go-owned as they were after phases 31 and 32
- `renderStudy` and `analyzeStudy` still remain on the Rust bridge for now

That means the initial study-open path now uses the Go-owned metadata reader, Go study registry, and Go recent-study catalog by default.

## 2. Rust-Owned Render And Analyze Jobs Now Use A Reverse Study-ID Bridge

Switching `openStudy` to Go changes the frontend-visible study ID in the default desktop runtime: the UI now stores the Go-generated study ID instead of the Rust-generated one.

Because `renderStudy` and `analyzeStudy` are still Rust-owned in this phase, the runtime adapter now bridges in the opposite direction from phase 31:

- it records each Go-opened study's input path under the frontend-visible study ID
- on the first Rust-owned render or analyze request, it lazily registers that same input path with the Rust backend
- it stores the matching Rust study ID privately and reuses it for later Rust-owned commands on that study
- job snapshots and completed job payloads from Rust are remapped back to the frontend-visible Go study ID before the UI consumes them

This keeps the workbench state model stable while removing the live open-study dependency from Rust.

## 3. Shell And Runtime Logging Now Reflect The New Mixed Ownership Split

The default desktop shell already launched the Go sidecar in `legacy-rust` mode because earlier phases made `processStudy` and manual line measurement Go-owned.

Phase 33 updates the logging so that split stays explicit:

- the frontend runtime description now reports `openStudy`, `processStudy`, and `measureLineAnnotation` as Go-owned in `legacy-rust`
- the Tauri shell startup log now says the sidecar is enabled for study open, process work, and manual line measurement

That avoids implying that the default desktop runtime is still Rust-first for the initial open flow.

## 4. Validation Coverage

Validated with:

```bash
npm --prefix frontend run build
cargo test --manifest-path frontend/src-tauri/Cargo.toml go_sidecar
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./internal/httpapi ./internal/persistence
```

This covers:

- TypeScript compilation of the hybrid runtime adapter after the study-ID bridge reversal
- Rust coverage for the shell-side sidecar runtime behavior
- Go regression coverage for `open_study` transport handling and recent-study persistence semantics

## 5. Exit Criteria Check

Phase 33 exit criteria are now met:

- the live desktop `openStudy` path is Go-owned by default
- recent-study persistence remains on the Go side and is exercised by the default open flow
- Rust-owned render and analyze jobs still work against Go-opened studies through the adapter bridge
- the cutover stays reversible because render and analyze are still isolated behind the Rust bridge for now
