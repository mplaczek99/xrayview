# Phase 22 Port Disk Cache Path Policy To Go

This document completes phase 22 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Go backend now owns the shared disk path policy for cache artifacts and simple persistence instead of letting that layout drift across config defaults, the Tauri sidecar launcher, and feature-local path joins.

Primary implementation references:

- [go-backend/internal/cache/store.go](go-backend/internal/cache/store.go)
- [go-backend/internal/cache/store_test.go](go-backend/internal/cache/store_test.go)
- [go-backend/internal/config/config.go](go-backend/internal/config/config.go)
- [go-backend/internal/config/config_test.go](go-backend/internal/config/config_test.go)
- [go-backend/internal/app/app.go](go-backend/internal/app/app.go)
- [go-backend/internal/persistence/catalog.go](go-backend/internal/persistence/catalog.go)
- [frontend/src-tauri/src/go_sidecar.rs](frontend/src-tauri/src/go_sidecar.rs)
- [backend/src/cache/disk.rs](backend/src/cache/disk.rs)

## 1. Go Now Owns The Same Root Layout Rust Used

The Rust backend policy was simple:

- one temp-rooted base directory
- artifact files under `cache/artifacts/<namespace>`
- state files under `state`

The Go backend now mirrors that layout directly. Its default base directory is:

```text
<temp>/xrayview
```

From that root it derives:

- `cache`
- `cache/artifacts/<namespace>`
- `state/<name>`

That removes the previous Go-specific `xrayview-go-backend` default and aligns the sidecar with the existing Tauri asset scope under `$TEMP/xrayview/cache/artifacts/...`.

## 2. Artifact And Persistence Paths Are Centralized In One Go Module

Phase 17 and phase 20 already used Go-owned artifact paths for render and process previews, but simple persistence still relied on separate directory handling.

Phase 22 closes that gap by extending the Go cache store with:

- `NewWithRoot`
- `NewWithPaths`
- `DefaultRootDir`
- `PersistenceDir`
- `PersistencePath`

That gives the backend one place to define:

- temp root selection
- artifact namespace layout
- state-file placement

The recent-study catalog now consumes a path generated through that shared policy instead of building its live app path ad hoc.

## 3. The Tauri Sidecar Now Passes A Base Root, Not Feature-Specific Paths

The Tauri launcher previously injected separate cache and persistence environment variables, with persistence rooted in app-local data while artifacts lived in temp storage.

Phase 22 changes that contract:

- the shell passes `XRAYVIEW_GO_BACKEND_BASE_DIR`
- the Go backend derives `cache` and `state` itself
- the shell performs a best-effort migration of the old recent-study catalog into the new `state/catalog.json` location
- the logged startup metadata now shows the shared root plus its derived cache/state directories

That keeps the path policy owned by the backend while preserving the critical preview-artifact location required by the Tauri asset protocol.

## 4. Config Defaults And Overrides Stay Explicit

The Go config loader now defaults to:

- `baseDir = <temp>/xrayview`
- `cacheDir = <baseDir>/cache`
- `persistenceDir = <baseDir>/state`

Explicit `XRAYVIEW_GO_BACKEND_CACHE_DIR` and `XRAYVIEW_GO_BACKEND_PERSISTENCE_DIR` overrides still work when needed, but the default and base-dir-derived behavior are now Rust-compatible.

## 5. Validation Coverage

Validated with:

```bash
cd go-backend
gofmt -w internal/cache/store.go internal/cache/store_test.go internal/config/config.go internal/config/config_test.go internal/persistence/catalog.go internal/app/app.go internal/jobs/service.go internal/httpapi/router_test.go
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./internal/cache ./internal/config ./internal/persistence ./internal/jobs ./internal/httpapi ./internal/app

cd ../
cargo fmt --manifest-path frontend/src-tauri/Cargo.toml
cargo test --manifest-path frontend/src-tauri/Cargo.toml go_sidecar
```

Coverage now includes:

- stable default root selection under the Rust-compatible temp layout
- artifact path generation under namespaced cache directories
- persistence path generation under the shared `state` directory
- config tests for base-dir-derived and explicitly overridden path values
- Tauri sidecar startup tests still compiling and passing after the base-dir handoff change

## 6. Exit Criteria Check

Phase 22 exit criteria are now met:

- Go owns artifact path generation
- temp root selection is defined in Go
- persistence paths are derived through Go-owned disk policy
- the live Tauri sidecar path wiring matches the expected cache artifact location
