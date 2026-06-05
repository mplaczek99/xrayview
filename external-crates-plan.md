# External-crate replacements — survey & plan

A scan of `backend-rs/src/` for code that looks like a reimplementation of an
existing crate. Each candidate is rated against the project's stated engineering
priorities (CLAUDE.md): performance, memory usage, memory safety, zero-copy /
streaming, no allocations in hot paths.

Current backend-rs deps (Cargo.toml):
`chrono`, `flate2`, `parking_lot`, `rayon`, `serde`, `serde_json`, `thiserror`.

## Verdict at a glance

| # | Candidate | Crate | LOC saved | New deps (direct + transitive) | Perf risk | Recommend |
|---|---|---|---:|---|---|---|
| 1 | LE byte read helpers | `byteorder` | ~55 | 1 + 0 | none | **DONE** (77e8420) |
| 2 | Recursive artifact-dir walk | `walkdir` | ~25 | 1 + 1 (`same-file`) | none | **DONE** (95dae4a) |
| 3 | Test temp dirs (`env::temp_dir() + pid`) | `tempfile` | ~30 (test-only) | 1 + ~4 (dev-dep only) | none | **DONE** (10ff124) |
| 4 | BMP decode/encode (`bmp.rs` + half of `render.rs`) | `image` | ~1100 | 1 + ~25 | **HIGH** | NO — see §4 |
| 5 | Morphology / hist-eq / box-blur / CCL | `imageproc` | ~500 | 1 + ~30 | **HIGH** | NO — see §5 |
| 6 | Unified CLI (legacy + subcommand) | `clap` (derive) | ~280 | 1 + ~13 | low (binary +~150 KB) | **DONE** — single unified parser |
| 7 | ~~New-style subcommand parser~~ | ~~`lexopt`~~ | — | — | — | Superseded by §6 |
| 8 | `SourcePreviewCache` LRU | `lru` / `mini-moka` | ~80 | 1 + 0–5 | medium | NO — see §7 |
| 9 | FNV-1a 64 (`app.rs`) | `fnv` / `rustc-hash` | ~8 | 1 + 0 | none | NO — not worth it |
| 10 | File reads → mmap (BMP decode) | `memmap2` | 0 (perf, not LOC) | 1 + 0 | n/a (a *gain*) | Worth a benchmark, separate task |

"LOC saved" counts the body of the replaced functions (excluding the
`use`/import-site changes). Sources of the counts are listed inline in each
section.

---

## 1. `byteorder` — replace hand-rolled LE readers  **[done — 77e8420]**

Eight near-identical helpers across two files:

- `backend-rs/src/bmp.rs:716-741` — `read_le_u16_at`, `read_le_u32_at`,
  `read_le_i32_at` (slice + offset form)
- `backend-rs/src/analysis.rs:1748-1786` — `read_le_u32`, `read_le_u32_from`,
  `read_le_i32`, `read_le_u64`, `read_le_f64` (Cursor / Read form)

The total `from_le_bytes`/`to_le_bytes` site count is 84 across the crate
(grep), so `byteorder::ReadBytesExt` / `WriteBytesExt` would also tidy the
encoder side of `render.rs:226-262` if desired.

- **LOC saved:** ~55 (eight helpers averaging 7 lines each, plus a handful of
  call-site cleanups).
- **Deps added:** `byteorder` (zero transitive deps).
- **Perf:** unchanged. `byteorder` compiles to the same `from_le_bytes`
  intrinsic the helpers use.
- **Risk:** none. This is the textbook "use the standard library of crates"
  case.

**Action:** add `byteorder = "1"` to `[dependencies]`, replace the helpers,
delete the eight `fn read_le_*` definitions, update call sites.

---

## 2. `walkdir` — replace `collect_artifact_files`  **[done — 95dae4a]**

`backend-rs/src/cache.rs:281-313` is a hand-rolled recursive directory walk
that tolerates per-entry errors and skips symlinked subtrees implicitly.

- **LOC saved:** ~25 (the whole `collect_artifact_files` function plus its
  recursive call site).
- **Deps added:** `walkdir` + `same-file` (`walkdir`'s only transitive dep).
- **Perf:** roughly identical; `walkdir` is a thin wrapper over `read_dir`
  with the same `Iterator` shape. The only behavior change is that
  `walkdir::WalkDir::new(root).follow_links(false)` doesn't follow symlinks —
  the current code does, by accident. That's a *bug-shaped* difference worth
  preserving deliberately: set `.follow_links(false)` explicitly.
- **Risk:** none if symlink handling is set explicitly.

**Action:** add `walkdir = "2"`, replace `collect_artifact_files` with a
`WalkDir::new(...).into_iter().filter_map(...)` chain that produces the same
`ArtifactFileInfo` vec.

---

## 3. `tempfile` — replace ad-hoc temp dirs in tests  **[done — 10ff124]**

Eight tests in `backend-rs/src/cache.rs` (lines 597, 627, 640, 658, 682, 703,
723, 745) construct paths like
`env::temp_dir().join(format!("xrayview-rs-cache-root-{}", process::id()))`.
There's no cleanup, so re-running the test suite leaves orphan dirs in
`/tmp`. `tempfile::TempDir` gives you RAII cleanup for free.

- **LOC saved:** ~30 (per-test path-construction lines + an explicit
  `fs::remove_dir_all` cleanup or two if anyone adds them later).
- **Deps added:** `tempfile` (transitively `fastrand`, `rustix`, `bitflags`,
  `linux-raw-sys`) — all `[dev-dependencies]`, so they don't ship in the
  release binary.
- **Perf:** test-only.
- **Risk:** none.

**Action:** add `tempfile = "3"` under `[dev-dependencies]`, replace each
ad-hoc temp path with a `TempDir::new()?` (or `tempfile::tempdir()?`) handle.

---

## 4. `image` crate for BMP decode/encode — **NOT recommended**

`backend-rs/src/bmp.rs` (1024 lines) and the encoder half of `render.rs`
(~250 lines: `encode_gray8_bmp`, `encode_rgba8_as_bgr24_bmp`,
`write_file_header`, `write_info_header`) would mostly disappear.

- **LOC saved:** ~1100.
- **Deps added:** `image` pulls ~25 crates including `png`, `jpeg-decoder`,
  `tiff`, `gif`, `zune-*`, etc. Even with feature gating
  (`default-features = false, features = ["bmp"]`) you get a few of them.
- **Perf:** `bmp.rs` is heavily tuned for this project's exact subset:
  - Boxed `[u8; 256]` palette LUT for indexed-8 decode (`bmp.rs:348`).
  - `Arc<[u8]>` pixel handoff so processing/analysis fan out zero-copy
    (`bmp.rs:51`).
  - Min/max stretch combined with the decode pass to avoid an extra full
    image traversal (`bmp.rs:191-237`).
  - Explicit rejection of compressed BMPs at parse time — keeps the decoder
    branch-free in the hot loop.
  - The "tooth analysis" path skips the LUT stretch precisely because
    analyzer behavior depends on raw byte values being comparable across
    images.
  `image` would force back-and-forth through its `DynamicImage`/`ImageBuffer`
  abstractions and lose all five of those optimizations.
- **Safety:** `image` is well-fuzzed; the hand-rolled code has been written
  with bounds checks and `Result` propagation throughout. Net safety roughly
  even.
- **Verdict:** **conflicts with the engineering priorities**. CLAUDE.md
  weights perf/memory heavily and the BMP path is the per-study hot loop.
  The 1100-line saving isn't worth giving up zero-copy `Arc<[u8]>` fan-out
  and the targeted decode optimizations.

If you ever want to re-evaluate: `tinybmp` is a smaller alternative (no_std,
no extra format deps) but it returns iterators rather than buffers and would
require its own adapter layer — not a clear win.

---

## 5. `imageproc` for morphology / equalization / blur / CCL — **NOT recommended**

In `backend-rs/src/analysis.rs`:

- `dilate_mask_into` (lines 950-1022), `erode_mask_into` (1024-1097),
  `close_mask_into` (1099-1122) — sliding-window O(width·height) per pass,
  independent of radius.
- `remove_small_components_into` (1124-1178) and `count_components`
  (1877-…) — flood-fill connected-component labelling.
- `fill_holes_into` (1180-1227) — BFS hole fill.
- `box_blur_gray` (1313-1406) — **rayon-parallelized** across rows.
- `gradient_gray` (1408-1434) — also rayon-parallelized.

In `backend-rs/src/processing.rs`:

- `equalize_histogram_in_place` + `equalize_lookup` (396-449) — histogram
  equalization with integer round-to-nearest.
- `apply_lookup_in_place` (388-392) — LUT apply.

- **LOC saved:** ~500 if all of the above are removed.
- **Deps added:** `imageproc` brings in `image` (see §4) plus extras.
- **Perf:** the hand-rolled versions beat `imageproc` on three axes:
  1. **Sliding-window morphology** — the dilate/erode here is O(area)
     regardless of structuring-element radius; `imageproc`'s `dilate`/`erode`
     are O(area · radius²) naïve implementations.
  2. **Parallelism** — box-blur and gradient use `rayon::par_chunks_mut`;
     `imageproc` is single-threaded.
  3. **Scratch reuse** — `MaskBuffers` (analysis.rs:226-251) is a recycled
     scratch pool so the morphology open/close sequence allocates zero
     `Vec<bool>` per call; `imageproc` returns owned `ImageBuffer`s.
- **Verdict:** would regress the hottest analysis path. Skip.

The one piece worth carving off separately is **histogram equalization in
`processing.rs`** — it's 50 lines, not perf-critical (runs once per process
command), and `imageproc::contrast::equalize_histogram` is a drop-in. But
pulling in `imageproc` for 50 lines isn't worth it on its own.

---

## 6. CLI argument parsing — **mostly NOT recommended**

Two parsers live in `backend-rs/src/cli.rs`:

a. **Legacy flag parser** — `parse_legacy_args` (121-199, ~80 lines) plus
   helpers `split_flag_value`, `canonical_legacy_flag`,
   `required_flag_value`, `parse_bool_flag`, `trim_leading_separators`
   (~40 lines). The code itself has a comment:

   > "Manual flag parser. We don't use clap here because the legacy shape
   > historically supports `--flag=value`, `--flag value`, and bare bool
   > flags (`--invert` meaning `--invert true`), and clap configured to
   > accept all three turned out to be more code than this loop."

   Clap *can* express all three (`Arg::default_missing_value`,
   `num_args(0..=1)`, etc.), but the original author already evaluated it
   and chose not to. The legacy CLI is also being phased out
   (commit `0768439 Simplify legacy CLI mode check`), so investing in it now
   is the wrong direction.

b. **New-style subcommand router** — `run` (71-105), plus
   `parse_render_preview_args` (589-…), `parse_process_preview_args`
   (617-…), `parse_analyze_preview_args` (693-…). The router itself is a
   `match` on `args[0]` — fine. The per-subcommand arg parsers are flat and
   could be tightened with `lexopt` (~70 LOC → ~30 LOC each, no derive
   macros, no transitive deps).

- **LOC saved:** ~120 (legacy) + ~120 (subcommands) if both replaced.
- **Deps added:**
  - `clap` w/ derive — ~10 transitive crates, ~150 KB binary growth.
  - `lexopt` — zero transitive deps.
- **Verdict:**
  - **Legacy parser:** leave alone (explicit author decision; phasing out).
  - **Subcommand parsers:** `lexopt` is a low-risk improvement worth doing
    in the same pass as §1 if you're already touching `cli.rs`. Otherwise
    skip — the existing code is readable.

---

## 7. `SourcePreviewCache` LRU — **NOT recommended**

`backend-rs/src/cache.rs:315-513` is a HashMap + VecDeque LRU with three
properties the obvious crates don't bundle:

1. **Dual budget:** evicts on both entry count *and* total byte size.
2. **Single-flight inflight:** concurrent decoders of the same key block on
   a `parking_lot::Condvar` instead of racing.
3. **`Arc<[u8]>` payloads:** entries are cheap to clone out without holding
   the cache lock.

The most common LRU crates each miss one of these:

- `lru` — count-bounded only, no byte budget, no single-flight.
- `moka` / `mini-moka` — byte budget + count budget + async/sync ergonomics,
  but no single-flight; you'd still need the Condvar layer.

- **LOC saved:** maybe ~80 if you replace the `HashMap + VecDeque` LRU
  plumbing with `moka::sync::Cache::builder().weigher(...)`, but the
  inflight rendezvous (the load-bearing part) stays.
- **Verdict:** not a clean replacement. Skip.

---

## 8. `fnv1a64` (`app.rs:1723`) — **NOT recommended**

Eight-line function. Replacing with the `fnv` crate gains nothing and adds a
dep. The hand-rolled version is correct, documented, and pinned for cache
stability. Leave it.

---

## 9. `memmap2` — *additive*, separate task

`bmp.rs:67` reads the entire BMP into a `Vec<u8>` via `fs::read` before
decoding. For multi-megabyte X-rays, swapping to `memmap2::Mmap` gives
zero-copy access and lets the OS page in only what the decoder touches.

- **LOC delta:** ~0 (the decode loop is already byte-slice based).
- **Deps added:** `memmap2` (zero transitive deps).
- **Caveat:** mmap'd files can SIGBUS if truncated under you. The decoder
  already validates the file size against the header, so this is bounded —
  but it's still a behavior change worth a small bench.
- **Verdict:** not a "save lines" play. Worth a separate, narrow benchmark
  PR if BMP decode shows up in a profile.

---

## Suggested order of execution

1. ~~**`byteorder`** (§1) — biggest LOC win for a zero-risk dep change.~~ **DONE — 77e8420**
2. ~~**`walkdir`** (§2) — drop-in, deletes a non-trivial recursive function.~~ **DONE — 95dae4a**
3. ~~**`tempfile` for tests** (§3) — dev-dep only, fixes a real `/tmp` leak.~~ **DONE — 10ff124**
4. (optional) **`lexopt` for new-style subcommand parsers only** (§6.b) —
   only if you're already in `cli.rs` for §1.

Everything else is either *against* the engineering priorities (perf-tuned
hand-rolls in §4/§5), explicitly rejected by a prior author decision (§6.a),
or doesn't actually replace the load-bearing logic (§7).

Net of the recommended steps:
- **~110 LOC removed** (mostly mechanical), **0 LOC added in tests beyond
  imports**.
- **2 runtime deps** (`byteorder`, `walkdir`) + **1 dev-dep** (`tempfile`).
- **No perf regression** in any hot path.
