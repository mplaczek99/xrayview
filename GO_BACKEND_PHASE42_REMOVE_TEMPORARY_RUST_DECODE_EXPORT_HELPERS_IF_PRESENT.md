# Phase 42 Remove Temporary Rust Decode/Export Helpers if Present

This document completes phase 42 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The supported desktop and CLI workflows no longer depend on temporary Rust decode or export helper binaries. Source-study decode and Secondary Capture export now execute directly in Go, and the helper-only codepaths have been deleted instead of being left behind as dormant compatibility baggage.

Primary implementation references:

- [go-backend/internal/dicommeta/decode.go](go-backend/internal/dicommeta/decode.go)
- [go-backend/internal/jobs/service.go](go-backend/internal/jobs/service.go)
- [go-backend/cmd/xrayview-cli/main.go](go-backend/cmd/xrayview-cli/main.go)
- [go-backend/cmd/xrayview-cli/legacy_cli.go](go-backend/cmd/xrayview-cli/legacy_cli.go)
- [go-backend/internal/export/secondary_capture.go](go-backend/internal/export/secondary_capture.go)
- [go-backend/internal/export/writer.go](go-backend/internal/export/writer.go)
- [backend/src/study/mod.rs](backend/src/study/mod.rs)
- [backend/src/export/mod.rs](backend/src/export/mod.rs)
- [README.md](README.md)
- [go-backend/README.md](go-backend/README.md)

## 1. Source-Study Decode Is Now Pure Go

The last live Rust boundary was source-study decode.

Phase 42 removes that boundary by adding [go-backend/internal/dicommeta/decode.go](go-backend/internal/dicommeta/decode.go), which reuses the existing Go DICOM parser and now owns:

- source-image pixel decode for the currently supported workflow matrix
- default window and invert extraction
- slope/intercept handling
- preserved export-metadata extraction
- generated study-instance UIDs when the source omits one

The live job service and both CLI surfaces now consume that Go decoder directly instead of spawning a helper process.

## 2. Export No Longer Has A Rust Fallback Switch

Phase 29 had already proven pure-Go Secondary Capture export, but phase 30 kept a narrow Rust fallback alive behind an environment flag.

Phase 42 removes that leftover indirection:

- [go-backend/internal/export/writer.go](go-backend/internal/export/writer.go) now resolves only the Go writer
- processed DICOM output still flows through the same Go-owned job orchestration, but there is no helper mode left to select
- export round-trip coverage now validates against the Go decoder rather than the deleted helper payload

That means both sides of the DICOM in/DICOM out workflow are now owned by Go.

## 3. Helper Code Was Deleted Rather Than Left Dormant

This phase removes the migration-era helper implementations and tests from both sides of the repo:

- the Go helper invocation packages under `go-backend/internal/rustdecode/` and `go-backend/internal/rustexport/`
- the Rust helper binaries and helper-only tests under `backend/src/bin/` and `backend/tests/`
- the Rust helper modules that only existed to support those binaries

The Rust backend crate still exists for phase 43, but it no longer provides decode/export helper binaries for the supported product flows.

## 4. Validation

Validated with:

```bash
env GOCACHE=/tmp/xrayview-go-build-cache GOTMPDIR=/tmp/xrayview-go-tmp GOPATH=/tmp/xrayview-go-path \
  go -C go-backend test ./...
cargo test --manifest-path backend/Cargo.toml
```

Concrete checks now covered:

- Go CLI workflows decode, render, process, analyze, and export without helper invocation
- Go job-service and router coverage complete render/process/analyze flows with the pure-Go decoder
- Go export coverage round-trips generated Secondary Capture output through the pure-Go decoder
- the remaining Rust backend crate still compiles and tests after helper-only modules are removed

## 5. Known Limits After Phase 42

Removing the helpers does not magically prove universal DICOM parity.

The current Go decoder explicitly documents these remaining limits:

- deflated transfer syntax is still rejected
- encapsulated multi-frame source decode is still unsupported
- the committed repo corpus is still narrow and mostly native single-frame monochrome data

Those are now normal Go-backend limitations to evaluate, not reasons to keep the Rust helpers alive by inertia.

## 6. Exit Criteria Check

Phase 42 exit criteria are now met:

- temporary Rust decode helper usage has been replaced in the supported Go runtime paths
- temporary Rust export helper selection has been removed
- helper code is deleted instead of merely unused
- the parity-oriented Go test suite runs without helper dependencies
