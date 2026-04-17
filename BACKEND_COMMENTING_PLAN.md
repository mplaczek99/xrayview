# Backend commenting plan

## Summary

Backend is already in reasonable shape comment-wise. The stuff around caches, SSE, the router body pool, the pixel LUT hoist — those already have the "why" notes a maintainer needs. The work here is mostly filling gaps in three dense subsystems (jobs, DICOM decode, DICOM export), documenting the tooth-analysis pipeline, and leaving short paragraphs at a few architectural seams (service boundary, transport, measurement semantics) so a new contributor can land without asking where to start.

Areas where someone walking in cold actually stalls:

- **`backend/internal/jobs/`** — 1,300-line `service.go` with no package doc. Three near-identical `StartXJob` entry points, a dual-queue worker pool, cache/dedupe/cancel all tangled in the execute path. Readers will misread `finishCancelledIfRequested` as a check, not a cleanup-and-terminal-transition. `registry.go`'s tri-state cancellation (`cancellationRequested` flag vs `Cancelling` state vs `Cancelled` state) is undocumented.
- **`backend/internal/dicommeta/decode.go`** — DICOM dataset parsing with tag/VR/length branching that only makes sense if you have PS3.5 open. The preserved-tag tables, the encapsulated pixel data layout, and the stored-pixel-value sign extension all assume prior knowledge.
- **`backend/internal/export/secondary_capture.go`** — Mirror image of decode, equally spec-heavy. Raw tag numbers like `0x00080016` with no "SOP Class UID" annotation. Even-length padding rule, explicit VR little-endian, file meta group-length calc — all invisible intent.
- **`backend/internal/analysis/analysis.go`** — Tooth-candidate detection pipeline with magic constants (`0.79`, `0.69`, `1.4`, `9.0`, `150`, `118`, `82`) and no overview of the steps. Easily the densest single file and least forgiving to a new reader.
- **Service boundary / transport** — `app/app.go` has four constructors with subtle behavioral differences; `httpapi/transport.go` has a loopback-only origin check that is a security boundary (per CLAUDE.md) but reads like plumbing.

Everything else is small, targeted notes — the kind of stuff you catch on a second read.

---

## Phase 1 — Jobs subsystem

**Goal:** Make the job lifecycle understandable without having to trace every state machine by hand.

**Files:** `backend/internal/jobs/service.go`, `backend/internal/jobs/registry.go`

**Why it matters:** This is where the whole request surface actually executes, and it carries the most implicit knowledge — dedupe by fingerprint, cache-hit short-circuit, priority split, cancellation propagation. These are all real behaviors the code relies on and none of them are labeled.

**What to add:**

- Short package-level doc on `service.go` describing the shape: a small worker pool with a priority-biased render queue plus a normal job queue, fingerprint-based dedupe, cache-first execution, and cancellation wired through `context.CancelFunc` stored on the registry entry.
- One line on each `Service` struct field that carries hidden intent: why `renderQueue` and `jobQueue` are separate (priority bias described in `runWorker`), why `decodeCache` is separate from `memoryCache` (source-study decode vs result payload), what `workerOnce` gates.
- Constants `maxConcurrentJobs = 3` and `maxArtifactBytes = 256 MB` each need a one-line justification — why 3 (CPU-bound work, desktop-scale load), why 256 MB (fits a desktop session's worth of previews before eviction).
- Short comment on `runWorker` noting the non-blocking `default:` drains a render job before competing on the select. That's the whole priority mechanism and it's one line of code.
- `StartRenderJob` / `StartProcessJob` / `StartAnalyzeJob` — all three follow the same shape (`validate → fingerprint → memory-cache check → registry.StartJob → launchJob`). One comment on the first one describing the sequence is enough; don't repeat it. Note that `memory-cache hit` returns a synthesized snapshot with `FromCache: true`.
- `cachedRenderSnapshot` and peers — note that these generate a fresh job ID for a cache hit rather than replaying a prior one. That's a deliberate choice for UI consistency; without the comment it looks wasteful.
- `executeRenderJob` and peers — stage names (`"validating"`, `"loadingStudy"`, `"renderingPreview"`, `"writingPreview"`) and percent values are part of the UI contract; the frontend renders them. One line at the top of each execute function saying so keeps someone from "cleaning up" by changing them.
- `finishCancelledIfRequested` — rename would be ideal but is out of scope here; instead a clear doc comment. It both *checks* for cancellation and, if cancelled, performs cleanup (remove partial preview, transition to `Cancelled`, fire completion callback) and returns `true`. The caller must immediately `return` on `true`.
- `completeProcessJob` cleans up **both** preview and DICOM paths on cancellation race; `completeRenderJob` and `completeAnalyzeJob` clean up only the preview. Worth a line on the process variant explaining the asymmetry (process has the DICOM artifact).
- `resolveProcessOutputPath` — the rule is "user-provided path takes precedence; otherwise fall back to cache artifact". That's not obvious from the control flow. One line.
- `analyzeFingerprint` / `renderFingerprint` — versioned namespace strings (`"render-study-v1"`, `"process-study-v3"`) are deliberate cache-bust keys; bump when inputs to the job change. One comment on the pattern (doesn't need repeating on each).
- `generateJobID` — note the UUID v4 layout with dashes; handwritten because `crypto/rand` output is already uniform so the draft-7 bit-twiddling isn't needed. (Unlike `dicommeta.generateSourceStudyUID` which does set the v4 bits for DICOM's OID encoding.)

In `registry.go`:

- Package/type comments on `Registry` and `registryEntry`. Crucial: `activeFingerprints` is the dedupe map keyed on job fingerprint; its presence is what makes a duplicate `StartJob` call return the existing snapshot instead of starting a second run. Not labeled anywhere today.
- Tri-state cancellation: comment on `registryEntry.cancellationRequested` that distinguishes it from the three `JobState` cancel variants. `cancellationRequested` is the latch that survives across `UpdateProgress` calls; the state is what the snapshot reports. Also note who nils `entry.cancel` (`Complete`, `Fail`, `markCancelledLocked`) so contributors don't fire a stale `cancel()`.
- `Cancel()` switch has three meaningful branches (queued, running/cancelling, terminal). Short doc comment summarizing the three outcomes.
- `evictOldTerminalJobsLocked` — note it only evicts terminal entries (running jobs can't be dropped) and that `keepJobID` is spared because it's typically the entry that just transitioned. That's a subtle invariant.
- `cloneJobSnapshot` — exists because snapshots are returned outside the registry lock. Worth one line so no one "optimizes" it away.

**What to avoid:** Don't comment every transition method (`UpdateProgress`, `Complete`, `Fail`, `MarkCancelled`) — their names and parameters already say what they do. Don't annotate the obvious short-circuits on terminal states. Keep the stage-name contract comment in one place, not on every execute function.

---

## Phase 2 — DICOM decode

**Goal:** Give a reader enough context to follow a parse without the DICOM standard open next to them.

**Files:** `backend/internal/dicommeta/reader.go`, `backend/internal/dicommeta/decode.go`

**Why it matters:** This is the bulk of the "DICOM in" side. The format is public but dense, and this package encodes a lot of assumptions (Part 10 preamble, file meta vs dataset encoding, explicit vs implicit VR, encapsulated pixel data, rescale slope/intercept, MONOCHROME1 auto-invert). Anyone touching decode should know what's load-bearing.

**What to add in `reader.go`:**

- Top-of-file comment: this is the metadata-only reader (no pixel data). Dataset is parsed until `PixelData` is encountered, at which point encoding is classified and parsing stops. Contrast with `decode.go` which continues into the pixel data.
- `loadTransferSyntaxUID` — note that the absence of the `DICM` magic at byte 128 is accepted and treated as raw implicit-little-endian (many older files). The file-meta loop reads group `0x0002` elements until it sees the first non-meta group tag.
- `syntaxFromUID` — the default branch comment is already there (explicit VR LE for compressed syntaxes). Keep it; add one line on the deflated-syntax refusal since that's the only hard-fail case.
- `uses32BitLength` — one line naming the VR category: "large binary/sequence VRs use the 4-byte length form with a 2-byte reserved prefix" (OB/OW/SQ/etc.). Right now it's a bare list.
- `parseSourceDataset`/`parseDataset` already have the stack-buffer comment. Keep.
- `MeasurementScale` precedence — the three-tier fallback (`PixelSpacing` > `ImagerPixelSpacing` > `NominalScannedPixelSpacing`) is a deliberate DICOM-spec ordering. One line.

**What to add in `decode.go`:**

- Top-of-file: what this does vs `reader.go` (decodes pixel data into an `imaging.SourceImage`), what transforms are applied (rescale slope/intercept, MONOCHROME1 invert, optional default window), what's deliberately not done (no color space transforms beyond RGB, no multi-frame encapsulated).
- `preservedSourceTagOrder` / `preservedSourceTagVRs` — one paragraph explaining these tables exist so that the secondary-capture writer in `export/` can round-trip patient / study / series identifiers through the processed DICOM. Today there's no link between the two files.
- `decodeStoredPixelValue` — the bit-mask + sign-extend code needs a one-line explanation: DICOM stores pixel values in `BitsStored` bits within a `BitsAllocated`-byte slot; when `PixelRepresentation == 1` the stored value is two's complement and the sign bit is at position `BitsStored-1`. This is a stock DICOM operation but the code reads like bit-banging without the name attached.
- `scaledStoredPixelValue` — note the `slope * value + intercept` applies the DICOM rescale pair to produce modality values. One line.
- `readU16Samples` / `readU32Samples` already have good zero-copy comments. Keep.
- `resolveDecodeConfig` — the `MONOCHROME1` branch sets `invert`. One line: MONOCHROME1 is "low value = white", so rendering the bytes as-is would produce a negative; the invert flag propagates to the render path.
- `decodeU8Color` — three branches (PlanarConfiguration 0 = interleaved RGB, 1 = planar R/G/B planes). Label them. Non-obvious if you haven't seen them before.
- `decodeEncapsulatedPixelData` — worth a paragraph. The structure is: Item header for the Basic Offset Table (always present, often empty), then one or more Item fragments holding the compressed bitstream, then a Sequence Delimitation Item. Currently the reader has to infer that from the code.
- `generateSourceStudyUID` — note this is a fallback for files missing `StudyInstanceUID`; the "2.25.0" return is the survival path when `crypto/rand` fails (shouldn't happen, but the fallback UID is still valid OID-wise and keeps the pipeline running).
- `grayFromRGB8` — the magic numbers are ITU-R BT.601 luma coefficients in Q16 fixed-point (`19595`, `38470`, `7471`). A half-line comment is enough.

**What to avoid:** Don't annotate every tag constant (`tagRows`, `tagColumns`, etc.) — their names are the spec names. Don't duplicate the PS3.5 tables in comments. Don't explain what `binary.LittleEndian.Uint16` does.

---

## Phase 3 — DICOM export

**Goal:** Make the writer legible to someone who's read the decode side.

**Files:** `backend/internal/export/secondary_capture.go`

**Why it matters:** It's a mirror of decode, encodes structure that only makes sense with the spec in hand, and contains the one place we actually write medical-adjacent output. A future contributor adding a tag or changing a VR needs to know the even-length padding rule, the file-meta group-length calc, and the insertion-sort invariant.

**What to add:**

- Top-of-file: writes a DICOM Secondary Capture Image SOP instance. Structure is 128-byte preamble + `DICM` + file meta group (with group length element) + sorted dataset elements. All explicit VR little-endian. Preserved patient/study tags are copied through from the decoded source metadata.
- Short comments beside the raw tag constants in `encodeSecondaryCapture` — every `stringElement(0x00080016, ...)` is opaque without the tag name. A trailing `// SOP Class UID` / `// SOP Instance UID` etc. on each is worth the noise in this specific file.
- `insertElement` — the comment it has is fine. Worth adding that maintaining sorted-tag-order is required by the DICOM spec for dataset elements.
- `evenLengthBytes` / `encodeStringValues` — DICOM values must be even-length; UI VRs pad with `0x00`, string VRs pad with `0x20`. One line on `encodeStringValues` is enough.
- `encodedElementLength` / `writeElement` — the switch on VR is the 32-bit-length class (same set as `uses32BitLength` in the reader). Cross-reference in a comment so they don't drift.
- `rgbaPixelElement` already has its "skip alpha, avoid intermediate copies" comment. Keep.
- `pixelElements` — the two hardcoded attribute sets (BitsAllocated 8, HighBit 7, PixelRepresentation 0, etc.) are the Secondary Capture image module requirements. One line saying so.
- `implementationClassUID` / `implementationVersionName` — note these are the writer's identity, not random UIDs; changing them is a release concern.

**What to avoid:** Don't explain the DICOM data model from first principles. Don't comment helper functions like `u16Element`/`stringElement` — they're trivial.

---

## Phase 4 — Tooth-analysis pipeline

**Goal:** Leave a map of the pipeline at the top of the file and a one-liner on each heuristic threshold, so the next person doesn't have to rediscover the flow.

**Files:** `backend/internal/analysis/analysis.go` (+ `analysis_test.go` only if an assertion's rationale needs a pointer)

**Why it matters:** It's the one place in the backend that does real image analysis and it's full of tuned constants. If you change one without understanding the interaction with the others (toothness percentile vs intensity percentile vs min detected area), you break detection in subtle ways.

**What to add:**

- Top-of-file comment describing the pipeline end-to-end: normalize intensity → dual Gaussian blur (small + large sigma for band-pass toothness signal) → threshold on percentiles with hard floors → morphological close+open to clean the mask → connected-component extraction → score and filter candidates → select primary. Mention that `measurementScale` is optional; pixel measurements are always returned, calibrated measurements only when spacing is available.
- Each threshold constant gets a short rationale:
  - `minDetectedArea = 150` — below this a component is noise (the comment already notes both call sites must stay in sync; keep that part).
  - Percentiles `0.79` / `0.69` and floors `118` / `82` — one line: tuned against the fixture in `images/` to keep recall on thin-crown molars without pulling in gum/bone noise.
  - Blur sigmas `1.4` / `9.0` — band-pass: small sigma suppresses sensor noise, large sigma subtracts the slowly-varying bone/gum background.
  - Search-region percentages in `defaultSearchRegion` (top 20%, bottom 78%) — rationale: the tooth crown sits in the middle third of a panoramic dental X-ray; trimming the margins removes frame text and jaw shadow.
- `buildToothnessMap` / `normalizePixels` — each gets a one-sentence function comment. `normalizePixels` does a percentile-based contrast stretch (2nd / 98th) — the "why" is robustness to outliers vs. `MinValue`/`MaxValue`.
- `selectPrimaryCandidate` and the `strict` flag — note that a relaxed candidate is returned when no component passes the primary filters, with a warning appended to the output. Users see that warning in the UI, so it's contractual.
- `buildToothCandidate` — the calibrated-vs-pixel duality is straightforward but worth noting: calibrated is `nil` when `measurementScale` is `nil`; the client renders "px" in that case.
- `bufpool.PutUint8(smallBlur)` / `PutUint8(largeBlur)` calls — the existing comments are good. Keep. Similar comments on `normalized` / `toothness` returns.

**What to avoid:** Don't restate the mechanical operations (loops, bounds). Don't add TODOs for threshold tuning. Don't over-explain Gaussian blur — just label the role of each pass.

---

## Phase 5 — Service boundary and transport

**Goal:** A new contributor should be able to find where requests enter, where they fan out, and where the loopback trust boundary lives — in under a minute.

**Files:** `backend/service.go`, `backend/internal/app/app.go`, `backend/internal/app/service.go`, `backend/internal/httpapi/router.go` (light additions), `backend/internal/httpapi/transport.go`

**Why it matters:** Two of the four constructors in `app/app.go` differ only in subtle wiring. `httpapi/transport.go` is the CORS/origin gate that CLAUDE.md calls out as a hard requirement. `backend/service.go` is the only exported surface the desktop shell uses. None of this is labeled.

**What to add:**

- `backend/service.go` — top-of-file: this is the stable, exported service facade used by the desktop (Wails) build. Embeds `internalapp.App` and forwards commands. Adds no logic; lives outside `internal/` so non-backend modules can depend on it.
- `app/app.go` — short comment on each constructor. `New` = server-ready app, constructor-only (caller calls `Run`). `NewService` = embedded/in-process mode (no HTTP server), prepares cache/persistence eagerly. `NewWithServices` = test or composition entry point accepting pre-built collaborators. `NewFromEnvironment` = production entry point, loads config from env.
- `app.prepare` — note the dual log message (server vs embedded) signals which mode we're in; useful when triaging logs.
- `app/service.go` — one-line comment on `BackendService` noting it's the command surface shared by the HTTP router and the desktop shell (via `backend/service.go`). Avoid duplicating the interface doc.
- `OpenStudy` — the canonical ingest path. Short doc: validate file → read metadata → register in studies registry → record to persistent catalog (best-effort; catalog failure logs and moves on). Today's flow looks like plumbing; the best-effort catalog write is the easy-to-miss part.
- `httpapi/transport.go` — `isAllowedOrigin` and `wrapLocalTransport` need a clear header. Something like: "Loopback-only transport guard. Rejects any `Origin` header that isn't localhost or a loopback IP. Required by the local-sidecar threat model — the HTTP listener binds only to loopback addresses and the router trusts the caller; anything that crosses this middleware and fails here never reaches the command dispatcher." Keep the CORS response minimal — no need to document preflight plumbing.
- `httpapi/router.go` — already decently commented. Additions:
  - One line on the `CommandsPath+"/"` handler noting the switch is the single dispatch table; adding a command means updating the schema, regenerating `contracts`, then adding a case here and a handler function.
  - `jsonWriterPool` already has a good comment. Keep.
  - `jobUpdateSubscriber` — the comment is fine. Keep.

**What to avoid:** Don't document each `handleXxx` function — they're all the same shape (decode → forward → write). A single comment on the first one is plenty, then let the pattern speak for itself. Don't comment `resolveStudyCount` / `resolveSupportedJobKinds` — reflection-style interface probes are clear enough from the code.

---

## Phase 6 — Cleanup pass

**Goal:** A dozen small, targeted comments across the rest of the backend. No phase deserves a whole round on its own, but collectively they smooth over the rough spots.

**Files (each touched briefly):**

- `backend/internal/studies/decode_cache.go` — label the `inflight` map: two concurrent decoders for the same path share a single decode; second caller blocks on `done`. Also note the "treat returned `SourceStudy` as read-only" invariant is already documented on `GetOrDecode`; leave it.
- `backend/internal/studies/registry.go` — `evictOldestLocked` is misnamed: there's no recency tracking, it evicts an arbitrary non-kept entry. Add one line so no one changes callers expecting LRU semantics. (Rename is tempting but out of scope.)
- `backend/internal/persistence/catalog.go` — `Load` silently returns an empty catalog when the file is missing (first run); the corrupt-JSON branch *renames* the bad file to `catalog.corrupt.json` and returns an error so subsequent writes don't clobber the evidence. Both behaviors deserve one line each.
- `backend/internal/processing/request.go` — merge rule: command overrides preset when explicitly set; `Invert`/`Equalize` are ORed (either source can enable). One line at the top of `ResolveProcessStudyCommand`.
- `backend/internal/processing/grayscale.go` — `equalizeHistogramInPlace` needs a one-liner naming the algorithm ("standard histogram-equalization CDF remap with `cdfMin` baseline") so readers don't re-derive the formula. The unsafe-LUT loop already has its comment; keep.
- `backend/internal/processing/compare.go` — note the overflow guard `left.Width > ^uint32(0)/2` prevents `combinedWidth` from wrapping; easy to strip by someone tidying.
- `backend/internal/render/render_plan.go` — the LUT fast path condition `source.MinValue >= 0 && source.MaxValue <= 65535` is the "source values fit a 16-bit index" guard. One line. Otherwise the hoist comment and LUT build are clear.
- `backend/internal/render/preview_png.go` / `preview_jpeg.go` — the `*image.Gray` / `*image.RGBA` constructors share the backing slice with `preview.Pixels`. The encoder must finish before the pool-backed pixel slice is returned. Worth a short note on `previewImage`.
- `backend/internal/cache/memory.go`, `cache/store.go`, `httpapi/sse.go`, `httpapi/router.go` body/JSON pool — already well-commented. **Skip entirely.** Don't add noise here.
- `backend/internal/bufpool/bufpool.go` — one line at the top: buffer pools for hot pixel-buffer paths; `Get(n)` returns a slice of exactly length `n` with pooled capacity when available. The per-function docs are already fine.
- `backend/internal/annotations/measurement.go` — note `roundMeasurement` rounds to 0.1 for display stability; changing it shifts every annotation label the UI shows.
- `backend/internal/annotations/suggestions.go` — one line: auto-generated annotations from tooth analysis; IDs are stable per tooth index so the UI can diff/update rather than re-create on re-analysis.
- `backend/internal/config/config.go` — short comment above the env-var constants noting the `LegacyXxx` aliases exist for backwards compatibility with the pre-rename `XRAYVIEW_GO_BACKEND_*` names and are intentionally still honored.
- `backend/cmd/xrayview-cli/legacy_cli.go` — `isPlainPreviewRequest` warrants one line: "no processing flags set; falls through to a straight render path". `validateLegacyModeSelection` — one line on the "exactly one mode" rule.
- `backend/cmd/xrayview-cli/main.go` — top-of-file paragraph listing the subcommand / workflow-flag split and noting that a `-`-prefixed first arg routes to the legacy flag parser.
- `backend/cmd/xrayviewd/main.go` — trivial, skip.
- `backend/internal/imaging/model.go` — one line: `SourceImage` is `float32` modality values (decode output); `PreviewImage` is `uint8` display bytes (render/process output). That clarifies the whole type system for the pipeline.

**What to avoid:** Don't add file-level docs to packages whose name already tells you what's in them (`jpeg`, `png`, `bufpool`). Don't comment test files — reviewers look at code under test, not the test scaffolding.

---

## Execution order

1. **Phase 1 (jobs)** first. It's the central coordinator; anything I learn about how cache/cancellation/dedupe interact shapes comments in phases 2–6.
2. **Phase 5 (service boundary)** second. Short phase, high value for orientation. Makes phases 2–4 easier to review in isolation because the "where does this get called from?" question is already answered.
3. **Phase 2 (decode)**, then **Phase 3 (export)**. Export is a natural extension of decode; doing them back-to-back keeps the spec context fresh.
4. **Phase 4 (analysis)** last of the big ones — it's isolated from the rest, so delaying it doesn't block anything.
5. **Phase 6 (cleanup)** at the end as a single focused pass. Easier to catch duplication and hold to the "only if it adds value" bar when done together.

Each phase is one commit. Keep them reviewable — a diff of only comment lines (and trivial comment-adjacent whitespace) makes it obvious nothing semantic moved.

---

## Risks

- **Over-commenting the already-commented.** `cache/`, `httpapi/sse.go`, the `router.go` body/JSON pool, `render/preview.go`'s format-choice note, and `grayscale.go`'s LUT hoist are already good. Adding adjacent comments there adds noise and will probably invite reviewer pushback. The plan skips them deliberately — worth resisting the pull to "do a full pass" once you're in the file.

- **Commenting magic constants as if they're settled.** The analysis thresholds (percentiles, blur sigmas, minimum area) are tuned heuristics, not specifications. Comments should say "tuned against fixture" and not pretend there's a derivation. If someone bumps them later, the comment should age gracefully.

- **Drift between decoded preserved tags and the export writer.** Phase 2 and Phase 3 both reference the preserved-tag tables; comments should cross-reference each other so future edits to the table touch both sides. Worth re-reading the paired files together before committing phase 3.

- **Embedding PR/context references.** Easy to slip into comments like "see issue X" or "added in phase 19" (that last one actually exists in the codebase — `legacy_cli.go` references "phase 19 preview processing pipeline" in usage text, which is fine for CLI output but would rot as a code comment). Keep the commenting pass forward-looking — describe behavior, not history.

- **Renames that should happen but aren't in scope.** `finishCancelledIfRequested` does two things, `evictOldestLocked` doesn't evict the oldest. Comments will paper over these. That's fine for this pass but worth flagging for a later cleanup — I'd track them in the commit message, not in a TODO in the code.

- **Unstable areas.** If `OPTIMIZATION_PLAN.md` has unfinished steps touching `render_plan.go` or `grayscale.go`, commenting them now is premature. Worth a quick check of that file before phase 6 — skip anything that's going to be rewritten.
