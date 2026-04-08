# Phase 17 Cut `renderStudy` to Go

This document completes phase 17 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Go sidecar now owns the first live job path in the desktop app: source preview rendering through `start_render_job`, `get_job`, and `cancel_job`.

Primary implementation references:

- [go-backend/internal/jobs/service.go](go-backend/internal/jobs/service.go)
- [go-backend/internal/httpapi/router.go](go-backend/internal/httpapi/router.go)
- [go-backend/internal/cache/store.go](go-backend/internal/cache/store.go)
- [go-backend/internal/contracts/jobs.go](go-backend/internal/contracts/jobs.go)
- [go-backend/internal/jobs/service_test.go](go-backend/internal/jobs/service_test.go)
- [go-backend/internal/httpapi/router_test.go](go-backend/internal/httpapi/router_test.go)
- [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts)
- [frontend/src/lib/runtime.ts](frontend/src/lib/runtime.ts)

## 1. The Go Sidecar Now Executes Real Render Jobs

Phase 16 stopped at reusable render code plus the `render-preview` CLI path. Phase 17 wraps that pipeline in live job orchestration.

The Go backend now:

- accepts `start_render_job` with a registered study ID
- tracks the job under the frozen v1 `JobSnapshot` contract
- decodes the study through the temporary Rust helper owned by Go
- renders the grayscale preview in Go
- writes the preview artifact into the cache tree expected by the Tauri asset protocol
- returns completed render payloads through `get_job`

This is the first desktop-visible feature where the React app can ask Go for work and poll Go for the result.

## 2. Preview Artifact Paths Stay Compatible With The Existing Shell

The render job writes preview PNGs under:

- `$CACHE_DIR/artifacts/render/<fingerprint>.png`

Under the current Tauri sidecar setup that resolves to the same temp-space shape already allowed by the shell asset scope:

- `$TEMP/xrayview/cache/artifacts/...`

That matters because the frontend still converts preview file paths with `convertFileSrc(...)`. Phase 17 keeps that stable instead of inventing a new preview transport while the shell is still Tauri.

## 3. Job Registry Semantics Exist In Go For The Render Path

The Go job service now covers the live semantics needed for preview rendering:

- queued, running, cancelling, completed, failed, and cancelled states
- stage/message progress updates that match the existing desktop expectations
- explicit `cancel_job` handling for in-flight render work
- active-job deduplication for repeated `start_render_job` calls on the same study
- in-memory cache hits for repeated render requests after a successful completion, while still validating that the preview artifact still exists on disk

That gives phase 17 enough real orchestration to exercise the desktop polling flow without waiting for process/analyze migration work.

## 4. The HTTP Command Surface Matches The Frontend Adapter

The command router now implements:

- `POST /api/v1/commands/start_render_job`
- `POST /api/v1/commands/get_job`
- `POST /api/v1/commands/cancel_job`

The frontend did not need a new API shape for this phase because phase 5 already reserved the Go-sidecar adapter surface. Phase 17 fills in the Go backend behavior behind that existing frontend/runtime abstraction.

## 5. Validation Coverage

Validated with:

```bash
cd go-backend
gofmt -w internal/contracts/jobs.go internal/cache/store.go internal/jobs/service.go internal/jobs/service_test.go internal/httpapi/router.go internal/httpapi/router_test.go internal/app/app.go
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./...
```

Coverage now includes:

- direct job-service tests for render completion, cache hits, deduplication, and cancellation
- router tests proving the live HTTP render-job flow can open a study, start rendering, poll until completion, and produce a real preview artifact

## 6. Exit Criteria Check

Phase 17 exit criteria are now met:

- `startRenderStudy` works through the Go sidecar
- `getJob` returns live Go-owned render job snapshots
- preview artifacts are written in the cache path the desktop shell already understands
- render preview jobs now work in the live app when the desktop runtime is `go-sidecar`
