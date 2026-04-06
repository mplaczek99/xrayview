# Phase 12 Decide DICOM Decode Strategy

This document completes phase 12 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The decode strategy is now locked: the migration will continue with Go orchestration plus a narrow Rust decode helper in phase 13 instead of committing to pure Go pixel decode yet.

Primary implementation references:

- [go-backend/internal/dicommeta/reader.go](go-backend/internal/dicommeta/reader.go)
- [go-backend/internal/dicommeta/inspect.go](go-backend/internal/dicommeta/inspect.go)
- [go-backend/internal/dicommeta/reader_test.go](go-backend/internal/dicommeta/reader_test.go)
- [go-backend/internal/dicommeta/inspect_test.go](go-backend/internal/dicommeta/inspect_test.go)
- [go-backend/cmd/xrayview-cli/main.go](go-backend/cmd/xrayview-cli/main.go)
- [backend/src/study/source_image.rs](backend/src/study/source_image.rs)

## 1. Repo Sample Coverage Is Real But Narrow

Phase 12 adds a Go-side `inspect-decode` command so the current study corpus can be profiled without pretending decode is already implemented in Go.

The new inspection output captures the decode-relevant attributes needed for the strategy decision:

- transfer syntax UID
- native vs encapsulated pixel data
- rows and columns
- samples per pixel
- bits allocated and bits stored
- pixel representation
- planar configuration
- number of frames
- photometric interpretation

Running that inspection against every DICOM file currently committed in the repository shows a very narrow corpus:

- 2 studies inspected
- both are explicit VR little endian: `1.2.840.10008.1.2.1`
- both use native pixel data
- both are single-frame
- both are monochrome
- both are 8-bit
- there are no encapsulated studies in the repo corpus
- there are no compressed transfer syntaxes in the repo corpus

That is useful evidence, but it is not enough evidence to lock the migration onto pure Go decode.

## 2. Rust Already Covers The Hard Cases The Go Side Does Not

The current Rust backend decode path in [backend/src/study/source_image.rs](backend/src/study/source_image.rs) already has a bounded split that matches the migration plan’s risk analysis:

- native primitive decode for uncompressed studies
- encapsulated pixel decode through `dicom_pixeldata`
- decode-time handling for monochrome and current RGB cases
- decode-time default window and invert extraction

The Go side now proves metadata parsing and sample-corpus inspection, but it still has no pixel decode implementation and no local corpus evidence for compressed or encapsulated studies.

Given that gap, choosing pure Go decode now would not be a technical decision. It would just be optimism.

## 3. Locked Decision

Phase 12 locks the decode strategy to:

- Go remains the primary backend owner
- Go will orchestrate study lifecycle, jobs, render pipeline, and later cutovers
- phase 13 will introduce a narrow Rust decode helper for source-pixel loading
- the helper scope must stay limited to decode and decode-adjacent source metadata only

This explicitly rejects two bad outcomes:

- no vague “we will decide later”
- no broad reuse of the existing Rust backend behind a thin wrapper

The decision is intentionally conservative because the current repo corpus does not exercise the subsystem that the migration plan identifies as highest risk.

## 4. Helper Boundary For Phase 13

The Rust helper should stay narrow enough that Go still owns the application, not Rust.

The helper boundary is now defined as:

- input: study path and a minimal decode request from Go
- output: normalized decode payload for one source study
- include width, height, grayscale pixel buffer or preview-ready bytes
- include decode defaults needed downstream such as default window and invert
- include only the preserved source metadata subset Go still needs for later export work
- do not include job orchestration, caching policy, HTTP transport, command routing, or frontend contracts

This keeps phase 13 aligned with the migration plan: preserve momentum around the hard DICOM subsystem without letting Rust remain the true backend.

## 5. Validation

Validation commands:

```bash
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go -C go-backend test ./...
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path \
  go -C go-backend run ./cmd/xrayview-cli inspect-decode \
  ../images/sample-dental-radiograph.dcm \
  ../images/sample-dental-radiograph_processed.dcm
```

Expected inspection findings:

- `studyCount` is `2`
- `transferSyntaxUids` contains only `1.2.840.10008.1.2.1`
- `pixelDataEncodings` contains only `native`
- `samplesPerPixelValues` contains only `1`
- `photometricInterpretations` contains only `MONOCHROME2`
- warnings call out the missing coverage for encapsulated, compressed, and multi-frame studies

## 6. Exit Criteria Check

Phase 12 exit criteria are now met:

- the decode decision is explicit
- the decision is based on reproducible sample inspection, not preference
- pure Go decode is not over-claimed from a metadata-only success
- the next phase boundary is clear: build a narrow Rust decode helper, not a second backend
