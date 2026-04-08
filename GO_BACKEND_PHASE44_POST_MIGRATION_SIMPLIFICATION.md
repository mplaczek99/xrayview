# Phase 44 Post-Migration Simplification

This document completes phase 44 from
[GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The migration is
now reflected in the day-to-day repository layout: the supported desktop shell
has been normalized under `desktop/`, legacy runtime compatibility shims have
been removed, and the live documentation now describes one final Go-first
architecture instead of a transition state.

Primary implementation references:

- [README.md](README.md)
- [go-backend/README.md](go-backend/README.md)
- [desktop/README.md](desktop/README.md)
- [package.json](package.json)
- [go.work](go.work)
- [frontend/src/lib/runtimeConfig.ts](frontend/src/lib/runtimeConfig.ts)
- [frontend/scripts/runtime-env.mjs](frontend/scripts/runtime-env.mjs)
- [frontend/scripts/release-launch-smoke.mjs](frontend/scripts/release-launch-smoke.mjs)
- [frontend/scripts/release-smoke-test.mjs](frontend/scripts/release-smoke-test.mjs)
- [desktop/sidecar.go](desktop/sidecar.go)
- [.github/workflows/build-release-artifacts.yml](.github/workflows/build-release-artifacts.yml)
- [.github/workflows/publish-release.yml](.github/workflows/publish-release.yml)

## 1. The Supported Desktop Shell Path Is Normalized

The shell stopped being a prototype in phase 40, so phase 44 removes the last
prototype-era path from the supported repo layout:

- the Wails shell now lives in `desktop/` instead of `wails-prototype/`
- root scripts, `go.work`, frontend build outputs, smoke tests, and release
  workflows all point at that final location
- developer onboarding and release packaging now reference one canonical shell
  path

That change removes migration language from the repo's core build and release
surface.

## 2. Runtime Mode Handling Now Uses Final Names Only

The phase 41 `go-sidecar` compatibility alias was useful during migration, but
it no longer belongs in the final architecture.

Phase 44 removes that temporary compatibility layer:

- frontend runtime selection now accepts only `mock` and `desktop`
- the Wails shell now uses `desktop` as its live runtime name internally and by
  default
- release smoke and frontend env helpers no longer translate between frontend
  and shell-specific runtime labels

This keeps the browser mock seam, which is still useful, while deleting the
migration-only naming shim.

## 3. Live Docs Now Tell The Final Architecture Story

The root README and module READMEs were rewritten around the supported system:

- `frontend/` owns the UI and mock mode
- `desktop/` owns native shell responsibilities
- `go-backend/` owns the backend runtime and CLI

The docs now focus on developer onboarding, supported runtime modes, release
validation, and current product boundaries instead of enumerating the migration
history.

## 4. Cleanup Kept Useful Diagnostics But Removed Dead Scaffolding

Phase 44 removes the unused `frontend/scripts/wails-prototype-build.mjs`
wrapper and keeps the remaining focused backend diagnostics documented as
diagnostics instead of migration utilities.

That matches the phase goal: simplify the repo without deleting the inspection
tools that are still genuinely useful.

## 5. Validation

Validated with:

```bash
npm run contracts:check
npm run go:backend:test
npm --prefix frontend run build
go -C desktop test ./...
npm run release:smoke
```

Concrete checks now covered:

- schema-driven contract generation still matches committed bindings
- the Go backend test suite still passes after the runtime simplification
- the frontend still type-checks and builds with the final `mock`/`desktop`
  runtime model
- the normalized desktop shell path still compiles and passes its Go tests
- release smoke still validates the supported desktop binary path and bundled
  sidecar

## 6. Exit Criteria Check

Phase 44 exit criteria are now met:

- migration toggles have been removed from the supported runtime path
- temporary abstractions were reduced to the still-useful `mock` versus
  `desktop` seam
- README and architecture-facing docs describe the final Go-first repo
- build, test, and release instructions are coherent from a clean checkout
