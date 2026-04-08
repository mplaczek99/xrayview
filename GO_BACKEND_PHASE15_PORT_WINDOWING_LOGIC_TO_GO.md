# Phase 15 Port Windowing Logic to Go

This document completes phase 15 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Go backend now has Rust-equivalent grayscale windowing primitives, including embedded-default resolution, manual window selection, full-range fallback, and byte-clamp behavior that later render work can reuse directly.

Primary implementation references:

- [go-backend/internal/render/windowing.go](go-backend/internal/render/windowing.go)
- [go-backend/internal/render/windowing_test.go](go-backend/internal/render/windowing_test.go)
- [backend/src/render/windowing.rs](backend/src/render/windowing.rs)

## 1. Window Semantics Are Now Explicit In Go

Phase 15 adds `go-backend/internal/render`, starting with the same core window math the Rust backend uses today.

It ports:

- DICOM-style window center/width transforms
- full-range linear mapping when no usable window is active
- byte rounding and clamp behavior
- default, full-range, and manual window-mode resolution

This keeps phase 15 focused on grayscale mapping semantics instead of prematurely mixing in render jobs, preview encoding, or processing controls.

## 2. Default And Manual Window Resolution Match The Rust Fallback Model

The new `WindowMode` helpers preserve the Rust behavior:

- default mode uses `SourceImage.DefaultWindow` when present and valid
- full-range mode always bypasses the embedded window
- manual mode uses the caller-supplied window instead of the embedded one
- invalid windows resolve to no transform, which cleanly falls back to full-range mapping in the render pipeline

That last point matters because the Rust backend already treats invalid window widths as unusable rather than trying to coerce them.

## 3. Clamp And Breakpoint Behavior Is Covered With Golden Tests

Phase 15’s tests intentionally mirror the Rust semantics instead of inventing a Go-specific interpretation.

Coverage now includes:

- the same DICOM breakpoint expectations used in Rust: `0 -> 0`, `127.5 -> 128`, `255 -> 255` for a `center=128`, `width=256` window
- full-range linear mapping using the full source min/max range
- clamp rounding at the `+0.5` threshold
- default-window fallback when the source window is missing or invalid
- manual-window override behavior
- full-range mode ignoring an embedded source window

## 4. Validation Coverage

Validated with:

```bash
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./...
cargo test --manifest-path backend/Cargo.toml render::windowing
```

The Rust test command confirms the parity source behavior still matches its own expectations, and the Go test suite now exercises the ported semantics directly.

## 5. Exit Criteria Check

Phase 15 exit criteria are now met:

- Rust window mapping behavior has a Go implementation
- default, manual, and full-range window handling all exist in Go
- clamp behavior is explicitly tested
- parity is validated with focused unit coverage before phase 16 starts the base render pipeline
