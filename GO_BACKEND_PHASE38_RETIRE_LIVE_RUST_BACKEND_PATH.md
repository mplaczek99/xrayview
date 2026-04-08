# Phase 38 Retire Live Rust Backend Path

This document completes phase 38 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The desktop app now defaults to the Go sidecar runtime instead of the in-process Rust bridge, while `legacy-rust` remains available only as an explicit temporary fallback for regression comparison.

Primary implementation references:

- [frontend/src/lib/runtimeConfig.ts](frontend/src/lib/runtimeConfig.ts)
- [frontend/src/lib/runtime.ts](frontend/src/lib/runtime.ts)
- [frontend/src-tauri/src/go_sidecar.rs](frontend/src-tauri/src/go_sidecar.rs)
- [frontend/scripts/release-launch-smoke.mjs](frontend/scripts/release-launch-smoke.mjs)
- [README.md](README.md)
- [go-backend/README.md](go-backend/README.md)

## 1. Desktop Runtime Selection Now Defaults To Go

Before phase 38, the repository still treated `legacy-rust` as the implicit desktop default even though the Go sidecar already owned most live workstation behavior and phase 37 had made packaged releases validate the bundled sidecar at launch.

Phase 38 removes that mismatch:

- Tauri desktop runtime selection now defaults to `go-sidecar`
- empty or unknown built shell runtime values now resolve to `go-sidecar`
- release-launch smoke validation uses the same Go-first default when no override is provided

That means the normal desktop app now comes up on the same backend path the migration has been building toward instead of a compatibility split.

## 2. `legacy-rust` Is Now Explicitly A Temporary Fallback

The old Rust-backed desktop route is still present because the plan allows a brief fallback window, but phase 38 stops treating it as normal behavior.

The runtime selector and shell logs now make that explicit:

- selecting `legacy-rust` produces a frontend warning that it is deprecated and temporary
- the Tauri shell prints a matching startup warning when `legacy-rust` is selected
- runtime logging now labels the Rust path as a fallback instead of the primary desktop description

The hybrid adapter remains intact for that fallback mode, so `renderStudy` can still use the Rust bridge there while the already-migrated commands continue through Go.

## 3. Packaging And Documentation Now Match The Runtime Cutover

Phase 37 already proved that the packaged app could launch with the bundled Go sidecar. Phase 38 updates the repository-facing defaults so the build and smoke path now describe the same runtime behavior users get by default.

Concretely:

- release launch smoke assumes `go-sidecar` when no runtime override is set
- the main README now documents `go-sidecar` as the default desktop runtime
- the Go backend README now describes the sidecar as the default desktop execution path and `legacy-rust` as the explicit fallback
- the migration plan marks phase 38 complete and points to this document

## 4. Validation Coverage

Validated with:

```bash
npm --prefix frontend run build
cargo test --manifest-path frontend/src-tauri/Cargo.toml go_sidecar
npm run release:smoke
```

This covers:

- TypeScript compilation of the updated desktop runtime selector
- Rust coverage for shell-side runtime defaulting and fallback behavior
- end-to-end release smoke validation for the Go-first packaged desktop path

## 5. Exit Criteria Check

Phase 38 exit criteria are now met:

- the desktop app defaults entirely to the Go backend path
- the Rust desktop route is no longer the normal runtime and remains only behind the explicit `legacy-rust` fallback flag
- packaging, smoke validation, and repository documentation all describe the same Go-first desktop behavior
