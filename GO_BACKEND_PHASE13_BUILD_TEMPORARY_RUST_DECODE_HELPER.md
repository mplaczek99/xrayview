# Phase 13 Build Temporary Rust Decode Helper

This document completes phase 13 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The repo now includes a narrow Rust decode helper binary and a Go-side invocation package, so decode work can keep moving without turning the legacy Rust backend back into the application boundary.

Primary implementation references:

- [backend/src/study/decode_helper.rs](backend/src/study/decode_helper.rs)
- [backend/src/bin/xrayview-decode-helper.rs](backend/src/bin/xrayview-decode-helper.rs)
- [backend/tests/decode_helper_cli.rs](backend/tests/decode_helper_cli.rs)
- [backend/src/study/source_image.rs](backend/src/study/source_image.rs)
- [go-backend/internal/rustdecode/helper.go](go-backend/internal/rustdecode/helper.go)
- [go-backend/internal/rustdecode/helper_test.go](go-backend/internal/rustdecode/helper_test.go)
- [go-backend/cmd/xrayview-cli/main.go](go-backend/cmd/xrayview-cli/main.go)

## 1. Helper Scope Stays Narrow

Phase 13 deliberately does not reuse the old Rust backend command surface.

The new helper does one thing:

- decode one source study from a DICOM path

Its JSON payload is limited to the decode-adjacent data Go needs next:

- normalized source image width and height
- grayscale `f32` pixels
- min and max pixel values
- default window and invert metadata
- measurement scale when present
- study instance UID
- preserved export-metadata elements as normalized string values

What it does not do:

- no job orchestration
- no cache management
- no HTTP transport
- no frontend contract handling
- no reuse of the legacy Rust app state

That keeps Rust constrained to the subsystem phase 12 explicitly identified as the temporary exception.

## 2. Go Now Owns The Invocation Boundary

The new Go package in [go-backend/internal/rustdecode/helper.go](go-backend/internal/rustdecode/helper.go) is the only place that knows how to run the helper process.

The rest of the Go backend sees a decoded study struct, not:

- cargo invocation details
- helper stderr handling
- raw JSON decoding
- payload validation rules

This package:

- resolves the helper command from `XRAYVIEW_RUST_DECODE_HELPER_BIN` when provided
- falls back to a repo-local dev command for migration work
- appends the decode request arguments
- turns helper failures into normal Go errors with stderr included
- validates that pixel count matches image dimensions before returning decoded data

That is the phase 13 process-invocation layer called for by the migration plan.

## 3. Dev-Facing Validation Path

Phase 13 adds a Go CLI entrypoint:

```bash
go run ./cmd/xrayview-cli decode-source ../images/sample-dental-radiograph.dcm
```

This command exercises the full boundary:

- Go launches the Rust helper
- Rust decodes the study
- Go parses and validates the helper payload
- Go prints a compact summary instead of dumping the full pixel array

That keeps the new helper easy to inspect without prematurely wiring a transport or frontend command that phase 14+ will likely reshape.

## 4. Preserved Metadata Is Explicit

The helper payload includes the preserved source-metadata subset as structured elements:

- tag group
- tag element
- VR
- normalized string values

This is intentionally narrower than cloning the full Rust export pipeline, but it preserves the exact subset already identified in [backend/src/study/source_image.rs](backend/src/study/source_image.rs) as relevant for later secondary-capture export work.

## 5. Validation

Validated with:

```bash
cargo test --manifest-path backend/Cargo.toml
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./...
go run ./cmd/xrayview-cli decode-source ../images/sample-dental-radiograph.dcm
```

Concrete checks now covered:

- the Rust helper binary parses `--input` and emits normalized JSON
- Rust unit coverage verifies preserved metadata and measurement scale on a synthetic decoded study
- Go unit coverage verifies command construction, payload parsing, validation, and helper error surfacing
- Go integration coverage runs the real Rust helper against the repo sample DICOM

## 6. Exit Criteria Check

Phase 13 exit criteria are now met:

- a narrow Rust decode helper exists
- helper output is consumed cleanly by Go
- helper scope is explicitly documented and bounded
- the migration can proceed into phase 14 without pretending pure Go decode is already solved
