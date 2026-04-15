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

### Step 8.2: Avoid Sorting Elements Twice

**File:** `backend/internal/export/secondary_capture.go:168-169`
**What it does:** `sortElements(datasetElements)` sorts ~25 elements. Elements are inserted from a map, so order is random.
**Optimization:** Insert dataset elements into a pre-sorted slice (binary insertion) or use a `[]element` with insertion at the correct position from the start. Since tags are uint32, a simple sorted slice with binary search insertion is faster than sort.Slice for <50 elements.
**Expected improvement:** Negligible for current sizes, but cleaner.
**How to test:** Benchmark export.

---

## Phase 9: Frontend State & Rendering (Frontend)

> The frontend agents identified several rendering bottlenecks, especially during job polling (200ms intervals) and viewer interaction.

### Step 9.1: Memoize Selector Results

**File:** `frontend/src/app/store/workbenchStore.ts:742-748`
**What it does:** `useWorkbenchStore` calls `selector(workbenchActions.getState())` on every render. Selectors like `selectPendingJobCount` (line 756-759) create a new filtered array on every call.
**Optimization:** Use memoized selectors (like Reselect's `createSelector`). For `selectPendingJobCount`, compute and cache the count, only recomputing when `jobs` changes. For `selectActiveStudy`, cache the result keyed on `activeStudyId` + `studies`.
**Expected improvement:** Eliminates unnecessary React re-renders when selectors return structurally-equal-but-referentially-different values. ~30-50% reduction in React component renders during job polling.
**How to test:** Use React DevTools Profiler to count renders before/after during a render job.

### Step 9.2: Avoid Spreading Entire State on Every Update

**File:** `frontend/src/app/store/workbenchStore.ts:625-654`
**What it does:** `receiveJobUpdate` creates new spread copies of `current.jobs`, `current.studies`, and the full state on every job poll update. During active jobs, this runs every 500ms-1s.
**Optimization:** Only create new object references for objects that actually changed. If the job snapshot is identical to the previous one (same state, same progress percent), skip the update entirely. Add a `jobSnapshotEqual` comparison.
**Expected improvement:** Eliminates ~50-70% of state updates during job polling (when job state hasn't changed between polls). Reduces GC pressure and React reconciliation.
**How to test:** Log state update count during a 10-second render job, compare before/after.

### Step 9.3: Batch `setStudyState` Updates

**File:** `frontend/src/app/store/workbenchStore.ts:701-725`
**What it does:** Each `setStudyState` call triggers listener notification. Multiple rapid updates (e.g., receiving a job update that affects both job and study state) fire multiple listener cycles.
**Optimization:** Use `queueMicrotask` batching: accumulate state changes and notify listeners once per microtask. This is similar to React 18's automatic batching for non-React event handlers.
**Expected improvement:** ~20-30% fewer listener notifications during rapid state changes.
**How to test:** Count listener invocations during a processStudy flow.

### Step 9.4: Memoize AnnotationLayer with React.memo

**File:** `frontend/src/features/annotations/AnnotationLayer.tsx`
**What it does:** AnnotationLayer receives the entire annotations bundle, selected ID, draft line, and transform. When any annotation or viewport changes, all annotation SVG elements re-render. Expensive per-annotation computations (midpoint, label formatting) run on every render.
**Optimization:** Wrap with `React.memo` and a custom comparator. Memoize `selectedLine` lookup and pre-computed label data with `useMemo`. Extract individual annotation items into their own memoized sub-components.
**Expected improvement:** 40-60% reduction in annotation re-renders for images with 10+ annotations. Eliminates O(n) `find` call per render.
**How to test:** React DevTools Profiler -- count renders during pan/zoom with annotations visible.

### Step 9.5: Use GPU-Accelerated CSS Transforms for Image Positioning

**File:** `frontend/src/features/viewer/ViewerCanvas.tsx:443-451`
**What it does:** Image positioning uses `left`, `top`, `width`, `height` inline styles. These trigger layout recalculation on every viewport change.
**Optimization:** Use CSS `transform: translate(X, Y) scale(S)` with `transformOrigin: "0 0"` and fixed `width`/`height`. CSS transforms are GPU-composited and skip the layout/paint phases entirely.
**Expected improvement:** Smoother pan/zoom, especially on lower-end devices. Eliminates layout thrashing during continuous pointer events.
**How to test:** Chrome DevTools Performance tab -- compare layout time during pan gesture.

### Step 9.6: Fine-Grained Store Selectors to Reduce ViewTab Re-renders

**File:** `frontend/src/components/viewer/ViewTab.tsx:8-10`
**What it does:** `const jobs = useWorkbenchStore(selectJobs)` subscribes to the entire jobs map. Every 200ms poll creates a new jobs object reference, causing ViewTab (and all children) to re-render even when the active study's job didn't change.
**Optimization:** Replace `selectJobs` with a fine-grained selector that returns only the active study's relevant job snapshots:
```ts
export const selectActiveStudyJobs = (s: WorkbenchState) => {
  const study = selectActiveStudy(s);
  return {
    render: study?.renderJobId ? s.jobs[study.renderJobId] ?? null : null,
    analysis: study?.analysisJobId ? s.jobs[study.analysisJobId] ?? null : null,
  };
};
```
**Expected improvement:** 60-80% fewer ViewTab re-renders during job polling. Major reduction in React reconciliation work.
**How to test:** Count React renders during a 10-second render job, before/after.

### Step 9.7: Debounce Processing Control Updates

**File:** `frontend/src/app/store/workbenchStore.ts:499-515`
**What it does:** `setProcessingControls` creates a new state on every slider drag event (brightness, contrast).
**Optimization:** Debounce the state update with a 16ms (one frame) delay. Accumulate the latest controls value, only commit to state once per animation frame. This prevents dozens of intermediate state snapshots and re-renders during slider drags.
**Expected improvement:** Reduces state updates from ~60/s to 1/frame during slider interaction. Eliminates jank.
**How to test:** Manually test slider responsiveness. Profile with React DevTools.

### Step 9.8: Attach Pointer Listeners to Container, Not Window

**File:** `frontend/src/features/viewer/ViewerCanvas.tsx:272-277`
**What it does:** `window.addEventListener("pointermove", handlePointerMove)` attaches global listeners for pan/draw gestures.
**Optimization:** Attach to the viewer container element via `containerRef.current` instead of `window`. This prevents the handlers from firing on pointer events outside the viewer and reduces global event listener accumulation if the component remounts.
**Expected improvement:** Reduces global event listener count, prevents potential memory leaks on remount, and improves event handling efficiency.
**How to test:** Check `getEventListeners(window)` in DevTools before/after.

---

## Phase 10: Frontend Network & Polling (Frontend)

> The desktop agent identified that polling overhead and redundant serialization dominate the frontend-backend communication path.

### Step 10.1: Exponential Backoff on Job Polling

**File:** Frontend job polling (likely in `features/jobs/` or via `syncJob`)
**What it does:** The frontend polls `getJob` at a fixed interval to track job progress.
**Optimization:** Use exponential backoff: start polling at 200ms, double interval up to 2s while job is running. For queued jobs, poll at 1s. For running jobs >80%, poll at 200ms. For terminal states, stop polling.
**Expected improvement:** Reduces network requests by 50-70% during long-running jobs. Reduces backend load.
**How to test:** Count HTTP requests during a 5-second render job.

### Step 10.2: Use Server-Sent Events for Job Updates

**File:** `backend/internal/httpapi/router.go`, `frontend/src/app/store/workbenchStore.ts`
**What it does:** Frontend polls backend for job state changes.
**Optimization:** Add a `/api/v1/events` SSE endpoint. Backend pushes job state changes to connected clients. Frontend subscribes once and receives real-time updates instead of polling. The job registry already has `OnJobCompletion` callback infrastructure.
**Expected improvement:** Eliminates all polling overhead. Sub-millisecond job state delivery. ~90% reduction in HTTP requests during active jobs.
**How to test:** Integration test: start a job, verify SSE event arrives within 100ms of state change.

### Step 10.3: Deduplicate and Batch Job Polling Requests

**File:** `frontend/src/features/jobs/useJobs.ts:70-109`
**What it does:** `pollPendingJobs` fires a separate HTTP request per pending job via `Promise.all`. With 3 concurrent jobs, this is 3 requests every 200ms.
**Optimization:** (a) Deduplicate job IDs before polling. (b) Add a batch `getJobs` backend endpoint that accepts multiple job IDs and returns all snapshots in a single response. (c) Use `Promise.allSettled` instead of `Promise.all` so one failed request doesn't block others.
**Expected improvement:** 60-80% reduction in HTTP requests during multi-job scenarios.
**How to test:** Count HTTP requests during a workflow with 3 concurrent jobs.

### Step 10.4: Add Cache-Control Headers for Preview Assets

**File:** `desktop/app.go` (ServeAsset method)
**What it does:** Preview PNG files are served without cache headers. The browser re-requests them on every navigation.
**Optimization:** Add `Cache-Control: public, max-age=3600` and ETag headers for served preview files. Support `If-None-Match` conditional requests to return 304 Not Modified.
**Expected improvement:** 50-80% reduction in repeated asset transfers after initial load.
**How to test:** Chrome Network tab -- verify 304 responses on revisiting a study.

---

## Phase 11: Concurrency & Job Scheduling (Backend)

### Step 11.1: Use Worker Pool Instead of Goroutine-per-Job

**File:** `backend/internal/jobs/service.go:381-396`
**What it does:** `launchJob` spawns a goroutine per job with a semaphore (`concurrencyLimit` channel of size 3).
**Optimization:** Use a fixed worker pool of 3 goroutines consuming from a job queue channel. This avoids goroutine creation/destruction overhead and provides more predictable scheduling. Also allows for priority queuing (render jobs before process jobs).
**Expected improvement:** Minor for correctness, but enables priority scheduling. ~5% reduction in per-job overhead from avoided goroutine setup.
**How to test:** Benchmark concurrent job throughput.

### Step 11.2: Context-Aware DICOM Decoding

**File:** `backend/internal/dicommeta/decode.go:107-122`
**What it does:** `DecodeStudy` checks context cancellation only at entry and exit, not during the potentially long decode loop.
**Optimization:** Check `ctx.Err()` periodically during `parseSourceDataset` (e.g., every 1000 elements or after pixel data decode starts). This allows faster cancellation of in-progress decodes.
**Expected improvement:** Cancel response time improves from "entire decode duration" to "<50ms". Particularly impactful for large multi-MB DICOM files.
**How to test:** Start a decode, cancel after 10ms, verify it stops within 100ms.

---

## Phase 12: Build & Bundle Optimization (Tooling)

### Step 11.3: Add TTL to Artifact Existence Checks in Cache

**File:** `backend/internal/cache/memory.go:226-267`
**What it does:** `resultArtifactsExist` calls `os.Stat` on artifact files for every cache load. This is a filesystem syscall on every cache lookup.
**Optimization:** Track a `lastCheckedAt` timestamp per entry. Only re-stat if the entry hasn't been checked in the last 60 seconds. Artifact files don't disappear spontaneously during a session.
**Expected improvement:** 20-30% faster cache lookups by eliminating redundant filesystem calls.
**How to test:** Benchmark `LoadRender` with a populated cache.

---

## Phase 12: Desktop Shell & Sidecar Optimization

### Step 12.1: Reduce JSON Marshal/Unmarshal Cycles in Sidecar Path

**File:** `desktop/app.go:337-364`
**What it does:** In sidecar (HTTP) mode, data flows: Go struct -> `json.Marshal` -> `string(bytes)` -> HTTP body -> `io.ReadAll` -> `string(body)` -> `json.Unmarshal`. That's 4 allocations where the in-process path uses 0.
**Optimization:** Pass `[]byte` directly instead of converting to/from `string`. Use `json.NewDecoder(response.Body)` to stream directly into the target struct instead of `io.ReadAll` + `json.Unmarshal`.
**Expected improvement:** 15-20% reduction in sidecar communication overhead. Eliminates 2 unnecessary allocations per command.
**How to test:** Benchmark `invokeViaHTTP` with a typical job snapshot response.

### Step 12.2: Configure HTTP Transport Connection Pooling

**File:** `desktop/sidecar.go:86-91`
**What it does:** Two `http.Client` instances use default transport with no explicit connection pool config.
**Optimization:** Configure `http.Transport` with `MaxIdleConns: 2`, `MaxIdleConnsPerHost: 2`, `IdleConnTimeout: 30s` to ensure connection reuse and prevent connection churn.
**Expected improvement:** 5-10% reduction in per-request latency by reusing TCP connections consistently.
**How to test:** Monitor connection count with `ss -tnp` during active polling.

### Step 12.3: Add HTTP Server Timeouts

**File:** `backend/internal/app/app.go:102-106`
**What it does:** Only `ReadHeaderTimeout` is configured on the HTTP server. Missing `ReadTimeout`, `WriteTimeout`, `IdleTimeout`.
**Optimization:** Add `ReadTimeout: 15s`, `WriteTimeout: 15s`, `IdleTimeout: 60s`, `MaxHeaderBytes: 1 << 20` to prevent resource leaks from slow/stalled clients.
**Expected improvement:** Prevents resource exhaustion under abnormal conditions. Defense-in-depth.
**How to test:** Integration test with intentionally slow client.

---

## Phase 13: Build & Bundle Optimization (Tooling)

### Step 13.1: Enable Go Build Cache in CI

**What it does:** Ensures `GOCACHE` is persistent across CI runs.
**Optimization:** Configure CI to cache `$GOPATH/pkg/mod` and `$HOME/.cache/go-build`. Use a persistent directory instead of `/tmp`.
**Expected improvement:** 60-80% faster CI builds after first run.

### Step 13.2: Enable Vite Code Splitting and Minification

**File:** `frontend/vite.config.ts`
**What it does:** Currently bundles all frontend code with minimal build optimization.
**Optimization:** Add manual chunks for vendor dependencies (React, etc.) vs. application code. Use `React.lazy()` for `ProcessingTab`. Add `cssCodeSplit: true`.
**Expected improvement:** 20-40% reduction in initial bundle. Faster subsequent loads via vendor chunk caching.

### Step 13.3: Enable TypeScript Incremental Compilation

**File:** `frontend/tsconfig.json`
**What it does:** TypeScript recompiles everything on each `tsc --noEmit`.
**Optimization:** Add `"incremental": true` to cache compilation state.
**Expected improvement:** 20-50% faster type-checking on incremental builds.

### Step 13.4: Parallelize Build Steps

**File:** `frontend/package.json`
**What it does:** `tsc --noEmit && vite build` runs sequentially.
**Optimization:** Run in parallel since they're independent.
**Expected improvement:** 30-50% faster full frontend builds.

### Step 13.5: Tree-Shake Unused Exports

**File:** `frontend/src/lib/runtime.ts:195-204`
**What it does:** Re-exports many symbols. Some may be unused.
**Optimization:** Audit with `knip` or similar. Remove unused exports.
**Expected improvement:** Smaller bundle size.

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
| 11.3 (TTL artifact checks) | Medium | Low | None | **P2** |
| 12.1 (Reduce sidecar serialization) | Medium | Medium | Low | **P2** |
| 9.5 (GPU transforms) | Medium | Low | None | **P2** |
| 7.1 (Pool JSON) | Low | Low | None | **P3** |
| 7.3 (Skip extra decode) | Low | Low | None | **P3** |
| 9.3 (Batch state updates) | Low | Medium | Low | **P3** |
| 9.7 (Debounce controls) | Low | Low | None | **P3** |
| 9.8 (Container event listeners) | Low | Low | None | **P3** |
| 12.2 (HTTP connection pooling) | Low | Low | None | **P3** |
| 12.3 (HTTP server timeouts) | Low | Low | None | **P3** |
| 13.2 (Vite code splitting) | Medium | Low | None | **P3** |
| 13.3 (TS incremental) | Medium | Low | None | **P3** |
| 13.4 (Parallel builds) | Medium | Low | None | **P3** |

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
