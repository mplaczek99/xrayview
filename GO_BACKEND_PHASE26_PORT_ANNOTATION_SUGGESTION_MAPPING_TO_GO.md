# Phase 26 Port Annotation Suggestion Mapping To Go

This document completes phase 26 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Go backend now has a Rust-parity owner for the transformation from `ToothAnalysis` results into editable annotation DTOs: stable suggestion IDs, editable width/height line suggestions, non-editable bounding-box rectangles, and confidence propagation.

Primary implementation references:

- [go-backend/internal/contracts/analysis.go](go-backend/internal/contracts/analysis.go)
- [go-backend/internal/contracts/annotations.go](go-backend/internal/contracts/annotations.go)
- [go-backend/internal/annotations/suggestions.go](go-backend/internal/annotations/suggestions.go)
- [go-backend/internal/annotations/suggestions_test.go](go-backend/internal/annotations/suggestions_test.go)
- [backend/src/analysis/auto_tooth.rs](backend/src/analysis/auto_tooth.rs)
- [backend/tests/fixtures/parity/sample-dental-radiograph/analyze-study.json](backend/tests/fixtures/parity/sample-dental-radiograph/analyze-study.json)

## 1. Go Now Owns The Suggestion Mapper

Before this phase, the Go backend had no native equivalent for Rust's `suggested_annotations` helper. The contract schema already described `ToothAnalysis`, `AnnotationBundle`, and `AnalyzeStudyCommandResult`, but the internal Go contracts package did not expose those shapes and there was no mapper implementation.

Phase 26 closes that gap by adding:

- full Go contract models for tooth-analysis payloads
- full Go contract models for rectangle annotations and annotation bundles
- a dedicated `annotations.SuggestedAnnotations` helper that mirrors the Rust transformation rules

This is the intended ownership boundary for converting analysis geometry into frontend-editable annotation data, even though the end-to-end Go analyze job still lands in phase 28.

## 2. Mapping Semantics Match Rust

The Go mapper follows the Rust implementation in [backend/src/analysis/auto_tooth.rs](backend/src/analysis/auto_tooth.rs):

- line IDs remain `auto-tooth-N-width` and `auto-tooth-N-height`
- rectangle IDs remain `auto-tooth-N-bounding-box`
- labels remain `Tooth N width`, `Tooth N height`, and `Tooth N bounding box`
- width and height suggestions are editable
- bounding-box rectangles are not editable
- line measurements are recomputed through the Go phase-25 measurement helper
- confidence values are copied onto both lines and rectangles

The helper also returns empty slices, not `null`, when analysis has no detected teeth so the bundle still matches the frozen contract shape.

## 3. Validation Coverage

Validated with:

```bash
gofmt -w go-backend/internal/contracts/annotations.go go-backend/internal/contracts/analysis.go go-backend/internal/annotations/suggestions.go go-backend/internal/annotations/suggestions_test.go
cd go-backend
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go test ./...
```

Coverage now includes:

- a direct Rust-parity unit case ported from `auto_tooth.rs`
- a fixture-parity test that loads `backend/tests/fixtures/parity/sample-dental-radiograph/analyze-study.json`
- an empty-analysis test that asserts array-shaped empty bundles

The fixture test is the strongest phase-26 validation because it compares the Go mapper output against a stored Rust-generated annotation bundle from the sample study.

## 4. Exit Criteria Check

Phase 26 exit criteria are now met:

- suggestion generation logic has a Go owner
- suggestion IDs, line suggestions, bounding boxes, and confidence propagation are implemented in Go
- annotation bundle output is compared directly with Rust fixture output
