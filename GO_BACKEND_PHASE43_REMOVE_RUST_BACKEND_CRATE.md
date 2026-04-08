# Phase 43 Remove Rust Backend Crate

This document completes phase 43 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The repository no longer builds, tests, or ships against the legacy Rust backend crate. The tracked `backend/` crate and the leftover `frontend/src-tauri/` shell that linked against it have been removed, and the supported release path is now the Wails shell plus Go backend binaries only.

Primary implementation references:

- [README.md](README.md)
- [go-backend/README.md](go-backend/README.md)
- [desktop/scripts/build.mjs](desktop/scripts/build.mjs)
- [desktop/README.md](desktop/README.md)
- [frontend/package.json](frontend/package.json)
- [.github/workflows/build-release-artifacts.yml](.github/workflows/build-release-artifacts.yml)
- [.github/workflows/publish-release.yml](.github/workflows/publish-release.yml)

## 1. The Legacy Rust Backend Crate Is Gone

Phase 42 already removed the last supported decode/export helper usage. Phase 43 removes the remaining crate instead of leaving it in the tree as inert fallback code:

- the tracked `backend/` sources, tests, fixtures, and Rust CLI are deleted
- repository docs no longer describe `backend/` as part of the supported workspace
- normal validation no longer includes Cargo-based backend steps

That is the actual migration cutoff: the product runtime now has one backend implementation.

## 2. The Obsolete Tauri Shell Was Removed With It

The leftover `frontend/src-tauri/` crate still linked `../../backend`, even though phase 40 had already moved the supported desktop shell to Wails.

Keeping that crate after phase 43 would leave a broken Rust-specific build path anchored to a deleted backend. This cleanup removes it outright instead of pretending it is still a viable fallback.

Frontend package metadata was updated at the same time so the supported frontend workspace no longer carries Tauri npm dependencies that nothing imports.

## 3. Release Automation Is Now Wails-Only

Phase 43 also updates the release path so it matches the supported runtime:

- GitHub Actions no longer install Rust or cache Rust build outputs
- release jobs now build and smoke-test the Wails desktop app
- release artifacts are archives of `desktop/build/bin/`, which contains the desktop shell binary plus the Go backend sidecar
- the Wails build script no longer forces `GOPROXY=off`, which keeps clean-checkout builds compatible with normal Go module resolution

That aligns the repository with the real shipping surface instead of the removed Tauri/AppImage/MSI flow.

## 4. Validation

Validated with:

```bash
npm run contracts:check
npm run go:backend:test
npm --prefix frontend run build
go -C desktop test ./...
npm run release:smoke
```

Concrete checks now covered:

- schema-driven contract generation still matches the committed bindings
- the Go backend test suite still passes after the Rust crate removal
- the frontend still type-checks and builds
- the Wails shell still compiles and tests without the deleted Rust/Tauri paths
- release smoke validates the supported desktop binary path only

## 5. Exit Criteria Check

Phase 43 exit criteria are now met:

- the old Rust backend crate is deleted
- the old Rust CLI is deleted
- obsolete Rust/Tauri tests and scripts are removed from the supported build path
- repository docs describe only the Wails shell plus Go backend runtime
- the app builds and ships without the old Rust backend
