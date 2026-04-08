# Phase 20 Port Process Job Orchestration To Go

This document completes phase 20 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Go sidecar now owns the live `start_process_job` workflow for preview processing: request validation, study decode through the temporary Rust helper, grayscale/palette/compare execution in Go, preview artifact writes, and Go-owned job snapshots over the sidecar HTTP API.

Primary implementation references:

- [go-backend/internal/contracts/jobs.go](go-backend/internal/contracts/jobs.go)
- [go-backend/internal/processing/request.go](go-backend/internal/processing/request.go)
- [go-backend/internal/jobs/service.go](go-backend/internal/jobs/service.go)
- [go-backend/internal/jobs/service_test.go](go-backend/internal/jobs/service_test.go)
- [go-backend/internal/httpapi/router.go](go-backend/internal/httpapi/router.go)
- [go-backend/internal/httpapi/router_test.go](go-backend/internal/httpapi/router_test.go)
- [backend/src/app/state.rs](backend/src/app/state.rs)
- [backend/src/app/mod.rs](backend/src/app/mod.rs)

## 1. `start_process_job` Now Executes Real Go-Owned Preview Work

Phase 19 stopped at reusable processing primitives plus the narrow CLI preview path. Phase 20 wraps that processing stack in the live sidecar job service.

The Go backend now:

- accepts `start_process_job` with the frozen phase 2 contract shape
- validates preset IDs, brightness, contrast, and palette overrides using Go-owned request resolution
- decodes the registered study through the temporary Rust decode helper
- renders and processes the preview in Go, including palette and compare handling from phases 18 and 19
- writes the processed preview PNG into the cache artifact tree already compatible with the Tauri asset protocol
- returns completed `processStudy` results through `get_job`

## 2. Job Semantics Match The Existing Desktop Expectations Closely Enough For The Live Flow

The process path now uses the same Go-side job machinery that phase 17 introduced for render jobs, extended for processing-specific stages:

- queued, running, cancelling, completed, failed, and cancelled states
- phase-specific progress stages for validation, study load, pixel processing, preview write, and output-path resolution
- active-job deduplication for repeated identical processing requests
- cached-complete snapshots for repeated processing requests after a successful run, as long as the preview artifact still exists
- cancellation cleanup for in-flight preview artifacts

This keeps the desktop polling model stable while moving the actual preview workload to Go.

## 3. Output Paths Are Resolved In Go, But DICOM Export Is Still Explicitly Deferred

Phase 20 does not claim pure-Go Secondary Capture output. That work remains later in the migration plan.

What phase 20 does own is the path decision:

- if the caller supplies `outputPath`, Go validates that its parent directory exists and returns that resolved target path
- if the caller omits `outputPath`, Go assigns a cache-scoped `.dcm` target path beside the preview artifact namespace

That keeps process-job ownership and frontend result handling on the Go side without pretending the export writer is already migrated. The remaining export gap stays explicit for phases 29 through 31.

## 4. The HTTP Command Surface Is Now Live For Process Jobs

The sidecar router now implements:

- `POST /api/v1/commands/start_process_job`
- `POST /api/v1/commands/get_job`
- `POST /api/v1/commands/cancel_job`

No frontend adapter changes were required for this cutover because the runtime abstraction from phases 4 and 5 already reserved the Go-sidecar command surface. Phase 20 fills in the backend behavior behind that existing frontend API.

## 5. Validation Coverage

Validated with:

```bash
cd go-backend
gofmt -w internal/contracts/jobs.go internal/processing/request.go internal/processing/request_test.go internal/jobs/service.go internal/jobs/service_test.go internal/httpapi/router.go internal/httpapi/router_test.go
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./internal/processing ./internal/jobs ./internal/httpapi
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./...
```

Coverage now includes:

- request-resolution tests for preset/override validation
- job-service tests for process completion, cache hits, and dedupe/cancellation behavior
- router tests proving the live HTTP process-job flow can open a study, start processing, poll until completion, and produce a real preview artifact

## 6. Exit Criteria Check

Phase 20 exit criteria are now met:

- `start_process_job` is live in the Go sidecar
- processed preview artifacts are written by the Go-owned pipeline
- `get_job` returns Go-owned `processStudy` results
- cache-hit and duplicate-request semantics exist for process jobs
- the remaining DICOM export gap is explicit instead of being hidden behind the old Rust backend path
