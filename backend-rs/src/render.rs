use std::{fs, path::Path};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PreviewFormat {
    Gray8,
    Rgba8,
}

#[derive(Debug, Clone, PartialEq)]
pub struct PreviewImage {
    pub width: u32,
    pub height: u32,
    pub format: PreviewFormat,
    pub pixels: Vec<u8>,
}

impl PreviewImage {
    pub fn gray(width: u32, height: u32, pixels: Vec<u8>) -> Self {
        Self {
            width,
            height,
            format: PreviewFormat::Gray8,
            pixels,
        }
    }

    pub fn rgba(width: u32, height: u32, pixels: Vec<u8>) -> Self {
        Self {
            width,
            height,
            format: PreviewFormat::Rgba8,
            pixels,
        }
    }
}

pub fn save_preview_bmp(path: impl AsRef<Path>, preview: &PreviewImage) -> Result<(), String> {
    let encoded = encode_preview_bmp(preview)?;
    fs::write(path.as_ref(), encoded)
        .map_err(|error| format!("write preview BMP {}: {error}", path.as_ref().display()))
}

pub fn save_gray_bmp(
    path: impl AsRef<Path>,
    width: u32,
    height: u32,
    pixels: &[u8],
) -> Result<(), String> {
    save_preview_bmp(path, &PreviewImage::gray(width, height, pixels.to_vec()))
}

pub fn encode_gray_bmp(width: u32, height: u32, pixels: &[u8]) -> Result<Vec<u8>, String> {
    encode_preview_bmp(&PreviewImage::gray(width, height, pixels.to_vec()))
}

pub fn encode_preview_bmp(preview: &PreviewImage) -> Result<Vec<u8>, String> {
    if preview.width == 0 || preview.height == 0 {
        return Err("preview dimensions must be non-zero".to_string());
    }
    let channels: usize = match preview.format {
        PreviewFormat::Gray8 => 1,
        PreviewFormat::Rgba8 => 4,
    };
    let expected = (preview.width as usize)
        .checked_mul(preview.height as usize)
        .and_then(|count| count.checked_mul(channels))
        .ok_or_else(|| "preview dimensions overflow".to_string())?;
    if preview.pixels.len() != expected {
        return Err(format!(
            "preview pixel length = {}, want {}",
            preview.pixels.len(),
            expected
        ));
    }

    match preview.format {
        PreviewFormat::Gray8 => encode_gray8_bmp(preview.width, preview.height, &preview.pixels),
        PreviewFormat::Rgba8 => {
            encode_rgba8_as_bgr24_bmp(preview.width, preview.height, &preview.pixels)
        }
    }
}

fn encode_gray8_bmp(width: u32, height: u32, pixels: &[u8]) -> Result<Vec<u8>, String> {
    let width_usize = width as usize;
    let height_usize = height as usize;
    let row_stride = width_usize.div_ceil(4) * 4;
    let palette_bytes: usize = 256 * 4;
    let pixel_offset: usize = 14 + 40 + palette_bytes;
    let pixel_bytes = row_stride
        .checked_mul(height_usize)
        .ok_or_else(|| "BMP pixel data overflow".to_string())?;
    let file_size = pixel_offset
        .checked_add(pixel_bytes)
        .ok_or_else(|| "BMP file size overflow".to_string())?;
    if file_size > u32::MAX as usize {
        return Err("BMP file exceeds 4 GB".to_string());
    }

    let mut bmp = Vec::with_capacity(file_size);
    write_file_header(&mut bmp, file_size as u32, pixel_offset as u32);
    write_info_header(&mut bmp, width, height, 8, pixel_bytes as u32, 256);

    for index in 0..=255_u8 {
        bmp.extend_from_slice(&[index, index, index, 0]);
    }

    let padding = row_stride - width_usize;
    for source_y in (0..height_usize).rev() {
        let row_start = source_y * width_usize;
        bmp.extend_from_slice(&pixels[row_start..row_start + width_usize]);
        bmp.extend(std::iter::repeat_n(0, padding));
    }

    Ok(bmp)
}

fn encode_rgba8_as_bgr24_bmp(width: u32, height: u32, pixels: &[u8]) -> Result<Vec<u8>, String> {
    let width_usize = width as usize;
    let height_usize = height as usize;
    let row_bytes = width_usize
        .checked_mul(3)
        .ok_or_else(|| "BMP row size overflow".to_string())?;
    let row_stride = row_bytes.div_ceil(4) * 4;
    let pixel_offset: usize = 14 + 40;
    let pixel_bytes = row_stride
        .checked_mul(height_usize)
        .ok_or_else(|| "BMP pixel data overflow".to_string())?;
    let file_size = pixel_offset
        .checked_add(pixel_bytes)
        .ok_or_else(|| "BMP file size overflow".to_string())?;
    if file_size > u32::MAX as usize {
        return Err("BMP file exceeds 4 GB".to_string());
    }

    let mut bmp = Vec::with_capacity(file_size);
    write_file_header(&mut bmp, file_size as u32, pixel_offset as u32);
    write_info_header(&mut bmp, width, height, 24, pixel_bytes as u32, 0);

    let padding = row_stride - row_bytes;
    for source_y in (0..height_usize).rev() {
        let row_start = source_y * width_usize * 4;
        for x in 0..width_usize {
            let offset = row_start + x * 4;
            bmp.push(pixels[offset + 2]);
            bmp.push(pixels[offset + 1]);
            bmp.push(pixels[offset]);
        }
        bmp.extend(std::iter::repeat_n(0, padding));
    }

    Ok(bmp)
}

fn write_file_header(output: &mut Vec<u8>, file_size: u32, pixel_offset: u32) {
    output.extend_from_slice(b"BM");
    output.extend_from_slice(&file_size.to_le_bytes());
    output.extend_from_slice(&[0, 0, 0, 0]);
    output.extend_from_slice(&pixel_offset.to_le_bytes());
}

fn write_info_header(
    output: &mut Vec<u8>,
    width: u32,
    height: u32,
    bits_per_pixel: u16,
    image_size: u32,
    colors_used: u32,
) {
    output.extend_from_slice(&40_u32.to_le_bytes());
    output.extend_from_slice(&(width as i32).to_le_bytes());
    output.extend_from_slice(&(height as i32).to_le_bytes());
    output.extend_from_slice(&1_u16.to_le_bytes());
    output.extend_from_slice(&bits_per_pixel.to_le_bytes());
    output.extend_from_slice(&0_u32.to_le_bytes());
    output.extend_from_slice(&image_size.to_le_bytes());
    output.extend_from_slice(&0_i32.to_le_bytes());
    output.extend_from_slice(&0_i32.to_le_bytes());
    output.extend_from_slice(&colors_used.to_le_bytes());
    output.extend_from_slice(&0_u32.to_le_bytes());
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::bmp::render_grayscale_preview;

    #[test]
    fn encode_gray_bmp_writes_bmp_signature_and_round_trips() {
        let pixels = vec![0, 128, 192, 255];
        let bmp = encode_gray_bmp(2, 2, &pixels).unwrap();

        assert!(bmp.starts_with(b"BM"));
        let preview = render_grayscale_preview(&bmp).unwrap();
        assert_eq!(preview.width, 2);
        assert_eq!(preview.height, 2);
    }

    #[test]
    fn encode_gray_bmp_rejects_wrong_pixel_count() {
        let error = encode_gray_bmp(2, 2, &[0, 1, 2]).unwrap_err();

        assert!(error.contains("preview pixel length"));
    }

    #[test]
    fn encode_gray_bmp_rejects_zero_dimensions() {
        let error = encode_gray_bmp(0, 2, &[]).unwrap_err();

        assert!(error.contains("non-zero"));
    }

    #[test]
    fn encode_gray_bmp_pads_rows_to_four_byte_boundary() {
        let pixels = vec![10, 20, 30, 40, 50, 60];
        let bmp = encode_gray_bmp(3, 2, &pixels).unwrap();

        let pixel_offset = u32::from_le_bytes([bmp[10], bmp[11], bmp[12], bmp[13]]) as usize;
        let stride = 4;
        let last_row_start = pixel_offset;
        assert_eq!(&bmp[last_row_start..last_row_start + 3], &[40, 50, 60]);
        assert_eq!(bmp[last_row_start + 3], 0);

        let first_row_start = pixel_offset + stride;
        assert_eq!(&bmp[first_row_start..first_row_start + 3], &[10, 20, 30]);
    }

    #[test]
    fn encode_rgba_preview_writes_24bit_bmp() {
        let pixels = vec![255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 255, 255, 255, 255];
        let preview = PreviewImage::rgba(2, 2, pixels);
        let bmp = encode_preview_bmp(&preview).unwrap();

        assert!(bmp.starts_with(b"BM"));
        let bits_per_pixel = u16::from_le_bytes([bmp[28], bmp[29]]);
        assert_eq!(bits_per_pixel, 24);

        let pixel_offset = u32::from_le_bytes([bmp[10], bmp[11], bmp[12], bmp[13]]) as usize;
        assert_eq!(pixel_offset, 14 + 40);

        let last_row = &bmp[pixel_offset..pixel_offset + 8];
        assert_eq!(&last_row[0..3], &[255, 0, 0]);
        assert_eq!(&last_row[3..6], &[255, 255, 255]);
    }
}
