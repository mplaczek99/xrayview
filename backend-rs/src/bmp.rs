use std::{fs, path::Path, sync::Arc};

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
    #[must_use]
    pub fn measurement_scale(&self) -> Option<MeasurementScale> {
        None
    }
}

#[derive(Debug, Clone, PartialEq)]
pub struct DecodedSourcePreview {
    pub width: u32,
    pub height: u32,
    pub pixels: Arc<[u8]>,
    pub measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct RenderedPreview {
    pub width: u32,
    pub height: u32,
    pub pixels: Arc<[u8]>,
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
    read_header(&bytes)
        .map_err(|error| format!("read BMP metadata from {}: {error}", path.display()))
}

pub fn read(bytes: &[u8]) -> Result<Metadata, String> {
    read_header(bytes)
}

pub fn read_header(bytes: &[u8]) -> Result<Metadata, String> {
    let header = parse_bmp_header(bytes)?;
    Ok(Metadata {
        rows: header.height as u16,
        columns: header.width as u16,
        samples_per_pixel: header.samples_per_pixel(),
        bits_allocated: 8,
        bits_stored: 8,
        photometric_interpretation: header.photometric_interpretation().to_string(),
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

pub fn decode_source_preview_file(path: impl AsRef<Path>) -> Result<DecodedSourcePreview, String> {
    let path = path.as_ref();
    let bytes = fs::read(path).map_err(|error| format!("open source file: {error}"))?;
    if !supports_bmp_path(path) {
        return Err(format!(
            "unsupported source image extension for {}; expected .bmp",
            path.display()
        ));
    }
    decode_source_preview(&bytes)
        .map_err(|error| format!("decode BMP image from {}: {error}", path.display()))
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
    Ok(render_decoded_bmp_with_options(
        image,
        preserve_eight_bit_range,
    ))
}

pub fn decode_source_preview(bytes: &[u8]) -> Result<DecodedSourcePreview, String> {
    let image = decode_bmp(bytes)?;
    Ok(DecodedSourcePreview {
        width: image.width,
        height: image.height,
        pixels: image.pixels.into(),
        measurement_scale: None,
    })
}

#[must_use]
pub fn render_grayscale_preview_from_source(source: &DecodedSourcePreview) -> RenderedPreview {
    render_grayscale_preview_from_source_with_options(source, false)
}

#[must_use]
pub fn render_grayscale_preview_from_source_for_tooth_analysis(
    source: &DecodedSourcePreview,
) -> RenderedPreview {
    render_grayscale_preview_from_source_with_options(source, true)
}

fn render_grayscale_preview_from_source_with_options(
    source: &DecodedSourcePreview,
    preserve_eight_bit_range: bool,
) -> RenderedPreview {
    if preserve_eight_bit_range {
        return RenderedPreview {
            width: source.width,
            height: source.height,
            pixels: Arc::clone(&source.pixels),
            measurement_scale: source.measurement_scale.clone(),
        };
    }

    let mut min = source.pixels[0];
    let mut max = source.pixels[0];
    for value in &source.pixels[1..] {
        min = min.min(*value);
        max = max.max(*value);
    }

    let lut: [u8; 256] = std::array::from_fn(|value| {
        let value = value as u8;
        map_linear(f32::from(value), f32::from(min), f32::from(max))
    });
    let pixels: Vec<u8> = source
        .pixels
        .iter()
        .copied()
        .map(|value| lut[usize::from(value)])
        .collect();

    RenderedPreview {
        width: source.width,
        height: source.height,
        pixels: pixels.into(),
        measurement_scale: source.measurement_scale.clone(),
    }
}

fn render_decoded_bmp_with_options(
    image: DecodedBmp,
    preserve_eight_bit_range: bool,
) -> RenderedPreview {
    if preserve_eight_bit_range {
        return RenderedPreview {
            width: image.width,
            height: image.height,
            pixels: image.pixels.into(),
            measurement_scale: None,
        };
    }

    let mut min = image.pixels[0];
    let mut max = image.pixels[0];
    for value in &image.pixels[1..] {
        min = min.min(*value);
        max = max.max(*value);
    }

    let lut: [u8; 256] = std::array::from_fn(|value| {
        let value = value as u8;
        map_linear(f32::from(value), f32::from(min), f32::from(max))
    });
    let pixels: Vec<u8> = image
        .pixels
        .into_iter()
        .map(|value| lut[usize::from(value)])
        .collect();

    RenderedPreview {
        width: image.width,
        height: image.height,
        pixels: pixels.into(),
        measurement_scale: None,
    }
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
    pixels: Vec<u8>,
}

#[derive(Debug, Clone, Copy)]
struct BmpHeader {
    pixel_offset: usize,
    dib_size: u32,
    width: usize,
    height: usize,
    top_down: bool,
    bits_per_pixel: u16,
}

impl BmpHeader {
    fn samples_per_pixel(self) -> u16 {
        if self.bits_per_pixel == 8 {
            1
        } else {
            3
        }
    }

    fn photometric_interpretation(self) -> &'static str {
        if self.samples_per_pixel() == 1 {
            "MONOCHROME2"
        } else {
            "RGB"
        }
    }

    fn bytes_per_pixel(self) -> usize {
        usize::from(self.bits_per_pixel / 8)
    }

    fn row_stride(self) -> Result<usize, String> {
        self.width
            .checked_mul(self.bytes_per_pixel())
            .ok_or_else(|| "BMP row size overflow".to_string())
            .map(|row_bytes| row_bytes.div_ceil(4) * 4)
    }
}

enum BmpPixelFormat {
    Gray8,
    Palette8(Box<[u8; 256]>),
    Bgr24,
    Bgra32,
}

fn parse_bmp_header(bytes: &[u8]) -> Result<BmpHeader, String> {
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

    Ok(BmpHeader {
        pixel_offset,
        dib_size,
        width: width as usize,
        height: raw_height.unsigned_abs() as usize,
        top_down: raw_height < 0,
        bits_per_pixel,
    })
}

fn decode_bmp(bytes: &[u8]) -> Result<DecodedBmp, String> {
    let header = parse_bmp_header(bytes)?;
    let width = header.width;
    let height = header.height;
    let row_stride = header.row_stride()?;
    let required = header
        .pixel_offset
        .checked_add(
            row_stride
                .checked_mul(height)
                .ok_or_else(|| "BMP pixel data size overflow".to_string())?,
        )
        .ok_or_else(|| "BMP pixel data size overflow".to_string())?;
    if bytes.len() < required {
        return Err(format!(
            "BMP pixel data length = {}, want at least {required}",
            bytes.len()
        ));
    }

    let pixel_format = bmp_pixel_format(bytes, header, row_stride)?;
    let mut pixels = vec![0; width * height];
    match pixel_format {
        BmpPixelFormat::Gray8 => decode_gray8_rows(bytes, header, row_stride, &mut pixels),
        BmpPixelFormat::Palette8(palette_lut) => {
            decode_palette8_rows(bytes, header, row_stride, &palette_lut, &mut pixels);
        }
        BmpPixelFormat::Bgr24 => decode_bgr24_rows(bytes, header, row_stride, &mut pixels),
        BmpPixelFormat::Bgra32 => decode_bgra32_rows(bytes, header, row_stride, &mut pixels),
    }

    Ok(DecodedBmp {
        width: width as u32,
        height: height as u32,
        pixels,
    })
}

fn bmp_pixel_format(
    bytes: &[u8],
    header: BmpHeader,
    row_stride: usize,
) -> Result<BmpPixelFormat, String> {
    match header.bits_per_pixel {
        8 => bmp_8bit_pixel_format(bytes, header, row_stride),
        24 => Ok(BmpPixelFormat::Bgr24),
        32 => Ok(BmpPixelFormat::Bgra32),
        _ => unreachable!(),
    }
}

fn bmp_8bit_pixel_format(
    bytes: &[u8],
    header: BmpHeader,
    row_stride: usize,
) -> Result<BmpPixelFormat, String> {
    let palette_start = 14 + header.dib_size as usize;
    let color_count = read_le_u32_at(bytes, 46).unwrap_or(0);
    let palette_entries = if color_count == 0 {
        256
    } else {
        color_count as usize
    };
    if palette_entries == 0 || palette_entries > 256 {
        return Err(format!(
            "unsupported BMP palette entry count: {palette_entries}"
        ));
    }
    let palette_bytes = palette_entries
        .checked_mul(4)
        .ok_or_else(|| "BMP palette size overflow".to_string())?;
    if header.pixel_offset < palette_start + palette_bytes
        || bytes.len() < palette_start + palette_bytes
    {
        return Err("truncated BMP palette".to_string());
    }

    let palette = &bytes[palette_start..palette_start + palette_bytes];
    let mut palette_lut = Box::new([0; 256]);
    let mut identity_gray = palette_entries == 256;
    for index in 0..palette_entries {
        let offset = index * 4;
        let gray = gray_from_rgb8(palette[offset + 2], palette[offset + 1], palette[offset]);
        palette_lut[index] = gray;
        identity_gray &= gray == index as u8;
    }

    if palette_entries < 256 {
        validate_8bit_palette_indexes(bytes, header, row_stride, palette_entries)?;
    }

    if identity_gray {
        Ok(BmpPixelFormat::Gray8)
    } else {
        Ok(BmpPixelFormat::Palette8(palette_lut))
    }
}

fn validate_8bit_palette_indexes(
    bytes: &[u8],
    header: BmpHeader,
    row_stride: usize,
    palette_entries: usize,
) -> Result<(), String> {
    for output_y in 0..header.height {
        let source_y = if header.top_down {
            output_y
        } else {
            header.height - 1 - output_y
        };
        let row_start = header.pixel_offset + source_y * row_stride;
        let row = &bytes[row_start..row_start + row_stride];
        if let Some((_, index)) = row[..header.width]
            .iter()
            .enumerate()
            .find(|(_, index)| usize::from(**index) >= palette_entries)
        {
            return Err(format!(
                "BMP palette index {} exceeds {palette_entries} entries",
                index
            ));
        }
    }

    Ok(())
}

fn decode_gray8_rows(bytes: &[u8], header: BmpHeader, row_stride: usize, pixels: &mut [u8]) {
    for output_y in 0..header.height {
        let source_y = if header.top_down {
            output_y
        } else {
            header.height - 1 - output_y
        };
        let row_start = header.pixel_offset + source_y * row_stride;
        let row = &bytes[row_start..row_start + header.width];
        let output_start = output_y * header.width;
        pixels[output_start..output_start + header.width].copy_from_slice(row);
    }
}

fn decode_palette8_rows(
    bytes: &[u8],
    header: BmpHeader,
    row_stride: usize,
    palette_lut: &[u8; 256],
    pixels: &mut [u8],
) {
    for output_y in 0..header.height {
        let source_y = if header.top_down {
            output_y
        } else {
            header.height - 1 - output_y
        };
        let row_start = header.pixel_offset + source_y * row_stride;
        let row = &bytes[row_start..row_start + header.width];
        let output_start = output_y * header.width;
        for (x, slot) in pixels[output_start..output_start + header.width]
            .iter_mut()
            .enumerate()
        {
            *slot = palette_lut[usize::from(row[x])];
        }
    }
}

fn decode_bgr24_rows(bytes: &[u8], header: BmpHeader, row_stride: usize, pixels: &mut [u8]) {
    for output_y in 0..header.height {
        let source_y = if header.top_down {
            output_y
        } else {
            header.height - 1 - output_y
        };
        let row_start = header.pixel_offset + source_y * row_stride;
        let row = &bytes[row_start..row_start + header.width * 3];
        let output_start = output_y * header.width;
        for (source, slot) in row
            .chunks_exact(3)
            .zip(pixels[output_start..output_start + header.width].iter_mut())
        {
            *slot = gray_from_rgb8(source[2], source[1], source[0]);
        }
    }
}

fn decode_bgra32_rows(bytes: &[u8], header: BmpHeader, row_stride: usize, pixels: &mut [u8]) {
    for output_y in 0..header.height {
        let source_y = if header.top_down {
            output_y
        } else {
            header.height - 1 - output_y
        };
        let row_start = header.pixel_offset + source_y * row_stride;
        let row = &bytes[row_start..row_start + header.width * 4];
        let output_start = output_y * header.width;
        for (source, slot) in row
            .chunks_exact(4)
            .zip(pixels[output_start..output_start + header.width].iter_mut())
        {
            *slot = gray_from_rgb8(source[2], source[1], source[0]);
        }
    }
}

fn gray_from_rgb8(red: u8, green: u8, blue: u8) -> u8 {
    let red = u32::from(red) | (u32::from(red) << 8);
    let green = u32::from(green) | (u32::from(green) << 8);
    let blue = u32::from(blue) | (u32::from(blue) << 8);
    ((19_595 * red + 38_470 * green + 7_471 * blue + (1 << 15)) >> 24) as u8
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
    fn read_file_reads_metadata_without_pixel_data() {
        let path = unique_temp_path("metadata-header-only", "bmp");
        let pixels = vec![(0, 0, 0); 854_usize * 1200_usize];
        let mut bmp = build_bmp_32(854, 1200, &pixels);
        bmp.truncate(54);
        std::fs::write(&path, bmp).unwrap();

        let metadata = read_file(path.to_str().unwrap()).unwrap();

        assert_eq!(metadata.rows, 1200);
        assert_eq!(metadata.columns, 854);
        assert_eq!(metadata.samples_per_pixel, 3);
        assert_eq!(metadata.bits_allocated, 8);
        assert_eq!(metadata.bits_stored, 8);
        assert_eq!(metadata.photometric_interpretation, "RGB");
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn render_still_rejects_missing_pixel_data() {
        let mut bmp = build_bmp_32(
            2,
            2,
            &[(0, 0, 0), (255, 0, 0), (0, 255, 0), (255, 255, 255)],
        );
        bmp.truncate(54);

        let error = render_grayscale_preview(&bmp).unwrap_err();

        assert!(error.contains("BMP pixel data length"));
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
            preview.pixels.as_ref(),
            full_range_mapped_u8(&[
                gray_from_rgb8(0, 0, 0),
                gray_from_rgb8(255, 0, 0),
                gray_from_rgb8(0, 255, 0),
                gray_from_rgb8(255, 255, 255),
            ])
            .as_slice()
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

        assert_eq!(default_preview.pixels.as_ref(), [0, 85, 170, 255]);
        assert_eq!(analysis_preview.pixels.as_ref(), [10, 20, 30, 40]);
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn decoded_source_preview_derives_matching_render_variants() {
        let bmp = build_bmp_32(
            2,
            2,
            &[(10, 10, 10), (40, 40, 40), (80, 80, 80), (160, 160, 160)],
        );
        let source = decode_source_preview(&bmp).unwrap();

        assert_eq!(
            render_grayscale_preview_from_source(&source),
            render_grayscale_preview(&bmp).unwrap()
        );
        assert_eq!(
            render_grayscale_preview_from_source_for_tooth_analysis(&source),
            render_grayscale_preview_with_options(&bmp, true).unwrap()
        );
    }

    #[test]
    fn render_bmp_supports_palette_pixels() {
        let bmp = build_bmp_8_palette(2, 1, &[(0, 0, 0), (255, 255, 255)], &[0, 1]);
        let preview = render_grayscale_preview(&bmp).unwrap();

        assert_eq!(preview.pixels.as_ref(), [0, 255]);
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
