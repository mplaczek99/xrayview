# Phase 16 Port Base Render Pipeline to Go

This document completes phase 16 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Go backend now has a reusable base render pipeline that turns helper-decoded source studies into grayscale preview buffers and writes PNG previews without going back through the legacy Rust app layer.

Primary implementation references:

- [go-backend/internal/render/render_plan.go](go-backend/internal/render/render_plan.go)
- [go-backend/internal/render/preview_png.go](go-backend/internal/render/preview_png.go)
- [go-backend/internal/render/render_plan_test.go](go-backend/internal/render/render_plan_test.go)
- [go-backend/internal/render/preview_png_test.go](go-backend/internal/render/preview_png_test.go)
- [go-backend/cmd/xrayview-cli/main.go](go-backend/cmd/xrayview-cli/main.go)
- [backend/src/render/render_plan.rs](backend/src/render/render_plan.rs)
- [backend/src/preview.rs](backend/src/preview.rs)

## 1. The Base Grayscale Render Plan Now Exists In Go

Phase 16 builds directly on the shared phase 14 image model and the phase 15 windowing math.

The new Go render plan now:

- renders a decoded `imaging.SourceImage` into a `gray8` preview buffer
- uses the same default-window and full-range semantics already ported in phase 15
- applies source-image inversion after grayscale mapping, matching the Rust pipeline
- keeps the render path independent from later processing, palette, compare, and job orchestration phases

That gives the migration a real end-to-end preview path before touching the live desktop job flow in phase 17.

## 2. PNG Preview Writing Is Now Go-Owned

Phase 16 also adds PNG encoding for the shared preview model.

The new preview writer:

- validates preview geometry and format before encoding
- supports both `gray8` and `rgba8` buffers so later phases can reuse it
- writes a standard PNG directly from the Go-owned preview image type

This is the missing piece that turns render parity from an in-memory exercise into a concrete artifact comparison step.

## 3. There Is Now A Dev-Facing Decode-To-Preview Path

The Go CLI now exposes:

```bash
go run ./cmd/xrayview-cli render-preview ../images/sample-dental-radiograph.dcm /tmp/xrayview-preview.png
go run ./cmd/xrayview-cli render-preview --full-range ../images/sample-dental-radiograph.dcm /tmp/xrayview-preview-full-range.png
```

This command path is intentionally narrow:

- Go invokes the temporary Rust decode helper from phase 13
- Go renders the grayscale preview with the phase 16 render plan
- Go writes the PNG preview itself

That keeps phase 16 focused on the base preview pipeline without prematurely claiming that the live `start_render_job` or `get_job` HTTP flow is migrated. That cutover still belongs to phase 17.

## 4. Validation Is Based On The Rust Preview Fixture

Phase 16 validation does not just test synthetic pixels.

Coverage now includes:

- unit tests mirroring the Rust render-plan semantics for default windowing, full-range mapping, and inversion
- PNG encode/decode validation for the Go preview writer
- fixture parity against the committed Rust preview image at `backend/tests/fixtures/parity/sample-dental-radiograph/render-preview.png`
- a real decode-helper integration path on the repo sample DICOM before rendering

Using the committed Rust preview fixture matters because PNG encoder byte streams may differ while decoded image content must not.

## 5. Validation Coverage

Validated with:

```bash
cd go-backend
gofmt -w cmd/xrayview-cli/main.go internal/render/*.go
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./...
go run ./cmd/xrayview-cli render-preview ../images/sample-dental-radiograph.dcm /tmp/xrayview-preview.png
```

The render-preview command gives a direct manual check that the new Go-owned render path can decode, render, and write a preview artifact without the Rust backend crate acting as the application boundary.

## 6. Exit Criteria Check

Phase 16 exit criteria are now met:

- Go can render the default grayscale preview from the decoded source image
- full-range preview behavior is available through the same render plan
- Go owns PNG preview writing for the shared preview model
- parity is checked against the existing Rust render fixture
- the repo now has a concrete Go render pipeline ready for phase 17 to wrap in live job and transport behavior
