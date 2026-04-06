# Phase 5 Introduce Backend Runtime Selection

This document completes phase 5 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The frontend can now select its backend implementation intentionally instead of inferring only `tauri` versus `mock`.

Primary implementation references:

- [frontend/src/lib/runtime.ts](frontend/src/lib/runtime.ts)
- [frontend/src/lib/runtimeConfig.ts](frontend/src/lib/runtimeConfig.ts)
- [frontend/src/lib/runtimeTypes.ts](frontend/src/lib/runtimeTypes.ts)
- [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts)
- [frontend/scripts/runtime-env.mjs](frontend/scripts/runtime-env.mjs)
- [frontend/scripts/tauri-dev.mjs](frontend/scripts/tauri-dev.mjs)
- [frontend/scripts/tauri-build.mjs](frontend/scripts/tauri-build.mjs)

## 1. Runtime Modes

Phase 5 defines three explicit backend runtime modes:

- `mock`
- `legacy-rust`
- `go-sidecar`

Selection is now centralized in [frontend/src/lib/runtimeConfig.ts](frontend/src/lib/runtimeConfig.ts).

Defaults:

- browser/Vite only: `mock`
- Tauri desktop shell: `legacy-rust`

That preserves current behavior while making the future Go path a first-class mode instead of an ad hoc branch.

## 2. Shell and Backend Stay Split

Phase 4 split `ShellAPI` from `BackendAPI`. Phase 5 keeps that split intact:

- `mock` uses the mock shell and mock backend
- `legacy-rust` uses the Tauri shell and the existing Rust command bridge
- `go-sidecar` uses the Tauri shell and an HTTP/JSON backend adapter

This is the point of the phase. The shell stays stable while the backend implementation can change under it.

## 3. Intentional Environment Flags

The selected backend runtime is now controlled through environment flags instead of implicit detection alone.

Supported variables:

- `XRAYVIEW_BACKEND_RUNTIME`
- `XRAYVIEW_GO_BACKEND_URL`

The frontend build also accepts the Vite-exposed forms:

- `VITE_XRAYVIEW_BACKEND_RUNTIME`
- `VITE_XRAYVIEW_GO_BACKEND_URL`

[frontend/scripts/runtime-env.mjs](frontend/scripts/runtime-env.mjs) normalizes and validates those values for Vite dev/build and Tauri dev/build entrypoints.

That means runtime selection is:

- documented
- validated early
- kept in one place

## 4. Go Sidecar Adapter Slot

[frontend/src/lib/backend.ts](frontend/src/lib/backend.ts) now includes a real `go-sidecar` backend adapter.

For now it assumes a temporary local HTTP command surface under:

- `POST /api/v1/commands/get_processing_manifest`
- `POST /api/v1/commands/open_study`
- `POST /api/v1/commands/start_render_job`
- `POST /api/v1/commands/start_process_job`
- `POST /api/v1/commands/start_analyze_job`
- `POST /api/v1/commands/get_job`
- `POST /api/v1/commands/cancel_job`
- `POST /api/v1/commands/measure_line_annotation`

Each endpoint returns the same frozen v1 contract payloads already used by the frontend.

This is intentionally narrow. Phase 5 does not implement the Go backend yet, but it removes the frontend blocker by giving the migration a concrete backend slot and selection path.

## 5. Safe Fallbacks

Runtime selection now fails safely:

- invalid runtime values fall back to the default mode with a warning
- non-mock backends requested outside Tauri fall back to `mock` with a warning
- invalid Go sidecar URLs fall back to the default localhost URL with a warning
- unreachable Go sidecars return normalized backend errors instead of raw fetch failures

That keeps the selector explicit without making bad configuration failures opaque.

## 6. Validation

Validate phase 5 with:

```bash
npm --prefix frontend run build
XRAYVIEW_BACKEND_RUNTIME=mock npm --prefix frontend run build
XRAYVIEW_BACKEND_RUNTIME=legacy-rust npm --prefix frontend run build
XRAYVIEW_BACKEND_RUNTIME=go-sidecar XRAYVIEW_GO_BACKEND_URL=http://127.0.0.1:38181 npm --prefix frontend run build
```

Desktop run examples:

```bash
npm run tauri:dev
XRAYVIEW_BACKEND_RUNTIME=legacy-rust npm run tauri:dev
XRAYVIEW_BACKEND_RUNTIME=go-sidecar XRAYVIEW_GO_BACKEND_URL=http://127.0.0.1:38181 npm run tauri:dev
```

## 7. Exit Criteria Check

Phase 5 exit criteria are now met:

- runtime selection is intentional and centralized
- the app recognizes `mock`, `legacy-rust`, and `go-sidecar`
- the current Tauri/Rust path remains the default desktop path
- the frontend has a concrete Go sidecar backend slot for later phases
