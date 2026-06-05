// Tooth + bone overlay analysis. This is the most CPU-intensive path in the
// crate: two gradient-boosted forests score every pixel, then morphology turns
// the raw masks into clean overlays the UI can draw.
//
// The pipeline at a glance:
//   1. Contrast-normalize the grayscale (crate::tooth_model::normalize_gray).
//   2. Build the position-free texture feature planes once (FeaturePlanes —
//      multi-scale integral-image mean/std, gradient, contrast ratios).
//   3. Score each pixel with the tooth forest and the bone forest and threshold
//      (crate::tooth_model). Both forests are trained offline on the labeled
//      masks with NO absolute pixel position, so they generalize across
//      subjects instead of memorizing one layout.
//   4. Morphology cleanup (close, small-component removal, hole-fill, the bone
//      section shaping + frame clearance), then draw fills/outlines.
//
// Both forest assets (learned_model = tooth, bone_model = bone) are lazily
// decoded into OnceLock<Result<…>> on first use. Loading errors are sticky: a
// corrupt asset disables that detector for the run but doesn't crash the app
// (tooth falls back to a percentile threshold, bone to an empty mask).
//
// Hot-path engineering notes:
//   * FeaturePlanes is built once and shared by both forests — the integral
//     images (the O(pixels) precompute) are paid for a single time per analyze.
//   * Rayon parallelizes over rows (par_chunks_mut), the only level of
//     parallelism that pays off here — per-pixel is too fine-grained.
//   * Morphology operations work in-place on Vec<bool> mask buffers held in
//     a reusable MaskBuffers so we don't alloc/free per frame.

use std::sync::OnceLock;

use rayon::prelude::*;

use crate::render::{PreviewFormat, PreviewImage};
use crate::tooth_model::{FeaturePlanes, ToothForest};

// RGBA outline colors. Green for tooth, red for bone — chosen to read well
// against both bright and dark X-rays.
const TOOTH_GREEN: [u8; 4] = [120, 255, 0, 255];
const BONE_RED: [u8; 4] = [255, 0, 0, 255];
// Sections view blends fills over the grayscale so anatomy stays readable
// beneath the color wash and the mode is visually distinct from Outlines.
const SECTION_FILL_ALPHA: u8 = 115;
// Gradient-boosted forests (XVLM2), trained offline by examples/train_tooth.rs
// on position-free texture features (crate::tooth_model). Baked into the binary
// via include_bytes! so we don't ship sidecar files.
const TOOTH_MODEL_DATA: &[u8] = include_bytes!("../assets/analysis/learned_model.bin");
const BONE_MODEL_DATA: &[u8] = include_bytes!("../assets/analysis/bone_model.bin");
// Forest score at/above this is the class. The trainer regresses toward {0, 1};
// both cuts are where balanced accuracy peaks on the labeled set (the
// examples/train_tooth threshold sweep). Tooth 0.55 stops greening the isodense
// bone; bone 0.50 is balanced — and bone false positives over teeth are hidden
// anyway, since the overlay draws tooth on top of bone.
const TOOTH_SCORE_THRESHOLD: f64 = 0.55;
const BONE_SCORE_THRESHOLD: f64 = 0.50;
// Sub-floor blob sizes get dropped. 24px for bone, 4px for tooth — both
// were empirically chosen to remove noise without erasing real anatomy.
const MINIMUM_BONE_AREA_FLOOR_PIXELS: usize = 24;
const MINIMUM_TOOTH_AREA_FLOOR_PIXELS: usize = 4;
// Outline thicknesses match what looks readable in the UI's typical zoom.
const TOOTH_OUTLINE_THICKNESS_PIXELS: usize = 2;
const BONE_OUTLINE_THICKNESS_PIXELS: usize = 2;
// Morphological close: dilate then erode by N pixels. 2px is enough to fill
// hairline cracks in the tooth mask without merging adjacent teeth.
const TOOTH_MASK_CLOSE_RADIUS_PIXELS: usize = 2;
// Cut a 24px-radius safety margin around the tooth mask before drawing the
// bone overlay so the two don't visually overlap at the boundary.
const BONE_TOOTH_CUTOUT_BRIDGE_RADIUS_PIXELS: usize = 24;
// Pixels at or below this gray value are treated as off-detector (i.e. the
// black border of the radiograph). Used to exclude background from analysis.
const RADIOGRAPH_BACKGROUND_MAX_GRAY: u8 = 2;
// Width of the image-edge strip used to snap the bone-section mask outward
// to the image border. Wherever the detected bone sits within this many
// pixels of an edge, the strip is filled so the outline ends up *at* the
// edge instead of tracing the thin dark-vignette gap as a rectangle.
const BONE_EDGE_SNAP_RADIUS_PIXELS: usize = 14;
// Lazy-loaded models. Each OnceLock holds a Result so a corrupt asset surfaces
// once and stays that way — we don't keep retrying the decode on every call.
static TOOTH_MODEL: OnceLock<Result<ToothForest, String>> = OnceLock::new();
static BONE_MODEL: OnceLock<Result<ToothForest, String>> = OnceLock::new();

// The public output. `preview` is the outline overlay (drawn on top of the
// grayscale); `filled_preview` is the filled-region version used for the
// "Sections" toggle. The counts and coverage are surfaced to the UI footer.
#[derive(Debug, Clone, PartialEq)]
pub struct ToothOverlayResult {
    pub preview: PreviewImage,
    pub filled_preview: PreviewImage,
    pub tooth_pixels: usize,
    pub bone_pixels: usize,
    pub coverage: f64,
    pub candidate_count: usize,
    pub mode: String,
}

// Reusable scratch buffers for morphology ops. Allocated once per analyze
// invocation and threaded through the helpers. `a`, `b` are alternating
// double-buffers for in-place chains; `scratch` is a side buffer for the
// few ops that need three; `visited` is the flood-fill marker.
struct MaskBuffers {
    a: Vec<bool>,
    b: Vec<bool>,
    scratch: Vec<bool>,
    visited: Vec<bool>,
}

impl MaskBuffers {
    fn new(len: usize) -> Self {
        Self {
            a: vec![false; len],
            b: vec![false; len],
            scratch: vec![false; len],
            visited: vec![false; len],
        }
    }
}

// Public entry. Validates input, then runs the full tooth + bone pipeline
// and produces both an outline overlay and a filled-region overlay.
//
// Pre-checks (fail-fast, before allocating mask buffers):
//   * Gray8 only — we depend on single-byte-per-pixel layout.
//   * 8×8 minimum — anything smaller can't host a meaningful tooth feature.
//   * Buffer length must match width × height (caller bug otherwise).
pub fn generate_tooth_overlay(preview: &PreviewImage) -> Result<ToothOverlayResult, String> {
    if preview.format != PreviewFormat::Gray8 {
        return Err("tooth analysis requires Gray8 preview input".to_string());
    }
    let width = preview.width as usize;
    let height = preview.height as usize;
    if width < 8 || height < 8 {
        return Err(format!(
            "image is too small for tooth analysis: {}x{}",
            preview.width, preview.height
        ));
    }
    let expected_pixels = width
        .checked_mul(height)
        .ok_or_else(|| "preview dimensions overflow".to_string())?;
    if preview.pixels.len() != expected_pixels {
        return Err(format!(
            "preview pixel length = {}, want {}",
            preview.pixels.len(),
            expected_pixels
        ));
    }

    // Normalize once, then build the texture feature planes once — both the
    // tooth and bone forests score off the same integral images, so we pay the
    // O(pixels) precompute a single time per analyze.
    let normalized = crate::tooth_model::normalize_gray(&preview.pixels);
    let planes = FeaturePlanes::build(&normalized, width, height);
    let mut mask_buffers = MaskBuffers::new(expected_pixels);
    let tooth_mask = detect_tooth_mask(&planes, width, height, &mut mask_buffers);
    let bone_mask = detect_bone_line_mask(&planes, width, height, &mut mask_buffers);
    let tooth_pixels = count_mask(&tooth_mask);
    let bone_pixels = count_mask(&bone_mask);
    // .max(1) just in case — pre-check rejects len 0, but belt + suspenders.
    let coverage = (tooth_pixels + bone_pixels) as f64 / preview.pixels.len().max(1) as f64;
    let candidate_count = count_components(&tooth_mask, width, height, &mut mask_buffers.visited);

    // The `mode` string surfaces in the UI footer. We tack on warnings if the
    // result looks suspect — better to admit uncertainty than silently show
    // a flaky overlay. Thresholds (1/150 of image, width/8) were tuned to
    // avoid false-alarming on tight crops.
    let mut mode = "dynamic tooth and bone level overlay".to_string();
    if tooth_pixels < preview.pixels.len() / 150 || candidate_count == 0 {
        mode.push_str("; no reliable tooth mask found");
    }
    if bone_pixels < width / 8 {
        mode.push_str("; no reliable bone level found");
    }

    // Outline + Sections share one bone shape — the smoothed contour — so the
    // red border and the red shading always trace the same boundary. We let
    // the bone reach the image edge: when the detector finds bone there, the
    // outline should sit at the edge, not be pushed inward.
    let mut bone_section = bone_section_mask_with_ignored_cutouts(
        &preview.pixels,
        &bone_mask,
        &tooth_mask,
        width,
        height,
        &mut mask_buffers,
    );
    // The radiograph has a dark vignette strip at its edges; the detector
    // (correctly) doesn't claim bone there, which leaves a thin gap between
    // the bone region and the image edge. Without this, the outline traces
    // that gap as a rectangle hugging the frame. Snap the section out to the
    // image edge wherever it's already close so the outline collapses onto
    // the edge.
    snap_mask_to_image_edge(
        &mut bone_section,
        width,
        height,
        BONE_EDGE_SNAP_RADIUS_PIXELS,
        &mut mask_buffers,
    );

    let outline_preview = overlay_preview(
        &preview.pixels,
        preview.width,
        preview.height,
        &tooth_mask,
        &bone_section,
        false,
        &mut mask_buffers,
    );
    let filled_preview = overlay_preview(
        &preview.pixels,
        preview.width,
        preview.height,
        &tooth_mask,
        &bone_section,
        true,
        &mut mask_buffers,
    );

    Ok(ToothOverlayResult {
        preview: outline_preview,
        filled_preview,
        tooth_pixels,
        bone_pixels,
        coverage,
        candidate_count,
        mode,
    })
}

// Tooth detection top-level. Runs the gradient-boosted forest over texture
// features (crate::tooth_model) — the discriminator that actually separates
// isodense tooth from bone — then hands the raw mask to clean_tooth_mask for
// morphological polish (small-component removal, closing, hole-filling).
//
// If the forest asset fails to load we fall back to a high-percentile intensity
// threshold. That fallback CANNOT separate tooth from bone (they are isodense),
// so it is a last resort to avoid an empty overlay, not a real detector.
fn detect_tooth_mask(
    planes: &FeaturePlanes,
    width: usize,
    height: usize,
    buffers: &mut MaskBuffers,
) -> Vec<bool> {
    let mask = loaded_tooth_model()
        .map(|forest| forest_score_mask(forest, planes, TOOTH_SCORE_THRESHOLD))
        .unwrap_or_else(|| {
            let normalized = planes.normalized();
            let threshold = percentile(normalized, 82).max(24);
            normalized.iter().map(|value| *value >= threshold).collect()
        });

    clean_tooth_mask(&mask, width, height, buffers)
}

// Threshold the forest score at every pixel. Per-pixel work is independent and
// integral-image feature lookups are O(1), so we parallelize over rows.
fn forest_score_mask(forest: &ToothForest, planes: &FeaturePlanes, threshold: f64) -> Vec<bool> {
    let width = planes.width();
    let mut mask = vec![false; width * planes.height()];
    mask.par_chunks_mut(width).enumerate().for_each(|(y, row)| {
        for (x, slot) in row.iter_mut().enumerate() {
            *slot = forest.score(&planes.features(x, y)) >= threshold;
        }
    });
    mask
}

fn loaded_tooth_model() -> Option<&'static ToothForest> {
    TOOTH_MODEL
        .get_or_init(|| ToothForest::decode(TOOTH_MODEL_DATA))
        .as_ref()
        .ok()
}

fn loaded_bone_model() -> Option<&'static ToothForest> {
    BONE_MODEL
        .get_or_init(|| ToothForest::decode(BONE_MODEL_DATA))
        .as_ref()
        .ok()
}

fn clean_tooth_mask(
    mask: &[bool],
    width: usize,
    height: usize,
    buffers: &mut MaskBuffers,
) -> Vec<bool> {
    if width == 0 || height == 0 || mask.len() != width * height {
        return mask.to_vec();
    }

    let min_area = minimum_tooth_area_pixels(width, height);
    remove_small_components_into(
        mask,
        width,
        height,
        min_area,
        &mut buffers.a,
        &mut buffers.visited,
    );
    let mut cleaned = buffers.a.clone();

    let close_radius = tooth_mask_close_radius(width, height);
    if close_radius > 0 {
        close_mask_into(&cleaned, width, height, close_radius, buffers);
        cleaned.copy_from_slice(&buffers.b);
    }

    fill_holes_into(&cleaned, width, height, &mut buffers.a);
    remove_small_components_into(
        &buffers.a,
        width,
        height,
        min_area,
        &mut buffers.b,
        &mut buffers.visited,
    );
    buffers.b.clone()
}

fn tooth_mask_close_radius(width: usize, height: usize) -> usize {
    TOOTH_MASK_CLOSE_RADIUS_PIXELS.min(width.min(height) / 24)
}

fn minimum_tooth_area_pixels(width: usize, height: usize) -> usize {
    (width * height / 1000).clamp(MINIMUM_TOOTH_AREA_FLOOR_PIXELS, 2048)
}

// Bone detection. Same position-free texture forest approach as tooth, trained
// on the red (bone) mask label. Bone is the high-variance trabecular region;
// the forest leans on the coarse-scale texture features to pick it out of the
// isodense tooth/bone mix. Returns an empty mask if the asset can't load (bone
// is an optional overlay — better blank than a bad guess).
fn detect_bone_line_mask(
    planes: &FeaturePlanes,
    width: usize,
    height: usize,
    buffers: &mut MaskBuffers,
) -> Vec<bool> {
    let Some(forest) = loaded_bone_model() else {
        return vec![false; width * height];
    };
    let mut mask = forest_score_mask(forest, planes, BONE_SCORE_THRESHOLD);

    // Light cleanup: close hairline gaps, drop specks, fill interior holes. The
    // heavier section shaping happens later in
    // bone_section_mask_with_ignored_cutouts.
    close_mask_into(&mask, width, height, 1, buffers);
    mask.copy_from_slice(&buffers.b);
    remove_small_components_into(
        &mask,
        width,
        height,
        minimum_bone_area_pixels(width, height),
        &mut buffers.a,
        &mut buffers.visited,
    );
    mask.copy_from_slice(&buffers.a);
    fill_holes_into(&mask, width, height, &mut buffers.a);
    mask.copy_from_slice(&buffers.a);
    mask
}

// Single rendering path for both Outline (`fill_sections = false`) and
// Sections (`fill_sections = true`). When shading is on, the alpha fills are
// drawn from the same masks the outlines are derived from, so the two layers
// always agree by construction. `bone_mask` is expected to already have any
// background/frame cleanup applied by the caller.
fn overlay_preview(
    gray: &[u8],
    width: u32,
    height: u32,
    tooth_mask: &[bool],
    bone_mask: &[bool],
    fill_sections: bool,
    buffers: &mut MaskBuffers,
) -> PreviewImage {
    let width_usize = width as usize;
    let height_usize = height as usize;
    let mut pixels = grayscale_rgba(gray);

    if fill_sections {
        // Translucent fills first so the outlines drawn next sit crisply on top.
        blend_mask_fill(
            &mut pixels,
            bone_mask,
            BONE_RED,
            SECTION_FILL_ALPHA,
            Some(tooth_mask),
        );
        blend_mask_fill(
            &mut pixels,
            tooth_mask,
            TOOTH_GREEN,
            SECTION_FILL_ALPHA,
            None,
        );
    }

    let bone_outline = centered_outline_mask(
        bone_mask,
        width_usize,
        height_usize,
        BONE_OUTLINE_THICKNESS_PIXELS,
        buffers,
    );
    composite_mask_fill(&mut pixels, &bone_outline, BONE_RED, Some(tooth_mask));

    let tooth_outline = inner_outline_mask(
        tooth_mask,
        width_usize,
        height_usize,
        TOOTH_OUTLINE_THICKNESS_PIXELS,
        buffers,
    );
    composite_mask_fill(&mut pixels, &tooth_outline, TOOTH_GREEN, None);

    PreviewImage::rgba(width, height, pixels)
}

fn grayscale_rgba(gray: &[u8]) -> Vec<u8> {
    let mut pixels = Vec::with_capacity(gray.len() * 4);
    for value in gray {
        pixels.extend_from_slice(&[*value, *value, *value, 255]);
    }
    pixels
}

fn composite_mask_fill(
    pixels: &mut [u8],
    mask: &[bool],
    color: [u8; 4],
    exclude_mask: Option<&[bool]>,
) {
    if mask.len() * 4 != pixels.len() {
        return;
    }
    for (index, value) in mask.iter().enumerate() {
        if !*value
            || exclude_mask.is_some_and(|exclude| exclude.get(index).copied().unwrap_or(false))
        {
            continue;
        }
        let base = index * 4;
        pixels[base] = color[0];
        pixels[base + 1] = color[1];
        pixels[base + 2] = color[2];
        pixels[base + 3] = 255;
    }
}

fn blend_mask_fill(
    pixels: &mut [u8],
    mask: &[bool],
    color: [u8; 4],
    alpha: u8,
    exclude_mask: Option<&[bool]>,
) {
    if mask.len() * 4 != pixels.len() {
        return;
    }
    let a = alpha as u32;
    let inv = 255 - a;
    for (index, value) in mask.iter().enumerate() {
        if !*value
            || exclude_mask.is_some_and(|exclude| exclude.get(index).copied().unwrap_or(false))
        {
            continue;
        }
        let base = index * 4;
        pixels[base] = blend_channel(pixels[base], color[0], a, inv);
        pixels[base + 1] = blend_channel(pixels[base + 1], color[1], a, inv);
        pixels[base + 2] = blend_channel(pixels[base + 2], color[2], a, inv);
        pixels[base + 3] = 255;
    }
}

fn blend_channel(dst: u8, src: u8, alpha: u32, inv_alpha: u32) -> u8 {
    let num = src as u32 * alpha + dst as u32 * inv_alpha;
    ((num + 127) / 255) as u8
}

fn bone_section_mask_with_ignored_cutouts(
    gray: &[u8],
    bone_mask: &[bool],
    tooth_mask: &[bool],
    width: usize,
    height: usize,
    buffers: &mut MaskBuffers,
) -> Vec<bool> {
    if bone_mask.is_empty() || tooth_mask.len() != bone_mask.len() {
        return bone_mask.to_vec();
    }

    let mut section_mask = bone_mask.to_vec();
    let radius = bone_tooth_cutout_bridge_radius(width, height);
    if radius == 0 || width < 8 || height < 8 {
        return section_mask;
    }

    dilate_mask_into(
        bone_mask,
        width,
        height,
        radius,
        &mut buffers.scratch,
        &mut buffers.a,
    );
    dilate_mask_into(
        tooth_mask,
        width,
        height,
        radius,
        &mut buffers.scratch,
        &mut buffers.b,
    );
    for (index, value) in section_mask.iter_mut().enumerate() {
        if buffers.b[index] && buffers.a[index] {
            *value = true;
        }
    }

    let close_radius = (radius / 2).clamp(1, 8);
    close_mask_into(&section_mask, width, height, close_radius, buffers);
    fill_holes_into(&buffers.b, width, height, &mut buffers.a);
    remove_small_components_into(
        &buffers.a,
        width,
        height,
        minimum_bone_outline_area_pixels(width, height),
        &mut buffers.b,
        &mut buffers.visited,
    );
    let mut cleaned = buffers.b.clone();
    clear_border_background_from_mask(&mut cleaned, gray, width, height, &mut buffers.visited);
    cleaned
}

// Push the mask out to the image edge along a thin strip. Only pixels within
// `snap_radius` of an edge are touched; each such pixel becomes true if the
// dilated mask is true there, which means the original mask had a true pixel
// within `snap_radius` (Chebyshev). Interior pixels are untouched.
//
// This bridges the dark vignette gap between the detected bone region and the
// image border so a downstream outline doesn't trace the gap as a rectangle.
fn snap_mask_to_image_edge(
    mask: &mut [bool],
    width: usize,
    height: usize,
    snap_radius: usize,
    buffers: &mut MaskBuffers,
) {
    if snap_radius == 0
        || width == 0
        || height == 0
        || mask.len() != width * height
        || buffers.a.len() != mask.len()
    {
        return;
    }

    dilate_mask_into(
        mask,
        width,
        height,
        snap_radius,
        &mut buffers.scratch,
        &mut buffers.a,
    );

    let strip = snap_radius.min(height);
    for y in 0..strip {
        let row = y * width;
        for x in 0..width {
            if buffers.a[row + x] {
                mask[row + x] = true;
            }
        }
    }
    for y in height.saturating_sub(strip)..height {
        let row = y * width;
        for x in 0..width {
            if buffers.a[row + x] {
                mask[row + x] = true;
            }
        }
    }
    let h_strip = snap_radius.min(width);
    for y in 0..height {
        let row = y * width;
        for x in 0..h_strip {
            if buffers.a[row + x] {
                mask[row + x] = true;
            }
        }
        for x in width.saturating_sub(h_strip)..width {
            if buffers.a[row + x] {
                mask[row + x] = true;
            }
        }
    }
}

fn bone_tooth_cutout_bridge_radius(width: usize, height: usize) -> usize {
    BONE_TOOTH_CUTOUT_BRIDGE_RADIUS_PIXELS
        .min(BONE_OUTLINE_THICKNESS_PIXELS.max(width.min(height) / 32))
}

fn minimum_bone_outline_area_pixels(width: usize, height: usize) -> usize {
    (width * height / 1000).clamp(16, 128)
}

// Both outline helpers treat the image boundary as if the mask continued
// past the edge: an outline pixel must straddle a true→false transition that
// occurs *inside* the image. We rely on De Morgan duality —
// `erode'(M) = NOT dilate(NOT M)` where erode' treats out-of-bounds as true —
// so the existing dilate (which treats out-of-bounds as false) is enough.
//
// Concretely: NOT erode'(M) = dilate(NOT M), so
//   inner_outline    = M AND NOT erode'(M)        = M AND dilate(NOT M)
//   centered_outline = dilate(M) AND NOT erode'(M) = dilate(M) AND dilate(NOT M).
fn inner_outline_mask(
    mask: &[bool],
    width: usize,
    height: usize,
    thickness: usize,
    buffers: &mut MaskBuffers,
) -> Vec<bool> {
    if thickness == 0 || mask.is_empty() {
        return mask.to_vec();
    }
    let not_mask: Vec<bool> = mask.iter().map(|v| !*v).collect();
    dilate_mask_into(
        &not_mask,
        width,
        height,
        thickness,
        &mut buffers.scratch,
        &mut buffers.a,
    );
    mask.iter()
        .zip(&buffers.a)
        .map(|(value, dilated_complement)| *value && *dilated_complement)
        .collect()
}

fn centered_outline_mask(
    mask: &[bool],
    width: usize,
    height: usize,
    thickness: usize,
    buffers: &mut MaskBuffers,
) -> Vec<bool> {
    if thickness == 0 || mask.is_empty() {
        return mask.to_vec();
    }
    dilate_mask_into(
        mask,
        width,
        height,
        thickness,
        &mut buffers.scratch,
        &mut buffers.a,
    );
    let not_mask: Vec<bool> = mask.iter().map(|v| !*v).collect();
    dilate_mask_into(
        &not_mask,
        width,
        height,
        thickness,
        &mut buffers.scratch,
        &mut buffers.b,
    );
    buffers
        .a
        .iter()
        .zip(&buffers.b)
        .map(|(dilated, dilated_complement)| *dilated && *dilated_complement)
        .collect()
}

fn clear_border_background_from_mask(
    mask: &mut [bool],
    gray: &[u8],
    width: usize,
    height: usize,
    visited: &mut [bool],
) {
    if mask.is_empty()
        || gray.len() != mask.len()
        || visited.len() != mask.len()
        || width == 0
        || height == 0
    {
        return;
    }

    let threshold = radiograph_background_threshold(gray);
    visited.fill(false);
    let mut queue = Vec::new();
    let push = |index: usize, visited: &mut [bool], mask: &mut [bool], queue: &mut Vec<usize>| {
        if index >= mask.len() || visited[index] || gray[index] > threshold {
            return;
        }
        visited[index] = true;
        mask[index] = false;
        queue.push(index);
    };

    for x in 0..width {
        push(x, visited, mask, &mut queue);
        push((height - 1) * width + x, visited, mask, &mut queue);
    }
    for y in 1..height {
        push(y * width, visited, mask, &mut queue);
        push(y * width + width - 1, visited, mask, &mut queue);
    }

    let mut head = 0;
    while head < queue.len() {
        let index = queue[head];
        head += 1;
        let x = index % width;
        let y = index / width;
        if x > 0 {
            push(index - 1, visited, mask, &mut queue);
        }
        if x + 1 < width {
            push(index + 1, visited, mask, &mut queue);
        }
        if y > 0 {
            push(index - width, visited, mask, &mut queue);
        }
        if y + 1 < height {
            push(index + width, visited, mask, &mut queue);
        }
    }
}

fn radiograph_background_threshold(gray: &[u8]) -> u8 {
    if percentile_fraction(gray, 0.01) > RADIOGRAPH_BACKGROUND_MAX_GRAY {
        0
    } else {
        RADIOGRAPH_BACKGROUND_MAX_GRAY
    }
}

fn dilate_mask_into(
    mask: &[bool],
    width: usize,
    height: usize,
    radius: usize,
    scratch: &mut [bool],
    output: &mut [bool],
) {
    if radius == 0 || mask.is_empty() {
        output.copy_from_slice(mask);
        return;
    }
    if width == 0 || height == 0 || mask.len() != width * height {
        output.fill(false);
        return;
    }

    scratch.fill(false);
    for y in 0..height {
        let row = y * width;
        let mut count = 0_usize;
        for x in 0..=radius.min(width - 1) {
            if mask[row + x] {
                count += 1;
            }
        }
        for x in 0..width {
            scratch[row + x] = count > 0;
            if x >= radius && mask[row + x - radius] {
                count -= 1;
            }
            let add = x + radius + 1;
            if add < width && mask[row + add] {
                count += 1;
            }
        }
    }

    let mut counts = vec![0_usize; width];
    for y in 0..=radius.min(height - 1) {
        let row = y * width;
        for x in 0..width {
            if scratch[row + x] {
                counts[x] += 1;
            }
        }
    }

    output.fill(false);
    for y in 0..height {
        let row = y * width;
        for x in 0..width {
            output[row + x] = counts[x] > 0;
        }
        if y >= radius {
            let remove_row = (y - radius) * width;
            for x in 0..width {
                if scratch[remove_row + x] {
                    counts[x] -= 1;
                }
            }
        }
        let add = y + radius + 1;
        if add < height {
            let add_row = add * width;
            for x in 0..width {
                if scratch[add_row + x] {
                    counts[x] += 1;
                }
            }
        }
    }
}

fn erode_mask_into(
    mask: &[bool],
    width: usize,
    height: usize,
    radius: usize,
    scratch: &mut [bool],
    output: &mut [bool],
) {
    if radius == 0 || mask.is_empty() {
        output.copy_from_slice(mask);
        return;
    }
    if width == 0 || height == 0 || mask.len() != width * height {
        output.fill(false);
        return;
    }

    let window = radius * 2 + 1;
    scratch.fill(false);
    for y in 0..height {
        let row = y * width;
        let mut count = 0_usize;
        for x in 0..=radius.min(width - 1) {
            if mask[row + x] {
                count += 1;
            }
        }
        for x in 0..width {
            scratch[row + x] = count == window;
            if x >= radius && mask[row + x - radius] {
                count -= 1;
            }
            let add = x + radius + 1;
            if add < width && mask[row + add] {
                count += 1;
            }
        }
    }

    let mut counts = vec![0_usize; width];
    for y in 0..=radius.min(height - 1) {
        let row = y * width;
        for x in 0..width {
            if scratch[row + x] {
                counts[x] += 1;
            }
        }
    }

    output.fill(false);
    for y in 0..height {
        let row = y * width;
        for x in 0..width {
            output[row + x] = counts[x] == window;
        }
        if y >= radius {
            let remove_row = (y - radius) * width;
            for x in 0..width {
                if scratch[remove_row + x] {
                    counts[x] -= 1;
                }
            }
        }
        let add = y + radius + 1;
        if add < height {
            let add_row = add * width;
            for x in 0..width {
                if scratch[add_row + x] {
                    counts[x] += 1;
                }
            }
        }
    }
}

fn close_mask_into(
    mask: &[bool],
    width: usize,
    height: usize,
    radius: usize,
    buffers: &mut MaskBuffers,
) {
    dilate_mask_into(
        mask,
        width,
        height,
        radius,
        &mut buffers.scratch,
        &mut buffers.a,
    );
    erode_mask_into(
        &buffers.a,
        width,
        height,
        radius,
        &mut buffers.scratch,
        &mut buffers.b,
    );
}

fn remove_small_components_into(
    mask: &[bool],
    width: usize,
    height: usize,
    min_area: usize,
    output: &mut [bool],
    visited: &mut [bool],
) {
    if min_area <= 1
        || width == 0
        || height == 0
        || mask.len() != width * height
        || visited.len() != mask.len()
    {
        output.copy_from_slice(mask);
        return;
    }

    output.fill(false);
    visited.fill(false);
    let mut stack = Vec::new();
    let mut component = Vec::new();
    for start in 0..mask.len() {
        if visited[start] || !mask[start] {
            continue;
        }
        visited[start] = true;
        stack.push(start);
        component.clear();
        while let Some(index) = stack.pop() {
            component.push(index);
            let x = index % width;
            let y = index / width;
            let min_y = y.saturating_sub(1);
            let max_y = (y + 1).min(height - 1);
            let min_x = x.saturating_sub(1);
            let max_x = (x + 1).min(width - 1);
            for yy in min_y..=max_y {
                for xx in min_x..=max_x {
                    let neighbor = yy * width + xx;
                    if visited[neighbor] || !mask[neighbor] {
                        continue;
                    }
                    visited[neighbor] = true;
                    stack.push(neighbor);
                }
            }
        }
        if component.len() >= min_area {
            for index in &component {
                output[*index] = true;
            }
        }
    }
}

fn fill_holes_into(mask: &[bool], width: usize, height: usize, output: &mut [bool]) {
    if width == 0 || height == 0 || mask.len() != width * height {
        output.copy_from_slice(mask);
        return;
    }

    output.fill(false);
    let mut queue = Vec::new();
    let push = |index: usize, output: &mut [bool], queue: &mut Vec<usize>| {
        if !mask[index] && !output[index] {
            output[index] = true;
            queue.push(index);
        }
    };
    for x in 0..width {
        push(x, output, &mut queue);
        push((height - 1) * width + x, output, &mut queue);
    }
    for y in 0..height {
        push(y * width, output, &mut queue);
        push(y * width + width - 1, output, &mut queue);
    }

    let mut head = 0;
    while head < queue.len() {
        let index = queue[head];
        head += 1;
        let x = index % width;
        let y = index / width;
        if x > 0 {
            push(index - 1, output, &mut queue);
        }
        if x + 1 < width {
            push(index + 1, output, &mut queue);
        }
        if y > 0 {
            push(index - width, output, &mut queue);
        }
        if y + 1 < height {
            push(index + width, output, &mut queue);
        }
    }

    for index in 0..output.len() {
        let outside = output[index];
        output[index] = mask[index] || !outside;
    }
}

fn minimum_bone_area_pixels(width: usize, height: usize) -> usize {
    (width * height / 12_000).max(MINIMUM_BONE_AREA_FLOOR_PIXELS)
}

fn percentile_fraction(pixels: &[u8], percentile: f64) -> u8 {
    if pixels.is_empty() {
        return 0;
    }

    let mut histogram = [0_usize; 256];
    for value in pixels {
        histogram[*value as usize] += 1;
    }
    let target = percentile_fraction_target(pixels.len(), percentile);

    let mut cumulative = 0_isize;
    for (value, count) in histogram.iter().enumerate() {
        cumulative += *count as isize;
        if cumulative > target {
            return value as u8;
        }
    }
    255
}

fn percentile_fraction_target(len: usize, percentile: f64) -> isize {
    let target = ((len - 1) as f64 * percentile).round() as isize;
    target.clamp(0, len as isize - 1)
}

// Histogram-based integer percentile. O(N) — one pass for the histogram,
// another over 256 bins for the CDF. Faster than sorting for image-sized
// inputs.
fn percentile(values: &[u8], percentile: usize) -> u8 {
    let mut histogram = [0_usize; 256];
    for value in values {
        histogram[*value as usize] += 1;
    }

    // div_ceil so 50th percentile of an even-length series picks the upper
    // of the two middle values — matches numpy's "higher" interpolation mode.
    let target = (values.len() * percentile).div_ceil(100);
    let mut seen = 0_usize;
    for (value, count) in histogram.iter().enumerate() {
        seen += count;
        if seen >= target {
            return value as u8;
        }
    }
    255
}

// Population count over a bool mask. .filter().count() — the compiler
// vectorizes this for big slices.
fn count_mask(mask: &[bool]) -> usize {
    mask.iter().filter(|value| **value).count()
}

fn count_components(mask: &[bool], width: usize, height: usize, visited: &mut [bool]) -> usize {
    if width == 0 || height == 0 || mask.len() != width * height || visited.len() != mask.len() {
        return 0;
    }

    visited.fill(false);
    let mut stack = Vec::new();
    let mut count = 0;
    for start in 0..mask.len() {
        if visited[start] || !mask[start] {
            continue;
        }
        count += 1;
        visited[start] = true;
        stack.push(start);
        while let Some(index) = stack.pop() {
            let x = index % width;
            let y = index / width;
            let min_y = y.saturating_sub(1);
            let max_y = (y + 1).min(height - 1);
            let min_x = x.saturating_sub(1);
            let max_x = (x + 1).min(width - 1);
            for yy in min_y..=max_y {
                for xx in min_x..=max_x {
                    let neighbor = yy * width + xx;
                    if visited[neighbor] || !mask[neighbor] {
                        continue;
                    }
                    visited[neighbor] = true;
                    stack.push(neighbor);
                }
            }
        }
    }
    count
}

#[cfg(test)]
mod tests {
    use super::*;

    // Smoke test: a 32×32 image with a bright smooth square (tooth-like — bright
    // and low-texture) confirms the pipeline runs end-to-end and returns valid
    // RGBA previews with the expected mode prefix. We assert the tooth side fires
    // on the obvious bright blob; we do NOT assert bone, since the bone forest
    // keys on trabecular texture that a synthetic toy image cannot reproduce.
    #[test]
    fn generate_tooth_overlay_returns_overlay_images() {
        let mut gray = vec![24_u8; 32 * 32];
        for y in 8..24 {
            for x in 9..23 {
                gray[y * 32 + x] = 210;
            }
        }

        let result = generate_tooth_overlay(&PreviewImage::gray(32, 32, gray)).unwrap();

        assert_eq!(result.preview.width, 32);
        assert_eq!(result.preview.height, 32);
        assert_eq!(result.preview.format, PreviewFormat::Rgba8);
        assert_eq!(result.filled_preview.format, PreviewFormat::Rgba8);
        assert!(result.tooth_pixels > 0);
        assert!(result.coverage > 0.0);
        assert!(result.candidate_count > 0);
        assert!(
            result
                .mode
                .starts_with("dynamic tooth and bone level overlay")
        );
    }

    #[test]
    fn generate_tooth_overlay_rejects_mismatched_buffer_length() {
        let error = generate_tooth_overlay(&PreviewImage::gray(8, 8, vec![0_u8; 63])).unwrap_err();

        assert!(error.contains("preview pixel length = 63, want 64"));
    }

    // Two morphological invariants in one: (a) lone pixels in the corner
    // get dropped (small-component removal), and (b) a single-pixel hole
    // in the middle of the big rectangle gets filled in (hole filling).
    #[test]
    fn clean_tooth_mask_removes_small_islands_and_fills_internal_holes() {
        const WIDTH: usize = 80;
        const HEIGHT: usize = 60;

        let mut mask = vec![false; WIDTH * HEIGHT];
        fill_mask_rect(&mut mask, WIDTH, 20, 10, 22, 40);
        mask[30 * WIDTH + 30] = false;
        mask[6 * WIDTH + 66] = true;

        let mut buffers = MaskBuffers::new(WIDTH * HEIGHT);
        let cleaned = clean_tooth_mask(&mask, WIDTH, HEIGHT, &mut buffers);

        assert!(cleaned[30 * WIDTH + 30]);
        assert!(cleaned[25 * WIDTH + 30]);
        assert!(!cleaned[6 * WIDTH + 66]);
    }

    // Confirms the overlay precedence rule: bone outline is suppressed
    // inside the tooth region (we don't want red lines crossing through
    // green ones), and tooth outline appears along the rectangle border.
    #[test]
    fn overlay_preview_outlines_tooth_and_suppresses_bone_inside_tooth() {
        const WIDTH: usize = 9;
        const HEIGHT: usize = 9;

        let gray = vec![96_u8; WIDTH * HEIGHT];
        let mut tooth_mask = vec![false; WIDTH * HEIGHT];
        let mut bone_mask = vec![false; WIDTH * HEIGHT];
        fill_mask_rect(&mut tooth_mask, WIDTH, 1, 1, 7, 7);
        bone_mask[4 * WIDTH + 4] = true;

        let mut buffers = MaskBuffers::new(WIDTH * HEIGHT);
        let preview = overlay_preview(
            &gray,
            WIDTH as u32,
            HEIGHT as u32,
            &tooth_mask,
            &bone_mask,
            false,
            &mut buffers,
        );
        let red_mask = red_mask_from_rgba(&preview);
        let green_mask = green_mask_from_rgba(&preview);

        assert!(!red_mask[4 * WIDTH + 4]);
        assert!(green_mask[WIDTH + 1]);
        assert!(green_mask[7 * WIDTH + 7]);
        assert!(!green_mask[4 * WIDTH + 4]);
    }

    // Filled overlay invariants: outside both masks → untouched grayscale;
    // strict interior of either mask (the eroded core, not on the outline
    // band) → alpha-blended fill; the outline band → solid color overlaying
    // the blend. Bone outline is suppressed inside the tooth region by the
    // exclude_mask, so tooth wins on overlap.
    #[test]
    fn overlay_preview_draws_outlines_over_blended_fills() {
        const WIDTH: usize = 30;
        const HEIGHT: usize = 30;

        let gray = vec![0_u8; WIDTH * HEIGHT];
        let mut tooth_mask = vec![false; WIDTH * HEIGHT];
        let mut bone_mask = vec![false; WIDTH * HEIGHT];
        // 10×10 tooth at (5..15, 5..15). 2-px erosion leaves a 6×6 strict
        // interior at (7..13, 7..13).
        fill_mask_rect(&mut tooth_mask, WIDTH, 5, 5, 10, 10);
        // 10×6 bone at (5..15, 20..26). Centered outline strips around
        // the rect; eroded core is rows 22..24, cols 7..13.
        fill_mask_rect(&mut bone_mask, WIDTH, 5, 20, 10, 6);

        let mut buffers = MaskBuffers::new(WIDTH * HEIGHT);
        let preview = overlay_preview(
            &gray,
            WIDTH as u32,
            HEIGHT as u32,
            &tooth_mask,
            &bone_mask,
            true,
            &mut buffers,
        );

        let expect_blend = |dst: u8, src: [u8; 4]| -> [u8; 3] {
            let a = SECTION_FILL_ALPHA as u32;
            let inv = 255 - a;
            let ch = |s: u8| -> u8 { ((s as u32 * a + dst as u32 * inv + 127) / 255) as u8 };
            [ch(src[0]), ch(src[1]), ch(src[2])]
        };
        let idx = |y: usize, x: usize| y * WIDTH + x;

        // Outside both masks → grayscale untouched.
        assert_eq!(rgb_at(&preview, idx(0, 0)), [0, 0, 0]);
        // Tooth strict interior (in eroded → not on inner outline) → alpha green.
        assert_eq!(rgb_at(&preview, idx(10, 10)), expect_blend(0, TOOTH_GREEN));
        // Tooth boundary pixel (in mask, outside eroded) → solid outline green.
        assert_eq!(
            rgb_at(&preview, idx(5, 5)),
            [TOOTH_GREEN[0], TOOTH_GREEN[1], TOOTH_GREEN[2]]
        );
        // Bone strict interior (in eroded core) → alpha red, no outline overlay.
        assert_eq!(rgb_at(&preview, idx(22, 8)), expect_blend(0, BONE_RED));
        // Bone boundary pixel (in mask, outside eroded → in centered outline)
        // and outside tooth_mask → solid outline red.
        assert_eq!(
            rgb_at(&preview, idx(20, 5)),
            [BONE_RED[0], BONE_RED[1], BONE_RED[2]]
        );
    }

    // Both shipped forests must decode and carry trees. Guards the assets
    // produced by examples/train_tooth.rs against corruption / format drift.
    #[test]
    fn tooth_forest_model_loads() {
        let forest = loaded_tooth_model().expect("tooth forest should load");

        assert!(!forest.trees.is_empty());
        assert!(forest.trees.iter().all(|tree| !tree.is_empty()));
    }

    #[test]
    fn bone_forest_model_loads() {
        let forest = loaded_bone_model().expect("bone forest should load");

        assert!(!forest.trees.is_empty());
        assert!(forest.trees.iter().all(|tree| !tree.is_empty()));
    }

    // Test fixture: paint a filled rectangle into a 1D row-major mask.
    fn fill_mask_rect(
        mask: &mut [bool],
        width: usize,
        x: usize,
        y: usize,
        rect_width: usize,
        rect_height: usize,
    ) {
        for yy in y..y + rect_height {
            for xx in x..x + rect_width {
                mask[yy * width + xx] = true;
            }
        }
    }

    // Extract a bool mask from an RGBA preview marking pixels that equal
    // BONE_RED — used to assert presence/absence of bone outline pixels.
    fn red_mask_from_rgba(preview: &PreviewImage) -> Vec<bool> {
        preview
            .pixels
            .chunks_exact(4)
            .map(|pixel| {
                pixel[0] == BONE_RED[0] && pixel[1] == BONE_RED[1] && pixel[2] == BONE_RED[2]
            })
            .collect()
    }

    fn green_mask_from_rgba(preview: &PreviewImage) -> Vec<bool> {
        preview
            .pixels
            .chunks_exact(4)
            .map(|pixel| {
                pixel[0] == TOOTH_GREEN[0]
                    && pixel[1] == TOOTH_GREEN[1]
                    && pixel[2] == TOOTH_GREEN[2]
            })
            .collect()
    }

    fn rgb_at(preview: &PreviewImage, index: usize) -> [u8; 3] {
        let base = index * 4;
        [
            preview.pixels[base],
            preview.pixels[base + 1],
            preview.pixels[base + 2],
        ]
    }
}
