# backend-rust

Rust backend work-in-progress for `xrayview`.

Current scope:

- accepts the Go-style single-dash CLI flags used by the frontend
- implements `-describe-presets`
- implements `-describe-study` using a metadata-only DICOM read path
- implements grayscale, pseudocolor, and compare `-input <dicom> -preview-output <png>` preview rendering, including invert/brightness/contrast/equalize/pipeline ordering
- includes a correctness test against the optimized Go backend preview output

Next steps:

- add preview benchmarks against the optimized Go backend
- implement DICOM writeback

Quick preview comparison:

```bash
backend-rust/scripts/compare-preview.sh
```

Pass extra preview flags after the output directory to compare processed grayscale previews too:

```bash
backend-rust/scripts/compare-preview.sh images/sample-dental-radiograph.dcm /tmp/xrayview-compare -invert -brightness 18 -contrast 1.35 -equalize -pipeline contrast,invert,brightness,equalize
```
