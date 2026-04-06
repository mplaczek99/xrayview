# Phase 1 Parity Fixture Suite

This document completes phase 1 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). It defines and commits the baseline fixtures the Go migration must match before any claim of backend parity is credible.

Primary implementation references:

- [backend/tests/parity.rs](backend/tests/parity.rs)
- [backend/tests/fixtures/parity/sample-dental-radiograph/describe-study.json](backend/tests/fixtures/parity/sample-dental-radiograph/describe-study.json)
- [backend/tests/fixtures/parity/sample-dental-radiograph/render-preview.png](backend/tests/fixtures/parity/sample-dental-radiograph/render-preview.png)
- [backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-preview.png](backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-preview.png)
- [backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-export.json](backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-export.json)
- [backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-compare-preview.png](backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-compare-preview.png)
- [backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-compare-export.json](backend/tests/fixtures/parity/sample-dental-radiograph/process-xray-compare-export.json)
- [backend/tests/fixtures/parity/sample-dental-radiograph/analyze-study.json](backend/tests/fixtures/parity/sample-dental-radiograph/analyze-study.json)
- [backend/tests/fixtures/parity/sample-dental-radiograph/analyze-preview.png](backend/tests/fixtures/parity/sample-dental-radiograph/analyze-preview.png)
- [backend/tests/fixtures/parity/sample-dental-radiograph/recent-study-catalog.json](backend/tests/fixtures/parity/sample-dental-radiograph/recent-study-catalog.json)
- [backend/tests/fixtures/parity/sample-dental-radiograph/render-cache-hit.json](backend/tests/fixtures/parity/sample-dental-radiograph/render-cache-hit.json)

## 1. Sample Matrix

Current canonical input set:

- `images/sample-dental-radiograph.dcm`

Current workflow matrix:

| Workflow | Generator path | Golden artifact(s) |
| --- | --- | --- |
| Study metadata extraction | `describe_study(...)` | `describe-study.json` |
| Default preview render | `render_preview(...)` | `render-preview.png` |
| Processed preview output | `process_study(...)` with preset `xray` | `process-xray-preview.png` |
| Xray-preset exported DICOM output | `process_study(...)` with preset `xray` | `process-xray-export.json` |
| Compare preview output | `process_study(...)` with preset `xray` and `compare = true` | `process-xray-compare-preview.png` |
| Compare-mode exported DICOM output | `process_study(...)` with preset `xray` and `compare = true` | `process-xray-compare-export.json` |
| Analysis output | `AppState::analyze_study(...)` | `analyze-study.json`, `analyze-preview.png` |
| Recent-study persistence | `StudyCatalogStore::record_opened_study(...)` | `recent-study-catalog.json` |
| Cache behavior baseline | `AppState::start_render_job(...)` twice on the same input | `render-cache-hit.json` |

## 2. Normalization Rules

Not every output can be compared byte-for-byte forever. The parity suite normalizes the dynamic fields that would otherwise make the fixtures unstable:

- PNG fixtures are committed as actual images, but the test compares decoded pixel data, dimensions, and color type instead of raw PNG file bytes.
- Exported DICOM fixtures are stored as JSON summaries, not full `.dcm` goldens.
- Export summaries keep stable fields verbatim and normalize dynamic fields into booleans:
  - generated SOP/Series instance UIDs only need to retain the `2.25.` form
  - instance creation date/time only need to satisfy the current format contract
  - pixel payload is validated through deterministic length + FNV-1a hash
- Recent-study fixtures normalize the catalog path to a repo-relative path and reduce `lastOpenedAt` to an RFC3339-validity check.
- Cache fixtures normalize away job IDs and study IDs while preserving the key current behavior:
  - first render run completes normally
  - second render run is returned from cache
  - cached render payload keeps the older nested `studyId` instead of rewriting it to the current top-level `studyId`

Those normalization rules are deliberate. They preserve the semantic contract the Go port has to match without hard-coding values that are expected to vary between runs.

## 3. Validation

Run the parity suite with:

```bash
cargo test --manifest-path backend/Cargo.toml --test parity
```

Refresh the committed fixtures intentionally with:

```bash
XRAYVIEW_WRITE_PARITY_FIXTURES=1 cargo test --manifest-path backend/Cargo.toml --test parity
```

The refresh path is explicit so a normal test run never rewrites the baseline silently.

## 4. Known Gaps

This phase 1 suite is a valid baseline, but it is not exhaustive:

- only one public DICOM sample is currently in the matrix
- the current sample does not exercise measurement-scale extraction because `describe-study.json` is empty
- there is no separate non-palette processed export fixture yet; the current `xray` export baseline is RGB because the preset applies the `bone` palette
- there are no parity fixtures yet for cancellation, failure payloads, compressed edge cases, big-endian inputs, or unsupported color source variants

Those are later expansion targets, not blockers to completing phase 1 as defined in the migration plan.
