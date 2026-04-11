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

### Step 3.3: Consider Using JPEG for Preview Artifacts

**File:** `backend/internal/render/preview_png.go`
**What it does:** All previews are saved as PNG.
**Optimization:** For grayscale previews displayed in the viewer (not DICOM export), encode as JPEG quality 92. JPEG encoding is 5-10x faster than PNG for grayscale images and produces smaller files. PNG would remain for any pixel-exact processing paths.
**Expected improvement:** ~5-10x speedup on preview artifact creation. Requires frontend changes to handle both formats.
**How to test:** Benchmark JPEG vs PNG encode for typical dental radiograph sizes.

---

## Phase 4: Analysis Pipeline Optimization (Backend)

### Step 4.1: Fuse Gaussian Blur Passes

**File:** `backend/internal/analysis/analysis.go:666-712`
**What it does:** `gaussianBlurGray` performs a 2-pass separable convolution (horizontal then vertical). `AnalyzeGrayscalePixels` calls it TWICE (sigma=1.4 and sigma=9.0), meaning 4 full image scans.
**Optimization:** Fuse the horizontal passes of both blurs into a single scan over the source pixels. Read each pixel once, compute both kernel convolutions, write to two transient buffers. Then fuse the vertical passes similarly. This reduces memory bandwidth from 4 full reads + 4 full writes to 2 reads + 4 writes.
**Expected improvement:** ~30-40% speedup on the analysis blur stage by halving memory reads (which are the bottleneck on large images).
**How to test:** `BenchmarkAnalyzeGrayscalePixels` before/after.

### Step 4.2: Use Integer Approximation for Small Gaussian Blur

**File:** `backend/internal/analysis/analysis.go:666-712`
**What it does:** Blur uses float32 accumulation for kernel weights.
**Optimization:** For sigma=1.4 (kernel size ~5), use fixed-point integer arithmetic with a [1, 4, 6, 4, 1]/16 binomial approximation. This avoids all float operations and uses only shifts and adds.
**Expected improvement:** ~2x speedup on small blur pass. Integer ops are cheaper than float on most architectures, and the quality difference is invisible for threshold-based tooth detection.
**How to test:** Compare blur output images pixel-by-pixel, verify max difference <= 1. Benchmark both.

### Step 4.3: Skip Analysis on Cached Source Preview

**File:** `backend/internal/jobs/service.go` (executeAnalyzeJob)
**What it does:** The analyze job always decodes the full DICOM, renders a preview, then analyzes. If a source preview is already cached, it still decodes the full source image.
**Optimization:** When `loadOrRenderSourcePreview` gets a cache hit, the DICOM pixel decode was wasted. Restructure so the analyze path checks for a cached source preview FIRST and only decodes the DICOM if needed (for measurement scale metadata). The metadata is much smaller and could be cached separately.
**Expected improvement:** Eliminates redundant ~100-500ms DICOM decode on cached source preview hits.
**How to test:** Integration test: open study, render, then analyze. Second analyze should skip decode.

### Step 4.4: Optimize Morphological Operations

**File:** `backend/internal/analysis/analysis.go:271-323`
**What it does:** `dilateBinaryMask` and `erodeBinaryMask` use 3x3 structuring elements with a nested 4-level loop (y, x, ny, nx).
**Optimization:** Use a sliding-window approach. For each row, maintain a running max/min of the 3-pixel window. Process horizontal first, then vertical. This reduces the inner operation from 9 comparisons per pixel to ~3. Also, represent the bool mask as `[]uint8` (0/1) to enable batch operations.
**Expected improvement:** ~2-3x speedup on morphological operations. For a 3M pixel mask, this saves ~18M comparisons per open/close cycle.
**How to test:** `BenchmarkMorphologicalOps` with a realistic mask.

### Step 4.5: Avoid Allocating Per-Candidate Pixel Slices

**File:** `backend/internal/analysis/analysis.go:349`
**What it does:** `collectCandidates` allocates a `[]int` pixel slice for every connected component, then stores it in the candidate. These slices can hold thousands of pixel indices.
**Optimization:** Only store the bounding box + area + statistics (intensitySum, toothnessSum) during flood fill. The full pixel list is only needed later for `geometryFromPixels` on selected candidates. Defer pixel collection to a second pass only for candidates that pass the area>150 filter.
**Expected improvement:** Eliminates allocation of pixel slices for dozens of small/rejected components. Saves ~100KB-1MB of allocations per analysis.
**How to test:** Benchmark analysis and compare allocs/op.

---

## Phase 5: DICOM Decode Optimization (Backend)

### Step 5.1: Use `binary.Read` with Preallocated Buffer

**File:** `backend/internal/dicommeta/decode.go:683-697`
**What it does:** `readU16Samples` and `readU32Samples` create new slices and manually decode bytes in a loop.
**Optimization:** For little-endian byte order (the most common DICOM transfer syntax), use `unsafe.Slice` to reinterpret the byte array directly as `[]uint16` or `[]uint32` without any copying. This eliminates the decode loop entirely. For big-endian, batch-swap bytes.
**Expected improvement:** ~3-5x speedup on pixel data decode for native (uncompressed) DICOM. Eliminates the decode allocation entirely.
**How to test:** `BenchmarkDecodeNativePixelData` with a 2048x1536 16-bit image.

### Step 5.2: Stream Min/Max Computation into Decode Loop

**File:** `backend/internal/dicommeta/decode.go:573-597`
**What it does:** `buildSourceImage` iterates over all pixels a second time to find min/max values.
**Optimization:** Compute min/max during the initial decode loop (in `decodeU8Monochrome`, `decodeU16Monochrome`, `decodeU32Monochrome`). Return them alongside the pixel slice. This eliminates one full scan of the pixel array.
**Expected improvement:** ~15-20% speedup on DICOM decode by eliminating redundant iteration over 3-12M floats.
**How to test:** Benchmark `DecodeFile` end-to-end.

### Step 5.3: Use `mmap` for Large DICOM Files

**File:** `backend/internal/dicommeta/decode.go:124-145`
**What it does:** `DecodeFile` opens the file and reads through it sequentially.
**Optimization:** Memory-map the file with `syscall.Mmap` for files larger than 1 MB. This lets the OS manage I/O buffering optimally and avoids userspace read copies. The decoder already uses `io.ReaderAt` + `io.Seeker`, which maps cleanly to mmap'd memory.
**Expected improvement:** ~10-20% speedup on large DICOM file decode (>5 MB) by eliminating userspace buffering overhead.
**How to test:** Benchmark `DecodeFile` with the sample dental radiograph.

### Step 5.4: Use `io.ReadFull` Instead of `readValue` for Known Sizes

**File:** `backend/internal/dicommeta/decode.go:234`
**What it does:** `readValue(source, header.length)` allocates a new `[]byte` for each DICOM element value, even for small fixed-size fields (2-4 bytes).
**Optimization:** For known small elements (US, SS, UL, SL - all <= 4 bytes), use a stack-allocated `[4]byte` buffer and `io.ReadFull` instead of heap-allocating. Only heap-allocate for large/variable elements.
**Expected improvement:** Eliminates hundreds of tiny allocations per DICOM parse. ~5% speedup on metadata parsing, significant GC reduction.
**How to test:** Profile DICOM decode with `go test -benchmem`.

---

## Phase 6: Cache & Eviction Improvements (Backend)

### Step 6.1: Replace Map-Based Eviction with LRU

**File:** `backend/internal/cache/memory.go:309-342`
**What it does:** `evictResultLocked` and `evictSourcePreviewLocked` iterate maps and delete a random entry when over capacity. Go map iteration order is randomized, so eviction is essentially random rather than LRU.
**Optimization:** Maintain a doubly-linked list (like `DecodeCache` already does in `studies/decode_cache.go`) for both `entries` and `sourcePreviews`. On access, move to front. On eviction, remove from tail. This ensures the least-recently-used entry is evicted.
**Expected improvement:** Higher cache hit rate, especially under repeated access patterns. A study opened, rendered, processed, then re-rendered will keep its render result in cache instead of potentially evicting it randomly. Estimated 20-40% improvement in cache hit rate.
**How to test:** Add a test that opens 33 studies (exceeding maxMemoryCacheEntries=32), then re-accesses the first. With LRU, it should hit. With random eviction, it's unpredictable.

### Step 6.2: Batch Artifact Eviction with Debounce

**File:** `backend/internal/cache/store.go:89-140`
**What it does:** `EvictArtifactsOverLimit` walks the entire artifact directory, stats every file, sorts by mtime, then removes oldest. This is called after every job completion.
**Optimization:** Debounce eviction: only run at most once per 30 seconds. Track approximate total size in memory (increment on write, decrement on eviction) to avoid the directory walk when clearly under limit. Only walk the filesystem when the tracked size exceeds the threshold.
**Expected improvement:** Eliminates expensive `filepath.Walk` + `os.Stat` calls on every job completion. For a cache with 100 artifacts, this saves ~100 stat syscalls per job.
**How to test:** Benchmark `EvictArtifactsOverLimit` with a populated cache directory.

### Step 6.3: Use `RWMutex` for Memory Cache Reads

**File:** `backend/internal/cache/memory.go:148-185`
**What it does:** Both `storeLocked` and `loadLocked` acquire a full `sync.Mutex`. Reads and writes are serialized.
**Optimization:** Change to `sync.RWMutex`. `loadLocked` takes an RLock (allowing concurrent reads). `storeLocked` takes a full Lock. Cache reads happen on every job start (fingerprint check), so concurrent reads matter.
**Expected improvement:** Eliminates read contention under concurrent job starts. ~10-20% throughput improvement when multiple studies are being processed simultaneously.
**How to test:** Benchmark concurrent cache lookups with `b.RunParallel`.

---

## Phase 7: HTTP Transport Optimization (Backend)

### Step 7.1: Pool JSON Encoders/Decoders

**File:** `backend/internal/httpapi/router.go:216-223, 337-355`
**What it does:** Every request creates `json.NewEncoder(writer)` and `json.NewDecoder(request.Body)`. These allocate internal buffers.
**Optimization:** Use `json.Marshal` + a single `writer.Write()` for responses (avoids encoder allocation). For request decoding, the decoder is harder to pool since it reads from the body, but we can pre-read the body into a pooled buffer and use `json.Unmarshal`.
**Expected improvement:** ~5% per-request overhead reduction. Minor but it adds up under polling.
**How to test:** `BenchmarkHandleGetJob` with a realistic payload.

### Step 7.2: Cache Runtime/Health Responses

**File:** `backend/internal/httpapi/router.go:74-86`
**What it does:** `/healthz` and `/api/v1/runtime` build and serialize the `runtimeResponse` struct on every call.
**Optimization:** Cache the JSON response bytes and only regenerate when the underlying data changes (study count changes, etc.). Most fields are static after startup. Use an `atomic.Value` to store the cached response with lock-free reads.
**Expected improvement:** ~95% reduction in healthz/runtime response time. Useful if monitoring tools poll frequently.
**How to test:** Benchmark `/healthz` endpoint.

### Step 7.3: Skip Extra JSON Decode Verification

**File:** `backend/internal/httpapi/router.go:345-353`
**What it does:** After decoding the command payload, `decodeJSONRequest` tries to decode again to check for trailing content. This second decode attempt touches the rest of the request body.
**Optimization:** Use `json.Unmarshal` on a pre-read body byte slice instead. Check for trailing content with a simple byte scan for non-whitespace after the JSON value, instead of running a full decoder pass.
**Expected improvement:** ~10% speedup on command request parsing by eliminating the second decode pass.
**How to test:** Benchmark `decodeJSONRequest` with typical payloads.

---

## Phase 8: DICOM Export Optimization (Backend)

### Step 8.1: Preallocate Element Map

**File:** `backend/internal/export/secondary_capture.go:101`
**What it does:** `elements := make(map[uint32]element)` with default capacity. Then ~25 elements are inserted.
**Optimization:** `make(map[uint32]element, 32)` to preallocate capacity and avoid map growth.
**Expected improvement:** Minor - eliminates 2-3 map rehashes. ~1-2% speedup on export.
**How to test:** Benchmark with benchmem.

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
