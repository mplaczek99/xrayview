# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

The repo's shared agent guidelines — structure, build/test commands, coding style, testing, commit/PR conventions, and the contracts + scope rules — live in `AGENTS.md` and apply here too:

@AGENTS.md

## Engineering priorities

Weight performance, memory usage, and memory safety heavily in design tradeoffs. Prefer zero-copy and streaming over buffering; avoid allocations in hot paths (decode, render, process). When trading off, pick the faster, more memory-efficient path unless it would break correctness.

## Build topology

The stack is pure Go + TypeScript — no Rust. The backend is Go under `backend/`; the desktop shell is a Go + [Wails v2](https://wails.io) app under `desktop/`, its own module so the webkit/cgo dependency stays out of the backend (which builds and tests as pure Go). The desktop module binds backend methods through the public `backend/shell` seam (Go forbids importing another module's `internal/` packages). Always go through the npm scripts (`backend:test`, `lint`, `release:smoke`) so Go, the desktop build, frontend, and contract checks run with the expected paths. Before declaring a change done, run the full gate: `npm run release:smoke`.

The Wails CLI must be installed (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`) and on `PATH` (or in `$(go env GOPATH)/bin`) for `npm run desktop:dev` / `desktop:build`. On Linux the `webkit2_41` build tag is passed automatically.

## Other gotchas

- Backend env vars (`XRAYVIEW_BACKEND_*`) are resolved in `backend/internal/config`; defaults apply if unset.
- Image fixtures `images/{BMP,PNG,TIF}/` are gitignored and developer-local — do not assume they exist on a fresh clone or in CI.
