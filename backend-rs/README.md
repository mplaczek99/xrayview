# XRayView Backend

This crate is the Rust backend library. The Tauri desktop shell
(`desktop-tauri/`) links it as a path dependency and hosts `App` in-process;
the same crate also produces a headless CLI binary for scripted/manual BMP
inspection.

Current Rust-owned surface:

- contract structs and constants for backend contract v2
- environment config using `XRAYVIEW_BACKEND_*`
- Tauri IPC command handlers: manifest, open study, render, analyze, process,
  job polling, cancellation, and line measurement
- native BMP metadata/decode, PNG previews, processing pipelines,
  deterministic analysis overlays,
  recent-study catalog persistence, source-preview decode caching, session-scoped
  job result caching, artifact eviction, and in-memory job state
- utility CLI subcommands for config, contract metadata, preset inspection,
  study metadata, render, process, and analysis preview

The `backend:*` npm scripts target the CLI binary (`xrayview-backend-rs`);
the desktop shell consumes the library directly via Tauri IPC commands.
