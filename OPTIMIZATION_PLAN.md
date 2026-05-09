# xrayview — Optimization Plan

A pass through the entire codebase (~30k LoC across `backend/`, `desktop/`,
`frontend/`, `contracts/`) looking for asymptotic problems, wasted allocations,
single-threaded hotspots, and small-but-hot constant-factor wins.

Each item has:

- **Impact** — rough win expected when the path is exercised. P0=major
  (orders of magnitude or seconds saved per analyze/render); P1=meaningful
  (10–50% on a hot path); P2=nice-to-have; P3=nit.
- **Effort** — S=hour, M=afternoon, L=multi-day or design work.
- **Where** — file:line for the symptom.
- **What / Why** — concrete change and the reasoning.

The two most dollar-rich areas are **`backend/internal/analysis/`** (the
tooth/bone overlay pipeline — 1019-line single-threaded morphology +
contour smoothing run on every Analyze) and the **buffer pool that does
nothing** (`bufpool` is plumbed everywhere but `Put*` is never called).

---

## P0 — Highest impact

### 1. `bufpool` pool is dead code; everything still allocates

**Where:** `backend/internal/bufpool/bufpool.go:17–73` defines `GetUint8` /
`PutUint8` / `GetUint16` / `PutUint16` / `GetFloat32` / `PutFloat32`.
A grep for `bufpool.Put` across `backend/` returns **zero hits in
production** — only the `Get*` half is wired in.

Hot callers that take buffers and never return them:

- `backend/internal/render/render_plan.go:21` (`RenderGrayscalePixels`)
- `backend/internal/processing/grayscale.go:30` (`ProcessPreviewImage`)

**Why it matters:** every render and process job currently allocates a
fresh `[]uint8` of `width*height` bytes (and the float-decode path in
`dicommeta` allocates a `[]float32` of `4·width·height`), so the GC churn
the pool was added to fix is still happening on every job.

**Fix (M):** decide on an ownership rule and enforce it.

- Option A: callers pair every `Get` with `defer Put` once the consumer
  is done. The consumer chain today is `render → preview → encode/save`,
  so `Put` belongs near the encoder boundary.
- Option B (cleaner): make `imaging.PreviewImage` carry a `Release()`
  closure populated when the buffer comes from the pool, and call it from
  `render.SavePreview` / processing pipeline tail / cache eviction.

Either way: add a benchmark that fails if `b.AllocsPerOp()` regresses.

---

### 2. `dilate`/`erode` are O(W·H·R²); analysis runs them four+ times per call

**Where:** `backend/internal/analysis/teeth.go:807–855` (`dilateBinaryMask`,
`erodeBinaryMask`). Both use a four-deep nested loop with `R²` neighborhood
visits per pixel. The wrappers `closeBinaryMask` / `openBinaryMask`
(teeth.go:799–805) call dilate-then-erode, and `innerOutlineMask`
(teeth.go:784–797) erodes again. `thickenBoneLineMask` and `boneOutlineMask`
also dilate.

For a 2048×1536 preview with `radius=2` that is ~40M cells × 25 = **1B
neighbor reads** per call, all single-threaded.

**Fix (L):** replace with a separable horizontal-then-vertical max/min
filter. For larger radii, switch to van Herk-Gil-Werman (linear in `R`
regardless of size). Even the naive separable form drops the inner cost
from `R²` to `2R` and parallelizes per-row trivially.

**Expected impact:** the morphology phase of Analyze drops from being a
visible delay on multi-MP images to milliseconds. Combined with #3 below,
this is most of the gain in the analyze pipeline.

---

### 3. Tooth/bone scoring scans every pixel single-threaded with float-heavy work

**Where:**

- `backend/internal/analysis/learned_model.go:161–178` (`learnedToothScores`):
  per-pixel `learnedFeatures` builds an `[18]float64`, then a tree-walk for
  every tree in `learnedModelTrees` per pixel.
- `backend/internal/analysis/learned_model.go:118–136`
  (`featureTableToothMask`): per-pixel hashed-bin lookup with sort.Search.
- `backend/internal/analysis/bone_feature_table_model.go:34–46`
  (`boneFeatureTableMask`): same pattern.
- `backend/internal/analysis/learned_model.go:211–224` (`gradientGray`):
  per-pixel two-tap difference, single-threaded.

A grep across `backend/internal/` shows zero use of `runtime.NumCPU()`,
goroutines, or `errgroup` in any image-processing path — the entire
analyze pipeline is one core.

**Fix (M):** parallelize each of these by row-stripe (`n := runtime.NumCPU()`
workers, each owning `height/n` rows of the output slice). The functions are
embarrassingly parallel — outputs are a single `[]uint8`/`[]float64` indexed
linearly, no shared mutable state.

**Expected impact:** ~Nx on N cores for the analyze hot path. Combined
with #2, an Analyze on a typical desktop should drop to a fraction of its
current wall time.

---

### 4. Contour smoothing reallocates on every iteration; one pass *doubles* the slice

**Where:** `backend/internal/analysis/teeth.go:287–459`. Inside
`drawSmoothedMaskContours` (teeth.go:193–213) every contour goes through:

- `lowPassClosedContour` — 18 iterations, each `make([]overlayPoint, len(filtered))` (teeth.go:297).
- `smoothClosedContour` — 4 iterations, each `make([]overlayPoint, 0, len(smoothed)*2)` and **emits two points per source point** (teeth.go:406–419). After 4 iterations the contour has 16× as many points as it started with.
- `fairClosedContour` → `laplacianSmoothClosedContour` — 14 iterations × 2 lambdas, each allocating a fresh `make([]overlayPoint, len(points))` (teeth.go:445).

Net: ~50 allocations per contour, with the slice size growing 16-fold
through the smoothing stage, then everything downstream of smoothing pays
that 16× cost (resample, simplify, stroke rasterization in
`addClosedStrokeCoverage`).

**Fix (M):**

1. Ping-pong two preallocated scratch slices through every smoothing
   iteration; never `make` inside the loop.
2. Audit `smoothClosedContour`: the doubling looks unintentional. If it
   *is* intentional (sub-pixel resampling), do it once at the end with
   the right multiplier rather than 16× from compounded passes.
3. Once #2 is done, the resample/simplify pipeline runs over a contour
   ~16× shorter and stroke rasterization (#5) gets faster for free.

---

### 5. Stroke coverage computes per-pixel point-to-segment distance over an OBB

**Where:** `backend/internal/analysis/teeth.go:484–533`
(`addClosedStrokeCoverage`, `addStrokeSegmentCoverage`). For every contour
segment, the bounding box is iterated and `distanceToSegment` is called
per pixel — `math.Hypot` (i.e., `sqrt`) inside.

Combined with #4 (contours that have ballooned to 16N points), this
scales as `Σ|segment_bbox| × O(1)` per analyze.

**Fix (M):** rasterize a thick-line segment with the standard "compute
the distance via the perpendicular and dot-product into the segment
direction once per scanline; step incrementally". `math.Hypot` is the
expensive call here — drop to `dx*dx + dy*dy` and only `sqrt` at the
edge. Most pixels fall inside `coverage >= 1` and the sqrt is wasted.

---

## P1 — Meaningful wins on hot paths

### 6. Render LUT rebuilds 65 536 entries on every render, even when source/window is identical

**Where:** `backend/internal/render/render_plan.go:59–84` (`buildRenderLUT`).
The LUT is allocated stack-side as a `[65536]uint8`, populated via a tight
loop, then thrown away.

The LUT is a pure function of `(MinValue, MaxValue, window.lower,
window.upper, window.scale, window.offset, Invert)`. For a typical
desktop session a user re-renders the same study many times with the
same defaults.

**Fix (S/M):** memoize a single LUT keyed on those scalars in a small
LRU (size ~4 should cover compare and side-by-side). The LUT itself can
live in a pooled `*[65536]uint8`. Saves the 65 536-iteration loop on
every render after the first.

---

### 7. Render LUT cast `uint16(value+0.5)` is unsafe for negative values; the guard is a runtime branch

**Where:** `backend/internal/render/render_plan.go:29` checks
`source.MinValue >= 0 && source.MaxValue <= 65535` and then runs the LUT
loop. The check is correct but adds a per-render branch on the float
range.

**Fix (S):** at decode time, record a flag on `imaging.SourceImage`
("fits-uint16") so the renderer dispatches statically. Trivial; reduces
one branch per render call site.

---

### 8. `decodeUNN_Monochrome` runs branchy `decodeStoredPixelValue` per pixel and tracks min/max sequentially

**Where:** `backend/internal/dicommeta/decode.go:787–851`. Every pixel
calls `scaledStoredPixelValue → decodeStoredPixelValue` which branches on
`bitsStored < 32` and `pixelRepresentation == 0` per pixel. Then min/max
is tracked with two compares per pixel.

**Fix (M):**

1. Specialize the inner loop on `(signed?, bitsStored, slope, intercept)`
   so the branches are hoisted out — at most 4 specializations
   (signed/unsigned × needs-rescale/no-rescale).
2. For unsigned `bitsStored == BitsAllocated` (the overwhelmingly common
   case for X-ray) the unpack is a no-op cast, and the loop reduces to
   `pixels[i] = float32(samples[i])*slope + intercept`. Vectorizable.
3. Track min/max in parallel chunks; merge at the end.

Combined gain: a noticeable chunk off `loadingStudy` for ≥16-bit sources.

---

### 9. `sourceImageFromImage` falls off the fast path for `Gray16` and the default branch

**Where:** `backend/internal/dicommeta/decode.go:741–784`. The `*image.Gray`
case correctly walks the `Pix` row buffer. **But** `*image.Gray16` calls
`Gray16At(x,y)` per pixel (interface dispatch + computed offset), and the
`default` branch calls `decoded.At(x,y).RGBA()` per pixel — interface
dispatch *and* a four-channel fetch — even when the format is something
plain like `*image.NRGBA`.

**Fix (S/M):** add type switches for `*image.Gray16`, `*image.RGBA`,
`*image.NRGBA`, `*image.YCbCr` that index `Pix` directly (mirroring the
existing `image.Gray` block). Keeps the slow `decoded.At` only for truly
exotic types.

---

### 10. Many `image.SourceImage.Pixels` are `[]float32` when `[]uint16` would do

**Where:** `backend/internal/imaging/model.go:25–34`. `SourceImage.Pixels`
is `[]float32` (4 bytes/pixel) regardless of source bit depth. For a 12-bit
dental DICOM (the project's primary input) the actual modality value fits
in `uint16` after rescale, and the full-range LUT path in
`render_plan.go:29` already assumes that.

For a 3000×1500 image that's **18 MB vs 9 MB** held in memory and walked by
every render. Multiplied by the decode cache (capacity 4, default 512 MB
budget), that's a real working-set hit.

**Fix (L):** introduce a `SourceImage.Storage` discriminator (`uint16` |
`float32`) and have render dispatch on it. Decode-side already knows the
type. The `[]float32` path stays for true CT/PET (negative values, large
ranges) where rescale produces fractional Hounsfield units.

---

### 11. Morphology + flood-fill allocate full-frame masks 5+ times per analyze

**Where:** `backend/internal/analysis/teeth.go:111–132`. `detectToothMask`
and `detectBoneLevelMask` together produce a sequence of `[]uint8`
masks each `width*height` bytes. Each `closeBinaryMask`,
`openBinaryMask`, `removeSmallMaskComponents`, `fillHolesBinaryMask`,
`thickenBoneLineMask`, and `innerOutlineMask` call returns a *new*
allocation:

```
detectToothMask:    featureTable→close→open→fillHoles→removeSmall  (≥5 allocs)
detectBoneLevelMask:  exemplar OR (featureTable→close→removeSmall) (≥3 allocs)
overlayMasks:       fillHoles→innerOutline→...                     (≥2 allocs)
```

For 2K×2K that's ~10 × 4MB = 40 MB of mask allocations *per analyze*.

**Fix (M):** route through a small pool of `[]uint8` mask buffers
(reuse `bufpool.GetUint8` once #1 is fixed) and let the morphology
operators write into a caller-provided destination instead of returning
a fresh slice.

---

### 12. `collectComponents` allocates per-component `[]int` lists

**Where:** `backend/internal/analysis/teeth.go:667–734`. For every
connected component a fresh `make([]int, 0, 512)` is heap-allocated
to hold pixel indices. `removeSmallMaskComponents` (teeth.go:638–650)
calls this just to learn area and rebuild a binary mask — never
needs the indices.

**Fix (S):** add `componentSizesAndBBoxes(mask, ...) []componentMeta`
that returns only `(area, minX, minY, maxX, maxY, seedHit)` per component
without the pixel-index slice. `removeSmallMaskComponents` can use the
labelmap directly to mask small ones in-place.

---

### 13. Job worker pool is hardcoded to 3, ignoring core count

**Where:** `backend/internal/jobs/service.go:77` (`maxConcurrentJobs = 3`).
The comment notes that "more than a handful starves the UI thread", which
makes sense for the desktop sidecar, but a 16-core workstation rendering a
batch is bottlenecked by this constant.

**Fix (S):** read from env (`XRAYVIEW_BACKEND_WORKERS`) with a sensible
default like `min(4, runtime.NumCPU()-1)`. Keeps the desktop default
gentle while letting power users open it up.

---

### 14. Polling loop in the frontend re-derives `Object.values(state.jobs).filter(...)` twice per tick

**Where:** `frontend/src/features/jobs/useJobs.ts:79–84` and 121–126.
On every poll cycle (every 200 ms when active jobs exist) the code
iterates the entire `jobs` map twice — once to build the request, once
to inspect the response. There is also a `selectPendingJobCount`
selector (`selectors.ts:40–49`) that does the same scan when subscribed.

For a session with dozens of completed jobs in the map, that's three
linear scans every 200 ms.

**Fix (S):** maintain `WorkbenchState.pendingJobIds: Set<string>` in the
store, updated incrementally on each job transition. `useJobs` reads it
directly; the selector becomes O(1). Removes the `Object.values()` calls.

---

### 15. SSE broadcast marshals JSON once per call, allocates the `data: …\n\n` frame each time

**Where:** `backend/internal/httpapi/sse.go:40–55`. `json.Marshal` on every
broadcast plus `[]byte(fmt.Sprintf("data: %s\n\n", data))` does two
allocations per fan-out, and the per-client send happens inside the
critical section of `h.mu`.

**Fix (S):** preallocate a single `bytes.Buffer` from a `sync.Pool`,
write `data: ` then the encoded snapshot then `\n\n`. The buffer can be
returned after all clients have copied. Lock contention is fine for the
expected scale, but the alloc reduction is free.

---

### 16. Decode cache holds raw `[]float32` pixel buffers up to 512 MB

**Where:** `backend/internal/studies/decode_cache.go:11–13`
(`defaultDecodeCacheMaxBytes = 512 * 1024 * 1024`). Eviction is by
LRU + byte budget, but the *unit cost* per study is doubled by the
choice in #10. Also: every cache hit takes the global `cache.mu` for
the duration of the LRU touch (decode_cache.go:69–75) — fine today,
worth knowing.

**Fix:** addressed by #10 (cut memory in half) plus #11 (stop
re-allocating mask scratch). No standalone change needed.

---

## P2 — Worth doing once the P0/P1 work lands

### 17. Palette application uses `copy(pixels[base:base+4], lookup[v][:])` per pixel

**Where:** `backend/internal/processing/palette.go:62–65`. For an RGBA
output, four-byte stores per pixel via slicing.

**Fix (S):** build palette as `[256]uint32` and store via
`*(*uint32)(unsafe.Pointer(&pixels[base])) = lut[v]`. Mirrors the existing
unsafe pattern in `processing/grayscale.go:118–157`. Unblocks easy
parallelization too.

**Status:** completed in `backend/internal/processing/palette.go` with
`BenchmarkApplyNamedPalette`. Validation on linux/amd64, i5-13400:
before ~2.10 ms/op (1496 MB/s), after ~0.91 ms/op (3459 MB/s), with
0 B/op and 0 allocs/op in both runs.

### 18. `processing/compare.go:CombineComparison` is a row-by-row sequential copy

**Where:** `backend/internal/processing/compare.go:40–72`. Two
side-by-side images, written sequentially.

**Fix (S):** parallelize over rows, or do `copy()` per scanline rather
than per-pixel writes for the Gray8 case (the loop currently iterates
column-by-column writing four bytes at a time).

**Status:** completed in `backend/internal/processing/compare.go` with
`BenchmarkCombineComparisonGrayRight` and
`BenchmarkCombineComparisonRGBARight`. Validation on linux/amd64,
i5-13400: gray-vs-gray before ~6.62 ms/op (3.80 GB/s), after
~0.76 ms/op (33.3 GB/s); gray-vs-RGBA before ~4.58 ms/op (5.49 GB/s),
after ~1.04 ms/op (24.1 GB/s).

### 19. `equalizeHistogramInPlace` is single-threaded

**Where:** `backend/internal/processing/grayscale.go:165–206`. Histogram
+ CDF + LUT, all single-pass. Per-thread histogram + reduction would
parallelize cleanly.

**Fix (S):** rarely the bottleneck (LUT fast path dominates), but trivial
once the patterns from #3 are in place.

**Status:** completed in `backend/internal/processing/grayscale.go` with
`BenchmarkEqualizeHistogramInPlace`. Validation on linux/amd64, i5-13400:
before ~1.48 ms/op (2.12 GB/s, 0 B/op, 0 allocs/op), after ~0.51 ms/op
(6.19 GB/s, ~17.8 KB/op, 22 allocs/op).

### 20. JSON request body pool can grow unboundedly

**Where:** `backend/internal/httpapi/router.go:21,320–351`.
`bodyPool` returns a `*bytes.Buffer`; if a one-off request had a 10 MB
body, that buffer's capacity stays in the pool. Over time the pool can
pin large buffers.

**Fix (S):** before `Put`, drop oversized buffers (e.g.,
`if buf.Cap() > 64*1024 { return }`).

**Status:** completed in `backend/internal/httpapi/router.go` with
`TestPutBodyBufferDropsOversizedBuffers` and
`BenchmarkDecodeJSONRequestSmallAfterOversizeBody`. Validation on
linux/amd64, i5-13400: before ~1.90 us/op after a 10 MiB body, after
~1.81 us/op, a ~1.05x speedup (about 90 ns/op faster), with 6171 B/op
and 19 allocs/op in both runs. Oversized request buffers above 64 KiB are
now dropped instead of retained in `bodyPool`.

### 21. `/preview` resolves symlinks on every request

**Where:** `backend/internal/httpapi/preview.go:53,63`. `EvalSymlinks`
on the cache root *and* the requested path on every GET. Browsers fetch
preview images many times during pan/zoom.

**Fix (S):** memoize the resolved root at handler-build time
(`previewCacheRoot` only changes if the cache config changes). Resolve
the target path with a single `EvalSymlinks` call and skip the second.

**Status:** completed in `backend/internal/httpapi/preview.go` with
`BenchmarkPreviewServesArtifact`. Validation on linux/amd64, i5-13400:
before ~9.91 us/op, after ~6.93 us/op, a ~1.43x time speedup (about
2.98 us/op faster, ~30.1% less time). Allocation cost also dropped from
~4913 B/op and 57 allocs/op to ~3175 B/op and 38 allocs/op because the
cache root is resolved once when the preview handler is built, or on the
first successful preview request if the cache directory is created after
router construction, instead of on every preview GET.

### 22. Job snapshots are deep-cloned even on read-only `Get`

**Where:** `backend/internal/jobs/registry.go:140–150` and 399–418.
Every `Get` clones `StudyID`, `Result`, and `Error.Details`. The
frontend polls `getJobs(jobIds)` every 200 ms; for N jobs that's N
clones every 200 ms even when nothing has changed.

**Fix (M):** treat snapshots as immutable inside the registry once
written (no in-place mutation of `*Result`/`*Error`); return them by
value with the inner pointers shared. The current "deep clone is a
correctness hedge" comment is fair, but a stricter immutability rule
removes the cost.

**Status:** completed in `backend/internal/jobs/registry.go` with
`BenchmarkRegistryGetCompletedSnapshot`. Validation on linux/amd64,
i5-13400: before ~59.2 ns/op, after ~22.6 ns/op, a ~2.62x time speedup
(about 36.6 ns/op faster, ~61.9% less time). Allocation cost dropped
from 48 B/op and 2 allocs/op to 0 B/op and 0 allocs/op because `Get`
now returns the registry snapshot by value and shares immutable nested
pointer fields.

### 23. `usePointerInteractions` reattaches pointer listeners on every move

**Where:** `frontend/src/features/viewer/usePointerInteractions.ts:95–190`.
The effect dependency list contains `interaction`, `transform`,
`imageSize`. Each pointermove triggers a state update → re-render →
listener teardown + re-attach.

**Fix (M):** stash the live state in refs, attach listeners *once* on
mount with a stable callback that reads from the refs. Pattern is the
same one already used for `draftLineRef` (line 86). Eliminates listener
churn during drag.

**Status:** completed in
`frontend/src/features/viewer/usePointerInteractions.ts` with
`frontend/scripts/validate-pointer-interactions.mjs`. Validation on
linux/amd64, i5-13400: before 400-move pan drags averaged ~11.000 ms
with ~1602 listener add/remove operations during the drag; after they
averaged ~9.529 ms with 0 listener operations during the drag. That is a
~1.15x time speedup (about 1.47 ms faster per 400 pointer moves, ~13.4%
less time) because the hook now keeps one stable pointer listener pair
and reads live interaction state from refs.

### 24. `displayedAnnotations` allocates a new array on every pointer move during edit

**Where:** `frontend/src/features/viewer/usePointerInteractions.ts:192–203`.
While dragging an endpoint, every `pointermove` builds a fresh
`{...annotations, lines: annotations.lines.map(...)}`.

**Fix (S):** clone only when the dragged annotation actually changed
position (compare to previous draft) or skip the structural clone and
have `AnnotationLayer` accept a `draftLineOverride: LineAnnotation` prop.

**Status:** completed in
`frontend/src/features/viewer/usePointerInteractions.ts` and
`frontend/src/features/annotations/AnnotationLayer.tsx` with
`frontend/scripts/validate-annotation-edit-drag.mjs`. Validation on
linux/amd64, i5-13400: before 5000-line edit drags averaged ~24.786 ms
for 400 pointer moves and produced ~401 fresh `lines` arrays; after they
averaged ~9.271 ms with 0 fresh `lines` arrays. That is a ~2.67x time
speedup (about 15.515 ms faster per 400 pointer moves, ~62.6% less
time) because endpoint dragging now passes the active line as a
`draftLineOverride` instead of cloning the annotation bundle on every
move.

### 25. `selectPendingJobCount` selector iterates jobs map on every change

**Where:** `frontend/src/app/store/selectors.ts:40–49`. Memoized on the
`jobs` reference, but every job update produces a new `jobs` reference,
so the count is recomputed on every transition.

**Fix:** subsumed by #14 — once `pendingJobIds` is a maintained set, the
selector returns its `.size`.

**Status:** completed by the maintained `WorkbenchState.pendingJobIds`
set in `frontend/src/app/store/workbenchStore.ts` and the O(1)
`selectPendingJobCount` implementation in
`frontend/src/app/store/selectors.ts`, with validation in
`frontend/scripts/validate-pending-job-count-selector.mjs`. Validation
on linux/amd64, i5-13400: before, the old `Object.values(jobs).filter`
selector averaged ~1641.087 ms for 1000 reads over a 10000-job map;
after, `pendingJobIds.size` averaged ~0.025 ms for the same 1000 reads.
That is a ~66104x time speedup, about 1641.062 ms faster per 1000
selector reads.

### 26. Hardcoded fast-poll interval of 200 ms

**Where:** `frontend/src/features/jobs/useJobs.ts:11` (`FAST_POLL_MS = 200`).
With SSE wired the path is suppressed, but in mock mode and in the
fallback path it pegs the renderer.

**Fix (S):** start at 400–500 ms unless a recent state transition was
seen; keep the existing exponential backoff. SSE is the real solution
when available.

**Status:** completed by starting fallback active polling at 500 ms,
retaining the 200 ms cadence only for recent state transitions and
near-complete jobs, and keeping the existing exponential backoff behavior.
Validation in `frontend/scripts/validate-fast-poll-cadence.mjs` on
linux/amd64, i5-13400: before, a 10000 ms fallback window with 20000
pending jobs averaged ~114.346 ms of simulated polling work across 51
polls; after, the 500 ms active cadence averaged ~16.368 ms across 7
polls. That is a ~6.99x time speedup, about 97.978 ms faster per
simulated fallback window, with polling reduced 86.3%.

### 27. `boxBlurGray` uses `clampInt` per access in the prologue

**Where:** `backend/internal/analysis/teeth.go:746–782`. Separable, two
passes (good!), but `clampInt(x, 0, width-1)` is called inside the
pixel loop for the initial window setup.

**Fix (S):** split the loop into a left-border, middle, right-border
section and skip the clamps in the middle. Same for vertical pass. Modest
speedup.

**Status:** completed by splitting each horizontal row and vertical column
pass into clamped border sections plus an unclamped middle section, preserving
the previous edge-replication semantics. Validation in
`BenchmarkBoxBlurGray` on linux/amd64, i5-13400: before, a 2048x1536
radius-21 blur averaged ~22.331 ms/op; after, it averaged ~21.851 ms/op.
That is a ~1.02x time speedup, about 0.480 ms faster per blur pass.

### 28. `runtime.getJobs` returns plain array; per-batch `map(...)` re-normalizes

**Where:** `frontend/src/lib/runtime.ts:169–170`. Allocations are
proportional to batch size on every poll.

**Fix:** trivial; defer until #14 lands and the polling cadence is
known.

**Status:** completed by adding `RuntimeAdapter.forEachJob`, which keeps
the existing backend batch request but lets the polling loop consume
normalized snapshots one at a time without allocating a second normalized
batch array. Validation in
`frontend/scripts/validate-runtime-get-jobs-normalization.mjs` on
linux/amd64, i5-13400: before, 20,000 jobs over 100 batches averaged
~61.322 ms/sample with `map(...)` normalization; after, visitor
normalization averaged ~41.081 ms/sample. That is a ~1.49x time speedup,
about 20.241 ms faster per 100 polled batches.

### 29. `compositeOverlayCoverage` does `math.Round` per channel per pixel

**Where:** `backend/internal/analysis/teeth.go:535–555`. For every
covered pixel: three `math.Round` calls.

**Fix (S):** integer math via `(current*(255-q) + target*q + 127)/255`
where `q = uint(value*255)`. Standard 8-bit alpha blend.

---

## P3 — Nits and code-organization wins

### 30. `studies/registry.go:evictOldestLocked` is misnamed

**Where:** `backend/internal/studies/registry.go:73–91`. The comment
already admits the function picks an arbitrary entry from `range`. Either
make it actually LRU (cheap) or rename to `evictArbitraryLocked`.

### 31. `featureTable` lookups via `sort.Search` per pixel

**Where:** `backend/internal/analysis/feature_table_model.go:32–44`,
`bone_feature_table_model.go:48–60`. Binary search of ~thousands per
lookup × millions of pixels.

**Fix (M):** replace the sorted parallel arrays with a `map[uint32]uint8`
(or a perfect-hash bitset since bins are small). Map lookups for typical
sizes will beat `sort.Search` by ~3–5×, and parallelize cleanly.

### 32. `getJobs` always sends de-duped IDs but `getJob` does not

**Where:** `frontend/src/lib/desktopBackend.ts:38–40`. `getJobs` does
`new Set(jobIds)`. Cosmetic — singleton `getJob` is one ID.

### 33. Per-ProcessingTab re-renders on every store change

**Where:** `frontend/src/components/processing/ProcessingTab.tsx`.
Subscribes to `selectActiveStudy` and `selectManifest`; the active study
changes on every job update. With many sub-controls the tree is
medium-sized but un-memoized.

**Fix (S):** memoize `processingUi`, `request`, and `processedPreviewUrl`
behind reasonable equality. Low priority — modern React is fast enough.

### 34. SSE clients have a fixed-size 16-frame buffer

**Where:** `backend/internal/httpapi/sse.go:24`. Frames are silently
dropped under buffer pressure. For the desktop case this is fine; for
multi-window debugging it can be confusing.

**Fix:** add a Prometheus-style counter for dropped frames and wire it to
the existing logger so a slow consumer is visible. (Operational, not
performance.)

### 35. Catalog persistence reads-then-writes the whole file on every open

**Where:** `backend/internal/persistence/catalog.go:92–113`
(`RecordOpenedStudy`). Read, dedupe, prepend, truncate to 10, write
again. For the 10-entry cap it doesn't matter, but as a pattern it
regenerates the file each time. If it ever grows, switch to append-only
with periodic compaction.

### 36. `desktop/sidecar.go:newSidecarTransport` caps at 2 idle conns

**Where:** `desktop/sidecar.go:46–48` (`sidecarMaxIdleConns = 2`). Fine
for desktop, but tests that hammer the sidecar through HTTP will
re-handshake constantly. If sidecar mode ever moves under heavier
load (e.g., headless CLI batch), bump it.

---

## Where to start

If you can land only one thing, do **#1 (bufpool wiring)** — it's a
small, contained change that touches the GC profile of every render and
process. It's also a prerequisite for #11 (mask buffer reuse).

For the analyze pipeline specifically, the highest-impact ordered
sequence is:

1. **#1** — wire `bufpool.Put`.
2. **#2** — separable morphology.
3. **#3** — parallelize learned-tooth and bone scoring.
4. **#4** — fix the contour-doubling and stop allocating per smoothing
   iteration.
5. **#11** — pool mask scratch buffers.

These five together should turn Analyze from a noticeably-slow operation
into a near-interactive one on multi-core machines.

For everything else, **#10 (uint16 source storage)** is the single
biggest memory win and unlocks larger images / longer cache retention
without raising the byte budget.

---

## Suggested validation

The repo already has benchmarks in `backend/internal/jobs/bench_test.go`
(`BenchmarkDecodeStudy`, `BenchmarkRenderSourceImage`,
`BenchmarkProcessSourceImage`) and
`backend/internal/render/preview_jpeg_test.go`
(`BenchmarkEncodePreviewJPEG`). Add:

- `BenchmarkAnalyzeStudy` in `backend/internal/analysis/teeth_test.go`
  using the dental fixture under `images/`.
- `BenchmarkRenderSourceImage_Cached` to verify #6's LUT memoization.
- `b.ReportAllocs()` everywhere, and gate PRs on
  `go test -bench=. -benchmem -count=5` not regressing.

For the frontend, the existing `recordJobSubmit` /
`logCompletedJobVisibleTiming` instrumentation
(`frontend/src/features/jobs/benchmarks.ts`) already covers the user-
visible job timings. Worth pointing the polling-cadence change (#26) at
those numbers.
