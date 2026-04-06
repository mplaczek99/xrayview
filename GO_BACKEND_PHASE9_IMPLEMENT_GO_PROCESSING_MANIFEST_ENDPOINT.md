# Phase 9 Implement Go Processing Manifest Endpoint

This document completes phase 9 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Go sidecar now serves the frozen processing manifest through the existing local command transport, giving the frontend its first real backend response from Go instead of a placeholder error.

Primary implementation references:

- [go-backend/internal/contracts/processing.go](go-backend/internal/contracts/processing.go)
- [go-backend/internal/httpapi/router.go](go-backend/internal/httpapi/router.go)
- [go-backend/internal/httpapi/router_test.go](go-backend/internal/httpapi/router_test.go)
- [backend/src/app/mod.rs](backend/src/app/mod.rs)
- [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts)
- [frontend/src/lib/mockProcessingManifest.ts](frontend/src/lib/mockProcessingManifest.ts)
- [README.md](README.md)
- [go-backend/README.md](go-backend/README.md)

## 1. Manifest Ownership In Go

Phase 9 moves the processing manifest definition into the Go backend as a concrete contract payload instead of leaving the command namespace entirely stubbed.

The Go backend now defines the same frozen preset manifest the Rust backend exposes today:

- default preset id: `default`
- preset ids: `default`, `xray`, `high-contrast`
- controls and palette defaults preserved exactly

This keeps phase 9 aligned with the migration rule that language ownership changes before behavior changes.

## 2. Transport Behavior

`POST /api/v1/commands/get_processing_manifest` now returns a normal `200 OK` contract payload instead of `501 Not Implemented`.

Other commands still remain intentionally unimplemented in Go for now and continue to return structured backend errors. That preserves the narrow scope of phase 9:

- prove contracts and transport with one stable payload
- avoid starting stateful or DICOM-backed work early

## 3. Frontend Impact

The frontend `go-sidecar` backend adapter already called `get_processing_manifest` through the HTTP transport. No frontend contract changes were needed for phase 9.

The practical result is that the React app can now load processing presets from Go when running against the sidecar runtime, while the rest of the backend surface remains on later migration phases.

## 4. Validation

Validate phase 9 with:

```bash
go -C go-backend test ./...
curl -s -X POST http://127.0.0.1:38181/api/v1/commands/get_processing_manifest
```

Expected manifest payload:

- `defaultPresetId` is `default`
- exactly 3 presets are returned
- preset ordering remains `default`, `xray`, `high-contrast`

## 5. Exit Criteria Check

Phase 9 exit criteria are now met:

- `getProcessingManifest` is implemented in Go
- the Go endpoint preserves current preset/default semantics
- the `go-sidecar` frontend runtime can read the manifest from Go
- the remaining commands stay clearly unimplemented instead of drifting into partial behavior
