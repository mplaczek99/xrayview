// Compressed DICOM transfer syntaxes are supported via dicom-pixeldata.
// Supported: JPEG Baseline, JPEG-LS, JPEG 2000.
// Unsupported: JPEG 2000 Lossless Part 2 (rare), HEVC (not in dicom-pixeldata).

use std::path::Path;

use anyhow::{bail, Context, Result};
use dicom_core::DicomValue;
use dicom_dictionary_std::tags;
use dicom_object::{DefaultDicomObject, DicomAttribute, DicomObject, OpenFileOptions};
use dicom_pixeldata::PixelDecoder;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PreviewImage {
    pub width: u32,
    pub height: u32,
    pub pixels: Vec<u8>,
    pub format: PreviewFormat,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PreviewFormat {
    Gray8,
    Rgba8,
}

#[derive(Debug, Clone, Copy)]
struct NativeRenderConfig {
    bits_stored: u16,
    pixel_representation: u16,
    slope: f64,
    intercept: f64,
    window_center: f64,
    window_width: f64,
    use_window: bool,
    invert: bool,
}

pub fn load_dicom(path: &Path) -> Result<(PreviewImage, DefaultDicomObject)> {
    let obj = OpenFileOptions::new()
        .open_file(path)
        .with_context(|| format!("decode DICOM: {}", path.display()))?;
    let preview = render_first_frame(&obj)?;
    Ok((preview, obj))
}

#[allow(dead_code)] // used by integration tests
pub fn load_preview(path: &Path) -> Result<PreviewImage> {
    let (preview, _) = load_dicom(path)?;
    Ok(preview)
}

impl PreviewImage {
    pub fn to_dynamic_image(&self) -> image::DynamicImage {
        match self.format {
            PreviewFormat::Gray8 => image::DynamicImage::ImageLuma8(
                image::GrayImage::from_raw(self.width, self.height, self.pixels.clone())
                    .expect("valid gray image dimensions"),
            ),
            PreviewFormat::Rgba8 => image::DynamicImage::ImageRgba8(
                image::RgbaImage::from_raw(self.width, self.height, self.pixels.clone())
                    .expect("valid rgba image dimensions"),
            ),
        }
    }
}

pub fn save_preview_png(path: &Path, preview: &PreviewImage) -> Result<()> {
    let color_type = match preview.format {
        PreviewFormat::Gray8 => image::ColorType::L8,
        PreviewFormat::Rgba8 => image::ColorType::Rgba8,
    };

    image::save_buffer_with_format(
        path,
        &preview.pixels,
        preview.width,
        preview.height,
        color_type,
        image::ImageFormat::Png,
    )
    .with_context(|| format!("encode output image: {}", path.display()))
}

fn render_first_frame(obj: &DefaultDicomObject) -> Result<PreviewImage> {
    let height = usize::from(required_u16_attr(obj, tags::ROWS, "Rows")?);
    let width = usize::from(required_u16_attr(obj, tags::COLUMNS, "Columns")?);
    let samples_per_pixel =
        usize::from(optional_u16_attr(obj, tags::SAMPLES_PER_PIXEL).unwrap_or(1));
    let bits_allocated = required_u16_attr(obj, tags::BITS_ALLOCATED, "BitsAllocated")?;
    let frame_pixels = width
        .checked_mul(height)
        .context("image dimensions overflow")?;
    let frame_sample_count = frame_pixels
        .checked_mul(samples_per_pixel)
        .context("frame sample count overflow")?;

    let pixel_data = obj.element(tags::PIXEL_DATA).context("find PixelData")?;

    let pixels = match pixel_data.value() {
        DicomValue::Primitive(_) => {
            let raw = pixel_data.to_bytes().context("read PixelData bytes")?;
            render_native_bytes(
                obj,
                raw.as_ref(),
                bits_allocated,
                frame_pixels,
                frame_sample_count,
                samples_per_pixel,
            )?
        }
        DicomValue::PixelSequence(_) => {
            return decode_encapsulated_frame(obj);
        }
        DicomValue::Sequence(_) => bail!("unsupported pixel data representation"),
    };

    Ok(PreviewImage {
        width: width as u32,
        height: height as u32,
        pixels,
        format: PreviewFormat::Gray8,
    })
}

fn decode_encapsulated_frame(obj: &DefaultDicomObject) -> Result<PreviewImage> {
    let ts_uid = obj.meta().transfer_syntax().trim();
    let decoded = obj
        .decode_pixel_data()
        .with_context(|| format!("decode encapsulated pixel data (transfer syntax {ts_uid})"))?;
    let dynamic_image = decoded
        .to_dynamic_image(0)
        .context("convert decoded pixel data to image")?;
    let gray = dynamic_image.into_luma8();
    let (width, height) = gray.dimensions();
    Ok(PreviewImage {
        width,
        height,
        pixels: gray.into_raw(),
        format: PreviewFormat::Gray8,
    })
}

fn render_native_bytes(
    obj: &DefaultDicomObject,
    raw: &[u8],
    bits_allocated: u16,
    frame_pixels: usize,
    frame_sample_count: usize,
    samples_per_pixel: usize,
) -> Result<Vec<u8>> {
    match bits_allocated {
        8 => {
            ensure_frame_len(raw.len(), frame_sample_count)?;
            let samples = &raw[..frame_sample_count];
            if samples_per_pixel == 1 {
                let cfg = resolve_native_render_config(obj, 8);
                Ok(render_u8_monochrome(samples, cfg))
            } else {
                render_u8_color(obj, samples, frame_pixels, samples_per_pixel)
            }
        }
        16 => {
            ensure_frame_len(raw.len(), frame_sample_count * 2)?;
            if samples_per_pixel != 1 {
                bail!("16-bit color DICOM preview is not supported yet")
            }
            let samples = read_u16_samples(&raw[..frame_sample_count * 2]);
            let cfg = resolve_native_render_config(obj, 16);
            Ok(render_generic_monochrome(&samples, cfg, |value| {
                u32::from(*value)
            }))
        }
        32 => {
            ensure_frame_len(raw.len(), frame_sample_count * 4)?;
            if samples_per_pixel != 1 {
                bail!("32-bit color DICOM preview is not supported yet")
            }
            let samples = read_u32_samples(&raw[..frame_sample_count * 4]);
            let cfg = resolve_native_render_config(obj, 32);
            Ok(render_generic_monochrome(&samples, cfg, |value| *value))
        }
        other => bail!("unsupported BitsAllocated for preview: {other}"),
    }
}

fn render_u8_color(
    obj: &DefaultDicomObject,
    samples: &[u8],
    frame_pixels: usize,
    samples_per_pixel: usize,
) -> Result<Vec<u8>> {
    if samples_per_pixel != 3 {
        bail!("unsupported SamplesPerPixel for color preview: {samples_per_pixel}")
    }

    let photometric = optional_str_attr(obj, tags::PHOTOMETRIC_INTERPRETATION)
        .unwrap_or_default()
        .trim()
        .to_ascii_uppercase();
    if photometric != "RGB" {
        bail!("unsupported color photometric interpretation: {photometric}")
    }

    let planar_configuration = optional_u16_attr(obj, tags::PLANAR_CONFIGURATION).unwrap_or(0);
    let mut pixels = vec![0_u8; frame_pixels];

    match planar_configuration {
        0 => {
            for (index, chunk) in samples.chunks_exact(3).enumerate() {
                pixels[index] = gray_from_rgb8(chunk[0], chunk[1], chunk[2]);
            }
        }
        1 => {
            let plane_len = frame_pixels;
            if samples.len() < plane_len * 3 {
                bail!(
                    "dicom frame sample count {} does not match image size {}",
                    samples.len(),
                    plane_len * 3
                )
            }
            let (red, remainder) = samples.split_at(plane_len);
            let (green, blue) = remainder.split_at(plane_len);
            for index in 0..frame_pixels {
                pixels[index] = gray_from_rgb8(red[index], green[index], blue[index]);
            }
        }
        other => bail!("unsupported planar configuration: {other}"),
    }

    Ok(pixels)
}

fn ensure_frame_len(actual: usize, expected: usize) -> Result<()> {
    if actual < expected {
        bail!(
            "dicom frame sample count {} does not match image size {}",
            actual,
            expected
        )
    }

    Ok(())
}

fn read_u16_samples(raw: &[u8]) -> Vec<u16> {
    raw.chunks_exact(2)
        .map(|chunk| u16::from_le_bytes([chunk[0], chunk[1]]))
        .collect()
}

fn read_u32_samples(raw: &[u8]) -> Vec<u32> {
    raw.chunks_exact(4)
        .map(|chunk| u32::from_le_bytes([chunk[0], chunk[1], chunk[2], chunk[3]]))
        .collect()
}

fn resolve_native_render_config(
    obj: &DefaultDicomObject,
    default_bits_stored: u16,
) -> NativeRenderConfig {
    let bits_stored = optional_u16_attr(obj, tags::BITS_STORED)
        .filter(|value| *value > 0)
        .unwrap_or(default_bits_stored);
    let pixel_representation = optional_u16_attr(obj, tags::PIXEL_REPRESENTATION).unwrap_or(0);
    let slope = optional_f64_attr(obj, tags::RESCALE_SLOPE).unwrap_or(1.0);
    let intercept = optional_f64_attr(obj, tags::RESCALE_INTERCEPT).unwrap_or(0.0);
    let window_center = optional_f64_attr(obj, tags::WINDOW_CENTER).unwrap_or(0.0);
    let window_width = optional_f64_attr(obj, tags::WINDOW_WIDTH).unwrap_or(0.0);
    let invert = optional_str_attr(obj, tags::PHOTOMETRIC_INTERPRETATION)
        .map(|value| value.eq_ignore_ascii_case("MONOCHROME1"))
        .unwrap_or(false);

    NativeRenderConfig {
        bits_stored,
        pixel_representation,
        slope,
        intercept,
        window_center,
        window_width,
        use_window: window_width > 1.0 && optional_f64_attr(obj, tags::WINDOW_CENTER).is_some(),
        invert,
    }
}

fn render_u8_monochrome(samples: &[u8], cfg: NativeRenderConfig) -> Vec<u8> {
    let mut transformed = [0.0_f64; 256];
    for (value, transformed_value) in transformed.iter_mut().enumerate() {
        *transformed_value = scaled_stored_pixel_value(value as u32, cfg);
    }

    let mut lut = [0_u8; 256];
    if cfg.use_window {
        let (lower, upper, scale, offset) = native_window_parameters(cfg);
        if cfg.invert {
            for (value, transformed_value) in transformed.iter().enumerate() {
                lut[value] =
                    255 - map_window_value(*transformed_value, lower, upper, scale, offset);
            }
        } else {
            for (value, transformed_value) in transformed.iter().enumerate() {
                lut[value] = map_window_value(*transformed_value, lower, upper, scale, offset);
            }
        }
    } else {
        let mut min_value = transformed[samples[0] as usize];
        let mut max_value = min_value;
        for sample in &samples[1..] {
            let value = transformed[*sample as usize];
            if value < min_value {
                min_value = value;
            }
            if value > max_value {
                max_value = value;
            }
        }

        if max_value <= min_value {
            let fill = if cfg.invert { 255 } else { 0 };
            return vec![fill; samples.len()];
        }

        let linear_scale = 255.0 / (max_value - min_value);
        let linear_offset = -min_value * linear_scale;
        if cfg.invert {
            for (value, transformed_value) in transformed.iter().enumerate() {
                lut[value] = 255 - clamp_to_byte(*transformed_value * linear_scale + linear_offset);
            }
        } else {
            for (value, transformed_value) in transformed.iter().enumerate() {
                lut[value] = clamp_to_byte(*transformed_value * linear_scale + linear_offset);
            }
        }
    }

    let mut pixels = vec![0_u8; samples.len()];
    for (index, sample) in samples.iter().enumerate() {
        pixels[index] = lut[*sample as usize];
    }
    pixels
}

fn render_generic_monochrome<T, F>(samples: &[T], cfg: NativeRenderConfig, raw_bits: F) -> Vec<u8>
where
    T: Copy,
    F: Fn(&T) -> u32 + Copy,
{
    let mut pixels = vec![0_u8; samples.len()];

    if cfg.use_window {
        let (lower, upper, scale, offset) = native_window_parameters(cfg);
        if cfg.invert {
            for (index, sample) in samples.iter().enumerate() {
                pixels[index] = 255
                    - map_window_value(
                        scaled_stored_pixel_value(raw_bits(sample), cfg),
                        lower,
                        upper,
                        scale,
                        offset,
                    );
            }
        } else {
            for (index, sample) in samples.iter().enumerate() {
                pixels[index] = map_window_value(
                    scaled_stored_pixel_value(raw_bits(sample), cfg),
                    lower,
                    upper,
                    scale,
                    offset,
                );
            }
        }

        return pixels;
    }

    let mut min_value = f64::INFINITY;
    let mut max_value = f64::NEG_INFINITY;
    for sample in samples {
        let value = scaled_stored_pixel_value(raw_bits(sample), cfg);
        if value < min_value {
            min_value = value;
        }
        if value > max_value {
            max_value = value;
        }
    }

    if max_value <= min_value {
        let fill = if cfg.invert { 255 } else { 0 };
        pixels.fill(fill);
        return pixels;
    }

    let linear_scale = 255.0 / (max_value - min_value);
    let linear_offset = -min_value * linear_scale;
    if cfg.invert {
        for (index, sample) in samples.iter().enumerate() {
            pixels[index] = 255
                - clamp_to_byte(
                    scaled_stored_pixel_value(raw_bits(sample), cfg) * linear_scale + linear_offset,
                );
        }
    } else {
        for (index, sample) in samples.iter().enumerate() {
            pixels[index] = clamp_to_byte(
                scaled_stored_pixel_value(raw_bits(sample), cfg) * linear_scale + linear_offset,
            );
        }
    }

    pixels
}

fn gray_from_rgb8(red: u8, green: u8, blue: u8) -> u8 {
    let red = u32::from(red);
    let green = u32::from(green);
    let blue = u32::from(blue);
    let red = red | (red << 8);
    let green = green | (green << 8);
    let blue = blue | (blue << 8);
    ((19595 * red + 38470 * green + 7471 * blue + (1 << 15)) >> 24) as u8
}

fn decode_stored_pixel_value(raw_value: u32, bits_stored: u16, pixel_representation: u16) -> i32 {
    let bits_stored = match bits_stored {
        1..=32 => bits_stored,
        _ => 32,
    };

    let masked = if bits_stored < 32 {
        let mask = (1_u32 << bits_stored) - 1;
        raw_value & mask
    } else {
        raw_value
    };

    if pixel_representation == 0 || bits_stored == 32 {
        return masked as i32;
    }

    let sign_bit = 1_u32 << (bits_stored - 1);
    if masked & sign_bit == 0 {
        return masked as i32;
    }

    let mask = (1_u32 << bits_stored) - 1;
    (masked | !mask) as i32
}

fn scaled_stored_pixel_value(raw_value: u32, cfg: NativeRenderConfig) -> f64 {
    f64::from(decode_stored_pixel_value(
        raw_value,
        cfg.bits_stored,
        cfg.pixel_representation,
    )) * cfg.slope
        + cfg.intercept
}

fn clamp_to_byte(value: f64) -> u8 {
    if value <= 0.0 {
        0
    } else if value >= 255.0 {
        255
    } else {
        (value + 0.5) as u8
    }
}

fn map_window_value(value: f64, lower: f64, upper: f64, scale: f64, offset: f64) -> u8 {
    if value <= lower {
        0
    } else if value > upper {
        255
    } else {
        clamp_to_byte(value * scale + offset)
    }
}

fn native_window_parameters(cfg: NativeRenderConfig) -> (f64, f64, f64, f64) {
    let scale = 255.0 / (cfg.window_width - 1.0);
    let offset = 127.5 - (cfg.window_center - 0.5) * scale;
    let lower = cfg.window_center - 0.5 - (cfg.window_width - 1.0) / 2.0;
    let upper = cfg.window_center - 0.5 + (cfg.window_width - 1.0) / 2.0;
    (lower, upper, scale, offset)
}

fn required_u16_attr(obj: &DefaultDicomObject, tag: dicom_core::Tag, name: &str) -> Result<u16> {
    obj.attr(tag)
        .with_context(|| format!("find {name}"))?
        .to_u16()
        .with_context(|| format!("parse {name}"))
}

fn optional_u16_attr(obj: &DefaultDicomObject, tag: dicom_core::Tag) -> Option<u16> {
    obj.attr(tag).ok()?.to_u16().ok()
}

fn optional_f64_attr(obj: &DefaultDicomObject, tag: dicom_core::Tag) -> Option<f64> {
    obj.attr(tag).ok()?.to_f64().ok()
}

fn optional_str_attr(obj: &DefaultDicomObject, tag: dicom_core::Tag) -> Option<String> {
    Some(obj.attr(tag).ok()?.to_str().ok()?.into_owned())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Instant;

    use tempfile::TempDir;

    #[test]
    fn gray_from_rgb8_known_values() {
        assert_eq!(gray_from_rgb8(0, 0, 0), 0);
        assert_eq!(gray_from_rgb8(255, 255, 255), 255);
        assert_eq!(gray_from_rgb8(255, 0, 0), 76);
        assert_eq!(gray_from_rgb8(0, 255, 0), 150);
        assert_eq!(gray_from_rgb8(0, 0, 255), 29);
    }

    #[test]
    fn decode_stored_pixel_value_sign_extends() {
        assert_eq!(decode_stored_pixel_value(0x0fff, 12, 1), -1);
        assert_eq!(decode_stored_pixel_value(0x07ff, 12, 1), 2047);
    }

    #[test]
    #[ignore = "manual timing benchmark"]
    fn timing_sample_preview_components() {
        let sample =
            Path::new(env!("CARGO_MANIFEST_DIR")).join("../images/sample-dental-radiograph.dcm");
        let output_dir = TempDir::new().expect("create temp dir");
        let iterations = 30;

        let load_start = Instant::now();
        let mut preview = None;
        for _ in 0..iterations {
            preview = Some(load_preview(&sample).expect("load preview"));
        }
        let load_avg = load_start.elapsed().as_secs_f64() / f64::from(iterations);

        let preview = preview.expect("preview image should exist");
        let save_start = Instant::now();
        for index in 0..iterations {
            let output = output_dir.path().join(format!("preview-{index}.png"));
            save_preview_png(&output, &preview).expect("save preview png");
        }
        let save_avg = save_start.elapsed().as_secs_f64() / f64::from(iterations);

        eprintln!("rust preview load_avg_s={load_avg:.6} save_png_avg_s={save_avg:.6}");
    }
}
