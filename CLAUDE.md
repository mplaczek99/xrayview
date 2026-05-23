# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A BMP bitewing X-ray visualization workstation. **Image visualization only, not a medical device.** See `README.md` for the full feature overview, IPC command surface, and architecture diagram.

Stack: Rust backend library (`backend-rs/`), Tauri 2 desktop shell (`desktop-tauri/`) that links it in-process, HTMX/TypeScript frontend (`frontend/`), JSON-schema contracts (`contracts/`).

## Engineering priorities

Weight performance, memory usage, and memory safety heavily in design tradeoffs. Prefer zero-copy and streaming over buffering; avoid allocations in hot paths (decode, render, process). When trading off, choose the path that's faster and more memory-efficient unless it would break correctness.

## Before declaring a change done

Run the full smoke gate — this is the team's pre-done check, not optional:

```bash
npm run release:smoke
```

Order: `lint` (clippy + Biome) → `contracts:check` → `backend:test` → frontend build → `tauri build --no-bundle`. Use `release:smoke:bundle` to also produce installer bundles.

Quick lint feedback while iterating: `npm run lint:ts` (Biome on the frontend) or `npm run lint:rust` (clippy on both Rust crates). `npm --prefix frontend run lint:fix` auto-fixes formatting and safe Biome lints.

## Contract changes

`contracts/backend-contract-v1.schema.json` is the single source of truth for the backend↔frontend interface. When the schema changes:

1. Edit the schema.
2. Run `npm run contracts:generate` to regenerate `frontend/src/lib/generated/contracts.ts` (never hand-edit this file).
3. **Manually mirror the change in `backend-rs/src/contracts.rs`** — Rust types are not auto-generated and drift is not auto-detected.
4. Run `npm run contracts:check` to verify the TS side is in sync.

## Out of scope

DICOM, TIFF, and the standalone HTTP backend transport have all been removed. **BMP only, in-process Tauri IPC only.** Do not reintroduce these formats or a network backend.

## Runtime modes

- `npm run dev` — HTMX UI with synthetic data in a browser, no backend.
- `npm run tauri:dev` — full desktop app with the in-process Rust backend.
- `XRAYVIEW_BACKEND_RUNTIME=mock` forces mock mode if the auto-detect picks wrong.

## Backend environment variables

Read in `backend-rs/src/config.rs`. Defaults are applied if unset:

- `XRAYVIEW_BACKEND_LOG_LEVEL` — `debug` | `info` | `warn` | `error`
- `XRAYVIEW_BACKEND_BASE_DIR` — root for cache + persistence dirs
- `XRAYVIEW_BACKEND_CACHE_DIR` — explicit override of the cache dir
- `XRAYVIEW_BACKEND_PERSISTENCE_DIR` — explicit override of the persistence dir

## Test image fixtures

`images/BMP/`, `images/PNG/`, and `images/TIF/` are **gitignored — developer-local**. Do not assume they exist on CI or on a fresh clone. See `images/README.md` for what should be there and where it comes from.
