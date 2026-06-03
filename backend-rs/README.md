# XRayView Backend

This crate is the Rust backend library. The Tauri desktop shell
(`desktop-tauri/`) links it as a path dependency and hosts `App` in-process;
the same crate also produces a standalone HTTP binary used by the CLI and
tests.

Current Rust-owned surface:

- contract structs and constants for backend contract v2
- environment config using `XRAYVIEW_BACKEND_*`
- loopback-local HTTP transport boundary, runtime metadata, command listing, `/preview`,
  and `/api/v1/events` job-update SSE
- all backend command endpoints: manifest, open study, render, analyze, process, job polling,
  cancellation, and line measurement
- native BMP metadata/decode, PNG previews, processing pipelines,
  deterministic analysis overlays,
  recent-study catalog persistence, source-preview decode caching, session-scoped
  job result caching, artifact eviction, and in-memory job state
- utility CLI subcommands for config, contract metadata, render, process,
  and analysis preview
- legacy workflow CLI flags for preset description, study description, preview output,
  processing output, and default processed-preview naming

The `backend:*` npm scripts target the standalone binary
(`xrayview-backend-rs`); the desktop shell consumes the library directly via
Tauri IPC commands.
