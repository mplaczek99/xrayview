# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Full conventions (structure, commands, style, contracts, scope) live in @AGENTS.md. Notes below are the non-obvious bits that aren't there.

## Gotchas

- **Run Go via the npm scripts**, not raw `go`. They pin `GOCACHE=$PWD/backend/.gocache` (`npm run backend:test`, `npm run lint:go`). Single test: `GOCACHE=$PWD/backend/.gocache go test ./backend/internal/<pkg> -run TestName`. `go.work` ties two modules: `backend/` (library + CLI) and `desktop/` (Wails shell, separate module, vets with `-tags webkit2_41`).
- **Desktop binary** lands at `desktop/build/bin/xrayview` after `npm run desktop:build`. `npm run desktop:dev` runs the Wails app (Go backend in-process) with hot reload.
- **IPC, not HTTP.** Backend is in-process; the frontend invokes commands as `window.go.main.App.<PascalCaseCommand>(payload)`. The stdio `serve-ipc` server in `backend/internal/ipc` is legacy and unused by the GUI.
- **No Rust.** The backend and detector are pure Go (`backend/internal/analysis/` GBDT forests + embedded `*.bin` XVLM2 assets). Ignore any stale Rust references in older tooling or docs.
- **Branch flow:** commit straight to `dev`; `main` is the release branch (a `v*` tag triggers the publish workflow).
