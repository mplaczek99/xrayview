#[cfg(test)]
use crate::contracts::BackendErrorCode;
use crate::{
    contracts::{BackendError, PaletteName, ProcessStudyCommand, default_processing_manifest},
    render::{PreviewFormat, PreviewImage},
};
use std::sync::Arc;

pub const MIN_CONTRAST: f64 = 0.1;

// The four knobs the user can flip on a study. Order of application matters
// and is set in process_grayscale_pixels — see comments there.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct GrayscaleControls {
    pub invert: bool,
    // Signed because brightness can shift the LUT down as well as up.
    // Bounded to [-256, 256] at validation time.
    pub brightness: i32,
    // 1.0 = no-op, >1.0 stretches contrast around the midpoint, <1.0 compresses.
    pub contrast: f64,
    pub equalize: bool,
}

// What the preset-resolver hands to the pipeline: the merged controls plus
// the chosen palette and whether to render a before/after compare strip.
#[derive(Debug, Clone, PartialEq)]
pub struct ResolvedProcessStudy {
    pub controls: GrayscaleControls,
    pub palette: Palette,
    pub compare: bool,
}

// Result of a processing run — the (possibly-recolored, possibly-compared)
// image plus a human-readable description for the UI footer.
#[derive(Debug, Clone, PartialEq)]
pub struct PipelineOutput {
    pub preview: PreviewImage,
    pub mode: String,
}

// All errors here are "you passed me the wrong shape of data" — never I/O
// or transient failures. That's why they're cheap to compare with Eq.
#[derive(Debug, thiserror::Error, PartialEq, Eq)]
pub enum ProcessingError {
    #[error("grayscale processing requires Gray8 preview input")]
    NonGray8Input,
    #[error("pseudocolor palettes require Gray8 preview input")]
    PaletteRequiresGray8,
    #[error("compare preview requires Gray8 source on the left side")]
    CompareRequiresGray8Left,
    #[error("compare preview requires matching image dimensions")]
    CompareDimensionMismatch,
    // Width gets doubled in the compare path — guard against u32 overflow.
    #[error("compare preview width overflow")]
    CompareWidthOverflow,
    #[error("palette must be one of: none, hot, bone")]
    UnknownPalette,
}

// Pseudocolor palettes. "Hot" is the classic black→red→yellow→white ramp,
// "Bone" is a grayscale tinted toward bluish/warm tones. Both expand a
// Gray8 input into RGBA8.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Palette {
    None,
    Hot,
    Bone,
}

impl Palette {
    // Used only in the human-readable `mode` string the UI displays. Keep
    // these stable — the frontend may match against them.
    pub fn label(self) -> &'static str {
        match self {
            Self::None => "none",
            Self::Hot => "hot",
            Self::Bone => "bone",
        }
    }
}

// Merge a user-supplied process command with the preset defaults. The rule
// is: every Option<T> field falls back to the named preset's value, while
// non-Option fields (invert/equalize/compare) come straight from the command.
//
// This is split out from process_rendered_preview so the validation errors
// can be reported to the user *before* we kick off the (potentially expensive)
// image work.
pub fn resolve_process_study_command(
    command: &ProcessStudyCommand,
) -> Result<ResolvedProcessStudy, BackendError> {
    let manifest = default_processing_manifest();
    // Normalize the preset id: trim whitespace + lowercase. The TS side
    // *should* already do this but we don't trust it.
    let preset_id = command.preset_id.trim().to_ascii_lowercase();
    let preset = manifest
        .presets
        .iter()
        .find(|preset| preset.id == preset_id)
        .ok_or_else(|| {
            // List the valid ids in the error so the frontend can show useful
            // feedback without round-tripping back to the manifest endpoint.
            BackendError::invalid_input(format!(
                "preset must be one of: {}",
                manifest
                    .presets
                    .iter()
                    .map(|preset| preset.id.as_str())
                    .collect::<Vec<_>>()
                    .join(", ")
            ))
        })?;

    // Brightness bound matches the slider range in the UI. ±256 is enough
    // to wash out or black out anything; tighter would clip valid use.
    if let Some(brightness) = command.brightness
        && !(-256..=256).contains(&brightness)
    {
        return Err(BackendError::invalid_input(format!(
            "brightness must be between -256 and 256, got {brightness}"
        )));
    }

    // Contrast must be finite and above the lowest useful UI value. Zero
    // collapses every pixel to the midpoint, producing a flat gray image.
    if let Some(contrast) = command.contrast
        && (!contrast.is_finite() || contrast < MIN_CONTRAST)
    {
        return Err(BackendError::invalid_input(format!(
            "contrast must be >= {MIN_CONTRAST}, got {contrast}"
        )));
    }

    // Fall back to the preset's palette if the user didn't choose one.
    let palette = Palette::from(command.palette.unwrap_or(preset.controls.palette));

    Ok(ResolvedProcessStudy {
        controls: GrayscaleControls {
            invert: command.invert,
            // .unwrap_or with preset defaults — same pattern as palette above.
            brightness: command.brightness.unwrap_or(preset.controls.brightness),
            contrast: command.contrast.unwrap_or(preset.controls.contrast),
            equalize: command.equalize,
        },
        palette,
        compare: command.compare,
    })
}

// The actual pipeline. Stages run in this order:
//   1. (capture) Stash a copy of the original if compare is requested.
//   2. Apply grayscale controls (invert → brightness → contrast → equalize).
//   3. Apply palette (Gray8 → RGBA8 expansion if not None).
//   4. Combine with the captured original side-by-side if compare is set.
//
// The compare snapshot has to happen *before* step 2 — otherwise we'd be
// comparing the processed image to itself.
pub fn process_rendered_preview(
    mut source_preview: PreviewImage,
    controls: GrayscaleControls,
    palette: Palette,
    compare: bool,
) -> Result<PipelineOutput, ProcessingError> {
    // Only Gray8 in. Anything else means upstream is broken and we should
    // surface that loudly rather than silently producing garbage.
    if source_preview.format != PreviewFormat::Gray8 {
        return Err(ProcessingError::NonGray8Input);
    }

    // Cheap clone — Arc<[u8]> just bumps a refcount. The CoW happens below
    // when Arc::make_mut is called.
    let source_for_compare = compare.then(|| source_preview.clone());
    // Arc::make_mut triggers a copy iff there are other refs; if the caller
    // gave us a unique buffer this is in-place. Snapshot before this is the
    // whole point of the clone above.
    let mut mode = process_grayscale_pixels(Arc::make_mut(&mut source_preview.pixels), controls);
    if palette != Palette::None {
        mode = format!("{mode} with {} palette", palette.label());
        source_preview = apply_named_palette(&source_preview, palette)?;
    }
    if let Some(source_for_compare) = source_for_compare {
        source_preview = combine_comparison(&source_for_compare, &source_preview)?;
        mode = format!("comparison of grayscale and {mode}");
    }

    Ok(PipelineOutput {
        preview: source_preview,
        mode,
    })
}

// Apply invert/brightness/contrast/equalize to a gray buffer in place.
//
// The trick here: invert/brightness/contrast all compose into a single 256-byte
// lookup table that we then apply with one linear pass. That's ~256× cheaper
// than running three passes over a multi-megapixel image. Equalize doesn't
// compose with the LUT (it depends on the histogram of the *current* pixels),
// so we flush the pending LUT first if equalize is on.
pub fn process_grayscale_pixels(pixels: &mut [u8], controls: GrayscaleControls) -> String {
    // Capacity 4 covers the worst case (invert + brightness + contrast + equalize).
    let mut mode_parts = Vec::with_capacity(4);
    mode_parts.push("grayscale".to_string());
    let mut lookup = identity_lookup_table();
    // pending_lookup tracks whether the LUT diverges from identity — saves us
    // from doing a no-op pass over the pixel buffer.
    let mut pending_lookup = false;

    if controls.invert {
        for value in &mut lookup {
            *value = 255 - *value;
        }
        pending_lookup = true;
        // Rewrite "grayscale" → "inverted grayscale" rather than appending,
        // so the mode string reads naturally.
        mode_parts[0] = "inverted grayscale".to_string();
    }
    if controls.brightness != 0 {
        for value in &mut lookup {
            // Widen to i32 first so the add can underflow/overflow into the
            // clamp range; clamp_lookup_value then truncates back to u8.
            *value = clamp_lookup_value(i32::from(*value) + controls.brightness);
        }
        pending_lookup = true;
        // {:+} forces a leading sign; "brightness +10" / "brightness -5".
        mode_parts.push(format!("brightness {:+}", controls.brightness));
    }
    if controls.contrast != 1.0 {
        for value in &mut lookup {
            // Stretch around the midpoint (128). At contrast=2.0, value 64
            // becomes 0 and value 192 becomes 255. .round() before clamp so
            // 127.5 doesn't get truncated to 127.
            let adjusted = 128.0 + controls.contrast * (f64::from(*value) - 128.0);
            *value = clamp_lookup_value(adjusted.round() as i32);
        }
        pending_lookup = true;
        mode_parts.push(format!("contrast {}", controls.contrast));
    }
    if controls.equalize {
        // Equalize needs the *post-LUT* histogram, so flush first.
        if pending_lookup {
            apply_lookup_in_place(pixels, &lookup);
            lookup = identity_lookup_table();
            pending_lookup = false;
        }
        equalize_histogram_in_place(pixels);
        mode_parts.push("histogram equalization".to_string());
    }

    // Single final pass through pixels if there's still an unapplied LUT.
    if pending_lookup {
        apply_lookup_in_place(pixels, &lookup);
    }
    // " with " separators — UI footer reads "grayscale with brightness +10 with contrast 1.4".
    mode_parts.join(" with ")
}

// String → Palette parser. Empty string also maps to None — the frontend
// sometimes sends "" when the user has explicitly chosen "no palette".
pub fn normalize_palette_name(name: &str) -> Result<Palette, ProcessingError> {
    match name.trim().to_ascii_lowercase().as_str() {
        "" | "none" => Ok(Palette::None),
        "hot" => Ok(Palette::Hot),
        "bone" => Ok(Palette::Bone),
        _ => Err(ProcessingError::UnknownPalette),
    }
}

// Expand a Gray8 image to RGBA8 by mapping each gray value through the
// chosen palette function. Function pointer indirection here is fine —
// it's hoisted out of the hot loop by flat_map.
fn apply_named_palette(
    preview: &PreviewImage,
    palette: Palette,
) -> Result<PreviewImage, ProcessingError> {
    if preview.format != PreviewFormat::Gray8 {
        return Err(ProcessingError::PaletteRequiresGray8);
    }

    let color_fn: fn(u8) -> [u8; 4] = match palette {
        Palette::Hot => hot_color,
        Palette::Bone => bone_color,
        // None is technically unreachable here (callers gate on this) but
        // we handle it gracefully rather than panicking.
        Palette::None => return Ok(preview.clone()),
    };

    // Pre-size the Vec to the exact output length to avoid reallocs on a
    // multi-megapixel image. 4 bytes per pixel for RGBA8.
    let mut pixels = Vec::with_capacity(preview.pixels.len() * 4);
    pixels.extend(preview.pixels.iter().copied().flat_map(color_fn));
    Ok(PreviewImage::rgba(preview.width, preview.height, pixels))
}

// Build a side-by-side comparison image: left = original Gray8 (expanded to
// RGBA on the fly), right = processed (Gray8 or RGBA8, either works). The
// output is always RGBA8 because the palette path may produce RGBA on the
// right and we need a uniform output format.
//
// This is on the hot path for the "compare" toggle, so the inner loop is
// written with raw pointer writes against an over-allocated Vec rather than
// the usual push()/extend(). Profiling showed Vec::push was the bottleneck
// on a 4k image.
fn combine_comparison(
    left: &PreviewImage,
    right: &PreviewImage,
) -> Result<PreviewImage, ProcessingError> {
    if left.format != PreviewFormat::Gray8 {
        return Err(ProcessingError::CompareRequiresGray8Left);
    }
    if left.width != right.width || left.height != right.height {
        return Err(ProcessingError::CompareDimensionMismatch);
    }

    let width = left.width as usize;
    // 2× width can overflow u32 for absurd inputs — refuse instead of wrapping.
    let combined_width = left
        .width
        .checked_mul(2)
        .ok_or(ProcessingError::CompareWidthOverflow)?;
    let combined_width_usize = combined_width as usize;
    let output_len = combined_width_usize * left.height as usize * 4;
    let mut pixels: Vec<u8> = Vec::with_capacity(output_len);
    let mut dst = 0;

    // SAFETY: pixels has capacity `output_len` (reserved above), and the
    // loop body below writes each byte in 0..output_len exactly once before
    // pixels.set_len(dst) exposes them. The inner SAFETY note before
    // set_len repeats the invariant where the bytes become visible.
    unsafe {
        let output = pixels.as_mut_ptr();
        for row in 0..left.height as usize {
            // --- left half: Gray8 → RGBA8 ---
            let left_start = row * width;
            for x in 0..width {
                let value = left.pixels[left_start + x];
                // R = G = B = gray, A = 255 (opaque).
                output.add(dst).write(value);
                output.add(dst + 1).write(value);
                output.add(dst + 2).write(value);
                output.add(dst + 3).write(255);
                dst += 4;
            }

            // --- right half: either Gray8 expansion or a direct memcpy ---
            match right.format {
                PreviewFormat::Gray8 => {
                    let right_start = row * width;
                    for x in 0..width {
                        let value = right.pixels[right_start + x];
                        output.add(dst).write(value);
                        output.add(dst + 1).write(value);
                        output.add(dst + 2).write(value);
                        output.add(dst + 3).write(255);
                        dst += 4;
                    }
                }
                PreviewFormat::Rgba8 => {
                    // Right is already RGBA — straight memcpy is fastest.
                    let right_start = row * width * 4;
                    std::ptr::copy_nonoverlapping(
                        right.pixels.as_ptr().add(right_start),
                        output.add(dst),
                        width * 4,
                    );
                    dst += width * 4;
                }
            }
        }
        // Debug-only invariant: we wrote exactly the bytes we reserved.
        debug_assert_eq!(dst, output_len);
        // SAFETY: `output_len` is the vector capacity, and the loop writes each
        // output byte exactly once before exposing it through the vector length.
        pixels.set_len(dst);
    }

    Ok(PreviewImage::rgba(combined_width, left.height, pixels))
}

// LUT that maps every byte to itself — the starting point when composing
// invert/brightness/contrast.
fn identity_lookup_table() -> [u8; 256] {
    let mut lookup = [0_u8; 256];
    for (index, value) in lookup.iter_mut().enumerate() {
        *value = index as u8;
    }
    lookup
}

// Single linear pass over the image substituting each byte via the LUT. The
// inner indexed access is bounds-checked, but the optimizer turns the table
// into register-resident memory because it's [u8; 256] (small + fixed-size).
fn apply_lookup_in_place(pixels: &mut [u8], lookup: &[u8; 256]) {
    for value in pixels {
        *value = lookup[*value as usize];
    }
}

// Histogram equalization in place. Two passes over the buffer: one to
// build the histogram, one to apply the resulting LUT.
fn equalize_histogram_in_place(pixels: &mut [u8]) {
    if pixels.is_empty() {
        return;
    }

    let mut histogram = [0_usize; 256];
    for value in pixels.iter() {
        histogram[*value as usize] += 1;
    }

    // If the image is flat (single value) equalize_lookup returns None — no
    // point applying an identity LUT.
    let Some(lookup) = equalize_lookup(histogram, pixels.len()) else {
        return;
    };
    apply_lookup_in_place(pixels, &lookup);
}

// Classic histogram equalization formula:
//   new[i] = round((cdf[i] - cdf_min) / (total - cdf_min) * 255)
// where cdf_min is the CDF value of the lowest non-empty bin. Doing it in
// integer math with `+ denom/2` for round-to-nearest avoids floats entirely.
fn equalize_lookup(histogram: [usize; 256], total: usize) -> Option<[u8; 256]> {
    let mut cdf = 0_usize;
    let mut cdf_min = 0_usize;
    let mut found = false;
    for count in histogram {
        cdf += count;
        // First non-empty bin's CDF is cdf_min by definition.
        if !found && count != 0 {
            cdf_min = cdf;
            found = true;
        }
    }

    // Flat image — every pixel is the same value. Nothing to redistribute.
    if cdf_min == total {
        return None;
    }

    let denom = total - cdf_min;
    let mut lookup = [0_u8; 256];
    cdf = 0;
    for (index, count) in histogram.into_iter().enumerate() {
        cdf += count;
        // Bins below cdf_min stay 0 — the spec says so and it's what makes
        // truly black pixels stay truly black after equalization.
        if cdf <= cdf_min {
            continue;
        }
        // `+ denom/2` is the integer round-to-nearest trick.
        lookup[index] = (((cdf - cdf_min) * 255 + denom / 2) / denom) as u8;
    }
    Some(lookup)
}

// i32 → u8 with clamping. Pulled out so the LUT build sites don't have to
// repeat the `.clamp(0, 255) as u8` dance.
fn clamp_lookup_value(value: i32) -> u8 {
    value.clamp(0, 255) as u8
}

// "Hot" colormap: black → red → yellow → white in three linear segments.
// Each segment covers ~85 gray values (256/3); the *3 multiplier inside each
// segment stretches that range back to [0, 255]. Last segment includes 255
// (wildcard arm) so the math doesn't overflow.
fn hot_color(value: u8) -> [u8; 4] {
    match value {
        0..=84 => [value * 3, 0, 0, 255],
        85..=169 => [255, (value - 85) * 3, 0, 255],
        _ => [255, 255, (value - 170) * 3, 255],
    }
}

// "Bone" colormap: a bluish-tinted grayscale that pushes whites cooler.
// white_boost adds extra brightness in the top half so bone structures pop.
// The 7/8 multiplier knocks the base down a hair so the boost doesn't blow
// out highlights. The exact coefficients are the kind of thing where you
// tweak until it looks right and then never touch again.
fn bone_color(value: u8) -> [u8; 4] {
    let value = i32::from(value);
    let white_boost = (value - 128).max(0);
    [
        clamp_lookup_value((value * 7) / 8 + white_boost),
        clamp_lookup_value((value * 7) / 8 + white_boost + value / 16),
        clamp_lookup_value(value + white_boost / 2),
        255,
    ]
}

impl From<PaletteName> for Palette {
    fn from(name: PaletteName) -> Self {
        match name {
            PaletteName::None => Self::None,
            PaletteName::Hot => Self::Hot,
            PaletteName::Bone => Self::Bone,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // Verifies the LUT composition order (invert THEN brightness):
    //   0 → invert → 255 → +10 → 255 (clamped, was already at the top)
    //   64 → invert → 191 → +10 → 201
    //   128 → invert → 127 → +10 → 137
    //   255 → invert → 0 → +10 → 10
    // If anyone reorders the composition in process_grayscale_pixels this
    // test will explode.
    #[test]
    fn process_grayscale_applies_controls_in_order() {
        let mut pixels = vec![0, 64, 128, 255];

        let mode = process_grayscale_pixels(
            &mut pixels,
            GrayscaleControls {
                invert: true,
                brightness: 10,
                contrast: 1.0,
                equalize: false,
            },
        );

        assert_eq!(mode, "inverted grayscale with brightness +10");
        assert_eq!(pixels, vec![255, 201, 137, 10]);
    }

    // End-to-end: palette + compare. 2-wide grayscale becomes 4-wide RGBA
    // because compare doubles width and palette expands depth. Also pins the
    // mode string format the UI displays in the footer.
    #[test]
    fn process_rendered_preview_applies_palette_and_compare() {
        let source = PreviewImage::gray(2, 1, vec![0, 255]);
        let output = process_rendered_preview(
            source,
            GrayscaleControls {
                invert: false,
                brightness: 0,
                contrast: 1.0,
                equalize: false,
            },
            Palette::Hot,
            true,
        )
        .unwrap();

        assert_eq!(
            output.mode,
            "comparison of grayscale and grayscale with hot palette"
        );
        assert_eq!(output.preview.width, 4);
        assert_eq!(output.preview.height, 1);
        assert_eq!(output.preview.format, PreviewFormat::Rgba8);
    }

    // The preset fallback path: brightness/contrast/palette all unset on the
    // command, so they fill in from the "xray" preset's defaults (10, 1.4,
    // Bone). If anyone tweaks the default manifest, update these numbers too.
    #[test]
    fn resolve_process_study_command_uses_preset_defaults() {
        let resolved = resolve_process_study_command(&ProcessStudyCommand {
            study_id: "study-1".to_string(),
            preset_id: "xray".to_string(),
            invert: false,
            brightness: None,
            contrast: None,
            equalize: true,
            compare: false,
            palette: None,
        })
        .unwrap();

        assert_eq!(resolved.controls.brightness, 10);
        assert_eq!(resolved.controls.contrast, 1.4);
        assert_eq!(resolved.palette, Palette::Bone);
    }

    #[test]
    fn resolve_process_study_command_rejects_zero_contrast() {
        let error = resolve_process_study_command(&ProcessStudyCommand {
            study_id: "study-1".to_string(),
            preset_id: "default".to_string(),
            invert: false,
            brightness: None,
            contrast: Some(0.0),
            equalize: false,
            compare: false,
            palette: None,
        })
        .unwrap_err();

        assert_eq!(error.code, BackendErrorCode::InvalidInput);
        assert!(error.message.contains("contrast must be >= 0.1"));
    }
}
