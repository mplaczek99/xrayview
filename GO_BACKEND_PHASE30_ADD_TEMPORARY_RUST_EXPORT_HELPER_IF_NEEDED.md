# Phase 30 Add Temporary Rust Export Helper if Needed

This document completes phase 30 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The repo now includes a narrow Rust Secondary Capture export helper and a Go-owned writer-selection layer, so the migration can keep moving even if the pure-Go export path needs to be bypassed temporarily on real studies.

Primary implementation references:

- [backend/src/export/helper.rs](backend/src/export/helper.rs)
- [backend/src/bin/xrayview-export-helper.rs](backend/src/bin/xrayview-export-helper.rs)
- [backend/tests/export_helper_cli.rs](backend/tests/export_helper_cli.rs)
- [go-backend/internal/rustexport/helper.go](go-backend/internal/rustexport/helper.go)
- [go-backend/internal/rustexport/helper_test.go](go-backend/internal/rustexport/helper_test.go)
- [go-backend/internal/export/writer.go](go-backend/internal/export/writer.go)
- [go-backend/internal/jobs/service.go](go-backend/internal/jobs/service.go)
- [go-backend/cmd/xrayview-cli/main.go](go-backend/cmd/xrayview-cli/main.go)

## 1. Helper Scope Stays Narrow

Phase 30 does not revive the legacy Rust backend surface.

The new helper does one thing:

- write one Secondary Capture DICOM file from a normalized preview image and preserved source metadata

Its request is intentionally narrow:

- processed preview width, height, format, and pixel bytes
- preserved metadata subset already identified for export
- output path supplied as a direct CLI flag

What it does not do:

- no study registry
- no jobs
- no cache ownership
- no frontend contract handling
- no reuse of the old Rust command bridge

That keeps Rust constrained to a temporary export-only escape hatch.

## 2. Go Owns The Export Strategy Boundary

The Go backend now resolves Secondary Capture writing through [go-backend/internal/export/writer.go](go-backend/internal/export/writer.go).

That layer:

- keeps pure Go export as the default behavior
- enables the Rust helper only when `XRAYVIEW_SECONDARY_CAPTURE_EXPORTER=rust-helper`
- resolves a helper binary from `XRAYVIEW_RUST_EXPORT_HELPER_BIN` when provided
- falls back to the repo-local dev command for migration work when the helper mode is explicitly selected

The rest of the Go backend sees a writer interface, not:

- cargo invocation details
- stdin JSON payload rules
- helper stderr parsing
- helper binary path lookup

That keeps phase 30 aligned with the migration plan requirement that Go remain the primary owner.

## 3. Process Jobs Still Belong To Go

[go-backend/internal/jobs/service.go](go-backend/internal/jobs/service.go) now writes processed DICOM output through the configured writer interface.

Go still owns:

- request validation
- study decode
- preview processing
- preview artifact writing
- job progress and cancellation
- result payload assembly

If the Rust helper is selected, it is used only for the final DICOM write step.

This avoids the phase 30 anti-pattern of routing export through the legacy backend as a whole.

## 4. CLI And App Startup Use The Same Selection Logic

The export selection is not hidden inside just one code path.

- [go-backend/internal/app/app.go](go-backend/internal/app/app.go) uses the environment-resolved writer for live sidecar jobs
- [go-backend/cmd/xrayview-cli/main.go](go-backend/cmd/xrayview-cli/main.go) uses the same writer resolution for `export-secondary-capture`

That gives one consistent way to validate:

- default pure-Go export
- explicit Rust-helper fallback

without creating divergent behavior between desktop and CLI flows.

## 5. Validation

Validated with:

```bash
cargo test --manifest-path backend/Cargo.toml
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./...
```

Concrete checks now covered:

- Rust unit tests validate preview-format checks, preserved-VR validation, and round-trip helper output
- Rust CLI coverage verifies the helper reads a JSON request from stdin and writes a Secondary Capture DICOM
- Go unit coverage verifies helper command construction, payload encoding, stderr propagation, and environment-based writer selection
- Go process-job tests still verify processed preview and processed DICOM artifacts complete through the Go-owned workflow
- Go integration coverage exercises the real Rust export helper and validates the produced DICOM metadata

## 6. Exit Criteria Check

Phase 30 exit criteria are now met:

- a narrow Rust export helper exists
- Go owns the export invocation boundary
- process jobs can complete while keeping Go as the primary application owner
- the helper remains optional and explicitly bounded instead of becoming a second backend
