# Phase 34 Cut `analyzeStudy` To Go In Live Desktop Flow

This document completes phase 34 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The default desktop app path now routes `analyzeStudy` through the Go sidecar even when the selected frontend runtime remains `legacy-rust`, so the live analysis flow no longer depends on the in-process Rust backend path.

Primary implementation references:

- [frontend/src/lib/runtime.ts](frontend/src/lib/runtime.ts)
- [frontend/src-tauri/src/go_sidecar.rs](frontend/src-tauri/src/go_sidecar.rs)
- [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts)
- [go-backend/internal/jobs/service.go](go-backend/internal/jobs/service.go)
- [go-backend/internal/httpapi/router.go](go-backend/internal/httpapi/router.go)

## 1. The Default Desktop Runtime Now Treats `analyzeStudy` As A Go-Owned Path

Before phase 34, the Go backend already implemented `start_analyze_job`, but the default `legacy-rust` desktop runtime still sent analysis through the in-process Rust bridge.

Phase 34 changes that split:

- `openStudy`, `processStudy`, `analyzeStudy`, and `measureLineAnnotation` now resolve to the Go sidecar in the default desktop runtime
- `renderStudy` remains the only interactive command path still owned by the Rust bridge in `legacy-rust`
- `getJob` and `cancelJob` continue to follow the owning backend for each live job

That makes the interactive analysis pipeline Go-owned without forcing the final render cutover ahead of schedule.

## 2. The Go-First Study Identity Flow Now Stays Consistent Through Analysis

Phase 33 already made the frontend-visible study ID Go-owned by default, while render work still lazily bridges back into Rust only when needed.

With phase 34:

- `startAnalyzeStudyJob` now uses the existing Go-visible study ID directly
- analysis jobs no longer need lazy Rust-side study registration
- completed job snapshots and payloads keep the same study identity the frontend already stores from `openStudy`

That removes another reverse-bridge dependency from the live desktop path and leaves render as the only remaining Rust-owned interactive workflow.

## 3. Shell And Runtime Logging Now Reflect The New Ownership Split

The shell already launched the Go sidecar in `legacy-rust` mode because earlier phases made other commands Go-owned.

Phase 34 updates the runtime description and shell startup log so that split stays explicit:

- the frontend runtime description now reports `openStudy`, `processStudy`, `analyzeStudy`, and `measureLineAnnotation` as Go-owned in `legacy-rust`
- the Tauri shell startup log now says the sidecar is enabled for study open, processing, analysis, and manual line measurement

That avoids implying the default desktop analysis flow is still Rust-backed.

## 4. Validation Coverage

Validated with:

```bash
npm --prefix frontend run build
cargo test --manifest-path frontend/src-tauri/Cargo.toml go_sidecar
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./internal/httpapi ./internal/jobs
```

This covers:

- TypeScript compilation of the mixed runtime adapter after the ownership switch
- Rust coverage for the shell-side sidecar runtime behavior
- Go regression coverage for analyze-job transport and job-service behavior

## 5. Exit Criteria Check

Phase 34 exit criteria are now met:

- the live desktop `analyzeStudy` path is Go-owned by default
- analysis job polling and cancellation stay aligned with backend ownership
- the cutover stays reversible because `renderStudy` is still isolated behind the Rust bridge
