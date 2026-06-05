// Tooth/bone overlay detection — the heavy ML-ish path: feature tables,
// morphology, exemplar matching. Worth knowing this is the biggest file.
pub mod analysis;
// Line annotation measurement (pixel distance + optional mm calibration).
pub mod annotations;
// The orchestrator. Owns the job lifecycle, fingerprint dedup, cache plumbing,
// subscriber notifications. If you're chasing where work *actually* happens,
// it starts here.
pub mod app;
// Artifact cache + in-memory source preview cache. Debounced eviction lives here.
pub mod cache;
// Argument parsing for the standalone inspection/batch utility.
pub mod cli;
// Env-var driven config (XRAYVIEW_BACKEND_*). Defaults to a tempdir layout.
pub mod config;
// The single source of truth for the backend↔frontend contract. Mirror any
// change here in contracts/backend-contract-v1.schema.json on the TS side or
// the contracts:check job will complain.
pub mod contracts;
// BMP decoding/rendering. The supported input format is BMP.
pub mod bmp;
// Recent-studies catalog with corrupt-file recovery.
pub mod persistence;
// Brightness/contrast/equalize/palette pipeline.
pub mod processing;
// Preview encoding to BMP (Gray8 or RGBA→BGR24).
pub mod render;
// Tooth-segmentation features + gradient-boosted forest, shared by the
// analysis inference path and the offline trainer (examples/train_tooth.rs).
pub mod tooth_model;
