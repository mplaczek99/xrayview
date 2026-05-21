# XRayView Backend

This crate is the Rust backend used by the desktop shell and backend scripts.

Current Rust-owned surface:

- contract structs and constants for backend contract v1
- environment config using `XRAYVIEW_BACKEND_*`
- loopback-local HTTP transport boundary, runtime metadata, command listing, `/preview`,
  and `/api/v1/events` job-update SSE
- all backend command endpoints: manifest, open study, render, analyze, process, job polling,
  cancellation, and line measurement
- native 8/16/32-bit grayscale and 8-bit RGB DICOM decode, modality rescale,
  DICOM inspection metadata, default Window Center/Width rendering with full-range
  overrides, PNG previews, processing pipelines, secondary-capture export with
  baseline DICOM identity and display metadata, deterministic analysis overlays,
  recent-study catalog persistence, source-preview decode caching, session-scoped
  job result caching, artifact eviction, and in-memory job state
- standalone uncompressed BMP and baseline TIFF metadata/preview fallback for
  detector-tuning assets
- utility CLI subcommands for config, contract metadata, DICOM inspection, render, process,
  analysis preview, and secondary-capture export
- legacy workflow CLI flags for preset description, study description, preview output,
  processing output, and default processed-DICOM naming

The Rust sidecar is the default `backend:*` script target and the bundled
desktop backend sidecar.
