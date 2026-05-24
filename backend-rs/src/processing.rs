use crate::{
    contracts::{BackendError, PaletteName, ProcessStudyCommand, default_processing_manifest},
    render::{PreviewFormat, PreviewImage},
};
use std::{borrow::Cow, sync::Arc};

#[derive(Debug, Clone, Copy, PartialEq)]
pub struct GrayscaleControls {
    pub invert: bool,
    pub brightness: i32,
    pub contrast: f64,
    pub equalize: bool,
}

#[derive(Debug, Clone, PartialEq)]
pub struct ResolvedProcessStudy {
    pub controls: GrayscaleControls,
    pub palette: Palette,
    pub compare: bool,
}

#[derive(Debug, Clone, PartialEq)]
pub struct PipelineOutput {
    pub preview: PreviewImage,
    pub mode: String,
}

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
    #[error("compare preview width overflow")]
    CompareWidthOverflow,
    #[error("palette must be one of: none, hot, bone")]
    UnknownPalette,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Palette {
    None,
    Hot,
    Bone,
}

impl Palette {
    pub fn label(self) -> &'static str {
        match self {
            Self::None => "none",
            Self::Hot => "hot",
            Self::Bone => "bone",
        }
    }
}

pub fn resolve_process_study_command(
    command: &ProcessStudyCommand,
) -> Result<ResolvedProcessStudy, BackendError> {
    let manifest = default_processing_manifest();
    let preset_id = command.preset_id.trim().to_ascii_lowercase();
    let preset = manifest
        .presets
        .iter()
        .find(|preset| preset.id == preset_id)
        .ok_or_else(|| {
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

    if let Some(brightness) = command.brightness
        && !(-256..=256).contains(&brightness)
    {
        return Err(BackendError::invalid_input(format!(
            "brightness must be between -256 and 256, got {brightness}"
        )));
    }

    if let Some(contrast) = command.contrast
        && (!contrast.is_finite() || contrast < 0.0)
    {
        return Err(BackendError::invalid_input(format!(
            "contrast must be >= 0.0, got {contrast}"
        )));
    }

    let palette = command
        .palette
        .unwrap_or(preset.controls.palette)
        .contract_name();
    let palette = normalize_palette_name(palette)
        .map_err(|error| BackendError::invalid_input(error.to_string()))?;

    Ok(ResolvedProcessStudy {
        controls: GrayscaleControls {
            invert: command.invert,
            brightness: command.brightness.unwrap_or(preset.controls.brightness),
            contrast: command.contrast.unwrap_or(preset.controls.contrast),
            equalize: command.equalize,
        },
        palette,
        compare: command.compare,
    })
}

pub fn process_rendered_preview(
    mut source_preview: PreviewImage,
    controls: GrayscaleControls,
    palette: Palette,
    compare: bool,
) -> Result<PipelineOutput, ProcessingError> {
    if source_preview.format != PreviewFormat::Gray8 {
        return Err(ProcessingError::NonGray8Input);
    }

    let source_for_compare = compare.then(|| source_preview.clone());
    let mut mode = process_grayscale_pixels(Arc::make_mut(&mut source_preview.pixels), controls);
    if palette != Palette::None {
        mode = format!("{mode} with {} palette", palette.label());
        source_preview = apply_named_palette(&source_preview, palette)?;
    }
    if compare {
        let source_for_compare = source_for_compare.expect("compare source captured");
        source_preview = combine_comparison(&source_for_compare, &source_preview)?;
        mode = format!("comparison of grayscale and {mode}");
    }

    Ok(PipelineOutput {
        preview: source_preview,
        mode,
    })
}

pub fn process_grayscale_pixels(pixels: &mut [u8], controls: GrayscaleControls) -> String {
    let mut mode_parts: Vec<Cow<'static, str>> = Vec::with_capacity(4);
    mode_parts.push("grayscale".into());
    let mut lookup = identity_lookup_table();
    let mut pending_lookup = false;

    if controls.invert {
        for value in &mut lookup {
            *value = 255 - *value;
        }
        pending_lookup = true;
        mode_parts[0] = "inverted grayscale".into();
    }
    if controls.brightness != 0 {
        for value in &mut lookup {
            *value = clamp_lookup_value(i32::from(*value) + controls.brightness);
        }
        pending_lookup = true;
        mode_parts.push(format!("brightness {:+}", controls.brightness).into());
    }
    if controls.contrast != 1.0 {
        for value in &mut lookup {
            let adjusted = 128.0 + controls.contrast * (f64::from(*value) - 128.0);
            *value = clamp_lookup_value(adjusted.round() as i32);
        }
        pending_lookup = true;
        mode_parts.push(format!("contrast {}", controls.contrast).into());
    }
    if controls.equalize {
        if pending_lookup {
            apply_lookup_in_place(pixels, &lookup);
            lookup = identity_lookup_table();
            pending_lookup = false;
        }
        equalize_histogram_in_place(pixels);
        mode_parts.push("histogram equalization".into());
    }

    if pending_lookup {
        apply_lookup_in_place(pixels, &lookup);
    }
    mode_parts.join(" with ")
}

pub fn normalize_palette_name(name: &str) -> Result<Palette, ProcessingError> {
    match name.trim().to_ascii_lowercase().as_str() {
        "" | "none" => Ok(Palette::None),
        "hot" => Ok(Palette::Hot),
        "bone" => Ok(Palette::Bone),
        _ => Err(ProcessingError::UnknownPalette),
    }
}

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
        Palette::None => return Ok(preview.clone()),
    };

    let mut pixels = Vec::with_capacity(preview.pixels.len() * 4);
    pixels.extend(preview.pixels.iter().copied().flat_map(color_fn));
    Ok(PreviewImage::rgba(preview.width, preview.height, pixels))
}

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
    let combined_width = left
        .width
        .checked_mul(2)
        .ok_or(ProcessingError::CompareWidthOverflow)?;
    let mut pixels = vec![0; combined_width as usize * left.height as usize * 4];

    for row in 0..left.height as usize {
        let left_start = row * width;
        let dst_start = row * combined_width as usize * 4;
        for x in 0..width {
            let value = left.pixels[left_start + x];
            let dst = dst_start + x * 4;
            pixels[dst..dst + 4].copy_from_slice(&[value, value, value, 255]);
        }

        match right.format {
            PreviewFormat::Gray8 => {
                let right_start = row * width;
                for x in 0..width {
                    let value = right.pixels[right_start + x];
                    let dst = dst_start + (width + x) * 4;
                    pixels[dst..dst + 4].copy_from_slice(&[value, value, value, 255]);
                }
            }
            PreviewFormat::Rgba8 => {
                let right_start = row * width * 4;
                let dst = dst_start + width * 4;
                pixels[dst..dst + width * 4]
                    .copy_from_slice(&right.pixels[right_start..right_start + width * 4]);
            }
        }
    }

    Ok(PreviewImage::rgba(combined_width, left.height, pixels))
}

fn identity_lookup_table() -> [u8; 256] {
    let mut lookup = [0_u8; 256];
    for (index, value) in lookup.iter_mut().enumerate() {
        *value = index as u8;
    }
    lookup
}

fn apply_lookup_in_place(pixels: &mut [u8], lookup: &[u8; 256]) {
    for value in pixels {
        *value = lookup[*value as usize];
    }
}

fn equalize_histogram_in_place(pixels: &mut [u8]) {
    if pixels.is_empty() {
        return;
    }

    let mut histogram = [0_usize; 256];
    for value in pixels.iter() {
        histogram[*value as usize] += 1;
    }

    let Some(lookup) = equalize_lookup(histogram, pixels.len()) else {
        return;
    };
    apply_lookup_in_place(pixels, &lookup);
}

fn equalize_lookup(histogram: [usize; 256], total: usize) -> Option<[u8; 256]> {
    let mut cdf = 0_usize;
    let mut cdf_min = 0_usize;
    let mut found = false;
    for count in histogram {
        cdf += count;
        if !found && count != 0 {
            cdf_min = cdf;
            found = true;
        }
    }

    if cdf_min == total {
        return None;
    }

    let denom = total - cdf_min;
    let mut lookup = [0_u8; 256];
    cdf = 0;
    for (index, count) in histogram.into_iter().enumerate() {
        cdf += count;
        if cdf <= cdf_min {
            continue;
        }
        lookup[index] = (((cdf - cdf_min) * 255 + denom / 2) / denom) as u8;
    }
    Some(lookup)
}

fn clamp_lookup_value(value: i32) -> u8 {
    value.clamp(0, 255) as u8
}

fn hot_color(value: u8) -> [u8; 4] {
    match value {
        0..=84 => [value * 3, 0, 0, 255],
        85..=169 => [255, (value - 85) * 3, 0, 255],
        _ => [255, 255, (value - 170) * 3, 255],
    }
}

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

trait PaletteContractName {
    fn contract_name(self) -> &'static str;
}

impl PaletteContractName for PaletteName {
    fn contract_name(self) -> &'static str {
        match self {
            PaletteName::None => "none",
            PaletteName::Hot => "hot",
            PaletteName::Bone => "bone",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

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
}
