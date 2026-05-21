use crate::{
    contracts::{BackendError, PaletteName, ProcessStudyCommand, default_processing_manifest},
    render::{PreviewFormat, PreviewImage},
};

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
    pub palette: String,
    pub compare: bool,
}

#[derive(Debug, Clone, PartialEq)]
pub struct PipelineOutput {
    pub preview: PreviewImage,
    pub mode: String,
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

    if let Some(brightness) = command.brightness {
        if !(-256..=256).contains(&brightness) {
            return Err(BackendError::invalid_input(format!(
                "brightness must be between -256 and 256, got {brightness}"
            )));
        }
    }

    if let Some(contrast) = command.contrast {
        if !contrast.is_finite() || contrast < 0.0 {
            return Err(BackendError::invalid_input(format!(
                "contrast must be >= 0.0, got {contrast}"
            )));
        }
    }

    let palette = command
        .palette
        .unwrap_or(preset.controls.palette)
        .contract_name();
    let palette = normalize_palette_name(palette).map_err(BackendError::invalid_input)?;

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
    source_preview: &PreviewImage,
    controls: GrayscaleControls,
    palette: &str,
    compare: bool,
) -> Result<PipelineOutput, String> {
    if source_preview.format != PreviewFormat::Gray8 {
        return Err("grayscale processing requires Gray8 preview input".to_string());
    }

    let mut processed_pixels = source_preview.pixels.clone();
    let mut mode = process_grayscale_pixels(&mut processed_pixels, controls);
    let normalized_palette = normalize_palette_name(palette)?;

    let mut output_preview = PreviewImage::gray(
        source_preview.width,
        source_preview.height,
        processed_pixels,
    );
    if normalized_palette != "none" {
        mode = format!("{mode} with {normalized_palette} palette");
        output_preview = apply_named_palette(&output_preview, &normalized_palette)?;
    }

    if compare {
        output_preview = combine_comparison(source_preview, &output_preview)?;
        mode = format!("comparison of grayscale and {mode}");
    }

    Ok(PipelineOutput {
        preview: output_preview,
        mode,
    })
}

pub fn process_grayscale_pixels(pixels: &mut [u8], controls: GrayscaleControls) -> String {
    let mut mode = "grayscale".to_string();
    let mut lookup = identity_lookup_table();
    let mut pending_lookup = false;

    if controls.invert {
        for value in &mut lookup {
            *value = 255 - *value;
        }
        pending_lookup = true;
        mode = "inverted grayscale".to_string();
    }
    if controls.brightness != 0 {
        for value in &mut lookup {
            *value = clamp_lookup_value(i32::from(*value) + controls.brightness);
        }
        pending_lookup = true;
        mode = format!("{mode} with brightness {:+}", controls.brightness);
    }
    if controls.contrast != 1.0 {
        for value in &mut lookup {
            let adjusted = 128.0 + controls.contrast * (f64::from(*value) - 128.0);
            *value = clamp_lookup_value(adjusted.round() as i32);
        }
        pending_lookup = true;
        mode = format!("{mode} with contrast {}", controls.contrast);
    }
    if controls.equalize {
        if pending_lookup {
            apply_lookup_in_place(pixels, &lookup);
            lookup = identity_lookup_table();
            pending_lookup = false;
        }
        equalize_histogram_in_place(pixels);
        mode = format!("{mode} with histogram equalization");
    }

    if pending_lookup {
        apply_lookup_in_place(pixels, &lookup);
    }
    mode
}

fn normalize_palette_name(name: &str) -> Result<String, String> {
    match name.trim().to_ascii_lowercase().as_str() {
        "" | "none" => Ok("none".to_string()),
        "hot" => Ok("hot".to_string()),
        "bone" => Ok("bone".to_string()),
        _ => Err("palette must be one of: none, hot, bone".to_string()),
    }
}

fn apply_named_palette(preview: &PreviewImage, palette: &str) -> Result<PreviewImage, String> {
    if preview.format != PreviewFormat::Gray8 {
        return Err("pseudocolor palettes require Gray8 preview input".to_string());
    }

    let color_fn: fn(u8) -> [u8; 4] = match palette {
        "hot" => hot_color,
        "bone" => bone_color,
        _ => return Err("palette must be one of: none, hot, bone".to_string()),
    };

    let mut pixels = Vec::with_capacity(preview.pixels.len() * 4);
    for value in &preview.pixels {
        pixels.extend_from_slice(&color_fn(*value));
    }
    Ok(PreviewImage::rgba(preview.width, preview.height, pixels))
}

fn combine_comparison(left: &PreviewImage, right: &PreviewImage) -> Result<PreviewImage, String> {
    if left.format != PreviewFormat::Gray8 {
        return Err("compare preview requires Gray8 source on the left side".to_string());
    }
    if left.width != right.width || left.height != right.height {
        return Err("compare preview requires matching image dimensions".to_string());
    }

    let width = left.width as usize;
    let combined_width = left
        .width
        .checked_mul(2)
        .ok_or_else(|| "compare preview width overflow".to_string())?;
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
            &source,
            GrayscaleControls {
                invert: false,
                brightness: 0,
                contrast: 1.0,
                equalize: false,
            },
            "hot",
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
            output_path: None,
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
        assert_eq!(resolved.palette, "bone");
    }
}
