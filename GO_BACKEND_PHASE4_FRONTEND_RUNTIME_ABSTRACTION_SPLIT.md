# Phase 4 Introduce Frontend Runtime Abstraction Split

This document completes phase 4 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The frontend now separates shell responsibilities from backend responsibilities and routes UI calls through a composed runtime adapter.

Primary implementation references:

- [frontend/src/lib/runtime.ts](frontend/src/lib/runtime.ts)
- [frontend/src/lib/runtimeTypes.ts](frontend/src/lib/runtimeTypes.ts)
- [frontend/src/lib/shell.ts](frontend/src/lib/shell.ts)
- [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts)
- [frontend/src/lib/backendErrors.ts](frontend/src/lib/backendErrors.ts)
- [frontend/src/app/store/workbenchStore.ts](frontend/src/app/store/workbenchStore.ts)

## 1. Runtime Split

Phase 4 introduces three explicit frontend interfaces:

- `ShellAPI`
- `BackendAPI`
- `RuntimeAdapter`

The responsibilities are now divided as follows:

- `ShellAPI` owns shell-only behavior such as file picking and preview-path translation.
- `BackendAPI` owns backend command execution, backend DTO retrieval, mock job state, and backend error normalization.
- `RuntimeAdapter` composes a shell implementation with a backend implementation and returns UI-facing application models.

This is the actual abstraction split the migration needs before adding a second real backend.

## 2. Tauri Assumptions Isolated

Phase 4 moves the direct Tauri-specific details behind the shell/backend implementations:

- `convertFileSrc(...)` now lives only in [frontend/src/lib/shell.ts](frontend/src/lib/shell.ts).
- Tauri file picker commands now live only in [frontend/src/lib/shell.ts](frontend/src/lib/shell.ts).
- Tauri backend command invocations now live only in [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts).

That means preview URL translation is no longer mixed into the same module that defines backend operations.

## 3. UI Surface Reduced to Runtime

The workbench store now talks only to the runtime abstraction in [frontend/src/lib/runtime.ts](frontend/src/lib/runtime.ts).

That runtime layer:

- selects the active runtime mode
- exposes the shell/backend pair for later migration work
- converts contract DTOs into the UI-facing study and job models
- normalizes preview URLs through the selected shell implementation

This keeps shell/backend composition in one place instead of leaking transport details through the store.

## 4. Error Handling Kept Stable

[frontend/src/lib/backendErrors.ts](frontend/src/lib/backendErrors.ts) now centralizes backend-style error normalization and formatting.

That preserves the current user-facing error handling semantics while removing the old coupling to a single mixed adapter module.

## 5. Validation

Validate phase 4 with:

```bash
npm --prefix frontend run build
```

That confirms:

- the runtime abstraction compiles
- the React frontend still builds against the split adapter
- the legacy Tauri/mock paths still satisfy the frontend type surface

## 6. Exit Criteria Check

Phase 4 exit criteria are now met:

- shell concerns and backend concerns are split explicitly
- the UI talks to the runtime abstraction instead of a mixed transport module
- preview URL handling is isolated to the shell layer
- the current mock and legacy Tauri/Rust flows remain available through the runtime composition path
