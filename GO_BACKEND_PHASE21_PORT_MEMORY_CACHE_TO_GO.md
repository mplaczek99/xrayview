# Phase 21 Port Memory Cache to Go

This document completes phase 21 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Go backend now owns the in-memory fingerprint cache as a first-class subsystem instead of keeping render and process cache behavior split across ad hoc maps inside the job service.

Primary implementation references:

- [go-backend/internal/cache/memory.go](go-backend/internal/cache/memory.go)
- [go-backend/internal/cache/memory_test.go](go-backend/internal/cache/memory_test.go)
- [go-backend/internal/jobs/service.go](go-backend/internal/jobs/service.go)
- [go-backend/internal/jobs/service_test.go](go-backend/internal/jobs/service_test.go)
- [backend/src/cache/memory.rs](backend/src/cache/memory.rs)

## 1. The Go Backend Now Has A Dedicated Typed Memory Cache

Phase 17 introduced render cache hits and phase 20 extended that idea to process jobs, but both paths were still implemented as job-service-local maps. Phase 21 moves that behavior into a shared Go cache module with explicit typed entry points:

- `StoreRender` and `LoadRender`
- `StoreProcess` and `LoadProcess`

That makes the cache a real backend concern instead of incidental job-service state, while keeping the scope intentionally narrow and in-memory only.

## 2. Artifact Validation Now Mirrors The Rust Cache Semantics

The new Go memory cache checks artifact existence before serving a cached result and evicts stale entries immediately.

The behavior now matches the Rust backend contract:

- render cache hits require the preview PNG to still exist
- process cache hits require both the preview PNG and the processed DICOM path to still exist
- invalid or mismatched typed entries are discarded instead of being reused

This closes the previous gap where Go process cache hits could be served even when the cached DICOM artifact path did not exist.

## 3. Cached Results Preserve The Original Typed Payload

The Go job service now stores and returns the cached typed result verbatim instead of reconstructing render/process payloads on cache hit.

That matters for Rust parity because cached render results are keyed by the underlying study input, not the transient `studyId`. Reopening the same file can now reuse the preview artifact while preserving the originally cached payload `studyId`, matching the existing parity fixture expectations from the Rust backend.

## 4. Render Cache Fingerprints Now Match Rust

The Go render fingerprint no longer includes `studyId`. It is now based on:

- cache namespace
- input path

That aligns with the Rust `render-study-v1` fingerprint behavior and allows cache reuse across separate registrations of the same DICOM file.

## 5. Validation Coverage

Validated with:

```bash
cd go-backend
gofmt -w internal/cache/memory.go internal/cache/memory_test.go internal/jobs/service.go internal/jobs/service_test.go
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./internal/cache ./internal/jobs
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./internal/httpapi ./internal/app ./...
```

Coverage now includes:

- direct memory-cache tests for stale preview invalidation
- direct memory-cache tests for process entries requiring both preview and DICOM artifacts
- typed retrieval invalidation tests for mismatched cached entry kinds
- job-service tests proving render cache reuse across reopened studies
- job-service tests proving process cache hits only occur once the referenced DICOM artifact exists

## 6. Exit Criteria Check

Phase 21 exit criteria are now met:

- the Go backend owns the in-memory fingerprint cache
- artifact existence checks are centralized and Rust-compatible
- render and process cached results are retrieved through typed Go APIs
- cache semantics now match Rust closely enough for the migrated job paths
