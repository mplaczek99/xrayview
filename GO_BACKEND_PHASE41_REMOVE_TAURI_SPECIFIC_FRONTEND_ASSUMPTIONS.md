# Phase 41 Remove Tauri-Specific Frontend Assumptions

This document completes phase 41 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The React frontend no longer exposes the live desktop path through the migration-era `go-sidecar` label or a shell-shaped preview URL adapter. Instead, the frontend now treats the supported live path as a generic `desktop` runtime, keeps mock mode intact, and leaves the Wails-specific details behind a lower-level binding layer.

Historical note: phase 41 kept a temporary `go-sidecar` compatibility alias for existing automation. Phase 44 later removed that alias so the final repo now accepts only `mock` and `desktop`.

Primary implementation references:

- [frontend/src/lib/runtime.ts](frontend/src/lib/runtime.ts)
- [frontend/src/lib/runtimeConfig.ts](frontend/src/lib/runtimeConfig.ts)
- [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts)
- [frontend/src/lib/shell.ts](frontend/src/lib/shell.ts)
- [frontend/src/lib/desktop.ts](frontend/src/lib/desktop.ts)
- [frontend/scripts/runtime-env.mjs](frontend/scripts/runtime-env.mjs)
- [frontend/scripts/release-launch-smoke.mjs](frontend/scripts/release-launch-smoke.mjs)
- [README.md](README.md)

## 1. The Frontend Runtime Is Back To `desktop` Versus `mock`

Before this phase, the live frontend path still surfaced the migration transport name `go-sidecar` in its public runtime model even though the supported shell had already moved to Wails in phase 40.

Phase 41 removes that frontend-facing leak:

- frontend runtime types now use `desktop` instead of `go-sidecar`
- runtime selection now defaults to `desktop` inside the live shell and `mock` in browser-only Vite mode
- the temporary `go-sidecar` alias was left in place in phase 41 and later removed in phase 44

That makes the frontend runtime model describe the user-visible execution environment again instead of the current backend transport detail.

## 2. Preview URL Conversion No Longer Lives On The Shell API

Before this phase, preview artifact normalization still flowed through the shell adapter surface even though the only live desktop shell was already Wails and Tauri's old asset-conversion concern was gone.

Phase 41 simplifies that boundary:

- the shell adapter now owns only native dialog responsibilities
- preview path normalization happens in the runtime layer based on `mock` versus `desktop`
- a small desktop binding helper owns the Wails-specific `/preview?path=...` translation

That keeps the mock path behavior unchanged while making preview loading a runtime concern instead of a shell contract.

## 3. Wails-Specific Details Are Pushed Down Behind A Desktop Binding Layer

The frontend still needs a concrete desktop implementation, but the higher-level runtime and backend adapters no longer talk directly in Wails-specific terms.

Concretely:

- `frontend/src/lib/desktop.ts` now exposes the generic desktop-facing helpers used by the rest of the frontend
- `frontend/src/lib/backend.ts` now creates a `desktop` backend API instead of a `go-sidecar`/Wails-named API
- `frontend/src/lib/runtime.ts` now resolves the live adapter through `desktop` terminology and logs that mode directly

The low-level Wails binding module remains as an implementation detail, which is the right level for that shell coupling after phase 40.

## 4. Runtime Env And Smoke Scripts Preserve Compatibility While Exposing The Simplified Frontend Model

This phase also aligns the frontend-owned scripts and documentation with the simplified runtime naming:

- frontend env handling now accepts `mock` and `desktop`
- frontend builds always receive the canonical `desktop` runtime name when the live path is selected
- phase 41 preserved the shell-compatible `go-sidecar` alias temporarily, but the final repo now passes `desktop` end to end
- the main README now documents `desktop` as the supported live frontend runtime name

That preserves the existing shell/runtime wiring while removing migration-specific terminology from the frontend-facing contract.

## 5. Validation Coverage

Validated with:

```bash
npm --prefix frontend run build
GOCACHE=/tmp/xrayview-go-build-cache GOTMPDIR=/tmp/xrayview-go-tmp go -C desktop test ./...
npm run wails:build
npm run release:smoke
```

Notes:

- the Wails Go test suite still covers the `/preview` asset-serving path
- `npm run release:smoke` passed, but desktop launch verification was skipped by the existing smoke helper because GTK could not initialize in the current Linux shell

## 6. Exit Criteria Check

Phase 41 exit criteria are now met:

- obsolete Tauri-era frontend runtime naming has been removed from the live frontend contract
- preview URL conversion no longer depends on a shell-shaped adapter abstraction
- mock mode remains intact
- the frontend is shell-agnostic again above the low-level desktop binding implementation
