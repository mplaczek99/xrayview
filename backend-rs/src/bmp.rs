use std::{fs, path::Path};

use crate::contracts::MeasurementScale;

#[derive(Debug, Clone, PartialEq, Default)]
pub struct Metadata {
    pub rows: u16,
    pub columns: u16,
    pub samples_per_pixel: u16,
    pub bits_allocated: u16,
    pub bits_stored: u16,
    pub photometric_interpretation: String,
}

impl Metadata {
    pub fn measurement_scale(&self) -> Option<MeasurementScale> {
        None
    }
}

#[derive(Debug, Clone, PartialEq)]
pub struct RenderedPreview {
    pub width: u32,
    pub height: u32,
    pub pixels: Vec<u8>,
    pub measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RenderWindowMode {
    Default,
    FullRange,
}

pub fn read_file(path: &str) -> Result<Metadata, String> {
    let path = Path::new(path);
    let bytes = fs::read(path).map_err(|error| format!("open source file: {error}"))?;
    if !supports_bmp_path(path) {
        return Err(format!(
            "unsupported source image extension for {}; expected .bmp",
            path.display()
        ));
    }
    read(&bytes).map_err(|error| format!("read BMP metadata from {}: {error}", path.display()))
}

pub fn read(bytes: &[u8]) -> Result<Metadata, String> {
    let image = decode_bmp(bytes)?;
    Ok(Metadata {
        rows: image.height as u16,
        columns: image.width as u16,
        samples_per_pixel: image.samples_per_pixel,
        bits_allocated: image.bits_allocated,
        bits_stored: image.bits_allocated,
        photometric_interpretation: image.photometric_interpretation,
    })
}

pub fn render_grayscale_preview_file(path: impl AsRef<Path>) -> Result<RenderedPreview, String> {
    render_grayscale_preview_file_with_window_mode(path, RenderWindowMode::Default)
}

pub fn render_grayscale_preview_file_with_window_mode(
    path: impl AsRef<Path>,
    _window_mode: RenderWindowMode,
) -> Result<RenderedPreview, String> {
    render_grayscale_preview_file_inner(path, false)
}

pub fn render_grayscale_preview_file_for_tooth_analysis(
    path: impl AsRef<Path>,
) -> Result<RenderedPreview, String> {
    render_grayscale_preview_file_inner(path, true)
}

fn render_grayscale_preview_file_inner(
    path: impl AsRef<Path>,
    preserve_eight_bit_range: bool,
) -> Result<RenderedPreview, String> {
    let path = path.as_ref();
    let bytes = fs::read(path).map_err(|error| format!("open source file: {error}"))?;
    if !supports_bmp_path(path) {
        return Err(format!(
            "unsupported source image extension for {}; expected .bmp",
            path.display()
        ));
    }
    render_grayscale_preview_with_options(&bytes, preserve_eight_bit_range)
        .map_err(|error| format!("decode BMP image from {}: {error}", path.display()))
}

pub fn render_grayscale_preview(bytes: &[u8]) -> Result<RenderedPreview, String> {
    render_grayscale_preview_with_options(bytes, false)
}

pub fn render_grayscale_preview_with_window_mode(
    bytes: &[u8],
    _window_mode: RenderWindowMode,
) -> Result<RenderedPreview, String> {
    render_grayscale_preview(bytes)
}

fn render_grayscale_preview_with_options(
    bytes: &[u8],
    preserve_eight_bit_range: bool,
) -> Result<RenderedPreview, String> {
    let image = decode_bmp(bytes)?;
    let mut min = image.pixels[0];
    let mut max = image.pixels[0];
    for value in &image.pixels[1..] {
        min = min.min(*value);
        max = max.max(*value);
    }

    let preserve_eight_bit_range = preserve_eight_bit_range && min >= 0.0 && max <= 255.0;
    let manual_eight_bit_window = WindowTransform::new(128.0, 256.0);
    let pixels = image
        .pixels
        .into_iter()
        .map(|value| {
            if preserve_eight_bit_range {
                manual_eight_bit_window
                    .expect("valid 8-bit analysis window")
                    .map(value)
            } else {
                map_linear(value, min, max)
            }
        })
        .collect();

    Ok(RenderedPreview {
        width: image.width,
        height: image.height,
        pixels,
        measurement_scale: None,
    })
}

fn supports_bmp_path(path: &Path) -> bool {
    matches!(
        path.extension()
            .and_then(|extension| extension.to_str())
            .map(str::to_ascii_lowercase)
            .as_deref(),
        Some("bmp")
    )
}

struct DecodedBmp {
    width: u32,
    height: u32,
    samples_per_pixel: u16,
    bits_allocated: u16,
    photometric_interpretation: String,
    pixels: Vec<f32>,
}

fn decode_bmp(bytes: &[u8]) -> Result<DecodedBmp, String> {
    if bytes.len() < 54 || &bytes[..2] != b"BM" {
        return Err("unsupported BMP header".to_string());
    }

    let pixel_offset = read_le_u32_at(bytes, 10)? as usize;
    let dib_size = read_le_u32_at(bytes, 14)?;
    if dib_size < 40 {
        return Err(format!("unsupported BMP DIB header size: {dib_size}"));
    }
    if bytes.len() < 14 + dib_size as usize {
        return Err("truncated BMP DIB header".to_string());
    }

    let width = read_le_i32_at(bytes, 18)?;
    let raw_height = read_le_i32_at(bytes, 22)?;
    if width <= 0 || raw_height == 0 {
        return Err(format!("invalid BMP dimensions: {width}x{raw_height}"));
    }
    if width > u16::MAX as i32 || raw_height.unsigned_abs() > u16::MAX as u32 {
        return Err(format!(
            "BMP dimensions exceed supported range: {width}x{raw_height}"
        ));
    }

    let planes = read_le_u16_at(bytes, 26)?;
    if planes != 1 {
        return Err(format!("unsupported BMP plane count: {planes}"));
    }
    let bits_per_pixel = read_le_u16_at(bytes, 28)?;
    let compression = read_le_u32_at(bytes, 30)?;
    if compression != 0 {
        return Err(format!("unsupported BMP compression: {compression}"));
    }
    if !matches!(bits_per_pixel, 8 | 24 | 32) {
        return Err(format!("unsupported BMP bit depth: {bits_per_pixel}"));
    }

    let width = width as usize;
    let height = raw_height.unsigned_abs() as usize;
    let top_down = raw_height < 0;
    let bytes_per_pixel = usize::from(bits_per_pixel / 8);
    let row_stride = (width * bytes_per_pixel).div_ceil(4) * 4;
    let required = pixel_offset
        .checked_add(row_stride.saturating_mul(height))
        .ok_or_else(|| "BMP pixel data size overflow".to_string())?;
    if bytes.len() < required {
        return Err(format!(
            "BMP pixel data length = {}, want at least {required}",
            bytes.len()
        ));
    }

    let palette = if bits_per_pixel == 8 {
        let palette_start = 14 + dib_size as usize;
        let color_count = read_le_u32_at(bytes, 46).unwrap_or(0);
        let palette_entries = if color_count == 0 {
            256
        } else {
            color_count as usize
        };
        let palette_bytes = palette_entries
            .checked_mul(4)
            .ok_or_else(|| "BMP palette size overflow".to_string())?;
        if pixel_offset < palette_start + palette_bytes
            || bytes.len() < palette_start + palette_bytes
        {
            return Err("truncated BMP palette".to_string());
        }
        Some(&bytes[palette_start..palette_start + palette_bytes])
    } else {
        None
    };

    let mut pixels = vec![0.0; width * height];
    for output_y in 0..height {
        let source_y = if top_down {
            output_y
        } else {
            height - 1 - output_y
        };
        let row_start = pixel_offset + source_y * row_stride;
        let row = &bytes[row_start..row_start + row_stride];
        for x in 0..width {
            pixels[output_y * width + x] = match bits_per_pixel {
                8 => {
                    let index = usize::from(row[x]);
                    if let Some(palette) = palette {
                        let offset = index * 4;
                        if offset + 3 >= palette.len() {
                            return Err(format!(
                                "BMP palette index {index} exceeds {} entries",
                                palette.len() / 4
                            ));
                        }
                        f32::from(gray_from_rgb8(
                            palette[offset + 2],
                            palette[offset + 1],
                            palette[offset],
                        ))
                    } else {
                        f32::from(row[x])
                    }
                }
                24 | 32 => {
                    let offset = x * bytes_per_pixel;
                    f32::from(gray_from_rgb8(
                        row[offset + 2],
                        row[offset + 1],
                        row[offset],
                    ))
                }
                _ => unreachable!(),
            };
        }
    }

    let samples_per_pixel = if bits_per_pixel == 8 { 1 } else { 3 };
    let photometric_interpretation = if samples_per_pixel == 1 {
        "MONOCHROME2"
    } else {
        "RGB"
    };
    Ok(DecodedBmp {
        width: width as u32,
        height: height as u32,
        samples_per_pixel,
        bits_allocated: 8,
        photometric_interpretation: photometric_interpretation.to_string(),
        pixels,
    })
}

fn gray_from_rgb8(red: u8, green: u8, blue: u8) -> u8 {
    let red = u32::from(red) | (u32::from(red) << 8);
    let green = u32::from(green) | (u32::from(green) << 8);
    let blue = u32::from(blue) | (u32::from(blue) << 8);
    ((19_595 * red + 38_470 * green + 7_471 * blue + (1 << 15)) >> 24) as u8
}

#[derive(Debug, Clone, Copy)]
struct WindowTransform {
    lower: f32,
    upper: f32,
    scale: f32,
    offset: f32,
}

impl WindowTransform {
    fn new(center: f32, width: f32) -> Option<Self> {
        if !center.is_finite() || !width.is_finite() || width <= 1.0 {
            return None;
        }

        let scale = 255.0 / (width - 1.0);
        Some(Self {
            lower: center - 0.5 - (width - 1.0) / 2.0,
            upper: center - 0.5 + (width - 1.0) / 2.0,
            scale,
            offset: 127.5 - (center - 0.5) * scale,
        })
    }

    fn map(self, value: f32) -> u8 {
        if value <= self.lower {
            return 0;
        }
        if value > self.upper {
            return 255;
        }

        clamp_to_byte(value * self.scale + self.offset)
    }
}

fn map_linear(value: f32, min: f32, max: f32) -> u8 {
    if max <= min {
        return 0;
    }

    clamp_to_byte((value - min) * (255.0 / (max - min)))
}

fn clamp_to_byte(value: f32) -> u8 {
    if value <= 0.0 {
        0
    } else if value >= 255.0 {
        255
    } else {
        (value + 0.5) as u8
    }
}

fn read_le_u16_at(bytes: &[u8], offset: usize) -> Result<u16, String> {
    let value = bytes
        .get(offset..offset + 2)
        .ok_or_else(|| format!("read u16 at byte offset {offset}: truncated input"))?;
    Ok(u16::from_le_bytes([value[0], value[1]]))
}

fn read_le_u32_at(bytes: &[u8], offset: usize) -> Result<u32, String> {
    let value = bytes
        .get(offset..offset + 4)
        .ok_or_else(|| format!("read u32 at byte offset {offset}: truncated input"))?;
    Ok(u32::from_le_bytes([value[0], value[1], value[2], value[3]]))
}

fn read_le_i32_at(bytes: &[u8], offset: usize) -> Result<i32, String> {
    let value = bytes
        .get(offset..offset + 4)
        .ok_or_else(|| format!("read i32 at byte offset {offset}: truncated input"))?;
    Ok(i32::from_le_bytes([value[0], value[1], value[2], value[3]]))
}

#[cfg(test)]
pub mod tests {
    use super::*;

    #[test]
    fn read_file_reads_bmp_metadata() {
        let path = unique_temp_path("metadata", "bmp");
        std::fs::write(
            &path,
            build_bmp_32(
                2,
                2,
                &[(0, 0, 0), (255, 0, 0), (0, 255, 0), (255, 255, 255)],
            ),
        )
        .unwrap();

        let metadata = read_file(path.to_str().unwrap()).unwrap();

        assert_eq!(metadata.rows, 2);
        assert_eq!(metadata.columns, 2);
        assert_eq!(metadata.samples_per_pixel, 3);
        assert_eq!(metadata.bits_allocated, 8);
        assert_eq!(metadata.photometric_interpretation, "RGB");
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn render_grayscale_preview_file_reads_bmp_pixels() {
        let path = unique_temp_path("render", "bmp");
        std::fs::write(
            &path,
            build_bmp_32(
                2,
                2,
                &[(0, 0, 0), (255, 0, 0), (0, 255, 0), (255, 255, 255)],
            ),
        )
        .unwrap();

        let preview = render_grayscale_preview_file(&path).unwrap();

        assert_eq!(preview.width, 2);
        assert_eq!(preview.height, 2);
        assert_eq!(
            preview.pixels,
            full_range_mapped_u8(&[
                gray_from_rgb8(0, 0, 0),
                gray_from_rgb8(255, 0, 0),
                gray_from_rgb8(0, 255, 0),
                gray_from_rgb8(255, 255, 255),
            ])
        );
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn render_tooth_analysis_preview_preserves_bmp_8bit_range() {
        let path = unique_temp_path("analysis-render", "bmp");
        std::fs::write(
            &path,
            build_bmp_8_palette(
                4,
                1,
                &[(10, 10, 10), (20, 20, 20), (30, 30, 30), (40, 40, 40)],
                &[0, 1, 2, 3],
            ),
        )
        .unwrap();

        let default_preview = render_grayscale_preview_file(&path).unwrap();
        let analysis_preview = render_grayscale_preview_file_for_tooth_analysis(&path).unwrap();

        assert_eq!(default_preview.pixels, vec![0, 85, 170, 255]);
        assert_eq!(analysis_preview.pixels, vec![10, 20, 30, 40]);
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn render_bmp_supports_palette_pixels() {
        let bmp = build_bmp_8_palette(2, 1, &[(0, 0, 0), (255, 255, 255)], &[0, 1]);
        let preview = render_grayscale_preview(&bmp).unwrap();

        assert_eq!(preview.pixels, vec![0, 255]);
    }

    #[test]
    fn rejects_non_bmp_extension() {
        let path = unique_temp_path("metadata", "tif");
        std::fs::write(&path, build_bmp_32(1, 1, &[(0, 0, 0)])).unwrap();

        let error = read_file(path.to_str().unwrap()).unwrap_err();

        assert!(error.contains("expected .bmp"));
        let _ = std::fs::remove_file(path);
    }

    pub fn unique_temp_path(name: &str, extension: &str) -> std::path::PathBuf {
        let nanos = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        std::env::temp_dir().join(format!(
            "xrayview-rs-bmp-{name}-{}-{nanos}.{extension}",
            std::process::id()
        ))
    }

    pub fn build_bmp_32(width: u32, height: u32, rgb_top_down: &[(u8, u8, u8)]) -> Vec<u8> {
        assert_eq!(rgb_top_down.len(), width as usize * height as usize);
        let row_stride = width as usize * 4;
        let pixel_bytes = row_stride * height as usize;
        let file_size = 54 + pixel_bytes;
        let mut bmp = Vec::with_capacity(file_size);
        bmp.extend_from_slice(b"BM");
        bmp.extend_from_slice(&(file_size as u32).to_le_bytes());
        bmp.extend_from_slice(&[0, 0, 0, 0]);
        bmp.extend_from_slice(&54_u32.to_le_bytes());
        bmp.extend_from_slice(&40_u32.to_le_bytes());
        bmp.extend_from_slice(&(width as i32).to_le_bytes());
        bmp.extend_from_slice(&(height as i32).to_le_bytes());
        bmp.extend_from_slice(&1_u16.to_le_bytes());
        bmp.extend_from_slice(&32_u16.to_le_bytes());
        bmp.extend_from_slice(&0_u32.to_le_bytes());
        bmp.extend_from_slice(&(pixel_bytes as u32).to_le_bytes());
        bmp.extend_from_slice(&0_i32.to_le_bytes());
        bmp.extend_from_slice(&0_i32.to_le_bytes());
        bmp.extend_from_slice(&0_u32.to_le_bytes());
        bmp.extend_from_slice(&0_u32.to_le_bytes());
        for output_y in (0..height as usize).rev() {
            let row = &rgb_top_down[output_y * width as usize..(output_y + 1) * width as usize];
            for &(red, green, blue) in row {
                bmp.extend_from_slice(&[blue, green, red, 255]);
            }
        }
        bmp
    }

    pub fn build_bmp_8_palette(
        width: u32,
        height: u32,
        palette_rgb: &[(u8, u8, u8)],
        indexes_top_down: &[u8],
    ) -> Vec<u8> {
        assert_eq!(indexes_top_down.len(), width as usize * height as usize);
        let row_stride = (width as usize).div_ceil(4) * 4;
        let palette_bytes = palette_rgb.len() * 4;
        let pixel_offset = 54 + palette_bytes;
        let pixel_bytes = row_stride * height as usize;
        let file_size = pixel_offset + pixel_bytes;
        let mut bmp = Vec::with_capacity(file_size);
        bmp.extend_from_slice(b"BM");
        bmp.extend_from_slice(&(file_size as u32).to_le_bytes());
        bmp.extend_from_slice(&[0, 0, 0, 0]);
        bmp.extend_from_slice(&(pixel_offset as u32).to_le_bytes());
        bmp.extend_from_slice(&40_u32.to_le_bytes());
        bmp.extend_from_slice(&(width as i32).to_le_bytes());
        bmp.extend_from_slice(&(height as i32).to_le_bytes());
        bmp.extend_from_slice(&1_u16.to_le_bytes());
        bmp.extend_from_slice(&8_u16.to_le_bytes());
        bmp.extend_from_slice(&0_u32.to_le_bytes());
        bmp.extend_from_slice(&(pixel_bytes as u32).to_le_bytes());
        bmp.extend_from_slice(&0_i32.to_le_bytes());
        bmp.extend_from_slice(&0_i32.to_le_bytes());
        bmp.extend_from_slice(&(palette_rgb.len() as u32).to_le_bytes());
        bmp.extend_from_slice(&0_u32.to_le_bytes());
        for &(red, green, blue) in palette_rgb {
            bmp.extend_from_slice(&[blue, green, red, 0]);
        }
        for output_y in (0..height as usize).rev() {
            let row = &indexes_top_down[output_y * width as usize..(output_y + 1) * width as usize];
            bmp.extend_from_slice(row);
            bmp.extend(std::iter::repeat_n(0, row_stride - width as usize));
        }
        bmp
    }

    fn full_range_mapped_u8(values: &[u8]) -> Vec<u8> {
        let min = values.iter().copied().min().unwrap();
        let max = values.iter().copied().max().unwrap();
        values
            .iter()
            .map(|value| {
                if min == max {
                    0
                } else {
                    (((u16::from(*value) - u16::from(min)) * 255)
                        / (u16::from(max) - u16::from(min))) as u8
                }
            })
            .collect()
    }
}
