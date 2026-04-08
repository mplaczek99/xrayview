# Phase 28 Implement Go Analyze Job

This document completes phase 28 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Go backend now serves `start_analyze_job` end-to-end through the same command surface the frontend already uses: it renders the analysis preview, runs the Go tooth-analysis primitives from phase 27, generates auto-tooth suggestions, returns a typed `analyzeStudy` job result, and reuses cached analyze artifacts on repeat runs.

Primary implementation references:

- [go-backend/internal/jobs/service.go](go-backend/internal/jobs/service.go)
- [go-backend/internal/cache/memory.go](go-backend/internal/cache/memory.go)
- [go-backend/internal/httpapi/router.go](go-backend/internal/httpapi/router.go)
- [go-backend/internal/jobs/service_test.go](go-backend/internal/jobs/service_test.go)
- [go-backend/internal/httpapi/router_test.go](go-backend/internal/httpapi/router_test.go)
- [go-backend/internal/analysis/analysis.go](go-backend/internal/analysis/analysis.go)
- [go-backend/internal/annotations/suggestions.go](go-backend/internal/annotations/suggestions.go)
- [backend/src/app/state.rs](backend/src/app/state.rs)

## 1. The Go Job Service Now Owns Analyze Job Orchestration

Phase 27 intentionally stopped at the primitive-analysis boundary. Phase 28 wires those primitives into the live Go job runner:

- `StartAnalyzeJob` now validates the study id, fingerprints the source input with the Rust-equivalent `analyze-study-v1` namespace, reserves the cached preview artifact path, and participates in active-job deduplication.
- `executeAnalyzeJob` now mirrors the Rust progress stages closely:
  - `validating` at 10%
  - `loadingStudy` at 35%
  - `renderingPreview` at 65%
  - `measuringTooth` at 88%
- the worker renders the analysis preview PNG, runs `analysis.AnalyzePreview`, maps suggestions with `annotations.SuggestedAnnotations`, and completes the job with a `contracts.AnalyzeStudyCommandResult`
- cancellation now cleans up the analysis preview artifact the same way render/process jobs already do

That closes the major remaining gap between the Go analysis primitives and the user-visible analyze workflow.

## 2. Analyze Results Now Participate In The Go Memory Cache

The in-memory cache previously only understood render and process results. Phase 28 extends it to `analyzeStudy` results so repeat runs can reuse the already-generated preview and payload instead of re-decoding and re-analyzing the same study.

The cache implementation now:

- stores and loads `contracts.AnalyzeStudyCommandResult`
- verifies the cached preview artifact still exists before serving the entry
- deep-clones the nested `ToothAnalysis` and `AnnotationBundle` payloads when storing/loading cached results

This preserves the existing cached-job semantics already used by the frontend job center without introducing aliasing bugs from shared slice or pointer fields.

## 3. The HTTP Transport Now Exposes `start_analyze_job`

The Go HTTP router now handles `start_analyze_job` as a first-class command instead of falling through to `501 not implemented`.

That means the existing Go-sidecar frontend adapter path in [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts) now has a working backend target for analyze jobs without any UI-specific changes.

## 4. Validation Coverage

Validated with:

```bash
gofmt -w go-backend/internal/jobs/service.go go-backend/internal/cache/memory.go go-backend/internal/httpapi/router.go go-backend/internal/jobs/service_test.go go-backend/internal/httpapi/router_test.go
cd go-backend
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./internal/cache ./internal/jobs ./internal/httpapi
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./...
```

Coverage added in this phase:

- service-level analyze job completion, caching, and active-job deduplication tests
- router-level end-to-end analyze command coverage against the sample DICOM fixture through the real decode helper path
- full Go backend package-suite regression coverage after the cutover

## 5. Exit Criteria Check

Phase 28 exit criteria are now met:

- `analyzeStudy` works through the Go backend job system
- the Go HTTP transport exposes `start_analyze_job`
- the frontend adapter already targeting `start_analyze_job` now has a live Go implementation behind it
- the analyze flow returns preview, analysis, and suggested annotation payloads end-to-end
