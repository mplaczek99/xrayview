# Phase 23 Port Job Registry To Go

This document completes phase 23 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Go backend now has a dedicated job registry that owns job-state transitions, duplicate-fingerprint reuse, cached-complete snapshots, cancellation intent, and terminal-state release semantics.

Primary implementation references:

- [go-backend/internal/jobs/registry.go](go-backend/internal/jobs/registry.go)
- [go-backend/internal/jobs/registry_test.go](go-backend/internal/jobs/registry_test.go)
- [go-backend/internal/jobs/service.go](go-backend/internal/jobs/service.go)
- [go-backend/internal/jobs/service_test.go](go-backend/internal/jobs/service_test.go)
- [backend/src/jobs/registry.rs](backend/src/jobs/registry.rs)

## 1. Job State Ownership Moved Out Of The Go Service

Before this phase, the Go `jobs.Service` owned its own mutexes, active fingerprint map, job snapshots, and cancellation callbacks directly. That was enough for the initial render and process cutovers, but it kept the state machine mixed into orchestration code.

Phase 23 extracts that responsibility into `go-backend/internal/jobs/registry.go`:

- start new queued jobs
- reuse active duplicate fingerprints
- mint cached-complete jobs
- update progress snapshots
- transition into completed, failed, cancelling, and cancelled states
- release active fingerprints when jobs reach terminal states

That gives Go the same explicit backend boundary that Rust already had in `backend/src/jobs/registry.rs`.

## 2. Cancellation Semantics Are Now Centralized

The new Go registry tracks cancellation intent separately from the worker goroutine and owns the state transitions for:

- queued cancellation becoming immediate `cancelled`
- running cancellation becoming `cancelling`
- cancellation preventing later progress writes from flipping the job back to `running`
- completion/failure calls collapsing into `cancelled` if cancellation already won the race

The job service still owns actual work execution and artifact cleanup, but it now delegates the state machine to the registry instead of mutating snapshots directly.

## 3. Cached And Duplicate Job Behavior Is Registry-Level

Cached-complete jobs and duplicate active requests no longer depend on ad hoc service maps. The registry now creates:

- `fromCache=true` completed snapshots with `cacheHit` progress metadata
- reused active jobs for identical in-flight fingerprints
- fresh jobs again after completion, failure, or cancellation releases the fingerprint

That matches the frontend polling model more directly and makes the concurrency rules testable at the registry layer.

## 4. Validation Coverage

Validated with:

```bash
gofmt -w go-backend/internal/jobs/registry.go go-backend/internal/jobs/registry_test.go go-backend/internal/jobs/service.go
cd go-backend
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./internal/jobs ./internal/httpapi
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./...
```

Coverage now includes:

- explicit registry tests for duplicate active jobs
- cached-complete snapshot creation tests
- fingerprint release after terminal states
- queued cancellation and running cancellation tests
- cancellation-vs-progress and cancellation-vs-completion/failure tests
- existing service and HTTP tests to confirm the public Go backend behavior still matches the desktop contract

## 5. Exit Criteria Check

Phase 23 exit criteria are now met:

- Go has a dedicated job registry mirroring the Rust backend boundary
- job-state transitions are explicit and tested directly
- duplicate requests, cache hits, completion, failure, and cancellation semantics are Go-owned
- the sidecar service still satisfies frontend polling expectations after the refactor
