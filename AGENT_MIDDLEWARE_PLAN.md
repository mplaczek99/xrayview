# Agent Middleware Plan

Plan for a middleware layer that lets Claude Code and Codex autonomously drive xrayview end-to-end: the real Go backend runs on loopback, the React frontend is served in a browser controllable by playwright-cli, and both agents share a documented contract.

## Goal

- Live Go backend reachable from a browser-hosted frontend (not just the Wails desktop shell).
- One-command harness that starts backend + frontend and tears them down cleanly.
- Agents can operate in two modes against the same running system:
  - Direct HTTP calls to the backend for deterministic scripted work.
  - Playwright-cli against the browser frontend for UI verification.
- Stable agent contract: ports, env vars, fixture locations, selectors.

## Core gap

The frontend has two runtime modes today:

- `mock` — browser-only, synthetic data, no backend bound.
- `desktop` — Wails shell, uses `window.go.main.DesktopApp` JS bridge.

There is no browser → HTTP path to the Go backend. A third runtime mode (`http`) is required.

Preview artifacts are currently served by the Wails shell (`desktop/app.go:269`), not by the Go HTTP API. Without a backend preview endpoint, the `http` runtime would receive filesystem paths from jobs and the browser would fail to load them.

## Architecture

### New frontend runtime mode `http`

- `frontend/src/lib/types.ts` — add `"http"` to `RuntimeMode`.
- `frontend/src/lib/runtimeConfig.ts` — update `isRuntimeMode` guard; allow `http` in browser, continue to block `desktop` in browser.
- `frontend/src/lib/httpBackend.ts` (new) — `createHttpBackendAPI(baseUrl)` implementing `BackendAPI` via `fetch()` to the existing HTTP command dispatch. Re-uses generated contract types.
- `frontend/src/lib/httpShell.ts` (new) — picker stubs that read `VITE_XRAYVIEW_AGENT_FIXTURE` and `VITE_XRAYVIEW_AGENT_OUTPUT_DIR`. Fail fast with a clear error if unset.
- `frontend/src/lib/runtime.ts` — add `mode === "http"` branch in `createRuntimeAdapter`; extend `resolvePreviewUrl` to prepend `VITE_XRAYVIEW_BACKEND_URL` to the preview path.

### Shared command-builder refactor

`buildProcessStudyCommand` in `frontend/src/lib/backend.ts:54` is contract-shaping logic that both adapters need. Move it to `frontend/src/lib/commandBuilders.ts` before adding the HTTP adapter so both adapters depend on a single source. Pure move, no behavior change.

### Backend preview endpoint

Add `GET /preview?path=...` to `backend/internal/httpapi`.

Do not port `desktop/app.go:ServeAsset` verbatim — it accepts any absolute path, which is safe for a Wails shell but unsafe for a browser-exposed HTTP endpoint (any localhost script could read arbitrary files).

Confine to the configured cache root using `filepath.Rel`, not a string prefix check:

```go
root, err := filepath.EvalSymlinks(cacheRoot)
if err != nil { /* 500 */ }

target, err := filepath.EvalSymlinks(requested)
if err != nil {
    if os.IsNotExist(err) { http.NotFound(w, r); return }
    /* 500 */
}

rel, err := filepath.Rel(root, target)
if err != nil ||
   rel == ".." ||
   strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
    http.Error(w, "preview path outside cache root", http.StatusForbidden)
    return
}
```

Tests in `preview_test.go`: happy path, escape via `..`, symlink escape, non-absolute input, missing file, method not allowed.

**The `?path=` shape is transitional.** It leaks filesystem shape into the agent contract. The target design is opaque ids: backend returns `previewUrl: "/preview/{studyId}/{artifactId}"` and holds the id → path map server-side. Called out as transitional in `AGENTS.md`; agent logic must not depend on filesystem shape.

### Orchestrator

`scripts/agent-harness.mjs`:

- Spawn `go -C backend run ./cmd/xrayviewd`; poll `GET http://127.0.0.1:38181/healthz` every 100 ms, timeout 10 s.
- Spawn Vite dev with:
  - `VITE_XRAYVIEW_BACKEND_RUNTIME=http`
  - `VITE_XRAYVIEW_BACKEND_URL=http://127.0.0.1:38181`
  - `VITE_XRAYVIEW_AGENT_FIXTURE=<repo>/agent-fixtures/default.dcm`
  - `VITE_XRAYVIEW_AGENT_OUTPUT_DIR=<repo>/agent-fixtures/out`
- Prefix child logs as `[backend]` / `[frontend]`.
- SIGINT/SIGTERM → kill children, drain, exit.
- npm script: `"agent:serve": "node scripts/agent-harness.mjs"`.

Harness boot tolerates a missing fixture; the smoke step is what fails on absence, with a single documented message.

### data-testid pass

Add stable automation hooks before wiring playwright flows. Initial set:

- `frontend/src/components/viewer/ViewTab.tsx:90` — open study button.
- `frontend/src/components/processing/ProcessingTab.tsx:127` — start process button.
- Cancel, measure, palette select, brightness/contrast sliders, preset picker.
- Job-list rows and status badges.

Convention: `data-testid="action-<verb>-<noun>"` (e.g. `action-open-study`, `action-start-process`). Documented in `AGENTS.md`.

### Fixtures

`agent-fixtures/` — content gitignored; tracked `README.md` explains required files. Only de-identified or public DICOM samples per `CLAUDE.md` safety scope.

### AGENTS.md

- Ports: backend 38181, frontend 5173.
- Env vars used by the harness.
- Fixture paths and how to populate them.
- data-testid list and naming convention.
- Playwright-cli example: open fixture, start process job, wait for preview.
- HTTP-direct example: `curl` sequence for the same flow.
- Shutdown protocol.
- Scope guard: "DICOM in, DICOM out"; no diagnostic features.
- Transitional preview-contract note.

## Work order

1. Move `buildProcessStudyCommand` to `frontend/src/lib/commandBuilders.ts` (pure move; lands first).
2. Backend `GET /preview` with `filepath.Rel` cache-root confinement + tests.
3. Frontend `types.ts` + `runtimeConfig.ts` changes for `http` mode.
4. `httpBackend.ts` + `httpShell.ts`.
5. `resolvePreviewUrl` http branch.
6. `runtime.ts` http wiring.
7. data-testid pass on named controls.
8. `scripts/agent-harness.mjs` + `agent:serve` npm script.
9. `agent-fixtures/` scaffold and README.
10. `AGENTS.md`.
11. Smoke: harness up, playwright-cli opens fixture by testid, starts process job, polls completion, preview loads.

## Acceptance

- `npm run agent:serve` brings up the backend on `127.0.0.1:38181` and the frontend on `127.0.0.1:5173`, and shuts both down cleanly on SIGINT/SIGTERM.
- The browser `http` runtime can call `openStudy`, `startRenderStudyJob`, `startProcessStudyJob`, `startAnalyzeStudyJob`, poll job state through to a terminal state, and load preview images in the browser.
- Missing fixture fails with the single documented message: `fixture not found at <path>: populate agent-fixtures/ per README and retry` (exit 1, no stack trace).
- `GET /preview` rejects escape attempts outside the cache root (tested for `..` traversal, symlink escape, non-absolute input) with 403 / 400 as appropriate.

## Deferred

- Opaque preview ids (`/preview/{studyId}/{artifactId}`) replacing the transitional `?path=` surface; backend-held id → path map so agents never see filesystem shape.
- SSE-based job updates via `/api/v1/events` replacing polling in the http runtime.

## Risks

- Preview endpoint is a new public surface on loopback. Any localhost process can read the cache. Acceptable for a single-user dev box; documented in `AGENTS.md`; revisit if scope expands beyond local agent use.
- Contract drift between desktop and HTTP adapters. Mitigated by the shared `commandBuilders.ts` refactor in step 1.
- Loopback-only binding stays in place. No remote control, no tunneling.

## Out of scope

- New backend features beyond the preview endpoint.
- Authentication, multi-session, or remote access.
- Any diagnostic, clinical-decision, or treatment-planning affordances.
