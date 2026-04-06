# Phase 11 Prototype Go DICOM Metadata Reader

This document completes phase 11 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Go sidecar now includes a DICOM metadata reader that proves the `open_study` path can inspect real image metadata without depending on the Rust backend.

Primary implementation references:

- [go-backend/internal/dicommeta/reader.go](go-backend/internal/dicommeta/reader.go)
- [go-backend/internal/dicommeta/reader_test.go](go-backend/internal/dicommeta/reader_test.go)
- [go-backend/internal/httpapi/router.go](go-backend/internal/httpapi/router.go)
- [go-backend/internal/httpapi/router_test.go](go-backend/internal/httpapi/router_test.go)
- [backend/src/study/source_image.rs](backend/src/study/source_image.rs)

## 1. Scope Of The Prototype

Phase 11 keeps the migration boundary narrow on purpose.

The new Go reader extracts exactly the metadata called out in the migration plan:

- `Rows`
- `Columns`
- `PixelSpacing`
- `ImagerPixelSpacing`
- `NominalScannedPixelSpacing`
- `WindowCenter`
- `WindowWidth`
- `PhotometricInterpretation`
- `TransferSyntaxUID`

This proves the first DICOM dependency for Go without pretending that full pixel decode is solved.

## 2. Reader Behavior

The Go metadata reader now:

- reads Part 10 file metadata and switches dataset parsing based on `TransferSyntaxUID`
- supports explicit VR little endian, implicit VR little endian, explicit VR big endian, and the common compressed-transfer-syntax metadata case where the dataset stays explicit VR little endian
- stops before `PixelData`, so the phase stays metadata-only
- derives the contract `measurementScale` using the same precedence as Rust:
  `PixelSpacing`, then `ImagerPixelSpacing`, then `NominalScannedPixelSpacing`

The prototype still rejects deflated transfer syntax explicitly. That is intentional and keeps the limitation visible for phase 12 instead of hiding it behind partial behavior.

## 3. `open_study` Integration

Phase 10 moved study registration into Go but left `measurementScale` unset. Phase 11 closes that gap.

`POST /api/v1/commands/open_study` now:

- reads DICOM metadata before registration
- rejects non-DICOM or unreadable metadata payloads as `invalidInput`
- passes the Go-derived `measurementScale` into the registered `StudyRecord`
- keeps the recent-study catalog hook unchanged

That brings the Go `open_study` path back in line with the Rust application flow, where metadata decoding happens before a study is accepted into the registry.

## 4. Validation

The new test coverage proves both the reader itself and the HTTP integration:

- the real sample DICOM fixture is read successfully in Go
- synthetic fixtures verify spacing-tag extraction and measurement-scale precedence
- synthetic fixtures verify explicit big-endian and raw implicit-little-endian dataset handling
- the HTTP `open_study` route now rejects non-DICOM input
- the HTTP `open_study` route now returns a populated `measurementScale` when spacing metadata exists

The sample fixture now verifies these concrete values in Go:

- rows: `1088`
- columns: `2048`
- photometric interpretation: `MONOCHROME2`
- transfer syntax UID: `1.2.840.10008.1.2.1`
- window center: `127.5`
- window width: `255`
- measurement scale: absent

## 5. Exit Criteria Check

Phase 11 exit criteria are now met:

- Go can read the metadata needed for `openStudy`
- the Go backend has a real metadata path instead of a placeholder
- measurement-scale extraction is proven and wired into the contract payload
- current reader limitations are explicit enough to drive phase 12's decode-strategy decision

## 6. Validation Commands

```bash
env GOCACHE=/tmp/xrayview-go-build-cache GOPATH=/tmp/xrayview-go-path go -C go-backend test ./...
curl -s -X POST http://127.0.0.1:38181/api/v1/commands/open_study \
  -H 'content-type: application/json' \
  -d '{"inputPath":"'"$(pwd)"'/images/sample-dental-radiograph.dcm"}'
```

Expected `open_study` behavior now:

- the input must be a readable DICOM study
- the response is still `OpenStudyCommandResult`
- `study.measurementScale` is populated only when a valid spacing tag exists
- the Go runtime `studyCount` increments after registration
