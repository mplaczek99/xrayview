# Phase 18 Port Grayscale Processing Controls

This document completes phase 18 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Go backend now has Rust-equivalent grayscale processing primitives for invert, brightness, contrast, and histogram equalization, along with a narrow CLI path and parity coverage against a Rust-generated sample fixture.

Primary implementation references:

- [go-backend/internal/processing/grayscale.go](go-backend/internal/processing/grayscale.go)
- [go-backend/internal/processing/grayscale_test.go](go-backend/internal/processing/grayscale_test.go)
- [go-backend/cmd/xrayview-cli/main.go](go-backend/cmd/xrayview-cli/main.go)
- [backend/src/processing.rs](backend/src/processing.rs)
- [backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-grayscale-preview.png](backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-grayscale-preview.png)

## 1. The Rust Grayscale Operator Stack Now Exists In Go

Phase 18 ports the processing logic from `backend/src/processing.rs` into `go-backend/internal/processing`.

The new Go package now covers:

- grayscale inversion
- brightness lookup-table composition
- contrast lookup-table composition
- histogram equalization
- the same fixed operator order as Rust: invert, brightness, contrast, then equalization

Like the Rust implementation, the point operations are composed into a single lookup table and only flushed before histogram equalization, because equalization depends on the already-adjusted pixel distribution.

## 2. The Go Path Stays Narrow On Purpose

Phase 18 does not claim the full process job path yet.

It deliberately stops at reusable grayscale processing math plus a dev-facing command:

```bash
go run ./cmd/xrayview-cli process-preview ../images/sample-dental-radiograph.dcm /tmp/xrayview-processed.png --brightness 10 --contrast 1.4 --equalize
```

That command:

- decodes through the temporary Rust helper from phase 13
- renders the source preview through the Go render pipeline from phases 15 and 16
- applies the ported grayscale processing controls in Go
- writes the processed grayscale PNG in Go

Palette application, compare composition, export, and live `start_process_job` cutover still remain for later phases.

## 3. Validation Covers Operator Semantics And A Real Sample Fixture

Phase 18 validation is split into two layers.

Focused unit coverage now checks:

- invert ordering relative to brightness and contrast
- brightness clamp behavior
- contrast rounding and clamp behavior
- histogram equalization redistribution
- the requirement that equalization runs after the pending point operations
- flat-image equalization no-op behavior

Fixture parity coverage now compares the Go output against a Rust-generated grayscale sample preview at:

- `backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-grayscale-preview.png`

That fixture uses the same sample DICOM and the same grayscale control set as the xray preset, but with palette output intentionally disabled so phase 18 stays focused on grayscale math before phase 19 ports palette and compare logic.

## 4. Validation Coverage

Validated with:

```bash
cargo run --quiet --manifest-path backend/Cargo.toml --bin xrayview-backend -- --input images/sample-dental-radiograph.dcm --preview-output backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-grayscale-preview.png --preset xray --palette none
cd go-backend
gofmt -w cmd/xrayview-cli/main.go internal/processing/grayscale.go internal/processing/grayscale_test.go
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./internal/processing ./cmd/xrayview-cli
```

The first command matters because it pins the golden sample to the current Rust backend instead of letting Go compare against a fixture it generated itself.

## 5. Exit Criteria Check

Phase 18 exit criteria are now met:

- Go has reusable grayscale processing math
- invert, brightness, contrast, and equalization all exist in the Go backend
- operator-order semantics are explicitly tested
- a Rust-backed golden image comparison exists for the sample study
- the repo is ready for phase 19 to add palette and compare behavior on top of the new grayscale core
