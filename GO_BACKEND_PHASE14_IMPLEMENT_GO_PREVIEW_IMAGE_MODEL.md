# Phase 14 Implement Go Preview Image Model

This document completes phase 14 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Go backend now has a shared imaging model for decoded source pixels and rendered preview buffers, and the current Rust decode-helper boundary feeds that model directly instead of carrying a private duplicate shape.

Primary implementation references:

- [go-backend/internal/imaging/model.go](go-backend/internal/imaging/model.go)
- [go-backend/internal/imaging/model_test.go](go-backend/internal/imaging/model_test.go)
- [go-backend/internal/rustdecode/helper.go](go-backend/internal/rustdecode/helper.go)
- [go-backend/internal/rustdecode/helper_test.go](go-backend/internal/rustdecode/helper_test.go)
- [go-backend/cmd/xrayview-cli/main.go](go-backend/cmd/xrayview-cli/main.go)
- [backend/src/study/decode_helper.rs](backend/src/study/decode_helper.rs)

## 1. The Stable Image Boundary Is Now Explicit

Phase 14 introduces `go-backend/internal/imaging`, which is the Go-owned image model for the next migration stages.

It defines:

- `ImageFormat` constants for `gray-f32`, `gray8`, and `rgba8`
- `WindowLevel` as a shared render-default type
- `SourceImage` for decoded grayscale `f32` source pixels
- `PreviewImage` for rendered byte buffers

This keeps the decode output, the upcoming windowing logic, and later preview/export code anchored on one package instead of re-declaring similar structs in multiple places.

## 2. The Decode Helper Now Targets The Shared Model

The phase 13 Go helper package no longer owns its own image structs.

Instead:

- `rustdecode.SourceStudy` now embeds `imaging.SourceImage`
- helper payload validation reuses `SourceImage.Validate()`
- the Rust helper now emits explicit source-image format metadata: `gray-f32`

That matters because phase 14 is the point where the image boundary stops being an informal shape and becomes a real contract between decode and render work.

For local compatibility, `SourceImage` still defaults missing format fields to `gray-f32` during JSON decode, so an older prebuilt helper binary in `target/debug/` does not break the Go side while the workspace catches up.

## 3. Validation Is Centralized Instead Of Ad Hoc

The shared model now owns the geometry and buffer checks that later phases depend on:

- source images must have non-zero dimensions
- source images must use `gray-f32`
- source pixel count must exactly match `width * height`
- source default-window width must be valid when present
- preview images must declare either `gray8` or `rgba8`
- preview byte count must match dimensions and format

This is the concrete stability work phase 14 called for. Later render and processing phases can assume these invariants instead of rechecking them in every caller.

## 4. Validation Coverage

Validated with:

```bash
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./...
cargo test --manifest-path backend/Cargo.toml decode_helper
```

Concrete checks now covered:

- shared-model JSON decode defaults the source format for helper backward compatibility
- valid source and preview image models pass shared validation
- invalid pixel counts and invalid preview formats fail validation
- Go helper tests assert the explicit `gray-f32` source format
- Go helper tests reject unexpected source-image formats
- Rust helper tests serialize the explicit source-image format

## 5. Exit Criteria Check

Phase 14 exit criteria are now met:

- the Go backend has a stable in-memory image model
- the model includes width, height, pixel buffer, format, min/max, default window, and invert semantics
- the helper-to-Go decode boundary uses that shared model
- render pipeline work in phases 15 and 16 now has a stable type boundary to build on
