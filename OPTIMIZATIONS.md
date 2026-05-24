# xrayview — Performance & Memory Optimization Notes

A deep scan of the backend (`backend-rs/`), Tauri shell (`desktop-tauri/`), and
frontend (`frontend/`) focused on the project's stated priorities: **speed,
performance, and low memory usage**. Each item lists where it is, what's there
now, the change, and why it helps. Items are grouped by tier (impact × effort).

The backend is CPU- and memory-bound on three hot paths — **decode**, **render/process**,
and **analyze** — and none of them are parallelized today. That, plus a
size-optimized release profile, is where the largest wins are.

| # | Optimization | Area | Impact | Effort |
|---|---|---|---|---|
| 1 | Release profile `opt-level = "s"` → `3` | build | ★★★ | trivial |
| 2 | Parallelize analysis hot loops (rayon) | analysis | ★★★ | medium |
| 3 | Compute `bone_section` mask once, not twice | analysis | ★★★ | low |
| 4 | `open_study` metadata-only header parse | bmp/app | ★★★ | low |
| 5 | Decode to `Vec<u8>` + 256-entry LUT (drop `Vec<f32>`) | bmp | ★★ | low |
| 6 | Compute `normalize_gray` once; make it a LUT | analysis | ★★ | low |
| 7 | Drop redundant `to_vec()` in `save_gray_bmp`/`encode_*` | render | ★★ | trivial |
| 8 | Hoist `bits_per_pixel` branch out of decode loop | bmp | ★★ | low |
| 9 | Isolate the draft line; stop rebuilding all SVG nodes per mousemove | viewer | ★★ | low |
| 10 | Bone exemplar: dimension pre-filter before full-image hash | analysis | ★★ | trivial |
| 11 | Reuse mask scratch buffers / consider bitsets | analysis | ★★ | medium |
| 12 | rAF-throttle `updateCanvas` + cache canvas rect | viewer | ★ | low |
| 13 | Decode source once; derive render & analysis previews from it | app | ★★ | medium |
| 14 | Fast-path progress/clock updates (skip full HTML rebuild) | frontend | ★ | medium |
| 15 | Misc micro-allocs (compare buffer, RGBA fill, byte pushes) | various | ★ | low |
| 16 | Tooth feature table: 67 MB + per-pixel binary search (locality) | analysis | note | high |

---

## Tier 1 — Highest impact

### 1. Release profile optimizes for size, not speed (COMPLETE)

`desktop-tauri/Cargo.toml:21`

```toml
[profile.release]
opt-level = "s"   # ← size-optimized
lto = true
codegen-units = 1
strip = true
```

There is **no Cargo workspace**, so when `tauri build` compiles `desktop-tauri`
(the actual shipped product), this profile is applied to the **entire dependency
graph including `backend-rs`**. The CPU-bound decode/render/analysis loops are
therefore compiled size-first, which suppresses the inlining, loop unrolling, and
auto-vectorization that tight numeric loops depend on. (`npm run backend:build`
builds the library standalone with no `[profile.release]` override, so it gets
cargo's default `opt-level = 3` — meaning the CLI is *faster* than the shipped
desktop backend today.)

**Change:** set `opt-level = 3` for release. Keep `lto = true` and
`codegen-units = 1` (both good for perf). This is the single highest
return-on-effort change in the repo and directly serves the engineering priority
of weighting performance heavily.

Better still, introduce a root **workspace** so the profile, `lto`, and
dependency versions are shared and consistent across both crates and any future
benchmarks:

```toml
# / Cargo.toml (new)
[workspace]
members = ["backend-rs", "desktop-tauri"]
resolver = "2"

[profile.release]
opt-level = 3
lto = true
codegen-units = 1
strip = true
```

Caveats:
- `panic = "abort"` would shrink the binary and remove unwinding tables, but jobs
  run on spawned threads (`app.rs:217,300,383`); with `abort`, a panic decoding a
  malformed image would kill the whole app instead of failing one job. Only adopt
  it together with `std::panic::catch_unwind` around each job body.
- `target-cpu=native` (via `RUSTFLAGS`) helps locally but is not portable for
  distributed binaries — reserve it for self-built installs.

---

### 2. The analysis pipeline is entirely single-threaded

`backend-rs/src/analysis.rs` — confirmed no `rayon`/`par_iter`/threads anywhere.

The dominant analysis cost is per-pixel work over the whole image:

- `learned_tooth_scores` (`analysis.rs:1053`): for **every pixel**, compute 18
  features and evaluate **every tree** in the gradient-boosted model, summing.
  This is O(pixels × trees) and is the heaviest loop in the program.
- `detect_bone_feature_table_mask` (`analysis.rs:250`): per-pixel hashmap lookup
  over the full image.
- `box_blur_gray` (`analysis.rs:932`, called 3×), `gradient_gray`, and the
  morphology column/row passes.

These loops are embarrassingly parallel — each output pixel/row is independent of
the others (inputs are read-only: `normalized`, `blur3`, `blur21`, `gradient`,
`trees`). On a multi-megapixel bitewing this is where seconds go.

**Change:** add `rayon` and parallelize over output rows. Example for the scores
loop:

```rust
use rayon::prelude::*;

scores
    .par_chunks_mut(width)
    .enumerate()
    .for_each(|(y, row)| {
        for (x, slot) in row.iter_mut().enumerate() {
            let index = y * width + x;
            let features = learned_features(
                x, y, width, height,
                normalized[index], blur3[index], blur21[index], gradient[index],
            );
            *slot = trees
                .iter()
                .map(|tree| LEARNED_MODEL_LEARNING_RATE * evaluate_learned_tree(tree, features))
                .sum();
        }
    });
```

Apply the same `par_chunks_mut(width)` pattern to the bone feature-table loop and
the box-blur passes (horizontal parallelizes over rows, vertical over columns).
Near-linear speedup with core count on the analysis job. If you want to avoid a
dependency, `std::thread::scope` with manual row ranges gets most of the win, but
`rayon` load-balances and is far simpler.

---

### 3. The bone-section mask is computed twice per analysis

`analysis.rs:307` and `analysis.rs:353`

`generate_tooth_overlay` builds two outputs — the outline preview and the filled
preview — and **both** call `bone_section_mask_with_ignored_cutouts(gray,
bone_mask, tooth_mask, w, h)` with **identical arguments**:

```rust
// overlay_outline_preview
let bone_section = bone_section_mask_with_ignored_cutouts(gray, bone_mask, tooth_mask, w, h);
...
// overlay_filled_preview  — same inputs, same (pure) function → same result
let bone_section = bone_section_mask_with_ignored_cutouts(gray, bone_mask, tooth_mask, w, h);
```

That function is the heaviest post-processing step: it allocates a fresh
`MaskBuffers` (3 × full `Vec<bool>`), runs **two dilations** (radius up to 24), a
morphological close (dilate+erode), `fill_holes`, `remove_small_components`, and a
border clear. Running it twice roughly doubles the overlay cost for no reason.

**Change:** compute `bone_section` once in `generate_tooth_overlay` and pass it
into both `overlay_outline_preview` and `overlay_filled_preview`. Roughly halves
overlay post-processing time and the associated allocations.

---

### 4. `open_study` fully decodes every pixel just to read dimensions (COMPLETE)

`bmp.rs:36-58`, used at `app.rs:484` and `cli.rs:187,443`

```rust
pub fn read(bytes: &[u8]) -> Result<Metadata, String> {
    let image = decode_bmp(bytes)?;   // decodes ALL pixels into a Vec<f32>...
    Ok(Metadata { rows: image.height as u16, columns: image.width as u16, ... })
    //            ...then throws every pixel away
}
```

`Metadata` only needs `rows`, `columns`, `samples_per_pixel`, `bits_allocated`,
and `photometric_interpretation` — all derivable from the ~54-byte BMP header.
`measurement_scale()` always returns `None` (`bmp.rs:17`). Yet every
`open_study` decodes the entire image (and allocates a full `Vec<f32>`) only to
discard the pixels. Worse: the render job that almost always follows decodes the
**same file again**, so the open→render flow performs **two full decodes**.

**Change:** add a header-only parser that returns `Metadata` without the pixel
loop, and call it from `open_study`/CLI describe paths:

```rust
pub fn read_header(bytes: &[u8]) -> Result<Metadata, String> {
    // validate "BM", read width @18, height @22, bits_per_pixel @28 only
    let bits = read_le_u16_at(bytes, 28)?;
    let samples_per_pixel = if bits == 8 { 1 } else { 3 };
    Ok(Metadata {
        rows: height, columns: width, samples_per_pixel,
        bits_allocated: 8, bits_stored: 8,
        photometric_interpretation: if samples_per_pixel == 1 { "MONOCHROME2" } else { "RGB" }.into(),
    })
}
```

Eliminates a full decode + large allocation on every file open, cutting the
common open→render sequence from two decodes to one.

---

## Tier 2 — High impact

### 5. Decode stores 8-bit pixels as `Vec<f32>` (4× memory, slow mapping) (COMPLETE)

`bmp.rs:156` (`pixels: Vec<f32>`), `bmp.rs:233`, `bmp.rs:104-138`

BMP source data is always 8-bit (palette gray, or RGB→gray via `gray_from_rgb8`,
both producing `u8` in `0..=255`). Storing it as `Vec<f32>` uses **4× the memory**
of the data and forces the min/max scan and the mapping loop to run on floats.

Two compounding wins:

1. **Decode into `Vec<u8>`** instead of `Vec<f32>`. The min/max scan becomes a
   trivial byte scan (vectorizable), and the decode buffer drops to ¼ the size —
   meaningful for multi-megapixel images held in the source-preview cache.

2. **Map via a 256-entry LUT.** Because the input domain is exactly `0..=255`,
   both the full-range linear map (`map_linear`) and the 8-bit window
   (`WindowTransform`) are pure functions of the byte value. Precompute a
   `[u8; 256]` once, then map the image with byte lookups instead of per-pixel
   float arithmetic:

   ```rust
   let lut: [u8; 256] = std::array::from_fn(|v| map_linear(v as f32, min as f32, max as f32));
   let pixels: Vec<u8> = image.pixels.iter().map(|&v| lut[v as usize]).collect();
   ```

   Identical output, but the hot pass is now a cache-friendly table lookup. O(256)
   to build + O(n) byte lookups, no floats in the loop.

Validation on `images/BMP/1.bmp` with
`cargo run --release --locked --example render_preview_bench -- ../images/BMP/1.bmp 200`:
before average render time was 4.78-4.93 ms with ~11.3 MB peak RSS; after average
render time was 1.84-1.93 ms with ~8.0-8.2 MB peak RSS.

### 6. `normalize_gray` runs twice and does per-pixel float-free but branchy math (COMPLETE)

`analysis.rs:180` (tooth path) and `analysis.rs:261` (bone path) both call
`normalize_gray(gray)` — a full O(n) pass plus allocation, computed twice per
analysis with the same input.

**Change:**
- Compute `normalize_gray` **once** in `generate_tooth_overlay` and thread the
  result into both detectors.
- Make it a **256-entry LUT** as well: the mapping depends only on `(low, high)`,
  so build `[u8; 256]` once and apply it in a single pass instead of the
  per-pixel compare/multiply/divide at `analysis.rs:896-907`.

Validation on `images/BMP/1.bmp` with a one-byte preview mutation to bypass the
bone exemplar shortcut and exercise the full tooth+bone detector path:
`cargo run --release --locked --example analyze_preview_bench -- ../images/BMP/1.bmp 20`.
Before average overlay analysis time was 638.985 ms with ~182.6 MB peak RSS;
after average overlay analysis time was 627.315 ms with ~183.0 MB peak RSS.

### 7. `save_gray_bmp` / `encode_gray_bmp` copy the whole image before encoding (COMPLETE)

`render.rs:43-54`

```rust
pub fn save_gray_bmp(path, width, height, pixels: &[u8]) -> ... {
    save_preview_bmp(path, &PreviewImage::gray(width, height, pixels.to_vec()))
    //                                                         ^^^^^^^^^^^^^ full copy
}
```

The render job calls this with the cached `Arc<[u8]>` pixels (`app.rs:555,913`),
so **every render-to-disk copies the entire image** just to wrap it in a
`PreviewImage`, even though `encode_gray8_bmp` already takes `&[u8]`.

**Change:** validate length and call `encode_gray8_bmp(width, height, pixels)`
directly — no intermediate owned `PreviewImage`, no copy.

### 8. The `bits_per_pixel` branch is inside the per-pixel decode loop (COMPLETE)

`bmp.rs:242-273`

```rust
for x in 0..width {
    pixels[output_y * width + x] = match bits_per_pixel {  // ← invariant, re-checked per pixel
        8 => ...,
        24 | 32 => ...,
        _ => unreachable!(),
    };
}
```

`bits_per_pixel` is constant for the whole image, yet it's matched on every
pixel, and the palette bounds check (`bmp.rs:248`) is re-evaluated per pixel.

**Change:** branch once outside the loops and run specialized row loops for
8-bit gray, 8-bit palette, 24-bit BGR, and 32-bit BGRA. The 8-bit palette path
builds a 256-entry grayscale LUT up front, validates short palettes before
decoding, and fast-paths identity grayscale palettes to a row copy. The 24-bit
and 32-bit paths use fixed-width chunks, so the hot loop no longer rematches
`bits_per_pixel` or recomputes bytes-per-pixel per pixel.

Validation on `images/BMP/1.bmp` (32-bit, 1,024,800 pixels) with
`cargo run --release --locked --example render_preview_bench -- ../images/BMP/1.bmp 500`:
before average render time was 1.738-1.750 ms with ~8.2 MB peak RSS; after
average render time was 1.445-1.463 ms with ~8.2 MB peak RSS. The midpoint moved
from ~1.744 ms to ~1.452 ms, a ~16.7% speedup for render-preview decode/map on
this fixture.

### 9. Drawing/editing an annotation rebuilds *every* SVG node each mousemove

`frontend/src/features/viewer/ViewerController.ts:529-535`

During a draw/edit drag, `handlePointerMove` mutates `draftLine` and calls
`updateCanvas` → `syncAnnotationLayer`. Because `draftKey` changes on every move,
the guard is bypassed and:

```rust
contentGroup.replaceChildren(
  ...this.buildAnnotationNodes(annotations, selectedAnnotationId, draftLine, draftLineOverride),
);
```

`buildAnnotationNodes` destroys and recreates **all** rectangles, polylines,
*every* line annotation, the label `<text>` nodes, and handles — at pointer-event
frequency (~60–120 Hz). The only thing actually changing is the draft line's
endpoints. On a study with many annotations this is O(N) DOM churn per move.

**Change:** give the draft line (and the dragged endpoint's two handles) their own
dedicated SVG nodes created once at drag start, and update just their
`x1/y1/x2/y2`/`cx/cy` attributes during the move. Leave the static annotation
nodes untouched until the annotation set actually changes. Big smoothness win when
annotations are present.

### 10. Bone-exemplar lookup hashes the entire image on every analysis

`analysis.rs:1218-1297`

```rust
fn bone_exemplar_mask(gray, width, height) -> Option<Vec<bool>> {
    let exemplars = loaded_bone_exemplar_model()?;
    let hash = hash_bone_exemplar_pixels(gray, ...);  // FNV over EVERY pixel, every time
    // ...then exact (hash,width,height) match
}
```

The exemplar table is a memorized exact-match lookup; for arbitrary user images it
essentially always misses, but it still pays a full-image hash (an extra O(n)
pass) before real detection begins. A match additionally requires
`exemplar.width == width && exemplar.height == height` (`analysis.rs:1226`).

**Change:** pre-filter by dimensions before hashing. If no exemplar shares the
image's `(width, height)`, the pixel hash cannot match — skip it entirely:

```rust
if !exemplars.iter().any(|e| e.width == width as u32 && e.height == height as u32) {
    return None;   // avoids the full-image hash for the common case
}
let hash = hash_bone_exemplar_pixels(gray, width as u32, height as u32);
```

Also avoids the `.iter().map(|v| *v != 0).collect()` `u8`→`bool` mask copy on the
miss path.

---

## Tier 3 — Worthwhile

### 11. Analysis allocates many full-size masks; consider buffer reuse / bitsets

`analysis.rs` — `MaskBuffers::new(len)` (`:99`) allocates 3 full `Vec<bool>`, and
helpers like `inner_outline_mask` (`:469`), `centered_outline_mask` (`:488`),
`close_mask_into` (`:760`), and `bone_section_mask_with_ignored_cutouts` (`:405`)
each allocate fresh buffers and often return `.clone()`/`.to_vec()` of a full mask
(`:215`, `:455`, `:279-289`). For a multi-MP image these are megabytes
allocated/freed repeatedly within one analysis.

**Change (incremental):** thread a single reusable scratch pool through the
pipeline instead of allocating per helper, and return masks by writing into a
caller-provided buffer rather than cloning.

**Change (larger, bigger payoff):** represent masks as a **bitset** (`Vec<u64>`
words) instead of `Vec<bool>`. That's 8× less memory and lets dilation/erosion
window counting and the AND/OR composites operate a word at a time. This is the
natural follow-on once buffer reuse is in place.

### 12. Pointer interactions aren't frame-throttled and read layout per event

`ViewerController.ts:48` and `:366,378,388`

- `pointerToLocalPoint` calls `getBoundingClientRect()` on **every** pointer event
  — a synchronous layout read that, interleaved with the style writes in
  `updateCanvas` (`:685-688`), can cause layout thrash.
- `handlePointerMove` calls `updateCanvas` synchronously for each event rather
  than coalescing to one update per frame.

**Change:** cache the canvas rect on `pointerdown`/resize and reuse it during the
drag; and coalesce `updateCanvas` through `requestAnimationFrame` so multiple
moves in a frame produce a single style/DOM update. Pairs naturally with #9.

### 13. Render and analysis decode the same file separately

`app.rs:690` (`load_source_preview`) and `app.rs:703` (`load_analysis_preview`)

The source-preview cache stores the *post-transform* preview under two different
keys (render uses full-range normalization; analysis uses an 8-bit-preserving
window). If a study is both rendered and analyzed, the file is read and decoded
**twice**.

**Change:** cache the **raw decoded grayscale `Vec<u8>`** keyed by file identity,
and derive both the render-normalized and analysis-preserved previews from it via
the 256-entry LUTs from #5/#6. Decodes the file once regardless of how many
transforms are requested.

### 14. Every state change rebuilds the whole UI HTML string and re-parses it

`frontend/src/app/htmxApp.ts:105-151`, `:369-384`

`render()` calls `renderApp(state, ui, Date.now())` to build the **entire** app
HTML string, then `patchAppShellPreservingViewer` parses it into a detached
`<template>` and selectively patches. For high-frequency updates — job progress
(polled every ~200–500 ms) and the **1-second clock tick** that triggers a full
`render()` while a job is live (`syncClock`, `:372`) — this rebuilds and re-parses
the whole document just to update a few text nodes (ETA, percent, status).

**Change:** add a fast path for progress/clock-only deltas that updates the
specific status/progress text nodes directly. The patch helpers already exist
(`patchStatusBar`, `patchAnalysisProgress`, `patchAnalysisProgressBadge`) — drive
those for timing-only updates without calling `renderApp`/building the full
string. Cuts per-tick CPU and GC pressure during active jobs.

### 15. Small allocation/copy micro-tweaks

- **`combine_comparison`** (`processing.rs:233`): `vec![0; combined_width *
  height * 4]` zero-initializes a buffer whose every byte is then overwritten
  (left half + right half cover the full width). Build it with
  `Vec::with_capacity` + `extend`, or skip the zeroing — saves one full memset.
- **`overlay_filled_preview`** (`analysis.rs:349-351`): allocates `vec![0;
  len*4]` then runs a separate strided loop to set alpha=255 on every pixel.
  Initialize RGBA in one pass (or set alpha during the fills) instead of a
  cache-unfriendly strided write.
- **`encode_rgba8_as_bgr24_bmp`** (`render.rs:143-148`): pushes 3 bytes per pixel
  with three `Vec::push` calls (bounds check each). Write `[b, g, r]` via
  `extend_from_slice`, or write into a preallocated slice by index.
- **`publish_job_update`** (`app.rs:682`): clones the full `JobSnapshot` —
  including the `serde_json::Value` result payload — per subscriber. Wrapping
  `JobResult` in an `Arc` makes the clone a refcount bump. Minor today (usually
  one subscriber), but free insurance.

---

## Tier 4 — Notes / informational

### 16. Tooth feature table: ~67 MB resident + per-pixel binary search

`analysis.rs:61-79`, `:1046`, test asserts **13,441,673 entries**
(`analysis.rs:1599`).

`FeatureProbabilityTable` holds `keys: Vec<u32>` (~53.7 MB) and `probabilities:
Vec<u8>` (~13.4 MB). For each pixel, `detect_learned_tooth_mask` does a
`binary_search` over the 53.7 MB key array — ~24 comparisons spread across a
buffer far larger than L2/L3, so it's a cache-miss per pixel on top of the tree
evaluation.

It's already lazily loaded via `OnceLock` (good — only materialized on the first
analysis), but it then stays resident for the process lifetime. Mitigations, in
order of effort:

- Keep it lazy (done). If analysis is optional in a session, this avoids the
  footprint entirely for view-only users.
- For locality, a more cache-friendly lookup than binary-search-over-53 MB (e.g.,
  bucketed by the high bits of the key, or a perfect hash) would cut the per-pixel
  miss cost — but it requires changing the asset's serialized format.
- These are coupled to the `outputVersion` fingerprint
  (`"sections-reference-mask-v16"`, `app.rs:728`); any change to scoring/binning
  precision (e.g., `f64`→`f32` scores) alters outputs, invalidates caches, and
  will move the model-loading tests. Treat as a deliberate, versioned change.

### Build/frontend minor

- **Vite `build.target`** (`frontend/vite.config.ts:14`) includes `safari13` /
  `chrome105`. This is a Tauri app running in a known, modern WebView
  (WebView2 / WebKitGTK), so targeting old browsers needlessly down-levels syntax
  and disables some optimizations. Raise the baseline to the actual WebView
  floor to ship smaller, faster JS.

---

## Suggested sequencing

1. **#1** (profile) and **#7** (drop `to_vec`) — minutes, immediate broad wins.
2. **#4**, **#5**, **#6**, **#8** — decode/render path; mechanical and high-value.
3. **#3** + **#10** — remove duplicated analysis work before parallelizing.
4. **#2** — parallelize the now-deduplicated analysis loops (biggest analyze win).
5. **#11** — buffer reuse, then bitsets, once the loops are stable.
6. **#9**, **#12** — viewer interaction smoothness.
7. **#13**, **#14** — structural caching / render fast-path.

Validate each step with the team's gate:

```bash
npm run release:smoke
```

Analysis changes (#2, #3, #6, #10, #11) are output-sensitive — the
`generate_tooth_overlay` tests in `analysis.rs` and the
`sections-reference-mask-v16` fingerprint are the guardrails. Keep results
bit-identical unless you intend to bump the output version.
