# backend-rust

Rust backend for `xrayview`.

Current scope:

- accepts both single-dash and double-dash CLI flags
- implements `--describe-presets`
- implements `--describe-study` using a metadata-only DICOM read path
- implements grayscale, pseudocolor, and compare `--input <dicom> --preview-output <png>` preview rendering, including invert/brightness/contrast/equalize/pipeline ordering

Next steps:

- implement DICOM writeback
