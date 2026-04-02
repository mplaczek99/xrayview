# backend

Rust backend for `xrayview`.

Current scope:

- accepts CLI DICOM workflows for preview rendering, derived DICOM output, and
  automatic tooth analysis
- implements `--describe-presets` and `--describe-study` JSON metadata modes
- supports presets, invert, brightness, contrast, equalize, compare output,
  palette selection, and grayscale pipeline ordering
- serves as the processing engine used by the Tauri desktop frontend

Current architecture note:

- the frontend still shells out to this backend binary today; later phases in
  `IMPLEMENTATION_PLAN.md` move that boundary to a library-first Rust API
