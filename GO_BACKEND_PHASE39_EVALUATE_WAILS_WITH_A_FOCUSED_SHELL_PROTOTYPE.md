# Phase 39 Evaluate Wails with a Focused Shell Prototype

This document completes phase 39 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). Instead of starting a full shell rewrite, phase 39 introduced a narrow `wails-prototype/` application that reused the existing React/Vite toolchain and proved the shell concerns that phase 39 explicitly called out: application launch, native open/save dialogs, preview artifact access, and one live backend call path.

Historical note: phase 40 later promoted that prototype into the supported desktop shell, so the prototype-only frontend entry files that existed during phase 39 are no longer present in the current tree.

Primary implementation references:

- [wails-prototype/main.go](wails-prototype/main.go)
- [wails-prototype/app.go](wails-prototype/app.go)
- [wails-prototype/sidecar.go](wails-prototype/sidecar.go)
- [wails-prototype/README.md](wails-prototype/README.md)
- [frontend/scripts/wails-build.mjs](frontend/scripts/wails-build.mjs)
- [frontend/vite.wails.config.ts](frontend/vite.wails.config.ts)

## 1. The Repository Now Has A Narrow Wails Shell Evaluation Path

Phase 39 exists to answer whether Wails is actually a good final shell, not to assume it. The new `wails-prototype/` application keeps that scope tight:

- it is separate from the production `frontend/src-tauri/` shell
- it uses the existing React frontend toolchain instead of introducing a second web stack
- it binds only the shell capabilities needed for evaluation instead of porting the whole workstation

That keeps the prototype meaningful without accidentally turning phase 39 into phase 40 early.

## 2. Dialogs And Preview Artifact Access Are Proven In Wails

The prototype exposes four bound Go methods into a small React evaluation screen:

- `PickDicomFile`
- `PickPreviewArtifact`
- `PickSaveDicomPath`
- `OpenStudy`

The dialog methods use Wails runtime dialogs directly, which proves that the current Tauri shell-only commands map cleanly onto Wails.

Preview access is handled through a dedicated Wails asset-handler route at `/preview`. The React prototype computes a preview URL from an absolute local file path, and the Go handler streams that file back into the webview. This is the exact shell concern phase 39 asked to sanity-check before a replacement decision.

## 3. One Real Backend Call Path Is Routed Through The Prototype

The prototype does not embed backend logic. It starts the existing Go sidecar from `wails-prototype/build/bin/xrayview-go-backend`, waits for `/healthz`, and then uses the same local HTTP command boundary the migration has already standardized on.

The UI drives a live `openStudy` request and displays the resulting study payload plus round-trip timing. That confirms the critical Wails question for this phase: the shell can host the frontend, own native shell responsibilities, and still communicate cleanly with the current Go backend boundary.

## 4. Build Workflow Findings And Decision

The prototype adds a repo-owned build path:

```bash
npm run wails:prototype:build
npm run wails:prototype:run
```

Key findings from this prototype:

- Wails fits the shell-only surface area cleanly
- native dialog APIs are straightforward
- preview artifacts can be served back into the webview without Tauri's asset protocol
- keeping the React build in the existing Vite toolchain is workable

The main caveat is frontend integration style: this prototype intentionally builds static frontend assets first and then serves them from Wails. That is the safer fit for the current repo than trying to combine phase 40 with a new Wails-specific frontend workflow at the same time.

## 5. Go/No-Go Decision

Phase 39 exit criteria are now met, and the decision is:

- go on Wails for phase 40

Reason:

- the shell responsibilities exercised in this repository map directly onto Wails
- no prototype-only blocker appeared around dialogs, asset serving, or frontend/backend communication
- the remaining work is replacement breadth, not shell viability

The prototype therefore validates the migration plan's long-term shell recommendation without forcing a premature production cutover in this phase.
