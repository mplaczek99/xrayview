# backend

Rust backend for `xrayview`.

Current scope:

- implements `--describe-presets` and `--describe-study` JSON metadata modes
- supports a fixed grayscale processing chain (invert, brightness, contrast,
  equalize), compare output, presets, and palette selection
- serves as the processing engine used by the Tauri desktop frontend
- still provides the Rust helper binaries used by the migration path

Current architecture note:

- the desktop frontend links this crate directly and calls the library-first
  app/service layer in-process
- the supported headless CLI workflows now live in
  `go-backend/cmd/xrayview-cli`, which preserves the established flag surface
  while moving CLI ownership to Go
