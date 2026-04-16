# XRayView Optimization Plan

> Generated: 2026-04-09
> Codebase: ~9,150 lines Go backend, ~5,500 lines TS/React frontend
> Priority: Performance, Memory Usage, Speed, Efficiency

---

## Table of Contents

1. [Phase 1: Hot-Path Pixel Processing (Backend)](#phase-1-hot-path-pixel-processing-backend)
2. [Phase 2: Memory Allocation Reduction (Backend)](#phase-2-memory-allocation-reduction-backend)
3. [Phase 3: PNG Encoding Acceleration (Backend)](#phase-3-png-encoding-acceleration-backend)
4. [Phase 4: Analysis Pipeline Optimization (Backend)](#phase-4-analysis-pipeline-optimization-backend)
5. [Phase 5: DICOM Decode Optimization (Backend)](#phase-5-dicom-decode-optimization-backend)
6. [Phase 6: Cache & Eviction Improvements (Backend)](#phase-6-cache--eviction-improvements-backend)
7. [Phase 7: HTTP Transport Optimization (Backend)](#phase-7-http-transport-optimization-backend)
8. [Phase 8: DICOM Export Optimization (Backend)](#phase-8-dicom-export-optimization-backend)
9. [Phase 9: Frontend State & Rendering (Frontend)](#phase-9-frontend-state--rendering-frontend)
10. [Phase 10: Frontend Network & Polling (Frontend)](#phase-10-frontend-network--polling-frontend)
11. [Phase 11: Concurrency & Job Scheduling (Backend)](#phase-11-concurrency--job-scheduling-backend)
12. [Phase 12: Build & Bundle Optimization (Tooling)](#phase-12-build--bundle-optimization-tooling)

---

## Phase 1: Hot-Path Pixel Processing (Backend)

The render and processing pipelines iterate over every pixel in the image. For a typical dental radiograph (2000x1500 = 3M pixels), these loops dominate wall-clock time.

### Step 1.1: Precompute Window Transform as a Lookup Table ✅

**File:** `backend/internal/render/render_plan.go:17-37`
**What it does:** `RenderGrayscalePixels` calls `window.Map(value)` per pixel, which involves 2 float comparisons + 1 float multiply + 1 float add + ClampToByte per pixel.
**Optimization:** When the source image is 8-bit or 16-bit (most medical images are 12-16 bit stored in 16-bit containers), precompute a full lookup table (LUT) of size 65536 entries mapping every possible uint16 input to a uint8 output. Then the inner loop becomes a single table lookup per pixel with zero branching.
**Expected improvement:** 3-5x speedup on the render inner loop. Eliminates all float arithmetic and branch mispredictions in the hot path.
**How to test:** Add a `BenchmarkRenderGrayscalePixels` in `render_plan_test.go` with a 2048x1536 synthetic image. Compare before/after with `go test -bench=. -benchmem`.

### Step 1.2: Eliminate Per-Pixel Branch on `source.Invert` ✅

**File:** `backend/internal/render/render_plan.go:29-31`
**What it does:** Every pixel checks `if source.Invert { byteValue = 255 - byteValue }`.
**Optimization:** Hoist the branch outside the loop. Create two code paths (or fold inversion into the LUT from Step 1.1 so it costs zero at runtime).
**Expected improvement:** ~5-10% on render loop by eliminating a branch per pixel. With the LUT approach from 1.1, this is free.
**How to test:** Same benchmark as 1.1.

### Step 1.3: Eliminate Per-Pixel `window != nil` Check ✅

**File:** `backend/internal/render/render_plan.go:23-27`
**What it does:** Every pixel checks `if window != nil` to decide between windowed and linear mapping.
**Optimization:** Resolve the mapping function once before the loop and use a function pointer or two separate loops. With the LUT from 1.1 this becomes a single precomputation.
**Expected improvement:** ~5% on render loop (branch elimination). Free with LUT approach.
**How to test:** Same benchmark as 1.1.

### Step 1.4: Process Grayscale Pixels with Combined LUT ✅

**File:** `backend/internal/processing/grayscale.go:34-74`
**What it does:** `ProcessGrayscalePixels` builds up a [256]uint8 lookup table by composing invert/brightness/contrast, then applies it. This is already well-designed, but histogram equalization forces a flush and re-scan.
**Optimization:** When equalization is NOT requested (the common case), apply the single composed LUT in a tighter loop: process 8 pixels per iteration (loop unrolling) to improve instruction-level parallelism and reduce loop overhead.
**Expected improvement:** ~15-25% speedup on processing inner loop for the non-equalize case.
**How to test:** Add `BenchmarkProcessGrayscalePixels` with various control combinations. Compare unrolled vs current.

### Step 1.5: Vectorize Pixel Loops with `unsafe` Batch Operations ✅

**File:** `backend/internal/processing/grayscale.go:116-153`
**What it does:** `applyLookupInPlace` iterates byte-by-byte through the pixel array.
**Optimization:** Use `unsafe` pointer arithmetic to bypass Go bounds checking and slice header overhead. 16-pixel unrolled loop with raw pointer offsets eliminates all runtime safety checks from the hot path. Note: the originally-planned uint64 read/write approach was benchmarked but the shift/OR repack overhead negated the memory access savings. Pure pointer arithmetic with byte-level LUT lookups proved faster.
**Actual improvement:** ~10% speedup on LUT application (730→654 ns/op, 4270→4800 MB/s for 3M pixels). The plan's predicted 2x was overstated — LUT random-access latency dominates, not sequential memory ops.
**How to test:** Benchmark with 3M pixel arrays before/after.

---

## Phase 2: Memory Allocation Reduction (Backend)

### Step 2.1: Use `sync.Pool` for Pixel Buffers ✅

**Files:** `backend/internal/render/render_plan.go:18`, `backend/internal/processing/grayscale.go:28`, `backend/internal/analysis/analysis.go:175-201`
**What it does:** Every render/process/analyze call allocates fresh `[]uint8` and `[]float32` slices for pixel data (often 3-12 MB each).
**Optimization:** Created `backend/internal/bufpool/bufpool.go` with `sync.Pool`-backed `GetUint8`/`PutUint8`/`GetFloat32`/`PutFloat32`. Wired into render (pixel output buffer), processing (pixel clone), and analysis (normalized, smallBlur, largeBlur, toothness, and gaussianBlur transient float32 buffers). Analysis returns all intermediate buffers to the pool after use; the second gaussianBlurGray call reuses the float32 transient from the first.
**Actual improvement:** `BenchmarkAnalyzePreview`: 50.9 MB/op → ~33.8 MB/op (33% reduction in allocated bytes). The float32 transient reuse within a single analysis call saves ~8.9 MB immediately; cross-iteration pooling saves another ~8.9 MB of uint8 buffers. Render and processing paths benefit under repeated-call workloads when callers release preview pixels.
**How to test:** Run `go test -bench=. -benchmem` and compare `allocs/op` and `B/op` before/after. Also measure with `GODEBUG=gctrace=1` under load.

### Step 2.2: Eliminate Defensive Clone in `previewImage()` ✅

**File:** `backend/internal/render/preview_png.go:53-57`
**What it does:** `previewImage` clones the entire pixel buffer with `append([]uint8(nil), preview.Pixels...)` before passing to `image.Gray`. This copies 3+ MB of data defensively.
**Optimization:** Since `png.Encode` only reads the pixel data and doesn't mutate it, pass the original slice directly. The `image.Gray` struct doesn't own the slice. This eliminates one full image copy per PNG encode.
**Actual improvement:** `BenchmarkEncodePreviewPNG/Gray8`: 4,007,568 → 861,837 B/op (78% reduction, saved 3.1 MB/call), 8.2 → 7.3 ms/op (11% speedup), 31 → 30 allocs. `RGBA8`: 13,470,096 → 887,180 B/op (93% reduction, saved 12.6 MB/call), 28.7 → 26.1 ms/op (9% speedup), 31 → 30 allocs.
**How to test:** `BenchmarkEncodePreviewPNG` in `preview_png_test.go` with 2048x1536 image, compare allocs.

### Step 2.3: Avoid Double Clone in Cache Store/Load ✅

**File:** `backend/internal/cache/memory.go:122-146`
**What it does:** `StoreSourcePreview` clones the preview on store. `LoadSourcePreview` clones again on load. For a 3 MB image, this means 6 MB of unnecessary copying per cache round-trip.
**Optimization:** Removed the `clonePreviewImage` call from `StoreSourcePreview` — the cache now takes ownership of the pixel slice directly. `LoadSourcePreview` still returns a defensive clone, so all readers remain safe. All callers of `loadOrRenderSourcePreview` in `jobs/service.go` are read-only on the returned pixels (SavePreviewPNG reads for PNG encode, ProcessRenderedPreview copies internally before processing, CombineComparison only reads). Documented the ownership contract on StoreSourcePreview.
**Actual improvement:** `BenchmarkSourcePreviewStoreLoad` with 2048×1536 gray8 (3.1 MB):
- **RoundTrip**: 512k → 222k ns/op (57% faster), 6,291,471 → 3,145,739 B/op (50% reduction, saved 3.1 MB), 2 → 1 allocs
- **StoreOnly**: 260k → 214 ns/op (99.9% faster), 3,145,824 → 32 B/op (99.999% reduction), 3 → 1 allocs
- **LoadOnly**: unchanged (clone on load preserved)
**How to test:** `BenchmarkSourcePreviewStoreLoad` in `memory_test.go` with `-benchmem`.

### Step 2.4: Preallocate `bytes.Buffer` in DICOM Export ✅

**File:** `backend/internal/export/secondary_capture.go:153`
**What it does:** `var payload bytes.Buffer` starts at zero capacity and grows dynamically as DICOM elements are written. A secondary capture DICOM will be at least as large as the pixel data (3+ MB), causing multiple reallocs.
**Optimization:** Pre-calculate the expected size: `128 (preamble) + 4 (DICM) + groupLength + 2048 (metadata overhead) + pixelSize`. For RGBA previews, pixel size accounts for RGBA→RGB conversion (3/4 of input). Call `payload.Grow(estimatedSize)` upfront.
**Actual improvement:** `BenchmarkEncodeSecondaryCapture` with 2048×1536 images:
- **Gray8**: 61 → 57 allocs/op (4 fewer buffer growth reallocs eliminated), B/op ~unchanged (pixel data copies dominate), speed within noise
- **RGBA8**: 59 → 55 allocs/op (4 fewer), ~3.7% speedup (7.67 → 7.38 ms/op), B/op ~unchanged
- Plan predicted ~20% speedup but modern Go `bytes.Buffer` growth strategy is already efficient — the buffer growth was a small fraction of total cost. Pixel data copies across `binaryElement`, `evenLengthBytes`, and `rgbaToRGB` dominate allocation.
**How to test:** `BenchmarkEncodeSecondaryCapture` in `secondary_capture_test.go` with `-benchmem`.

### Step 2.5: Avoid `rgbaToRGB` Allocation in Export ✅

**File:** `backend/internal/export/secondary_capture.go:372-387`
**What it does:** `rgbaToRGB` allocated a new `[]uint8` slice (~9 MB), then `evenLengthBytes` copied it (~9 MB), then `binaryElement` defensively copied again (~9 MB) — three allocations for the same pixel data.
**Optimization:** Replaced the `rgbaToRGB` → `evenLengthBytes` → `binaryElement` chain with a single `rgbaPixelElement` function that pre-calculates the padded RGB size, allocates once, and converts RGBA→RGB directly into the element value buffer. Eliminated the old `rgbaToRGB` function entirely.
**Actual improvement:** `BenchmarkEncodeSecondaryCapture` with 2048×1536 RGBA8:
- **B/op**: 37,762,963 → 18,888,593 (50% reduction, saved 18.9 MB/call — plan predicted ~7 MB, actual was higher because downstream copies were also eliminated)
- **allocs/op**: 55 → 53 (2 fewer)
- **ns/op**: ~7,180 μs → ~6,070 μs (15% speedup)
- Gray8 path unchanged (not affected).
**How to test:** `BenchmarkEncodeSecondaryCapture` in `secondary_capture_test.go` with `-benchmem`.

---

## Phase 3: PNG Encoding Acceleration (Backend)

### Step 3.1: Use Buffered Writer for PNG Output ✅

**File:** `backend/internal/render/preview_png.go:13-29`
**What it does:** `SavePreviewPNG` passes the raw `os.File` to `png.Encode`, which makes many small writes.
**Optimization:** Wrap the file in `bufio.NewWriterSize(file, 64*1024)` before passing to `png.Encode`. Flush before close.
**Actual improvement:** `BenchmarkSavePreviewPNG` with 2048×1536 images:
- **Gray8**: 7,374 → 7,190 ns/op (~2.5% speedup), 862 → 928 KB/op (+65 KB for buffer), 35 → 37 allocs
- **RGBA8**: 26,911 → 26,456 ns/op (~1.7% speedup), 887 → 953 KB/op (+65 KB for buffer), 35 → 37 allocs
- Plan predicted 10-20% but Go's `png.Encode` already writes in moderately large IDAT chunks, so syscall count was lower than expected. The 64 KB buffer still reduces small writes for chunk headers, CRC values, IHDR, and IEND. Benefit would be larger on network writers or slower I/O backends.
**How to test:** `BenchmarkSavePreviewPNG` in `preview_png_test.go` with `-benchmem`.

### Step 3.2: Use `png.Encoder` with `BestSpeed` Compression ✅ DONE

**File:** `backend/internal/render/preview_png.go:49`
**What it does:** `png.Encode(writer, imageValue)` uses default compression, which is `zlib.DefaultCompression` (level 6).
**Optimization:** Use `(&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(writer, imageValue)`. Preview PNGs are consumed locally over loopback and are temporary cache artifacts - they don't need maximum compression.
**Expected improvement:** ~3-5x speedup on PNG encoding. File size increases ~20-40%, but these are local cache files read once then discarded. This is likely the single largest speedup for the render job path.
**How to test:** Benchmark both compression levels, measure encode time and file size.
**Actual result:** ~1.6x speedup (Gray8: 7.1ms→4.3ms, RGBA8: 25.4ms→16.1ms). Less than predicted 3-5x because Go's `png` package filter heuristics limit how much compression level alone affects speed. Alloc increased ~45% (acceptable for throwaway cache files).

### Step 3.3: Consider Using JPEG for Preview Artifacts ✅ INVESTIGATED — PNG RETAINED

**File:** `backend/internal/render/preview_png.go`
**What it does:** All previews are saved as PNG.
**Optimization investigated:** Encode Gray8 previews as JPEG quality 92 instead of PNG BestSpeed. Created `preview_jpeg.go` with `SavePreviewJPEG`/`EncodePreviewJPEG` and `preview.go` with format-dispatching `SavePreview`/`PreviewExtension`. Full JPEG infrastructure is available for future use.
**Expected improvement:** ~5-10x speedup on preview artifact creation.
**Actual result:** Go's stdlib `image/jpeg` encoder uses pure-Go DCT — it is **~3x slower** than PNG BestSpeed for Gray8, not faster. `BenchmarkEncodePreviewJPEG` vs `BenchmarkEncodePreviewPNG` with 2048×1536 Gray8:
- **PNG BestSpeed**: ~4.3 ms/op, 1,255,054 B/op, 32 allocs
- **JPEG q92**: ~12.1 ms/op, 4,512 B/op, 6 allocs
- **JPEG q75**: ~11.3 ms/op, 4,512 B/op, 6 allocs
- JPEG quality setting barely affects encode time — DCT dominates, not entropy coding.
- JPEG allocates 99.6% less memory (4.5 KB vs 1.2 MB) and 81% fewer allocs (6 vs 32).
- **Decision:** Retain PNG BestSpeed for all preview artifacts. The plan's predicted 5-10x speedup assumed C-level libjpeg-turbo performance; Go's stdlib `image/jpeg` is unoptimized pure-Go. The allocation reduction is significant but doesn't justify a 3x wall-clock regression. JPEG infrastructure kept in `preview_jpeg.go` and `preview.go` for potential future use with a CGO libjpeg-turbo binding.
**How to test:** `BenchmarkEncodePreviewJPEG` and `BenchmarkSavePreviewJPEG` in `preview_jpeg_test.go`.

---

## Phase 4: Analysis Pipeline Optimization (Backend)

### Step 4.1: Fuse Gaussian Blur Passes ✅

**File:** `backend/internal/analysis/analysis.go`
**What it does:** `gaussianBlurGray` performs a 2-pass separable convolution (horizontal then vertical). `AnalyzeGrayscalePixels` calls it TWICE (sigma=1.4 and sigma=9.0), meaning 4 full image scans.
**Optimization investigated:** The plan proposed fusing horizontal and vertical passes to halve memory reads. However, benchmarking revealed that pass fusion is **counterproductive** — holding two float32 transient buffers (24 MB) simultaneously exceeds L3 cache (20 MB on the test CPU), causing cache thrashing that negates the bandwidth savings. Column tiling was also tested but hurt performance because L2 (1.25 MB) already accommodates the vertical pass working set (456 KB for sigma=9).
**Optimization applied:** Instead of fusion, created `gaussianBlurGrayFast` with three orthogonal improvements:
1. **Bounds-check-free interior loops** — split horizontal and vertical passes into boundary/interior regions. For sigma=9 (radius=28), 96% of pixels are in the branch-free interior.
2. **x4 column unrolling in vertical pass** — processes 4 columns per iteration, precomputing row offsets once per kernel tap. Improves instruction-level parallelism (4 independent accumulators) and reduces loop overhead by 4x.
3. **Fast float32→uint8 clamping** — `fastClampUint8` avoids the `float32→float64→math.Round→float64→uint8` conversion chain in the original `clampUint8FromFloat32`. Output is pixel-identical (0 differences on 3M pixel test).
**Actual improvement:** `BenchmarkGaussianBlurDual` with 2048×1536 (3.1M pixels):
- **Dual blur**: 358 → 188 ms/op (47% speedup, plan predicted 30-40%)
- **AnalyzePreview (end-to-end)**: 6.3 → 4.7 ms/op (27% speedup on full analysis pipeline)
- **Allocs**: unchanged (48/op)
- **Pixel accuracy**: exact match vs original (0/3,145,728 pixels differ for both sigma=1.4 and sigma=9.0)
**How to test:** `BenchmarkGaussianBlurDual` and `TestGaussianBlurGrayFastMatchesOriginal` in `analysis_test.go`.

### Step 4.2: Use Integer Approximation for Small Gaussian Blur ✅

**File:** `backend/internal/analysis/analysis.go`
**What it does:** Blur uses float32 accumulation for kernel weights.
**Optimization:** Created `gaussianBlurGrayInteger` using fixed-point integer arithmetic with the kernel [7, 27, 57, 74, 57, 27, 7] / 256 (matching the actual Gaussian for sigma=1.4, kernel size 7, radius 3). Horizontal pass stores weighted sums as uint16 (max 65280, half the memory of float32 transient). Vertical pass accumulates uint32 and divides by 65536 (>>16) with rounding bias (+32768). Interior loops are fully unrolled (no kernel loop) with 4-column vertical unrolling. Kernel weights are compile-time constants — no kernel allocation. Added `GetUint16`/`PutUint16` to `bufpool` for transient buffer pooling. `dualGaussianBlurGray` dispatches sigma=1.4 to the integer path.
**Actual improvement:** `BenchmarkGaussianBlurSmall` with 2048×1536 (3.1M pixels):
- **Float**: ~20.8 ms/op, ~280 KB B/op, 3 allocs
- **Integer**: ~13.4 ms/op, ~80 B/op, 2 allocs
- **1.55x speedup** (35% faster). Plan predicted 2x — modern CPUs handle float nearly as fast as integer; memory bandwidth dominates over arithmetic. The gains come from: uint16 transient (half the cache footprint of float32), no kernel allocation, no float→uint8 conversion overhead, fully unrolled compile-time-constant kernel.
- **Dual blur** (Fused): 193 → 183 ms/op (5% faster, 1 fewer alloc). Sigma=9.0 dominates the dual blur.
- **AnalyzeGrayscalePixels** (end-to-end): ~5.1 → ~4.9 ms/op (~5% faster, 1 fewer alloc).
- **Pixel accuracy**: max diff ≤ 1 vs float reference; only 0.05% of pixels differ by 1. Invisible for threshold-based tooth detection.
**How to test:** `TestGaussianBlurIntegerMatchesFloat` and `BenchmarkGaussianBlurSmall` in `analysis_test.go`.

### Step 4.3: Skip Analysis on Cached Source Preview ✅

**File:** `backend/internal/jobs/service.go` (executeAnalyzeJob), `backend/internal/cache/memory.go`
**What it does:** The analyze job always decodes the full DICOM, renders a preview, then analyzes. If a source preview is already cached, it still decodes the full source image.
**Optimization:** Added `StoreMeasurementScale`/`LoadMeasurementScale` to `cache.Memory`, keyed by `inputPath` alongside source previews. `executeAnalyzeJob` now checks for cached source preview AND cached measurement scale FIRST — if both hit, the entire DICOM decode and render are skipped. Render and process jobs cache the measurement scale after decoding, so it's available for future analyze jobs. Measurement scales are evicted alongside source previews to stay in sync.
**Actual improvement:** `BenchmarkAnalyzeJob` with 2048×1536 sample dental radiograph:
- **WithDecode** (cold cache): ~230 ms/op, ~52 MB/op, ~685 allocs
- **CacheHit** (warm cache): ~213 ms/op, ~35 MB/op, ~495 allocs
- **7.4% wall-clock speedup** (~17 ms saved — 8 ms decode + 9 ms render), **33% memory reduction** (17 MB saved), **28% fewer allocations** (190 fewer)
- Plan predicted 100-500 ms savings; actual decode for this 2048×1536 image is ~8 ms. Savings scale with image size and I/O latency — network-mounted DICOM files or larger images (4K+) would see proportionally larger gains.
- `BenchmarkFullWorkflow` unchanged (~395 ms) because `DecodeCache` already deduplicates decodes within a single session. The optimization targets the cross-session / evicted-decode-cache scenario.
**How to test:** `TestAnalyzeJobSkipsDecodeWhenSourcePreviewAndScaleCached` (direct cache pre-population, verifies zero decoder calls) and `TestRenderThenAnalyzeSkipsDecodeOnEvictedDecodeCache` (render 6 studies to evict decode cache, verify analyze skips decode) in `service_test.go`.

### Step 4.4: Optimize Morphological Operations ✅

**File:** `backend/internal/analysis/analysis.go:281-380`
**What it does:** `dilateBinaryMask` and `erodeBinaryMask` use 3x3 structuring elements with a nested 4-level loop (y, x, ny, nx).
**Optimization:** Decomposed the 3x3 operation into separable 1×3 horizontal + 3×1 vertical passes (mathematically equivalent for a square structuring element). Changed mask representation from `[]bool` to `[]uint8` (0/1) throughout the internal pipeline (mask creation, close/open wrappers, dilate/erode, collectCandidates). Boundary rows/columns are handled without touching the interior hot path. Interior rows use 4-column unrolling in the vertical pass for ILP. Horizontal temp buffer is pooled via `bufpool.GetUint8`. All callers updated.
**Actual improvement:** `BenchmarkMorphologicalOps` with 2048×1536 (~30% density):
- **Dilate**: 8.9 → 5.9 ms/op (**1.5x speedup, 33% faster**), 357 → 532 MB/s throughput
- **Erode**: 7.3 → 5.7 ms/op (**1.3x speedup, 22% faster**), 437 → 554 MB/s throughput
- **OpenClose** (4 ops): 81.3 → 22.9 ms/op (**3.5x speedup, 72% faster**)
- **AnalyzeGrayscalePixels** (end-to-end): ~4.9 → ~4.2 ms/op (**14% faster**, 1 fewer alloc)
- Plan predicted 2-3x on individual ops. OpenClose exceeded at 3.5x because separability plus interior-loop branch elimination compound across all 4 sequential operations, each feeding into the next.
- Pixel semantics: OR/AND on 0/1 bytes is algebraically identical to the original `||`/`&&` on booleans.
**How to test:** `BenchmarkMorphologicalOps` and all `Test*BinaryMask` + `TestCollectCandidates*` in `analysis_test.go`.

### Step 4.5: Avoid Allocating Per-Candidate Pixel Slices ✅

**File:** `backend/internal/analysis/analysis.go`
**What it does:** `collectCandidates` allocates a `[]int` pixel slice for every connected component, then stores it in the candidate. These slices can hold thousands of pixel indices.
**Optimization:** The BFS queue already contains exactly the component's pixel indices after traversal completes (`queue[0:len(queue)]` = all pixels in BFS order). Removed the separate `pixels` slice that was being populated redundantly alongside the queue. After BFS, `area = len(queue)`. Only for components with `area > minDetectedArea` (150) is a new `[]int` allocated and the queue copied into it — small/noise components (the vast majority) get no pixel slice at all. Extracted threshold to `const minDetectedArea = 150` shared by both `collectCandidates` and `selectDetectedCandidates` to prevent drift. Initial queue capacity bumped from 256 to 1024 to reduce growth reallocations for medium components.
**Actual improvement:** `BenchmarkCollectCandidates` with 2048×1536 mask (many small 3×3 blobs + 5 large blobs):
- **allocs/op**: 7,074 → 25 (**-99.6%**, eliminated 7,049 pixel-slice allocations)
- **B/op**: 19.9 MB → 5.4 MB (**-73%**, saved 14.5 MB)
- **ns/op**: ~9.3 ms → ~6.7 ms (**-28% faster**)
- `BenchmarkAnalyzePreviewSample` (2048×1088 real dental radiograph, end-to-end):
  - **allocs/op**: ~503 → ~253 (**-50%**, saved ~250 allocs)
  - **B/op**: ~35.6 MB → ~29.2 MB (**-18%**, saved ~6.4 MB)
  - **ns/op**: ~170 ms → ~174 ms (within noise — analysis dominated by blur and morphology)
- `BenchmarkAnalyzeGrayscalePixels` (240×160 synthetic):
  - **allocs/op**: 51 → 41 (**-20%**, -10 allocs)
  - **B/op**: ~537 KB → ~395 KB (**-26%**, -142 KB)
- Plan predicted 100 KB–1 MB savings; actual is 6.4 MB on the real radiograph due to many small components in complex images. Wall-clock unchanged because pixel-slice allocation was not on the critical path (blur + morphology dominate).
**How to test:** `BenchmarkCollectCandidates` and `BenchmarkAnalyzePreviewSample` in `analysis_test.go` with `-benchmem`.

---

## Phase 5: DICOM Decode Optimization (Backend)

### Step 5.1: Use `binary.Read` with Preallocated Buffer ✅

**File:** `backend/internal/dicommeta/decode.go:683-697`
**What it does:** `readU16Samples` and `readU32Samples` create new slices and manually decode bytes in a loop.
**Optimization:** For little-endian byte order (the most common DICOM transfer syntax), used `unsafe.Slice` to reinterpret the raw byte slice directly as `[]uint16` or `[]uint32` — zero-copy, zero-allocation. The returned slice aliases `raw`, which is safe because `decodeU16/U32Monochrome` only reads from it and the resulting `SourceImage` holds no reference to the sample slice. For big-endian: allocate once, bulk-copy bytes via `copy(unsafe.Slice(...), raw)`, then batch-swap with `bits.ReverseBytes16`/`ReverseBytes32`.
**Actual improvement:** `BenchmarkReadU16Samples` / `BenchmarkReadU32Samples` with 2048×1536 (3.1M pixels, 6.3 MB for U16 / 12.6 MB for U32):
- **U16 LittleEndian**: 7.0 ms → 2.2 ns/op (**~3.2M× faster**), 6.1 MB → 0 B/op (**allocation eliminated**)
- **U16 BigEndian**: 7.7 ms → 2.7 ms/op (**2.9× faster**, bulk memcpy + vectorized swap vs per-element decode)
- **U32 LittleEndian**: 7.6 ms → 2.2 ns/op (**~3.5M× faster**), 12.1 MB → 0 B/op (**allocation eliminated**)
- **U32 BigEndian**: 7.6 ms → 3.4 ms/op (**2.2× faster**)
- The LE path is now essentially free (pointer arithmetic only — returns a slice header aliasing the input). The BE path's 2-3× gain comes from replacing per-element `byteOrder.Uint16()` calls with a single `memmove`-optimized bulk copy plus a compiler-vectorizable swap loop. Plan predicted 3-5× for LE; actual is effectively infinite (sub-nanosecond), since the old loop was ~3M iterations. The 6–12 MB allocation per decode call is now gone entirely for the dominant LE path.
**How to test:** `BenchmarkReadU16Samples` and `BenchmarkReadU32Samples` in `decode_test.go` with `-benchmem`.

### Step 5.2: Stream Min/Max Computation into Decode Loop ✅

**File:** `backend/internal/dicommeta/decode.go:573-597`
**What it does:** `buildSourceImage` iterates over all pixels a second time to find min/max values.
**Optimization:** Changed `decodeU8Monochrome`, `decodeU16Monochrome`, `decodeU32Monochrome` to return `([]float32, float32, float32)`, tracking min/max during the decode pass. Changed `buildSourceImage` to accept pre-computed min/max parameters (second scan removed). Also updated `decodeU8Color` to return min/max and `sourceImageFromImage` to track min/max in its pixel-building loops — all paths now single-pass. The `BenchmarkDecodeNativePixelData` benchmark (2048×1536 16-bit LE) was added in `decode_test.go` for end-to-end measurement.
**Actual improvement:** `BenchmarkDecodeNativePixelData` (2048×1536 16-bit LE, 12.6 MB pixel data): ~11.2 ms → ~10.7 ms/op (~5% speedup). The predicted 15-20% was overstated for this image size — at 12.6 MB the pixel array fits in L3 cache (20 MB on this CPU), so the second float32 scan was reading from warm cache rather than main memory. The optimization eliminates the second scan entirely and produces a real but modest gain; larger DICOM files (>20 MB) that exceed L3 will see the full bandwidth saving.
**How to test:** `BenchmarkDecodeNativePixelData` in `decode_test.go` with `-benchmem`.

### Step 5.3: Use `mmap` for Large DICOM Files ✅

**File:** `backend/internal/dicommeta/decode.go:126-162`, `mmap_unix.go`, `mmap_other.go`
**What it does:** `DecodeFile` opens the file and reads through it sequentially.
**Optimization:** Added `openFileSource` helper in `decode.go` and platform-split `tryMmapFile` in `mmap_unix.go` (build tag `unix`) / `mmap_other.go` (`!unix` stub). For files ≥ 1 MB on Unix, `syscall.Mmap(PROT_READ, MAP_SHARED)` maps the file, wraps the data in `bytes.Reader` (satisfies `readerAtSeeker`), and returns a closer that calls `Munmap` + `file.Close`. Small files and non-Unix platforms fall through to regular `*os.File`. `DecodeFile` now calls `openFileSource` instead of `os.Open`.
**Actual result:** `BenchmarkDecodeFile` with 2.2 MB sample radiograph:
- **BEFORE**: ~8.5 ms/op, 268 MB/s, 11,144,436 B/op, 184 allocs/op
- **AFTER**: ~8.5 ms/op, 262 MB/s, 11,144,738 B/op, 187 allocs/op
- **Wall-clock**: within noise (< 1% difference). Allocs +3 (Stat call + tryMmapFile internals). B/op +302 bytes (mapping overhead).
- The predicted 10-20% speedup did not materialize for this 2.2 MB file. Root cause: the sample fits in L3 cache (20 MB on i5-13400); the warm-cache benchmark means I/O is not the bottleneck — pixel decode (3.1M float32 conversions) dominates. mmap eliminates the kernel→userspace copy but that copy was already served from the L3-resident page cache, so the win is sub-percent. The optimization is correctly implemented and will benefit cold reads and files >20 MB that exceed L3 (where mmap's demand-paging avoids the single large `read()` syscall entirely). `BenchmarkDecodeFile` added in `decode_test.go`.
**How to test:** `BenchmarkDecodeFile` in `decode_test.go` with `-benchmem`.

### Step 5.4: Use `io.ReadFull` Instead of `readValue` for Known Sizes ✅

**Files:** `backend/internal/dicommeta/reader.go`, `backend/internal/dicommeta/decode.go`
**What it does:** `readValue(source, header.length)` allocates a new `[]byte` for each DICOM element value, even for small fixed-size fields (2-4 bytes). `readElementHeader` called `readValue` 3-4 times per element (4-byte tag, 2-byte VR, 2-4 byte length) — each a separate heap allocation.
**Optimization:**
1. **`readElementHeader`**: Replaced 3-4 `readValue(source, N)` calls with a single `var buf [4]byte` reused for all reads within the function. Reduces from 3-4 heap allocations to 1 per element header.
2. **`parseDataset` / `parseSourceDataset`**: Added `var smallBuf [4]byte` at function scope. For `header.length <= 4`, reads into `smallBuf` and passes `smallBuf[:header.length]` to `applyValue`/`state.applyValue` (safe — all callers extract values without storing the slice). This collapses N small-element allocations into 1 allocation per parse call. Large elements (> 4 bytes) unchanged.
3. Added `BenchmarkReadFile` in `reader_test.go` to isolate metadata-only performance.
**Actual improvement:** `BenchmarkReadFile` (2.2 MB sample, metadata-only, 2048×1088 image):
- **allocs/op**: 175 → 98 (**-44%**, eliminated 77 tiny allocations per decode)
- **B/op**: 1,064 → 920 (**-14%**, saved 144 bytes of heap per decode)
- **ns/op**: ~43.9 µs → ~41.7 µs (**-5% faster**)
`BenchmarkDecodeFile` (full decode including pixel data):
- **allocs/op**: 187 → 110 (**-41%**)
- **B/op**: 11,144,736 → 11,144,577 (**unchanged** — pixel data float32 conversion dominates)
- **ns/op**: ~8.7 ms → ~8.4 ms (within noise — pixel decode dominates)
- Plan predicted ~5% speedup on metadata parsing; actual is ~5%. Allocation reduction of 44% exceeded expectations.
**How to test:** `BenchmarkReadFile` in `reader_test.go` and `BenchmarkDecodeFile` in `decode_test.go` with `-benchmem`.

---

## Phase 6: Cache & Eviction Improvements (Backend)

### Step 6.1: Replace Map-Based Eviction with LRU ✅

**File:** `backend/internal/cache/memory.go`
**What it does:** `evictResultLocked` and `evictSourcePreviewLocked` iterate maps and delete a random entry when over capacity. Go map iteration order is randomized, so eviction is essentially random rather than LRU.
**Optimization:** Added `resultEntry` and `sourcePreviewEntry` node types with prev/next pointers. Both `entries` and `sourcePreviews` maps now store pointers to list nodes. `storeLocked` and `StoreSourcePreview` push new entries to the front of their respective lists. `loadLocked` and `LoadSourcePreview` call `moveToFront` on hits. Eviction pops the tail (O(1)). `discardInvalidEntry` and artifact-missing paths call `removeEntryLocked` to keep map and list in sync. `evictSourcePreviewLocked` stops early when only one entry remains to avoid evicting a single oversized entry that was just inserted.
**Actual improvement:**
- **Correctness:** `TestMemorySourcePreviewLRU` and `TestMemoryResultLRU` verify deterministic LRU eviction — the entry accessed most recently survives over-capacity eviction while the true LRU victim is evicted. This is unprovable with random eviction.
- **Eviction throughput:** O(1) tail-pop vs O(n) map iteration. `BenchmarkMemoryEviction/Results`: 238 ns/op, 136 B/op, 4 allocs. `BenchmarkMemoryEviction/SourcePreviews`: 223 ns/op, 112 B/op, 3 allocs.
- **Store/Load benchmarks unchanged:** `StoreOnly` ~198 ns/op (was 223 ns/op, -11% due to allocation path change from map value to pointer node). `RoundTrip` and `LoadOnly` ~219–223 ns/op (dominated by 3 MB pixel clone — unchanged).
- Plan predicted 20-40% cache hit rate improvement; this is a correctness fix, not a raw throughput win. Hit-rate improvement is workload-dependent and not directly benchmarkable in a unit test.
**How to test:** `TestMemorySourcePreviewLRU` and `TestMemoryResultLRU` in `memory_test.go`. `BenchmarkMemoryEviction` for throughput.

### Step 6.2: Batch Artifact Eviction with Debounce ✅

**File:** `backend/internal/cache/store.go`
**What it does:** `EvictArtifactsOverLimit` walked the entire artifact directory, statted every file, sorted by mtime, then removed oldest. This was called after every job completion.
**Optimization:** Two fast paths added to `Store` (new fields: `evictMu sync.Mutex`, `evicting bool`, `lastEviction time.Time`, `trackedBytes int64`):
1. **Size fast path**: if `trackedBytes >= 0 && trackedBytes <= maxTotalBytes`, skip the walk entirely (no eviction needed). After each full walk `trackedBytes` is set to the post-eviction total; `AddArtifactBytes(delta)` is called in `jobs/service.go` after each PNG preview write to keep the estimate current.
2. **Debounce fast path**: if a previous walk ran within the last 30 seconds (and size is unknown or over limit), skip. Prevents concurrent job completions from each triggering a walk.
If a walk fails, `trackedBytes` is reset to -1 (unknown) to force a retry. The `evicting` flag prevents concurrent walks.
**Actual improvement:**
- **Walk** (full walk, unchanged): ~103 K ns/op, 255 allocs — identical before/after (the walk itself is unchanged).
- **Sequential** (rapid successive calls, as in production): 43,000 ns/op → **10 ns/op** (99.98% faster), 150 allocs → **0 allocs**. The `trackedBytes ≤ limit` fast path is a lock-check + comparison — no syscalls, no allocations.
**How to test:** `TestEvictArtifactsDebounceSkipsWalkWithinInterval`, `TestEvictArtifactsSkipsWalkWhenTrackedBytesUnderLimit`, `TestAddArtifactBytesAccumulatesWhenKnown`, `TestAddArtifactBytesIgnoresNonPositive` in `store_test.go`. `BenchmarkEvictArtifactsOverLimit/Walk` and `/Sequential` show before/after contrast.

### Step 6.3: Use `RWMutex` for Memory Cache Reads ✅

**File:** `backend/internal/cache/memory.go`
**What it does:** Both `storeLocked` and `loadLocked` acquired a full `sync.Mutex`. Reads and writes were serialized.
**Optimization applied:** Changed `mu sync.Mutex` to `mu sync.RWMutex`. Three distinct patterns applied:
1. **`LoadMeasurementScale`**: Pure `RLock` — no LRU list, just a map read + struct clone. True concurrent reads with no write lock contention.
2. **`loadLocked` / `LoadSourcePreview`**: Two-phase locking — `RLock` for the existence check (miss path returns immediately without blocking writers), then release + `Lock` for artifact validation, kind check, and LRU promotion (hit path). Re-check under write lock handles the race between unlock and relock.
3. **All store paths and `discardInvalidEntry`**: Full `Lock` unchanged.
**Actual improvement:** `BenchmarkConcurrentLoad` with 10 parallel goroutines, 16 pre-populated keys (cpu: 13th Gen Intel Core i5-13400):
- **MeasurementScale**: 144 ns/op → **71 ns/op** (**2.0x faster**). Pure RLock allows all goroutines to proceed simultaneously — lock contention eliminated.
- **SourcePreview concurrent hit**: 287K ns/op → 334K ns/op (**+16% overhead**). Two-phase locking adds a second lock acquisition for hits; regression is dominated by the 3 MB pixel clone (the lock overhead is ~50 μs on a ~330 μs operation). Cache misses (cold start, the concurrent job-start scenario) now return under RLock without serialization.
- **LoadOnly / RoundTrip** (serial): within noise — single goroutine sees no contention difference.
- **Race detector**: clean (`go test -race` passes).
**Why hit path didn't gain from two-phase**: `LoadSourcePreview` always calls `movePreviewToFrontLocked` (LRU promotion), which mutates the linked list and requires a write lock. Two lock acquisitions per hit is more expensive than one when all goroutines hit the same key. The win is on the miss path (concurrent job starts for uncached studies) and on `LoadMeasurementScale` (pure read, no LRU).
**How to test:** `BenchmarkConcurrentLoad/SourcePreview` and `BenchmarkConcurrentLoad/MeasurementScale` in `memory_test.go` with `b.RunParallel`. All 28 existing cache tests pass.

---

## Phase 7: HTTP Transport Optimization (Backend)

### Step 7.1: Pool JSON Encoders/Decoders ✅

**File:** `backend/internal/httpapi/router.go`
**What it does:** Every request creates `json.NewEncoder(writer)` and `json.NewDecoder(request.Body)`. These allocate internal buffers.
**Optimization applied:**
1. **`writeJSON`**: Replaced `json.NewEncoder(writer).Encode(payload)` with a pooled `{bytes.Buffer, json.Encoder}` pair (`jsonWriterPool sync.Pool`). The encoder is permanently wired to the pooled buffer; callers `Reset()` the buffer before reuse. `bytes.Buffer.Write` never errors, so `enc.err` stays nil across reuses. This also fixes a correctness issue: encoding now happens BEFORE `WriteHeader`, so a marshal error can properly return 500 without conflicting with an already-committed status line.
2. **`decodeJSONRequest`**: Pre-reads the request body into a pooled `*bytes.Buffer` (`bodyPool sync.Pool`), then passes the buffer directly to `json.NewDecoder(buf)` (no intermediate `bytes.NewReader`). Preserves `DisallowUnknownFields` and the two-decode trailing-content check unchanged.
**Investigation note:** The plan's suggested `json.Marshal + writer.Write` approach was benchmarked first and was **worse** (+1 alloc, +224 B/op). Reason: `json.Marshal` allocates the full result `[]byte`; `json.NewEncoder(writer)` uses a pooled `encodeState` internally and writes directly, so the Encoder struct (~40 B) is cheaper than the result bytes (~224 B for a typical JobSnapshot). The pooled encoder+buffer approach eliminates the encoder struct allocation entirely for pool hits.
**Actual improvement:** `BenchmarkHandleGetJob` / `BenchmarkWriteJSON` / `BenchmarkDecodeJSONRequest` (cpu: 13th Gen Intel Core i5-13400):
- **BenchmarkHandleGetJob**: ~4,256 ns/op, 7,536 B/op, 34 allocs/op → ~4,282 ns/op, 7,546 B/op, 34 allocs/op (**within noise, no regression**)
- **BenchmarkWriteJSON**: ~989 ns/op, 1,296 B/op, 10 allocs/op → ~977 ns/op, 1,297 B/op, 10 allocs/op (**~1% faster, no regression**)
- **BenchmarkDecodeJSONRequest**: ~2,049 ns/op, 6,178 B/op, 20 allocs/op → ~2,067 ns/op, 6,183 B/op, 20 allocs/op (**within noise, no regression**)
- **Serial benchmark shows near-zero change**: pool hits on every iteration in a single-goroutine benchmark; the dominant costs are `httptest.NewRecorder()`, request body readers, and JSON reflection — not the encoder/decoder struct allocation. The real win is **GC pressure reduction under concurrent polling load**: each goroutine that hits a warmed pool avoids allocating a new 1–4 KB buffer and encoder struct per request.
**How to test:** `BenchmarkHandleGetJob`, `BenchmarkWriteJSON`, `BenchmarkDecodeJSONRequest` in `router_test.go` with `-benchmem`.

### Step 7.2: Cache Runtime/Health Responses ✅

**File:** `backend/internal/httpapi/router.go`
**What it does:** `/healthz` and `/api/v1/runtime` build and serialize the `runtimeResponse` struct on every call.
**Optimization applied:**
- Added `runtimeCacheEntry` struct holding pre-serialized JSON bytes + the study count used to build them.
- Closure-scoped `atomic.Value` inside `NewRouter` stores the cache; lock-free reads on the polling hot path.
- `getRuntimeJSON` checks study count on each call; on a hit it returns cached bytes directly. On a miss it calls `json.Marshal` + appends `\n` (matching `json.Encoder` output), stores, and returns.
- Both `/healthz` and `GET /api/v1/runtime` share the same `writeRuntimeJSON` closure.
**Actual improvement:** `BenchmarkHealthz` (cpu: 13th Gen Intel Core i5-13400):
- **Before:** ~3,760 ns/op, 7,260 B/op, 25 allocs/op
- **After:** ~2,000 ns/op, 6,786 B/op, 19 allocs/op (~47% faster, 6 fewer allocs/op)
- Note: plan's ~95% estimate assumed serialization dominated; httptest recorder + HTTP infrastructure account for the remaining cost.
**How to test:** `BenchmarkHealthz` in `router_test.go` with `-benchmem`.

### Step 7.3: Skip Extra JSON Decode Verification ✅

**File:** `backend/internal/httpapi/router.go:395-424`
**What it does:** After decoding the command payload, `decodeJSONRequest` tries to decode again to check for trailing content. This second decode attempt touches the rest of the request body.
**Optimization:** Captured `bodyBytes := buf.Bytes()` before the first decode (a zero-alloc slice into the pooled buffer), then used `decoder.InputOffset()` to slice directly to the first unread byte. Replaced the second full `decoder.Decode(&extra)` pass with a bare `for _, b := range bodyBytes[decoder.InputOffset():]` whitespace scan. Kept `DisallowUnknownFields` on the existing decoder — no `json.Unmarshal` workaround needed.
**Actual improvement:** `BenchmarkDecodeJSONRequest` (45-byte payload): 2147 → 2109 ns/op (1.8% faster), 6183 → 6167 B/op, 20 → 19 allocs/op. Plan's predicted 10% applies to larger payloads where the second decode has meaningful work; for the small-payload benchmark the main win is the eliminated alloc (the `var extra any` that was passed to Decode).
**How to test:** `BenchmarkDecodeJSONRequest` in `router_test.go`; functional trailing-content coverage at line 481 of `router_test.go`.

---

## Phase 8: DICOM Export Optimization (Backend)

### Step 8.1: Preallocate Element Map ✅

**File:** `backend/internal/export/secondary_capture.go:101`
**What it does:** `elements := make(map[uint32]element)` with default capacity. Then ~25 elements are inserted.
**Optimization:** `make(map[uint32]element, 32)` to preallocate capacity and avoid map growth.
**Expected improvement:** Minor - eliminates 2-3 map rehashes. ~1-2% speedup on export.
**Actual improvement:** `BenchmarkEncodeSecondaryCapture` with 2048×1536 images:
- **Gray8**: 57 → 53 allocs/op (−4 allocs), B/op ~unchanged (~3 KB less), ns/op within noise
- **RGBA8**: 53 → 51 allocs/op (−2 allocs), B/op ~unchanged, ns/op within noise
- Eliminated map growth rehashes as predicted. Speed impact negligible (pixel data copies dominate cost).
**How to test:** `BenchmarkEncodeSecondaryCapture` in `secondary_capture_test.go` with `-benchmem`.

### Step 8.2: Avoid Sorting Elements Twice ✅

**File:** `backend/internal/export/secondary_capture.go`
**What it does:** `elements map[uint32]element` collected ~25 dataset elements, then converted to a slice and called `sortElements`. `metaElements` literal was already in ascending tag order but `sortElements(metaElements)` was called unnecessarily — the second redundant sort.
**Optimization:** Replaced the map with a `[]element` maintained in sorted tag order via `insertElement` (binary search insertion, handles overwrite for preserved elements). Removed `sortElements(metaElements)` (already ordered). Removed `putElement`, `sortElements`, and the `sort` import entirely.
**Actual improvement:** `BenchmarkEncodeSecondaryCapture` (2048×1536):
- **Gray8**: 53 → 43 allocs/op (−10 allocs), B/op ~unchanged, ns/op within noise
- **RGBA8**: 51 → 41 allocs/op (−10 allocs), B/op ~unchanged, ns/op within noise
- Eliminated map allocation, map buckets, and map-to-slice copy loop. Speed unchanged (pixel data dominates).
**How to test:** `BenchmarkEncodeSecondaryCapture` in `secondary_capture_test.go` with `-benchmem`.

---

## Phase 9: Frontend State & Rendering (Frontend)

> The frontend agents identified several rendering bottlenecks, especially during job polling (200ms intervals) and viewer interaction.

### Step 9.1: Memoize Selector Results ✅

**File:** `frontend/src/app/store/workbenchStore.ts`
**What it does:** `useWorkbenchStore` calls `selector(workbenchActions.getState())` on every render. Selectors like `selectPendingJobCount` created a new filtered array on every call, even when `jobs` hadn't changed.
**Optimization:** Added `createSelector` (single-input) and `createSelector2` (two-input) memoization helpers using module-level closure over the singleton store. Re-runs the result function only when the input reference changes (`Object.is`). `selectPendingJobCount` memoized on `s.jobs` — skips `Object.values().filter()` on non-jobs state changes (study updates, activeStudyId changes, etc.). `selectActiveStudy` memoized on `(s.activeStudyId, s.studies)` pair — returns cached object reference when neither changes. No external dependencies (no reselect).
**Actual improvement:** `Object.values().filter()` on the jobs map now runs only when `s.jobs` reference changes (i.e., an actual job update), not on every state notification. During polling at 200 ms intervals, non-job state changes (study updates, viewer state) no longer trigger redundant filter computations. `selectActiveStudy` returns the same object reference from cache when the active study and studies map are unchanged, preventing downstream `Object.is`-based re-render checks from seeing spurious new references.
**Validation:** `node frontend/scripts/validate-selectors.mjs` — 9 tests, all pass. Confirms: (a) unmemoized selector runs body 5× for 5 same-input calls; (b) memoized selector runs body 1× for 5 same-input calls; (c) recomputes correctly on new input references; (d) returns same cached object reference. `npm run build` passes with zero type errors.
**How to test:** `node frontend/scripts/validate-selectors.mjs` for unit validation. React DevTools Profiler to count renders before/after during a render job.

### Step 9.2: Avoid Spreading Entire State on Every Update ✅

**File:** `frontend/src/app/store/workbenchStore.ts`
**What it does:** `receiveJobUpdate` creates new spread copies of `current.jobs`, `current.studies`, and the full state on every job poll update. During active jobs, this runs every 500ms-1s.
**Optimization:** Added `jobSnapshotEqual` comparing `state`, `progress.percent`, `progress.stage`, `progress.message`, `fromCache`, and null-transitions for `result`/`error`. `receiveJobUpdate` returns `current` unchanged when the check passes — the existing `nextState === this.state` guard in `setState` then skips listener notification and React reconciliation entirely. Terminal states (completed/failed/cancelled) correctly coalesce subsequent identical polls.
**Note on timing:** `timing` is intentionally excluded from the equality check. It is computed locally by `advanceJobProgressTiming`, not from the backend. Stall detection uses `lastProgressAtMs` (which only advances on real progress) and `useProgressClock` (a `setInterval`) — both work correctly without store updates when progress is unchanged.
**Actual improvement:** In the common polling scenario (job not advancing between two 200ms polls):
- **BEFORE:** Every poll creates new `jobs` + `studies` + state object references → listener notification → React reconciliation on every poll
- **AFTER:** No-op polls return `current` unchanged → 0 listener notifications → 0 React reconciliation work
- **Listener notification reduction:** 0 for no-op polls (vs 1 per poll before). In a 10s render job with 200ms polling (50 polls) where progress advances ~5 times, listener count drops from 50 → ~5 (~90% reduction).
**Validation:** `node frontend/scripts/validate-job-updates.mjs` — 8 tests, all pass. Confirms: (a) BEFORE fires listener 5× for 5 identical polls; (b) AFTER fires 0× for identical polls; (c) progress change, state transition, null→error, null→result all fire correctly; (d) mixed 10-poll sequence with 6 no-ops → exactly 4 notifications. `npm run build` passes with zero type errors.
**How to test:** `node frontend/scripts/validate-job-updates.mjs`. React DevTools Profiler to count renders during a render job.

### Step 9.3: Batch `setStudyState` Updates ✅

**File:** `frontend/src/app/store/workbenchStore.ts`
**What it does:** Each `setStudyState` call triggered listener notification immediately and synchronously. Multiple synchronous `setState` calls (e.g., three concurrent job poll responses from `Promise.all` resolving together) each fired their own listener cycle and React reconciliation.
**Optimization:** Added `pendingNotification: boolean` flag to `WorkbenchStore`. `setState` now updates state synchronously (so `getState()` always returns the latest value — required for `useSyncExternalStore` tearless semantics) but defers listener notification via `queueMicrotask`. If multiple `setState` calls happen before the microtask fires, only one microtask is scheduled. Flag is reset BEFORE iterating listeners (re-entrancy safe: if a listener triggers another `setState`, a fresh microtask is queued for that next batch). No-op updates (same state ref) still return early before touching the flag.
**Actual improvement:** N synchronous `setState` calls → 1 listener notification (N-1 notifications eliminated). Directly benefits the `Promise.all` polling pattern in `useJobs.ts` where 3 concurrent job fetches resolve in the same microtask: 3 `receiveJobUpdate` calls → 1 React reconciliation cycle instead of 3. Also future-proofs any subsequent rapid-fire update patterns.
**Validation:** `node frontend/scripts/validate-batched-updates.mjs` — 10 tests, all pass. Confirms: (a) BEFORE fires listener 3× for 3 synchronous calls; (b) AFTER fires 0× immediately, 1× after microtask flush; (c) state is readable synchronously before flush; (d) no-op updates never queue microtask; (e) second batch after flush correctly queues new microtask; (f) re-entrancy guard (second batch runs independently). `npm run build` passes with zero type errors.
**How to test:** `node frontend/scripts/validate-batched-updates.mjs`. React DevTools Profiler to count render cycles during concurrent job polling.

### Step 9.4: Memoize AnnotationLayer with React.memo ✅

**File:** `frontend/src/features/annotations/AnnotationLayer.tsx`
**What it does:** AnnotationLayer receives the entire annotations bundle, selected ID, draft line, and transform. When any annotation or viewport changes, all annotation SVG elements re-render. Expensive per-annotation computations (midpoint, label formatting) run on every render.
**Optimization:**
1. **`React.memo(AnnotationLayer, annotationLayerPropsEqual)`** — custom comparator compares `transform` field-by-field (`offsetX`, `offsetY`, `scale`) and skips callback comparison (ViewerCanvas recreates `beginHandleDrag` each render without `useCallback`). Prevents re-renders from `hoverCoord` updates (mouse-move state in ViewerCanvas that fires every pointermove but is not passed to AnnotationLayer) and other unrelated parent state changes.
2. **Extracted `LineAnnotationItem = React.memo(..., lineItemPropsEqual)`** — per-annotation sub-component with comparator on `annotation` (by ref), `isSelected`, and `scale` only. On pan: `AnnotationLayer` re-renders (SVG `<g transform>` must update), but each `LineAnnotationItem` skips re-render because `scale` and `annotation` ref are unchanged. On zoom: `scale` changes → items re-render to adjust label y-offset.
3. **`useMemo` for `selectedLine`** — keyed on `[annotations.lines, selectedAnnotationId]`. Eliminates the O(n) `.find()` per render; for a 10-annotation image with 200 ms polling, this ran ~300×/min regardless of whether annotations changed.
4. **Pre-computed label text in `LineAnnotationItem`** — `label` string and `mid` point computed once in the item's render body (memoized by the component itself via `memo`).
**Actual improvement:**
- **Pan scenario** (most frequent interaction): `AnnotationLayer` re-renders once (SVG transform update); all N `LineAnnotationItem` instances skip re-render. BEFORE: N+1 renders per pointer move. AFTER: 1 render.
- **Unrelated parent state change** (e.g. hoverCoord): BEFORE: `AnnotationLayer` + all items re-render. AFTER: 0 renders (custom comparator catches no-op).
- **Selection change**: Only the 1 affected item re-renders (isSelected toggled); all other N-1 items skip. BEFORE: N re-renders. AFTER: 1 re-render.
- **useMemo selectedLine**: find() runs 1× per unique (lines, selectedId) pair vs 1× per render. During polling at 200 ms intervals with annotations visible, eliminates ~5 find() calls/second.
**Validation:** `node frontend/scripts/validate-annotation-memo.mjs` — 16 tests, all pass. Covers: BEFORE/AFTER comparator behavior for pan, zoom, annotation change, selection change, callback churn, unrelated parent re-render, useMemo selectedLine call count. `npm run build` passes with zero type errors.
**How to test:** `node frontend/scripts/validate-annotation-memo.mjs`. React DevTools Profiler to count renders during pan/zoom with annotations visible.

### Step 9.5: Use GPU-Accelerated CSS Transforms for Image Positioning ✅

**File:** `frontend/src/features/viewer/ViewerCanvas.tsx:443-451`, `frontend/src/styles/base.css`
**What it does:** Image positioning uses `left`, `top`, `width`, `height` inline styles. These trigger layout recalculation on every viewport change.
**Optimization:** Replaced `left/top/width/height` inline styles with `width: naturalW`, `height: naturalH`, `transform: translate(offsetX, offsetY) scale(S)`, `transformOrigin: "0 0"`. Added `will-change: transform` to CSS to promote element to its own compositor layer. CSS transforms skip the layout and paint phases — only the compositor stage runs on every pan/zoom frame.
**Actual improvement:** `node frontend/scripts/validate-gpu-transforms.mjs` — 11 tests, all pass. Confirms:
- **BEFORE pan**: 2 layout-triggering properties change per frame (`left` + `top`)
- **BEFORE zoom**: 4 layout-triggering properties change per frame (`left` + `top` + `width` + `height`)
- **AFTER pan**: 0 layout properties change — only `transform` string updates (compositor only)
- **AFTER zoom**: 0 layout properties change — only `transform` string updates (compositor only)
- **100 pan frames**: 200 → 0 layout triggers (100% eliminated)
- **100 zoom frames**: 400 → 0 layout triggers (100% eliminated)
- Pixel math verified: `translate(offsetX, offsetY) scale(S)` with `transformOrigin: 0 0` is algebraically identical to the original `left/top/width*S/height*S` positioning for all image corners.
**How to test:** `node frontend/scripts/validate-gpu-transforms.mjs` for logic validation. Chrome DevTools Performance tab → compare Layout block during pan gesture before/after.

### Step 9.6: Fine-Grained Store Selectors to Reduce ViewTab Re-renders ✅

**File:** `frontend/src/components/viewer/ViewTab.tsx:8-10`, `frontend/src/app/store/workbenchStore.ts`
**What it does:** `const jobs = useWorkbenchStore(selectJobs)` subscribed to the entire jobs map. Every 200ms poll created a new `jobs` spread object reference, causing ViewTab (and all children) to re-render even when the active study's job didn't change.
**Optimization:** Added `selectActiveStudyJobs` — an IIFE-closure selector that memoizes on the two specific job snapshot references for the active study (`s.jobs[renderJobId]` and `s.jobs[analysisJobId]`), not on `s.jobs` as a whole. Returns the same result object reference when neither job snapshot has changed. `ViewTab` now uses `useWorkbenchStore(selectActiveStudyJobs)` and accesses `activeStudyJobs.analysis` directly (no `useMemo` needed — selector already guarantees reference stability).
**Why IIFE closure over `createSelector2`:** `createSelector2(selectActiveStudy, s => s.jobs, ...)` would check `s.jobs` reference — which changes every poll for any study's job — defeating the purpose. The IIFE compares the extracted snapshot refs directly, so cross-study job churn produces zero ref changes.
**Actual improvement:** `node frontend/scripts/validate-active-study-jobs-selector.mjs` — 9 tests, all pass. Confirms:
- **BEFORE**: any job update (including other studies) creates new `jobs` ref → ViewTab re-renders
- **AFTER**: 10 cross-study polls → 0 new result refs (0 re-renders)
- **AFTER**: 10 mixed polls with 2 active-study advances → exactly 2 new refs (only real changes trigger re-render)
- **AFTER**: no active study → stable `{ render: null, analysis: null }` ref
- **AFTER**: active study switch, render job advance, analysis job advance all correctly produce new refs
- In a 10-second render job with 200ms polling (50 polls) where the active study's job advances ~5 times, ViewTab re-renders drop from 50 → ~5 (~90% reduction from cross-study churn elimination).
**How to test:** `node frontend/scripts/validate-active-study-jobs-selector.mjs`. React DevTools Profiler to count renders during a render job with multiple studies open.

### Step 9.7: Debounce Processing Control Updates ✅

**File:** `frontend/src/app/store/workbenchStore.ts` (`setProcessingControls`, `commitPendingControls`)
**What it does:** `setProcessingControls` created a new state on every slider drag event (brightness, contrast).
**Optimization:** Added three private fields (`_pendingControls`, `_pendingControlsStudyId`, `_controlsRaf`) to `WorkbenchStore`. `setProcessingControls` now accumulates the latest controls and schedules `requestAnimationFrame` on the first call per frame. Subsequent calls within the same frame update `_pendingControls` without scheduling additional rAFs. The rAF callback calls `commitPendingControls()`, which clears pending state then calls `setStudyState` with the accumulated value. The study ID is captured at call time (not at rAF time) so controls commit to the correct study even if the active study changes before the frame fires.
**Actual improvement:** `node frontend/scripts/validate-debounce-controls.mjs` — 11 tests, all pass. Confirms:
- **BEFORE**: 5 rapid slider events → 5 state updates → 5 listener notifications
- **BEFORE**: 10 rapid events in one frame → 10 state updates (no coalescing)
- **AFTER**: 5 events → 1 rAF scheduled (not 5), 0 updates until rAF flush → 1 state update after flush
- **AFTER**: 10 events coalesced to 1 state update (**90% reduction**)
- **AFTER**: last-value wins (the only meaningful one per frame)
- **AFTER**: cross-frame correctness — second frame schedules new rAF independently
- **AFTER**: no rAF scheduled when no active study
- **AFTER**: study ID isolation correct across study switches
- The combined path (rAF → `setStudyState` → `queueMicrotask` notify from step 9.3) means the full pipeline is: many slider events → 1 rAF → 1 microtask → 1 React reconciliation per frame. Previously: N slider events → N `setState` → N microtasks → N reconciliations.
- During slider drags where 3–10 events fire per frame (fast drag on high-refresh display or busy main thread), state updates drop from N → 1. At 60fps steady-state they're equivalent (1 event/frame), but GC pressure is reduced by eliminating N-1 intermediate state objects.
**How to test:** `node frontend/scripts/validate-debounce-controls.mjs` for logic validation. React DevTools Profiler to count renders during brightness/contrast slider drag.

### Step 9.8: Attach Pointer Listeners to Container, Not Window ✅

**File:** `frontend/src/features/viewer/ViewerCanvas.tsx`, `frontend/src/features/annotations/AnnotationLayer.tsx`
**What it does:** `window.addEventListener("pointermove", handlePointerMove)` attaches global listeners for pan/draw/edit gestures.
**Optimization:** Three changes:
1. `beginBackgroundInteraction` calls `event.currentTarget.setPointerCapture(event.pointerId)` on the container div — captures subsequent pointer events to the container element even when the pointer leaves its bounds.
2. Both annotation handle `onPointerDown` handlers (start/end circles) call `event.currentTarget.setPointerCapture(event.pointerId)` on the SVG circle — captured events bubble through SVG → container div, where the container listener catches them.
3. `useEffect` captures `const container = containerRef.current` at effect start, attaches `pointermove` + `pointerup` to the container instead of `window`. Cleanup uses the same captured reference.
**Why pointer capture is required:** Simply moving listeners from `window` to container without capture would break gestures when the pointer leaves the container mid-drag. `setPointerCapture` ensures the element continues to receive events regardless of pointer position, preserving existing UX while eliminating global listeners.
**Actual improvement:** Zero `window` pointer listeners during gestures. No memory leaks from missed cleanup on component remount (captured ref is closed over, so cleanup always removes from the correct element). `getEventListeners(window)` shows no `pointermove`/`pointerup` entries during pan/draw/edit.
**How to test:** Check `getEventListeners(window)` in DevTools — no `pointermove` or `pointerup` entries during pan/draw. Verify pan still works when dragging outside the viewer bounds (pointer capture keeps the gesture alive).

---

## Phase 10: Frontend Network & Polling (Frontend)

> The desktop agent identified that polling overhead and redundant serialization dominate the frontend-backend communication path.

### Step 10.1: Exponential Backoff on Job Polling ✅

**File:** `frontend/src/features/jobs/useJobs.ts`
**What it does:** The frontend polled `getJob` at a fixed 200ms interval regardless of job state or progress.
**Optimization:** Exponential backoff using schedule-then-double pattern. Constants: `FAST_POLL_MS=200`, `QUEUED_POLL_MS=1000`, `MAX_POLL_MS=2000`. Per-poll cycle: snapshot pre-poll `{percent, state}` for each job. After fetches: if any percent advanced OR state transitioned (queued→running) OR any running job >80% → reset to 200ms. If all jobs are queued → 1000ms (steady, `currentIntervalMs` unchanged so backoff restarts fresh at 200ms when running begins). Otherwise → schedule at `currentIntervalMs` then double (schedule-then-double prevents first-poll doubling to 400ms). `currentIntervalMs` is closure-local; effect re-mount on `pendingJobCount` change resets it to 200ms automatically.
**Actual improvement:** `node frontend/scripts/validate-exponential-backoff.mjs` — 12 tests, all pass.
- **BEFORE:** 50 polls at fixed 200ms over 10s job
- **AFTER:** 16 polls with exponential backoff — **68% reduction** (plan predicted 50-70% ✓)
- **Backoff sequence** (no progress, running): 200 → 400 → 800 → 1600 → 2000 → 2000 (capped)
- **Queued phase:** steady 1000ms (5× less frequent than BEFORE's 200ms)
- **Progress detected / near-complete (>80%):** resets to 200ms immediately
- **AFTER interval pattern** for a queued→running job: `[1000, 1000, ..., 200, 200, 400, 800, 1600, 2000, ...]`
- Effect re-mount (new job submitted) naturally resets backoff via fresh closure.
**How to test:** `node frontend/scripts/validate-exponential-backoff.mjs`.

### Step 10.2: Use Server-Sent Events for Job Updates ✅

**Files:** `backend/internal/jobs/service.go`, `backend/internal/httpapi/sse.go`, `backend/internal/httpapi/router.go`, `desktop/app.go`, `frontend/src/features/jobs/useJobs.ts`
**What it does:** Frontend polled `get_job` every 200ms while jobs were pending.
**Optimization:** Added `GET /api/v1/events` SSE endpoint. `sseHub` fans out job-update frames to all connected clients (buffered channels, non-blocking broadcast — slow clients drop frames). Added `OnJobUpdate` callback to `jobs.Service` fired on every `transitionJob` call (progress) and every terminal-state notification. Backend in-process mode wires `OnJobUpdate` → `wailsruntime.EventsEmit`; sidecar mode launches a bridge goroutine that reads the SSE stream and re-emits as Wails events (auto-reconnects on error). Frontend tracks `lastEventAtMs`; when events are fresh (< 10s), HTTP polling is suppressed entirely and a 10s heartbeat is scheduled. Falls back to normal polling if events go stale.
**Actual improvement:** `node frontend/scripts/validate-sse-polling-reduction.mjs` — 6 tests, all pass.
- **3s job (events every 400ms):** 12 → 0 HTTP `get_job` requests (**100% reduction**)
- **30s job (events every 5s):** 56 → 0 HTTP `get_job` requests (**100% reduction**)
- **No SSE events (stale fallback):** 16 polls in 25s — polling resumes correctly after 10s stale window
- **SSE delivery latency:** < 100ms (verified by `TestSSEHubBroadcastDeliveredWithinLatencyBudget`)
- **Multi-client fan-out:** verified by `TestSSEHubMultipleClientsAllReceiveBroadcast`
**How to test:** `node frontend/scripts/validate-sse-polling-reduction.mjs`. Go: `go -C backend test ./internal/httpapi/... -run TestSSE -v`.

### Step 10.3: Deduplicate and Batch Job Polling Requests ✅

**Files:** `backend/internal/contracts/jobs.go`, `backend/internal/contracts/contracts.go`, `backend/internal/httpapi/router.go`, `backend/internal/jobs/service.go`, `backend/internal/app/service.go`, `backend/contracts.go`, `backend/service.go`, `desktop/app.go`, `frontend/src/lib/wails.ts`, `frontend/src/lib/runtimeTypes.ts`, `frontend/src/lib/backend.ts`, `frontend/src/lib/runtime.ts`, `frontend/src/features/jobs/useJobs.ts`
**What it does:** `pollPendingJobs` fired a separate HTTP request per pending job via `Promise.all`. With 3 concurrent jobs, this was 3 requests every 200ms.
**Optimization:** Added `get_jobs` batch command to backend contracts and router. `GetJobsCommand { JobIDs []string }` returns `[]JobSnapshot`, silently omitting unknown IDs. Desktop app exposes `GetJobsSnapshot`. Frontend wires `getJobs(jobIds)` through `BackendAPI` / `RuntimeAdapter` / mock + desktop impls. `useJobs.ts` replaces N individual `getJob` calls with one deduplicated `getJobs` batch call per poll cycle. Deduplication via `new Set()` at call site.
**Actual improvement:** `node frontend/scripts/validate-batch-polling.mjs` — 8 tests, all pass.
- **3 jobs, 5s no-progress run**: 18 → 6 HTTP requests (**66.7% reduction**)
- **5 jobs, 5s no-progress run**: 30 → 6 HTTP requests (**80.0% reduction**)
- **Deduplication**: duplicate job IDs collapsed to unique set before batch send
- **Empty pending list**: 0 requests sent (unchanged)
- Plan predicted 60-80%; actual is 67% for 3 jobs, 80% for 5 jobs — matches prediction ✓
- Note: `Promise.allSettled` not needed — existing inner `try/catch` per individual call was already equivalent. Batch error handling: if the batch request throws, all job states remain unchanged until next cycle (same semantics as individual failure with try/catch).
**How to test:** `node frontend/scripts/validate-batch-polling.mjs`. Monitor Network tab with 3+ concurrent jobs — each poll cycle shows 1 `get_jobs` POST instead of N `get_job` POSTs.

### Step 10.4: Add Cache-Control Headers for Preview Assets ✅

**File:** `desktop/app.go` (ServeAsset method)
**What it does:** Preview PNG files were served without cache headers. Browser re-requested them on every navigation.
**Optimization:** Added `Cache-Control: public, max-age=3600` and ETag (`"<modtime_ns>-<size>"`) headers in `ServeAsset` before calling `http.ServeContent`. `ServeContent` already calls `checkIfNoneMatch` internally — if ETag matches `If-None-Match` request header, it returns 304 Not Modified with empty body automatically. No extra seek or read needed for the conditional path.
**Actual improvement:** `BenchmarkServeAssetFromDisk` vs `BenchmarkServeAssetCacheHit` with 512 KB payload (cpu: 13th Gen Intel Core i5-13400):
- **200 (cold / stale ETag):** ~259 µs/op, ~1,051 KB/op, 34 allocs — full file read + send
- **304 (warm cache / matching ETag):** ~7.5 µs/op, ~7.6 KB/op, 34 allocs — **~35x faster, 99.3% fewer bytes transferred**
- Plan predicted 50-80% reduction in repeated transfers; actual is 99.3% because the 304 path skips the entire file read and body write. The alloc count is equal because `httptest.NewRequest` + `httptest.NewRecorder` dominate; in production the browser holds the connection and per-request allocations are much lower.
**How to test:** `TestServeAssetSetsCacheControlAndETag`, `TestServeAssetReturns304OnMatchingETag`, `TestServeAssetReturns200OnStaleETag` in `app_test.go`. `BenchmarkServeAssetCacheHit` vs `BenchmarkServeAssetFromDisk` in `app_bench_test.go`. Chrome Network tab: revisit a study → 304 responses for preview PNGs.

---

## Phase 11: Concurrency & Job Scheduling (Backend)

### Step 11.1: Use Worker Pool Instead of Goroutine-per-Job ✅

**File:** `backend/internal/jobs/service.go`
**What it does:** `launchJob` spawned a new goroutine per job using a buffered channel as a semaphore (`concurrencyLimit chan struct{}`, size 3) to limit concurrency. Each submitted job paid goroutine-creation cost and held a blocked goroutine while waiting for the semaphore.
**Optimization:** Replaced with a fixed worker pool: 3 persistent goroutines (`runWorker`) consume from two priority channels — `renderQueue` (render jobs, high priority) and `jobQueue` (process + analyze, normal priority). Workers drain `renderQueue` first via a priority-biased double-select before blocking on either queue. Added `Stop()` method with `sync.Once` to cleanly shut down workers (used in tests). `launchJob(kind, run)` routes to the appropriate channel; `run()` is always called (executors own terminal-state transitions via `finishCancelledIfRequested`). `concurrencyLimit` field removed.
**Actual improvement:** `BenchmarkLaunchJobThroughput` (serial, no-op job, 2048×1536 cpu):
- **ns/op**: ~438 → ~498 ns/op (~14% slower in serial micro-benchmark — channel roundtrip has more sync cost than goroutine+semaphore in the no-contention case)
- **B/op**: 80 → 32 B/op (**60% reduction** — goroutine stack allocation eliminated)
- **allocs/op**: 3 → 2 (**1 fewer alloc per job** — no goroutine created)
- `BenchmarkFullWorkflow` (full render+process+analyze pipeline): ~344 ms/op (unchanged — pixel work dominates)
- The ns/op increase in the serial micro-benchmark is expected: before, a single goroutine did submit+run with no channel crossing; after, work crosses to a persistent worker goroutine. Under real job load (ms-to-seconds per job with 3 concurrent workers), the per-job goroutine creation cost becomes negligible but memory overhead is eliminated — especially under burst load where many jobs queue up as blocked goroutines vs sitting in a buffered channel.
- **Priority scheduling**: render jobs now skip the queue of pending process/analyze jobs, reducing render latency when all 3 workers are busy with lower-priority work.
- Race detector: clean (`go test -race` passes).
**How to test:** `BenchmarkLaunchJobThroughput` and `BenchmarkLaunchJobConcurrent` in `bench_test.go` with `-benchmem`. Existing service tests pass (`go -C backend test ./internal/jobs/...`).

### Step 11.2: Context-Aware DICOM Decoding ✅

**File:** `backend/internal/dicommeta/decode.go`
**What it does:** `DecodeStudy` previously checked context cancellation only at entry and exit, not during the decode loop. A context cancelled mid-decode would not be detected until `DecodeFile` returned.
**Optimization:** Threaded `ctx` from `DecodeStudy` → `decodeFileWithCtx` → `decodeWithCtx` → `parseSourceDataset`. Added `ctx.Err()` check at the top of every loop iteration in `parseSourceDataset`. `ctx.Err()` is an atomic load (~1 ns) — no measurable overhead for the typical ~100-element metadata parse. Kept public `Decode(source)` and `DecodeFile(path)` signatures unchanged (delegate to internal ctx-aware helpers using `context.Background()`). Context-cancelled decodes skip the `supportsStandaloneImagePath` image fallback (cancellation is not a "not-a-DICOM" signal).
**Actual improvement:** `BenchmarkDecodeFile` / `BenchmarkDecodeStudy` (2.2 MB sample, 2048×1088):
- **ns/op**: ~8.4 ms → ~7.5-8.7 ms (within noise — per-element atomic load is unmeasurable)
- **B/op**: 11,144,577 (unchanged)
- **allocs/op**: 110 (unchanged)
- The plan's predicted "<50ms cancellation" applies to the metadata-parse phase only. The pixel data read (`readValue`) and decode (`decodeU16Monochrome`) are not interruptible — for a 2 MB file the pixel phase is ~3 ms; for a 50 MB+ file it could be 100+ ms. This is a best-effort gate: cancellation is caught before or after the pixel decode block, not during it. The per-element check is free; the limitation is Go's lack of mid-read cancellation primitives.
**How to test:** `TestDecodeStudyContextCancelledBeforeStart` (pre-cancelled ctx returns `context.Canceled` immediately), `TestDecodeWithContextCancelledDuringParse` (deterministic mid-parse cancel via `cancelAfterNReadsReader`), both in `decode_test.go`.

---

## Phase 12: Build & Bundle Optimization (Tooling)

### Step 11.3: Add TTL to Artifact Existence Checks in Cache ✅

**File:** `backend/internal/cache/memory.go:226-267`
**What it does:** `resultArtifactsExist` calls `os.Stat` on artifact files for every cache load. This is a filesystem syscall on every cache lookup.
**Optimization:** Added `lastCheckedAt time.Time` to `resultEntry`. `loadLocked` skips `resultArtifactsExist` when `time.Since(lastCheckedAt) < 60s`. `storeLocked` sets `lastCheckedAt = time.Now()` on both insert and update so the first load after a store also skips the stat. Three invalidation tests updated to zero `lastCheckedAt` before testing stale-file detection.
**Expected improvement:** 20-30% faster cache lookups by eliminating redundant filesystem calls.
**Actual result:** `BenchmarkLoadRender/Hit` with 2048×1536 preview:
- **Before**: ~698 ns/op, 272 B/op, 2 allocs/op
- **After**: ~59 ns/op, 0 B/op, 0 allocs/op
- **11.8x speedup**, 100% allocation elimination. The predicted 20-30% underestimated the `os.Stat` cost: the syscall + `os.FileInfo` allocation dominated the hot path. `EvictArtifactsOverLimit` can still delete artifacts within the TTL window (60s stale-hit risk), documented at the const.
**How to test:** `BenchmarkLoadRender/Hit` in `memory_test.go`; `go -C backend test ./internal/cache/... -race` passes.

---

## Phase 12: Desktop Shell & Sidecar Optimization

### Step 12.1: Reduce JSON Marshal/Unmarshal Cycles in Sidecar Path ✅

**File:** `desktop/app.go`, `desktop/sidecar.go`
**What it does:** In sidecar (HTTP) mode, data flowed: Go struct → `json.Marshal` → `string(bytes)` → `bytes.NewBufferString` → HTTP body → `io.ReadAll` → `string(body)` → `[]byte(body)` → `json.Unmarshal`. That's 5 unnecessary string/bytes copies per command.
**Optimization:** Replaced `InvokeCommand(payloadJSON string)` + `invokeBackendCommand` with `invokeCommandRaw(payload []byte)`. `invokeViaHTTP` now marshals directly to `[]byte` (no string conversion), passes bytes to `bytes.NewReader` (no copy), and decodes via `json.NewDecoder(response.Body)` (eliminates `io.ReadAll` + string + bytes round-trip). Removed dead code: `InvokeCommand`, `invokeBackendCommand`, `errorResponse`, `backendCommandResponse`, `backendErrorPayload`.
**Actual result:** `BenchmarkInvokeViaHTTP` with `GetJobSnapshot` against a local httptest server:
- **Before**: ~72 µs/op, 18,693 B/op, 199 allocs/op
- **After**: ~72 µs/op, 18,639 B/op, 198 allocs/op
- **Net**: −1 alloc/op, −54 B/op visible in benchmark. The `/healthz` probe called on every `EnsureStarted` adds ~2× HTTP overhead that dilutes the signal. The actual serialization path savings are ~3 allocs + ~280 B per command; these are invisible against the probe's dominance. The code is correct and the unnecessary copies are eliminated.
**How to test:** `BenchmarkInvokeViaHTTP` in `desktop/app_bench_test.go`.

### Step 12.2: Configure HTTP Transport Connection Pooling ✅

**File:** `desktop/sidecar.go`
**What it does:** Two `http.Client` instances use default transport with no explicit connection pool config.
**Optimization:** Added `newSidecarTransport()` helper returning `*http.Transport` with `MaxIdleConns: 2`, `MaxIdleConnsPerHost: 2`, `IdleConnTimeout: 30s`. Applied to both `probeClient` and `httpClient` in `NewSidecarController()`. Updated `BenchmarkInvokeViaHTTP` to use `newSidecarTransport()` so benchmark tests actual production config.
**Actual result:** `BenchmarkInvokeViaHTTP` before vs after: ~71-74 µs/op / 18,660 B/op / 198 allocs both runs. No measurable delta — `http.DefaultTransport` already reuses connections in benchmark context. Real benefit is production isolation (each `SidecarController` owns its own pool, no sharing with other global HTTP clients) and `IdleConnTimeout: 30s` (vs default 90s) more appropriate for a desktop app that may be idle.
**How to test:** `BenchmarkInvokeViaHTTP` in `desktop/app_bench_test.go`.

### Step 12.3: Add HTTP Server Timeouts ✅

**File:** `backend/internal/app/app.go:102-106`
**What it does:** Only `ReadHeaderTimeout` is configured on the HTTP server. Missing `ReadTimeout`, `WriteTimeout`, `IdleTimeout`.
**Optimization:** Add `ReadTimeout: 15s`, `WriteTimeout: 15s`, `IdleTimeout: 60s`, `MaxHeaderBytes: 1 << 20` to prevent resource leaks from slow/stalled clients.
**Actual result:** All four timeout fields + `MaxHeaderBytes` now set on `http.Server`. Defense-in-depth: prevents resource exhaustion from slow/stalled clients. No measurable throughput change (expected — this is a safety net, not a hot path).
**How to test:** `TestNewConfiguresHTTPServerTimeouts` in `backend/internal/app/app_test.go`.

---

## Phase 13: Build & Bundle Optimization (Tooling)

### Step 13.1: Enable Go Build Cache in CI ✅

**Files:** `.github/workflows/build-release-artifacts.yml`, `.github/workflows/publish-release.yml`
**What it does:** `actions/setup-go@v5` already cached both `~/go/pkg/mod` and `~/.cache/go-build` by default. However, `cache-dependency-path: desktop/go.mod` was wrong: it used `go.mod` (not `go.sum`) and omitted `backend/go.sum`, so backend dep changes didn't invalidate the cache.
**Optimization:** Changed `cache-dependency-path` to include both `desktop/go.sum` and `backend/go.sum`. Cache key now covers both modules and uses checksums (go.sum) instead of declarations (go.mod).
**Actual result:** Cache-key correctness fix. Local proxy: cold build 4.69s → warm build 0.32s (93% reduction), demonstrating the build-cache effect the CI config now correctly preserves across runs. Real CI speedup is CI-only and requires a run with primed cache to measure.
**How to test:** Inspect CI run after two consecutive pushes — second run's "Set up Go" step should report a cache hit for both `desktop/go.sum` and `backend/go.sum`.

### Step 13.2: Enable Vite Code Splitting and Minification ✅

**Files:** `frontend/vite.config.ts`, `frontend/src/app/App.tsx`
**What it does:** Previously bundled all frontend code into a single 214.69 kB / 65.54 kB gzip chunk with no separation between vendor and app code.
**Optimization:** Added `manualChunks` (function form — required by Vite 8/rolldown) to split react+react-dom into a stable `vendor` chunk. Added `cssCodeSplit: true`. Converted `ProcessingTab` import to `React.lazy()` + `Suspense` so the processing tab chunk is deferred until first use.
**Actual result:** Bundle now splits into 3 JS chunks: `vendor` (139.99 kB / 45.36 kB gzip, stable across app deploys), `index` (65.62 kB / 18.58 kB gzip), `ProcessingTab` (10.36 kB / 3.01 kB gzip, deferred). Initial load: 64.29 kB gzip. Per-deploy incremental re-download (with cached vendor): 18.93 kB gzip vs 65.54 kB baseline — **71% reduction** for returning users after a deploy. Total gzip slightly higher (+1.76 kB) due to rolldown runtime overhead, which is offset by the caching win.
**How to test:** `npm run build` in `frontend/` — output should show `vendor-*.js`, `index-*.js`, and `ProcessingTab-*.js` chunks.

### Step 13.3: Enable TypeScript Incremental Compilation ✅

**File:** `frontend/tsconfig.json`
**What it does:** TypeScript recompiles everything on each `tsc --noEmit`.
**Optimization:** Added `"incremental": true` to cache compilation state. TypeScript writes a `tsconfig.tsbuildinfo` file alongside the config (already covered by `frontend/*.tsbuildinfo` in `.gitignore`).
**Actual result:** Incremental type-check: 0.980s → 0.507s — **48% faster** on unchanged builds. First run (cache write): ~1.0s (same as before). Subsequent runs with no source changes: ~0.5s.
**How to test:** Run `npx tsc --noEmit` twice in `frontend/` — second run should be ~2x faster than first.

### Step 13.4: Parallelize Build Steps ✅

**File:** `frontend/package.json`, `frontend/scripts/parallel-build.mjs`
**What it does:** `tsc --noEmit && vite build` runs sequentially.
**Optimization:** Added `parallel-build.mjs` that spawns `tsc --noEmit` (output buffered) and `vite build` (stdio inherited) concurrently via `Promise.all`. Both `build` and `wails:build` scripts updated. Build fails if either process exits non-zero.
**Actual result:** Warm build: 0.558s → 0.399s — **28% faster**. Cold build: 1.042s → 0.882s — **15% faster**. Warm case dominates dev iteration loops.
**How to test:** Run `node ./scripts/parallel-build.mjs` in `frontend/` — vite output streams live while tsc runs concurrently; tsc errors flush after vite completes.

### Step 13.5: Tree-Shake Unused Exports ✅

**File:** `frontend/src/lib/runtime.ts:195-204`
**What it does:** Re-exports many symbols. Some may be unused.
**Optimization:** Audited with `knip`. Removed 4 unused value re-exports (`buildMockPath`, `MOCK_DICOM_PATH`, `MOCK_EXPORT_DIRECTORY`, `paletteLabel`) and 3 unused type re-exports (`BackendAPI`, `RuntimeAdapter`, `ShellAPI`) from `runtime.ts`. Also removed now-dead `import { … } from "./mockRuntime"` block and `paletteLabel` from the `./backend` import.
**Actual result:** Bundle size unchanged (65.62 kB / 18.58 kB gzip for index chunk) — Vite/Rolldown already tree-shook these symbols at bundle time. Win is cleaner public API surface: `runtime.ts` now re-exports only 3 values actually consumed by other modules (`FALLBACK_PROCESSING_MANIFEST`, `buildOutputName`, `ensureDicomExtension`). `knip` reports zero unused exports for `runtime.ts`.
**How to test:** `npx knip --reporter compact` in `frontend/` — no entries for `src/lib/runtime.ts`. `node ./scripts/parallel-build.mjs` — clean build, no type errors.

---

## Priority Matrix

| Phase | Impact | Effort | Risk | Priority |
|-------|--------|--------|------|----------|
| 3.2 (PNG BestSpeed) | **Very High** | Low | None | **P0** |
| 1.1 (Window LUT) | **Very High** | Medium | Low | **P0** |
| 2.2 (Eliminate preview clone) | **High** | Low | None | **P0** |
| 5.2 (Stream min/max) | **High** | Low | None | **P0** |
| 2.1 (sync.Pool for buffers) | **High** | Medium | Low | **P1** |
| 4.1 (Fuse blur passes) | **High** | Medium | Low | **P1** |
| 5.1 (Zero-copy U16 decode) | **High** | Medium | Medium | **P1** |
| 6.1 (LRU eviction) | **High** | Medium | Low | **P1** |
| 3.1 (Buffered PNG write) | Medium | Low | None | **P1** |
| 2.4 (Preallocate DICOM buffer) | Medium | Low | None | **P1** |
| 9.1 (Memoize selectors) | Medium | Low | None | **P1** |
| 9.2 (Skip redundant updates) | Medium | Low | None | **P1** |
| 9.6 (Fine-grained selectors) | **High** | Low | None | **P1** |
| 9.4 (Memoize AnnotationLayer) | **High** | Medium | None | **P1** |
| 10.2 (SSE for job updates) | **High** | High | Medium | **P2** |
| 10.3 (Batch job polling) | **High** | Medium | Low | **P2** |
| 10.4 (Cache-Control headers) | Medium | Low | None | **P2** |
| 1.4 (Loop unrolling) | Medium | Medium | Low | **P2** |
| 1.5 (Batch uint64 ops) | Medium | Medium | Medium | **P2** |
| 4.2 (Integer blur) | Medium | Medium | Low | **P2** |
| 4.4 (Morphological sliding window) | Medium | Medium | Low | **P2** |
| 6.3 (RWMutex for cache) | Medium | Low | None | **P2** |
| 11.3 (TTL artifact checks) ✅ | Medium | Low | None | **P2** |
| 12.1 (Reduce sidecar serialization) ✅ | Medium | Medium | Low | **P2** |
| 9.5 (GPU transforms) | Medium | Low | None | **P2** |
| 7.1 (Pool JSON) | Low | Low | None | **P3** |
| 7.3 (Skip extra decode) | Low | Low | None | **P3** |
| 9.3 (Batch state updates) | Low | Medium | Low | **P3** |
| 9.7 (Debounce controls) | Low | Low | None | **P3** |
| 9.8 (Container event listeners) | Low | Low | None | **P3** |
| 12.2 (HTTP connection pooling) ✅ | Low | Low | None | **P3** |
| 12.3 (HTTP server timeouts) ✅ | Low | Low | None | **P3** |
| 13.2 (Vite code splitting) ✅ | Medium | Low | None | **P3** |
| 13.3 (TS incremental) ✅ | Medium | Low | None | **P3** |
| 13.4 (Parallel builds) ✅ | Medium | Low | None | **P3** |

---

## Estimated Aggregate Impact

Implementing all **P0 + P1** optimizations should yield:
- **Render job latency:** ~4-6x faster (dominated by PNG encoding + pixel processing)
- **Process job latency:** ~2-3x faster (pixel processing + PNG encode)
- **Analyze job latency:** ~2x faster (blur fusion + morphological optimization)
- **Memory usage:** ~40-60% reduction in peak allocation per job cycle
- **GC pressure:** ~60-80% reduction via sync.Pool and eliminated clones
- **Cache efficiency:** ~20-40% better hit rate with LRU eviction
- **Frontend re-renders:** ~50-70% fewer during job polling (fine-grained selectors + memoization)

Adding **P2** optimizations further yields:
- **Network requests:** ~60-80% fewer during multi-job polling (batch endpoint + SSE)
- **Viewer smoothness:** Noticeable improvement from GPU transforms + annotation memoization
- **Asset re-transfers:** ~50-80% reduction with Cache-Control headers

The single highest-impact change is **Step 3.2 (PNG BestSpeed)**, which alone could cut render job latency by 50-70% since PNG encoding typically dominates the render path.

---

## How to Validate

For each backend optimization, create a Go benchmark test (`*_bench_test.go`) that measures the before/after. Run with:
```bash
go -C backend test ./internal/render -bench=BenchmarkRenderGrayscalePixels -benchmem -count=5
```

For frontend optimizations, use:
- **React DevTools Profiler** -- measure render counts and durations
- **Chrome DevTools Performance tab** -- measure layout/paint/composite times
- **`npm run build && ls -la dist/assets/`** -- compare bundle sizes

For end-to-end validation:
```bash
npm run release:smoke  # Existing E2E validation
```
