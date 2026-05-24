# xrayview optimization & idiom-cleanup plan

Scope: concrete, file-level fixes to reduce sloppy code and lean harder on
language features. Ordered by impact, aligned with the project priorities
in `CLAUDE.md` (performance, memory usage, memory safety).

Verify each change with `npm run release:smoke` before declaring it done.

> **Line numbers in this document are as of commit `d74b707`.** They will drift
> as the codebase evolves — before starting any item, `rg` for the symbol or
> snippet rather than trusting the line number blindly.

---

## Phase 1 — Hot-path allocations (highest ROI)

These touch per-job and per-pixel paths. They are the only items in this
plan that should noticeably move a profiler needle.

### 1.1 Share `StudyRecord` via `Arc` instead of cloning out of the registry

- **File:** `backend-rs/src/app.rs` (lines 155–161, 293, 379, 410, 425, 519, 702, 800, 817, 832, 845 — every `studies.lock()…cloned()` site)
- **Today:** Every IPC entry point clones a full `StudyRecord` out from under the mutex so the lock can be released.
- **Prerequisite — audit mutation paths first:** `Arc<T>` only exposes `&T`. Before starting, run:
  ```bash
  rg -n 'studies\.lock\(\)' backend-rs/src/
  rg -n '&mut StudyRecord|\.get_mut\(' backend-rs/src/
  ```
  For every site that mutates a `StudyRecord` after fetching (job state updates, annotation edits, cache invalidation), decide between (a) `Arc<RwLock<StudyRecord>>`, (b) interior mutability inside `StudyRecord` for the mutable fields only, or (c) `Arc::make_mut` clone-on-write. Don't start the refactor until each mutation site has a chosen strategy — discovering them mid-port will block the PR.
- **Fix:** Store `Arc<StudyRecord>` in the registry. The clone-out-of-the-map becomes a refcount bump; the mutex critical section also shrinks.
  ```rust
  // type changes
  studies: Mutex<HashMap<String, Arc<StudyRecord>>>,

  // call site
  let study = self.studies.lock().expect("…").get(study_id).cloned() // Arc clone, cheap
      .ok_or_else(|| BackendError::not_found(format!("study not found: {study_id}")))?;
  ```
- **Effort:** Medium (touches ~10 call sites, plus internal helpers that take `&StudyRecord`).
- **Risk:** Low for read paths — `Arc<T>` derefs transparently to `&T`, so most call sites compile unchanged. Mutation paths drive the real complexity; see the prerequisite.

### 1.2 Stop cloning `Vec<u8>` pixel buffers in the processing pipeline

Split into two sub-items because the right fix depends on whether the source preview is shared with the cache. Audit `app.rs` callers first to decide which sub-item applies to which call site — some may be 1.2a, others 1.2b.

#### 1.2a Take `PreviewImage` by value where the caller drops it anyway

- **File:** `backend-rs/src/processing.rs:92` (`processed_pixels = source_preview.pixels.clone()`)
- **Today:** Every processing job clones the whole grayscale buffer just to feed `process_grayscale_pixels(&mut [u8], …)`, even when the source preview is owned by the caller anyway.
- **Fix:** Change the signature to `source_preview: PreviewImage` (owned). At the call site, pass the buffer by value and let `process_grayscale_pixels` mutate `source_preview.pixels` directly. Where the caller currently does `let source = self.preview_for(…)?; process(&source)?;`, the lifetime ends after the call — pass it by value.
- **Tests:** `cargo test -p backend-rs processing::` — any test that re-uses `source_preview` after calling the processing function will break and must be updated to clone explicitly at the test site.
- **Effort:** Small once the callers are identified.
- **Win:** Drops one full-image allocation per process/analyze job for non-cached source previews.

#### 1.2b Wrap cached pixel buffers in `Arc<[u8]>` for shared ownership

- **Today:** When the cache layer keeps `source_preview` alive across multiple processing jobs, 1.2a's "take by value" is unsafe — we'd be moving a buffer the cache still owns.
- **Fix:** Change `PreviewImage.pixels` from `Vec<u8>` to `Arc<[u8]>` for cache-held previews. The processing pipeline then takes `Arc<[u8]>`, and the only place that pays for a clone is the in-place mutation path, which can use `Arc::make_mut` (clone-on-write).
- **Tests:** Any code that today does `preview.pixels.push(…)` or `&mut preview.pixels` needs to migrate to `Arc::make_mut`. Grep `&mut.*pixels` first to size the change.
- **Effort:** Medium — touches the `PreviewImage` type and any mutation site.
- **Win:** Multiple processing jobs over the same source preview share one allocation.

### 1.3 Reuse the boolean mask buffer in `analysis.rs`

- **File:** `backend-rs/src/analysis.rs` (e.g. `detect_tooth_mask:150`, `dilate_mask:538`, `erode_mask:613`, `fill_holes:644`)
- **Today:** Each morphological step allocates a fresh `Vec<bool>` of `width * height`. A typical analysis chains ≥4 of these.
- **Fix:** Have the mask functions take `(input: &[bool], output: &mut [bool])` and let the caller ping-pong two reusable buffers. Add a `MaskBuffers` helper in `analysis.rs` that owns the two `Vec<bool>`s for the lifetime of one analyze job.
  ```rust
  struct MaskBuffers { a: Vec<bool>, b: Vec<bool> }
  impl MaskBuffers {
      fn new(len: usize) -> Self { Self { a: vec![false; len], b: vec![false; len] } }
      fn swap(&mut self) { std::mem::swap(&mut self.a, &mut self.b); }
  }
  ```
- **Effort:** Medium-high (mechanical but ~6 functions to rewrite).
- **Tests:** Existing `analysis.rs` tests likely call the mask functions directly. After changing signatures to `(input, output)` form, update each test to allocate its own output buffer. `cargo test -p backend-rs analysis::` should still pass.
- **Win:** Drops O(steps) full-image allocations per analyze job; better cache behavior.

### 1.4 Replace `Result<String, String>` error returns with typed errors

- **Files:**
  - `backend-rs/src/processing.rs` (every `Err("…".to_string())` and `Err(format!(…))` — lines 89, 94 via `?`, 102 via `?`, 162, 165, 171, 177, 188, etc.)
  - `backend-rs/src/cli.rs` (lines 20, 25 signature; ~40 internal `.map_err(|e| e.to_string())?` and `Err("…".to_string())` sites)
- **Today:** Errors are allocated `String`s, often from static literals. Every error path pays a heap allocation; callers can only match on substrings.
- **Fix:** Introduce focused error enums and let `?` do the conversion.
  ```rust
  // processing.rs
  #[derive(Debug, thiserror::Error)]
  pub enum ProcessingError {
      #[error("grayscale processing requires Gray8 preview input")]
      NonGray8Input,
      #[error("palette must be one of: none, hot, bone")]
      UnknownPalette,
      // …
  }
  ```
  Adopt `thiserror` (already idiomatic; tiny crate). For `cli.rs`, define `enum CliError` and `impl From<io::Error>` so the `.map_err(|e| e.to_string())` boilerplate disappears.
- **Effort:** Medium. The change is mechanical once the enum exists.
- **Tests:** Any test that asserts on error strings (e.g. `assert!(err.contains("palette must be"))`) breaks. Grep `rg 'assert.*contains.*Err|assert.*Err.*contains' backend-rs/` first, then rewrite to match on the enum variant: `assert!(matches!(err, ProcessingError::UnknownPalette))`. IPC error serialization on the frontend must also be checked — the JSON shape may change.
- **Win:** Removes 40+ allocations on error paths and gives callers structured matching. Memory-safety wise it's a wash; the real gain is correctness + idiom.

### 1.5 Make `normalize_palette_name` return an enum, not `Result<String, String>`

- **File:** `backend-rs/src/processing.rs:160` and call site at line 94/101
- **Today:** Returns one of three `String`s, then `apply_named_palette` matches on the `&str` and returns yet another `Err("palette must be …")` if it doesn't recognize what we just produced.
- **Fix:**
  ```rust
  #[derive(Copy, Clone, Debug, PartialEq, Eq)]
  enum Palette { None, Hot, Bone }

  fn normalize_palette_name(name: &str) -> Result<Palette, ProcessingError> {
      match name.trim().to_ascii_lowercase().as_str() {
          "" | "none" => Ok(Palette::None),
          "hot"       => Ok(Palette::Hot),
          "bone"      => Ok(Palette::Bone),
          _           => Err(ProcessingError::UnknownPalette),
      }
  }
  ```
  Then `apply_named_palette` takes `Palette` and the redundant `_ =>` arm goes away. `mode = format!("{mode} with {normalized_palette} palette")` becomes `mode = format!("{mode} with {} palette", normalized_palette.label())` — same allocation, but no more impossible error path.
- **Effort:** Small.
- **Win:** Eliminates allocations + a dead error arm; the type system now enforces what the substring match used to.
- **Coordinate with 2.6:** 2.6 rewrites the whole `mode`-building path with `Cow`. If 2.6 lands first, the `format!("{mode} with {} palette", …)` snippet above becomes `parts.push(format!("{} palette", normalized_palette.label()).into())` — same idea, different shape. Pick an order and stick to it; don't review both diffs in isolation.

### 1.6 Tighten the palette pixel builder (readability only)

- **File:** `backend-rs/src/processing.rs:180-184`
- **Today:** `Vec::with_capacity` followed by `extend_from_slice` per pixel — verbose but correct.
- **Fix:** Keep the pre-allocation; `extend` on top of a `flat_map` reads cleaner without losing the single-allocation property.
  ```rust
  let mut pixels = Vec::with_capacity(preview.pixels.len() * 4);
  pixels.extend(preview.pixels.iter().copied().flat_map(color_fn));
  ```
  **Do not** drop the `with_capacity` and rely on `collect()` alone: `FlatMap` is not `ExactSizeIterator` (its `size_hint` is `(outer_lower, None)`), so plain `collect()` will reallocate as the `Vec` grows. The current code is more memory-efficient than a bare `flat_map().collect()` would be.
- **Effort:** Trivial. **Win:** Readability only — this is not a perf change. Demote or skip if higher-impact phases need the cycles.

---

## Phase 2 — Idiomatic Rust cleanups (correctness + readability)

These don't move the needle on a flame graph but they eliminate sloppy
patterns that make future bugs more likely.

### 2.1 Replace `expect("… mutex poisoned")` with a single helper

- **File:** `backend-rs/src/app.rs`, `cache.rs`, `persistence.rs` (30+ sites)
- **Today:** Same `.expect("… mutex poisoned")` boilerplate everywhere; if any thread does panic while holding one of these locks, all subsequent IPC commands panic too.
- **Fix:** Either:
  - **(a)** Switch to `parking_lot::Mutex` / `RwLock`. Their `lock()` doesn't return a `Result` — no poison concept. Single `Cargo.toml` change + drop the `.expect(...)` calls everywhere.
  - **(b)** If staying with `std`, add `fn lock_or_recover<T>(m: &Mutex<T>) -> MutexGuard<'_, T>` that calls `into_inner()` on poison and logs.
- **Recommendation:** (a). The `parking_lot` versions are also faster and smaller, which suits this project's priorities.
- **Tradeoff to acknowledge before picking (a):** `parking_lot` has no poison concept. With `std`, a panic mid-critical-section poisons the mutex and the next `lock()` returns `Err` — surfacing the broken-invariant case loudly. With `parking_lot`, the next thread silently gets the half-mutated state. This is acceptable here because our critical sections only mutate `HashMap`s, `Vec`s, and similar containers — there are no multi-step invariants that would be left half-applied across a panic. If that ever stops being true (e.g. we start mutating two correlated fields under the same lock), revisit option (b).
- **Effort:** Small (one-line per call site after the dependency swap).

### 2.2 Drop the `.iter().map(String::as_str).collect::<Vec<_>>()` adapter

- **File:** `backend-rs/src/cli.rs:20-22`
- **Today:** `run(args: &[String])` immediately reallocates a `Vec<&str>` to call `run_args(&[&str])`.
- **Fix:** Make `run` accept `&[impl AsRef<str>]` or change callers to pass `&[&str]` directly. `desktop-tauri/src/main.rs` is the only real caller besides `main.rs`; both can pass `&[&str]` with `args.iter().map(String::as_str)` once at the top of `main`.
- **Effort:** Small.

### 2.3 Add `#[must_use]` to lookup-style methods

- **Today:** Methods that return `Option<T>` from a lookup can be silently dropped — losing the lookup is almost always a bug.
- **Worklist — generate with rg:**
  ```bash
  rg -n 'pub fn \w+\([^)]*\) -> Option<' backend-rs/src/
  rg -n 'pub fn \w+\([^)]*\) -> Result<' backend-rs/src/
  ```
  Triage the output: anything that's a pure lookup or computed predicate gets `#[must_use]`. Side-effectful operations (e.g. functions that return `Result` from an IO operation) do not need it — the `?` operator at call sites already enforces handling.
- **Fix:** `#[must_use]` on each method whose return value carries the only signal of success.

### 2.4 `&Vec<T>` / `&String` → `&[T]` / `&str` where present

- **Files:** Sweep all of `backend-rs/src/` and `desktop-tauri/src/` with `cargo clippy -- -W clippy::ptr_arg` (already configured via `npm run lint:rust`?). Apply the suggestions.
- **Today:** A few signatures take owned references to growable collections when a slice would do.
- **Effort:** Trivial; clippy autofixes most.

### 2.5 Use `matches!` and `.is_some_and` consistently

- **Files:** Wide; check `analysis.rs`, `processing.rs`, `app.rs` for `if let Some(x) = … { x.cond() } else { false }` patterns.
- **Fix:** `opt.is_some_and(|x| x.cond())`, `matches!(state, JobState::Completed | JobState::Failed | JobState::Cancelled)`.
- *(The `cleanup_*` consolidation that used to live here has moved to Phase 4.1, where it belongs as a polish item.)*

### 2.6 Use `Cow<'static, str>` for the processing `mode` description string

- **File:** `backend-rs/src/processing.rs:117-158` (`process_grayscale_pixels`)
- **Today:** Builds `mode` with repeated `format!("{mode} with …")` — every modifier allocates a fresh `String` and discards the previous one. For the common case (no modifiers), the `"grayscale".to_string()` is also pure waste.
- **Fix:** Collect mode parts into a `SmallVec<[&'static str; 4]>` (or `Vec<Cow<'static, str>>`) and `join(" with ")` at the end. Saves up to 4 allocations per call in the worst case, 1 in the common case.
  ```rust
  let mut parts: Vec<Cow<'static, str>> = Vec::with_capacity(4);
  parts.push("grayscale".into());
  if controls.invert { parts[0] = "inverted grayscale".into(); }
  if controls.brightness != 0 {
      parts.push(format!("brightness {:+}", controls.brightness).into());
  }
  // … then: parts.join(" with ")
  ```

---

## Phase 3 — Frontend cleanups

### 3.1 Extract the duplicated `clamp` helper

- **Files (all three are the same function):**
  - `frontend/src/features/jobs/progressFormatting.ts:178`
  - `frontend/src/features/jobs/progressTiming.ts:132`
  - `frontend/src/features/viewer/viewport.ts:101`
- **Fix:** Add `frontend/src/lib/math.ts` exporting `clamp(value, min, max)`, import from there in all three sites. Delete the local copies.
- **Effort:** Trivial.

### 3.2 Mark module-level constant arrays `readonly`

- **File:** `frontend/src/app/htmxApp.ts:22` — `const TABS: ActiveTab[] = …`
- **Fix:** `const TABS: readonly ActiveTab[] = ["view", "processing"] as const;`. Apply the same to any other module-level "lookup" array.
- **Effort:** Trivial. Catches accidental `TABS.push(...)` at compile time.

### 3.3 Tighten the `setCompareView` narrowing

- **File:** `frontend/src/app/htmxView.ts:305`
- **Today:** Validates `value` against three literals, then `as CompareView` to assign.
- **Fix:** Let the type system narrow — the `as` cast is unnecessary once the guard returns `value is CompareView`.
  ```ts
  function isCompareView(value: string | undefined): value is CompareView {
    return value === "original" || value === "processed" || value === "split";
  }

  private setCompareView(value: string | undefined): void {
    if (isCompareView(value)) this.ui.compareView = value;
  }
  ```

### 3.4 ~~Collapse the verbose null/undefined check~~ — drop this item

- **File:** `frontend/src/features/annotations/tools.ts:75,89`
- **Why dropping:** The "collapse to `!= null`" recommendation is wrong for this project. `frontend/biome.json` enables `"recommended": true`, which includes `suspicious/noDoubleEquals` — `!=` is a lint error. The verbose `measurement.calibratedLengthMm !== null && measurement.calibratedLengthMm !== undefined` already in use is the lint-clean form, not "the verbose alternative." Leave it.
- **Alternative if you really want it shorter:** narrow with a type guard (`function hasCalibration(m): m is Measurement & { calibratedLengthMm: number }`) and call it once. That's a real readability win and lint-clean. Only worth doing if the same `!== null && !== undefined` pair appears in 4+ sites.

### 3.5 Stop re-parsing the annotation SVG on every pointer-move tick

- **File:** `frontend/src/features/viewer/ViewerController.ts:566-578`
- **Today:** `updateCanvas()` runs on every pointer-move during pan/draw; it regenerates the full annotation SVG string and assigns it to `innerHTML` each tick, even when only the transform changed.
- **Real fix — mutate DOM nodes, not strings.** Build the annotation SVG once into actual DOM elements (a single `<svg>` containing a `<g class="annotation-transform">` containing one child per annotation). On a pan/zoom tick, update only the group's `transform` attribute:
  ```ts
  this.transformGroup.setAttribute(
    "transform",
    `matrix(${scale} 0 0 ${scale} ${offsetX} ${offsetY})`,
  );
  ```
  This skips the HTML parser entirely on the hot path — the only DOM work per tick is one attribute assignment. Rebuild the inner `<g>` children only when annotations, selection, or draft state change, keyed by `(model.annotations identity, selectedAnnotationId, draftLine snapshot)`.
- **Weaker fallback** (if going DOM-native is too invasive): cache the inner SVG markup string keyed on annotation state and only re-wrap it with the transform-aware outer `<svg>` each tick. This still hits the HTML parser per tick, so it understates the win — prefer the DOM-mutation approach.
  ```ts
  if (this.annotationsKey !== nextKey) {
    this.cachedInner = renderAnnotationGroup(this.model.annotations, …);
    this.annotationsKey = nextKey;
  }
  this.annotationHost.innerHTML = wrapWithTransform(this.cachedInner, this.frame, transform);
  ```
- **Effort:** Medium for the DOM-mutation rewrite, small for the fallback.
- **Win:** With DOM mutation, pan/zoom ticks drop from "parse and rebuild N SVG elements" to "set one attribute." Measure on a study with many annotations to confirm.

### 3.6 ~~Avoid `as` after a checked branch in `htmxView`~~ — drop this item

`rg -n ' as [A-Z]\w+' frontend/src/app/htmxView.ts` returns 2 hits, both `as const` (lines 629 and 658). There are no problematic type casts here — the only real `as` cast (the `setCompareView` one at line 305) is already covered by Phase 3.3. Delete this item from the plan.

---

## Phase 4 — Maintenance & dead code

### 4.1 Consolidate `cleanup_file` / `cleanup_files` / `cleanup_path`

- **File:** `backend-rs/src/app.rs:1755-1767`
- **Fix:** Single `fn cleanup<P: AsRef<Path>>(paths: impl IntoIterator<Item = P>)`. Delete the three callers' wrappers.

### 4.2 Run clippy with `-W clippy::pedantic` once and triage

- Many of the items in Phase 2 will show up automatically. Use this as a one-time pass after Phases 1–3 land; don't enable `pedantic` in CI (too noisy).
- **Where the findings go:** triaged hits feed back into Phase 2 follow-ups (file new sub-items under 2.x rather than fixing them inline in the pedantic-pass PR). The pedantic-pass PR itself should contain *zero* code changes — it's just a triage exercise that produces a worklist.

### 4.3 Run `cargo udeps` to find unused crate deps

- Run via `cargo +nightly udeps --all-targets` if you have a nightly toolchain handy. One-off, not for CI.

---

## Explicitly out of scope (don't waste time here)

- **`addEventListener` "leak" in `ViewerController.mount`** — initial audit flagged
  this, but the handlers are arrow-function class properties, bound once per
  instance. `mount()` calls `detach()` first; references match; nothing leaks.
- **"DOM thrash in `updateCanvas`"** — `updateCanvas` already groups reads
  (one `getBoundingClientRect` happens upstream in `updateFrame`) and writes
  (text → styles → innerHTML). The reads-then-writes ordering is fine. Leave it.
- **"Manual `for (let index = 0; …)` loop in `workbenchStore.ts:142-149`"** —
  doesn't exist; the function at those lines is `processingRequestForStudy`,
  which is already idiomatic.
- **DICOM / TIFF / standalone HTTP backend** — `CLAUDE.md` says don't reintroduce.

---

## Suggested execution order

0. **Day-one, in parallel with everything else:** run Phase 1.1's
   mutation-audit `rg` commands and write down the chosen strategy for each
   `StudyRecord` mutation site. This is read-only and doesn't block any other
   PR, but it's the gating risk for the whole plan — finding a surprise
   mutation site mid-port would force the 1.1 PR to be rewritten.
1. Phase 1.4 + 1.5 first (typed errors / palette enum) — they unlock cleaner
   call sites for the other Phase 1 work.
2. Phase 1.1 (`Arc<StudyRecord>`) — biggest hot-path win, low risk *given the
   day-zero audit is complete*.
3. Phase 1.2a + 1.2b + 1.3 (pixel/mask buffer reuse) — measure with `cargo bench` or a
   one-off `Instant::now()` around `start_analyze_job` to confirm the win.
   1.2a and 1.2b can ship as separate PRs.
4. Phase 2.1 (`parking_lot`) as one PR — mechanical, but read the
   poison-detection tradeoff note before merging.
5. Phase 3 in any order; 3.1 + 3.2 are five-minute changes. Skip 3.4 and 3.6
   (deleted above).
6. Phase 4 at the end as polish.

Each phase should be its own PR. Run `npm run release:smoke` before opening
each one.
