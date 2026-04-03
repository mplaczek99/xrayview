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

- the desktop frontend links this crate directly and calls the library-first
  app/service layer in-process
- the CLI binary remains available for direct DICOM workflows and release smoke
  validation
