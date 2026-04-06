# Phase 2 Backend Contract Freeze

This document completes phase 2 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). It freezes the current desktop/backend API surface as contract version 1 before any Go implementation work begins.

Primary implementation references:

- [backend/src/api/contracts.rs](backend/src/api/contracts.rs)
- [backend/src/api/mod.rs](backend/src/api/mod.rs)
- [backend/src/error.rs](backend/src/error.rs)
- [backend/src/tooth_measurement.rs](backend/src/tooth_measurement.rs)
- [frontend/src/lib/generated/contracts.ts](frontend/src/lib/generated/contracts.ts)
- [backend/tests/contracts.rs](backend/tests/contracts.rs)

## 1. Contract v1 Declaration

Phase 2 now declares the frozen desktop/backend contract explicitly:

- `backend/src/api/contracts.rs` declares `BACKEND_CONTRACT_VERSION = 1`.
- `frontend/src/lib/generated/contracts.ts` emits the same version as `BACKEND_CONTRACT_VERSION`.
- The committed file at [frontend/src/lib/generated/contracts.ts](frontend/src/lib/generated/contracts.ts) is the exact TypeScript projection of contract v1.

This freeze is intentionally scoped to the request/response DTOs that the React frontend and Tauri shell rely on today. It does not freeze internal Rust-only structs such as:

- CLI orchestration requests in [backend/src/api/mod.rs](backend/src/api/mod.rs) like `RenderPreviewRequest`, `ProcessStudyRequest`, and `AnalyzeStudyRequest`
- shell-only file-picker commands in [frontend/src-tauri/src/main.rs](frontend/src-tauri/src/main.rs)

Any post-phase-2 change to this API surface should now be treated as one of:

- a bug fix that preserves contract v1 semantics
- an intentional contract v2 change with explicit versioning

## 2. Audited Correction Applied Before Freeze

One concrete mismatch was fixed before freezing v1:

- Rust serializes `BackendError.details` only when the array is non-empty because [backend/src/error.rs](backend/src/error.rs) uses `#[serde(default, skip_serializing_if = "Vec::is_empty")]`.
- The generated TypeScript contract had incorrectly declared `details` as always present.
- Contract v1 now reflects the real wire semantics: `details` is optional and consumers must treat omission the same as an empty array.

That is the kind of low-risk correction phase 2 should absorb before the migration makes the current surface harder to change safely.

## 3. Frozen v1 Surface

### 3.1 Manifest payloads

The frozen manifest surface is:

- `PaletteName`: closed enum of `"none" | "hot" | "bone"`
- `ProcessingControls`: always includes `brightness`, `contrast`, `invert`, `equalize`, and `palette`
- `ProcessingPreset`: stable pair of `id` plus `controls`
- `ProcessingManifest`: stable `defaultPresetId` plus ordered `presets`

Semantics:

- palette names are part of the contract, not presentation-only labels
- manifest fields are always present; there are no optional manifest keys today

### 3.2 Study-open payloads

The frozen study-open surface is:

- `OpenStudyCommand`
- `OpenStudyCommandResult`
- `StudyRecord`
- `MeasurementScale`

Semantics:

- `OpenStudyCommand.inputPath` is serialized as a filesystem path string
- `StudyRecord.studyId` is a backend-generated opaque identifier
- `StudyRecord.inputPath` and `StudyRecord.inputName` echo the registered source study
- `measurementScale` is omitted when the source DICOM does not expose usable scale metadata
- `MeasurementScale.source` remains an opaque backend label such as the current DICOM-tag source names

### 3.3 Job-start and job-result payloads

The frozen job-control surface is:

- `StartedJob`
- `JobCommand`
- `RenderStudyCommand`
- `RenderStudyCommandResult`
- `ProcessStudyCommand`
- `ProcessStudyCommandResult`
- `AnalyzeStudyCommand`
- `AnalyzeStudyCommandResult`

Semantics:

- render and analyze requests currently key only on `studyId`
- `ProcessStudyCommand` keeps the current preset-plus-override model:
  - `presetId` is required
  - `outputPath`, `brightness`, `contrast`, and `palette` are optional overrides
  - `invert`, `equalize`, and `compare` are always explicit booleans
- `RenderStudyCommandResult` and `ProcessStudyCommandResult` keep `previewPath` as a backend filesystem path, not a transport-neutral URL
- `ProcessStudyCommandResult.mode` remains an opaque backend string
- `measurementScale` on render/process results is optional and omitted when unavailable

### 3.4 Job snapshot payloads

The frozen job-state surface is:

- `JobKind`
- `JobState`
- `JobProgress`
- `JobResult`
- `JobSnapshot`

Semantics:

- `JobKind` is a closed enum of `renderStudy`, `processStudy`, and `analyzeStudy`
- `JobState` is a closed enum of `queued`, `running`, `cancelling`, `completed`, `failed`, and `cancelled`
- `JobResult` is serialized as a discriminated union with a top-level `kind` tag and `payload` object
- `JobSnapshot.fromCache` is always an explicit boolean, even on non-cached jobs
- `JobSnapshot.studyId`, `result`, and `error` remain optional fields rather than always-present nullable keys

### 3.5 Error payloads

The frozen error surface is:

- `BackendErrorCode`
- `BackendError`

Semantics:

- `BackendErrorCode` is currently the closed enum:
  - `invalidInput`
  - `notFound`
  - `cancelled`
  - `conflict`
  - `cacheCorrupted`
  - `internal`
- `message` is always present and is the primary user-facing error string
- `details` is omitted when empty and present only when the backend has structured extra detail
- `recoverable` remains a required boolean derived from backend classification rules

### 3.6 Measurement payloads

The frozen measurement surface is:

- `AnnotationSource`
- `AnnotationPoint`
- `LineMeasurement`
- `LineAnnotation`
- `RectangleAnnotation`
- `AnnotationBundle`
- `MeasureLineAnnotationCommand`
- `MeasureLineAnnotationCommandResult`

Semantics:

- annotation coordinates stay in image-space numeric coordinates
- `AnnotationSource` remains the closed enum `"manual" | "autoTooth"`
- `LineMeasurement.pixelLength` is always present
- `LineMeasurement.calibratedLengthMm` is optional and omitted when no real-world calibration is available
- `LineAnnotation.confidence`, `LineAnnotation.measurement`, and `RectangleAnnotation.confidence` remain optional payload members
- `MeasureLineAnnotationCommandResult` returns the updated annotation DTO rather than a separate measurement-only shape

### 3.7 Analysis payloads

The frozen analysis surface is:

- `ToothAnalysis`
- `ToothImageMetadata`
- `ToothCalibration`
- `ToothCandidate`
- `ToothMeasurementBundle`
- `ToothMeasurementValues`
- `ToothGeometry`
- `BoundingBox`
- `LineSegment`
- `Point`

Semantics:

- `ToothAnalysis.image` always reports rendered preview dimensions
- `ToothCalibration.pixelUnits` currently remains `"px"`
- `ToothCalibration.measurementScale` is optional
- `ToothCalibration.realWorldMeasurementsAvailable` mirrors whether calibration metadata exists
- `ToothAnalysis.tooth` is optional and omitted when no primary candidate is selected
- `ToothAnalysis.teeth` is always present as an array, even when empty
- `ToothAnalysis.warnings` is always present as an array
- `ToothMeasurementBundle.calibrated` is optional and omitted when real-world calibration is unavailable
- geometry values are serialized as numbers even when sourced from integer Rust fields

## 4. Drift Protection Added In Phase 2

Phase 2 now adds an explicit automated guard in [backend/tests/contracts.rs](backend/tests/contracts.rs):

- it compares `generated_typescript_contracts()` against the committed file at [frontend/src/lib/generated/contracts.ts](frontend/src/lib/generated/contracts.ts)
- it asserts that the declared contract version remains `1`
- it locks specific JSON serialization semantics that are easy to break accidentally:
  - `BackendError.details` omission when empty
  - `JobResult` serialization as `{ kind, payload }`
  - omission of absent optional measurement fields

That is the current practical freeze mechanism while Rust remains the contract source of truth. Phase 3 should replace this handwritten Rust-to-TypeScript path with a language-neutral schema, but phase 2 now makes contract drift explicit and testable.

## 5. Validation

Validate the phase 2 freeze with:

```bash
cargo test --manifest-path backend/Cargo.toml --test contracts
```

Regenerate the committed TypeScript contract intentionally with:

```bash
npm --prefix frontend run generate:contracts
```

The frontend build should continue to compile against the committed v1 contract file:

```bash
npm --prefix frontend run build
```
