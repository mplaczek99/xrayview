# Phase 35 Move CLI Ownership To Go

This document completes phase 35 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The supported headless CLI workflows now run through the Go CLI in `go-backend/cmd/xrayview-cli`, so backend ownership of the command-line surface no longer depends on the Rust `xrayview-backend` binary.

Primary implementation references:

- [go-backend/cmd/xrayview-cli/main.go](go-backend/cmd/xrayview-cli/main.go)
- [go-backend/cmd/xrayview-cli/legacy_cli.go](go-backend/cmd/xrayview-cli/legacy_cli.go)
- [go-backend/cmd/xrayview-cli/main_test.go](go-backend/cmd/xrayview-cli/main_test.go)
- [README.md](README.md)
- [go-backend/README.md](go-backend/README.md)

## 1. The Go CLI Now Owns The Existing Headless Workflow Surface

Phase 35 does not introduce a new public command shape for the supported DICOM workflows. Instead, the Go CLI now accepts the same top-level flag surface the Rust CLI exposed:

- `--describe-presets`
- `--describe-study --input <path>`
- `--analyze-tooth --input <path> [--preview-output <png>]`
- preview rendering via `--input <path> --preview-output <png>`
- processed preview and DICOM output via `--input <path>` plus the existing processing flags

That keeps the user-visible workflow stable while moving command ownership to Go.

## 2. The Compatibility Path Reuses The Go-Owned Decode, Render, Process, Export, And Analyze Stages

The new compatibility layer is intentionally thin:

- study description uses the Go DICOM metadata reader
- preview, processing, and analysis use the Go decode-helper orchestration plus Go render/processing/analysis packages
- DICOM export uses the Go secondary-capture writer, while still honoring the temporary Rust export-helper fallback when configured

The older utility-style subcommands (`inspect-decode`, `render-preview`, `process-preview`, and related migration helpers) remain available for backend development, but they are no longer the primary CLI contract for supported headless workflows.

## 3. CLI Parity Coverage Moved Into Go Tests

Phase 35 adds Go-side coverage for the supported command-line workflows:

- metadata commands return JSON with the expected shape
- preview rendering writes PNG output
- processing writes preview and DICOM artifacts
- processing without explicit outputs still defaults to `input_processed.dcm`
- tooth analysis returns JSON and preserves the Rust-compatible `tooth`, `teeth`, and `warnings` field shape

That makes the CLI itself part of the Go validation harness rather than relying on the Rust binary as the primary smoke path.

## 4. Documentation Now Points To The Go CLI

The repository README and the Go backend README now document `go -C go-backend run ./cmd/xrayview-cli -- ...` as the supported headless interface. This changes the documented owner of the workflow without removing the Rust library crate that still supports the in-process desktop bridge and helper binaries.

## 5. Validation Coverage

Validated with:

```bash
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./cmd/xrayview-cli
```

This covers:

- legacy-compatible flag parsing at the Go CLI entrypoint
- preview, process, describe, and analyze workflow execution against the sample study
- output-shape normalization needed for Rust-compatible analysis JSON

## 6. Exit Criteria Check

Phase 35 exit criteria are now met:

- the supported CLI workflows are implemented in Go
- the public workflow shape remains the existing flag-based interface instead of a premature replacement
- the validation harness for those workflows now lives with the Go backend ownership boundary
