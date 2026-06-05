# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

The repo's shared agent guidelines — structure, build/test commands, coding style, testing, commit/PR conventions, and the contracts + scope rules — live in `AGENTS.md` and apply here too:

@AGENTS.md

## Engineering priorities

Weight performance, memory usage, and memory safety heavily in design tradeoffs. Prefer zero-copy and streaming over buffering; avoid allocations in hot paths (decode, render, process). When trading off, pick the faster, more memory-efficient path unless it would break correctness.

## Rust crates are not a Cargo workspace

`backend-rs/` and `desktop-tauri/` are two independent crates — there is no root `Cargo.toml`. A bare `cargo test` / `cargo clippy` from the repo root finds nothing; always go through the npm scripts (`backend:test`, `lint:rust`, `release:smoke`), which pass the correct `--manifest-path`. Before declaring a change done, run the full gate: `npm run release:smoke`.

## Other gotchas

- Backend env vars (`XRAYVIEW_BACKEND_*`) are resolved in `backend-rs/src/config.rs`; defaults apply if unset.
- Image fixtures `images/{BMP,PNG,TIF}/` are gitignored and developer-local — do not assume they exist on a fresh clone or in CI.
