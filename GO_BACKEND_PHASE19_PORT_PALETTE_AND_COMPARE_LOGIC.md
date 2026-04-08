# Phase 19 Port Palette And Compare Logic

This document completes phase 19 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Go backend now owns the full preview-side processing pipeline: grayscale controls from phase 18, pseudocolor palette application, and compare-mode side-by-side composition.

Primary implementation references:

- [go-backend/internal/processing/palette.go](go-backend/internal/processing/palette.go)
- [go-backend/internal/processing/compare.go](go-backend/internal/processing/compare.go)
- [go-backend/internal/processing/pipeline.go](go-backend/internal/processing/pipeline.go)
- [go-backend/internal/processing/pipeline_test.go](go-backend/internal/processing/pipeline_test.go)
- [go-backend/cmd/xrayview-cli/main.go](go-backend/cmd/xrayview-cli/main.go)
- [backend/src/palette.rs](backend/src/palette.rs)
- [backend/src/compare.rs](backend/src/compare.rs)
- [backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-preview.png](backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-preview.png)
- [backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-compare-preview.png](backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-compare-preview.png)

## 1. The Remaining Preview Pipeline Stages Now Exist In Go

Phase 19 ports the Rust preview finishing stages from `backend/src/palette.rs` and `backend/src/compare.rs` into `go-backend/internal/processing`.

The new Go code now covers:

- `hot` palette mapping
- `bone` palette mapping
- palette-name normalization and validation
- compare-mode image composition
- the same output format expectations as Rust: palette output is RGBA, compare output is RGBA, and compare doubles the rendered width

The pipeline orchestration now mirrors the Rust order exactly:

1. render source image to grayscale
2. apply grayscale controls
3. optionally apply palette
4. optionally build a side-by-side compare canvas

## 2. The Dev CLI Can Exercise All Current Preview Modes

The narrow CLI path from phase 18 is now widened just enough to cover every preview mode phase 19 is responsible for.

`process-preview` now accepts:

- `--palette <none|hot|bone>`
- `--compare`

Example commands:

```bash
go run ./cmd/xrayview-cli process-preview ../images/sample-dental-radiograph.dcm /tmp/xrayview-processed-bone.png --brightness 10 --contrast 1.4 --equalize --palette bone
go run ./cmd/xrayview-cli process-preview ../images/sample-dental-radiograph.dcm /tmp/xrayview-compare.png --brightness 10 --contrast 1.4 --equalize --palette bone --compare
```

This still stops short of the live `processStudy` job cutover. That remains phase 20.

## 3. Validation Covers Palette Math, Compare Geometry, And Rust Fixtures

Focused unit coverage now checks:

- `hot` palette breakpoint behavior
- `bone` palette formula behavior
- promotion from grayscale to RGBA during palette application
- compare composition for both grayscale and RGBA right-hand inputs
- compare input validation for left-side format and matching dimensions

Fixture parity coverage now compares Go output against the committed Rust-generated preview fixtures at:

- `backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-preview.png`
- `backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-compare-preview.png`

Those fixture tests use the phase 13 Rust decode helper, the Go render pipeline, the Go grayscale controls from phase 18, and the new Go palette/compare stages from phase 19. That keeps the comparison pinned to Rust-owned expected output instead of self-validating against Go-generated images.

## 4. Validation Coverage

Validated with:

```bash
cd go-backend
gofmt -w cmd/xrayview-cli/main.go cmd/xrayview-cli/main_test.go internal/processing/grayscale.go internal/processing/palette.go internal/processing/compare.go internal/processing/pipeline.go internal/processing/palette_test.go internal/processing/compare_test.go internal/processing/pipeline_test.go
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./internal/processing ./cmd/xrayview-cli
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./...
```

## 5. Exit Criteria Check

Phase 19 exit criteria are now met:

- Go can produce `hot` and `bone` palette previews
- Go can compose compare previews at the correct doubled width
- the output format behavior matches the current Rust pipeline
- Rust-backed preview fixtures now cover both the normal processed preview and compare-mode preview
- the repo is ready for phase 20 to move process-job orchestration onto the Go backend
