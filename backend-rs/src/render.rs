use std::{fs, path::Path, sync::Arc};

// Two formats only: 8-bit grayscale (the raw bitewing) and RGBA8 (the
// post-palette/comparison output). Adding more would mean teaching the BMP
// encoder a new bit depth — not casually.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PreviewFormat {
    Gray8,
    Rgba8,
}

// The in-memory representation handed around between bmp/processing/cache.
// Pixels are Arc<[u8]> so cloning a PreviewImage is free — we hand the same
// buffer to multiple consumers (encoder, GPU upload, hash) without copying.
// If you need to mutate, use Arc::make_mut to CoW.
#[derive(Debug, Clone, PartialEq)]
pub struct PreviewImage {
    pub width: u32,
    pub height: u32,
    pub format: PreviewFormat,
    pub pixels: Arc<[u8]>,
}

impl PreviewImage {
    // Convenience ctor — accepts anything that can become Arc<[u8]>, so
    // callers can pass Vec<u8>, Box<[u8]>, or an Arc directly.
    pub fn gray(width: u32, height: u32, pixels: impl Into<Arc<[u8]>>) -> Self {
        Self {
            width,
            height,
            format: PreviewFormat::Gray8,
            pixels: pixels.into(),
        }
    }

    pub fn rgba(width: u32, height: u32, pixels: impl Into<Arc<[u8]>>) -> Self {
        Self {
            width,
            height,
            format: PreviewFormat::Rgba8,
            pixels: pixels.into(),
        }
    }
}

// Encode + write in one shot. The encoded buffer is built fully in memory
// before fs::write — BMP files for typical bitewings are small (a few MB).
pub fn save_preview_bmp(path: impl AsRef<Path>, preview: &PreviewImage) -> Result<(), String> {
    let encoded = encode_preview_bmp(preview)?;
    fs::write(path.as_ref(), encoded)
        .map_err(|error| format!("write preview BMP {}: {error}", path.as_ref().display()))
}

// Same as save_preview_bmp but bypasses the PreviewImage wrapper when the
// caller already has raw gray pixels. Used by analysis/processing hot paths
// that don't want to allocate an Arc just to immediately tear it down.
pub fn save_gray_bmp(
    path: impl AsRef<Path>,
    width: u32,
    height: u32,
    pixels: &[u8],
) -> Result<(), String> {
    let encoded = encode_gray_bmp(width, height, pixels)?;
    fs::write(path.as_ref(), encoded)
        .map_err(|error| format!("write preview BMP {}: {error}", path.as_ref().display()))
}

// Raw-pixel encode entry point. Validation has to come first — once we start
// writing headers we're committed to a specific buffer size.
pub fn encode_gray_bmp(width: u32, height: u32, pixels: &[u8]) -> Result<Vec<u8>, String> {
    validate_preview_pixels(width, height, PreviewFormat::Gray8, pixels.len())?;
    encode_gray8_bmp(width, height, pixels)
}

// Dispatch by format. Note RGBA gets downconverted to BGR24 — BMP doesn't
// really have a portable RGBA path (BITFIELDS works but viewers vary), and
// the alpha channel is currently always 255 anyway.
pub fn encode_preview_bmp(preview: &PreviewImage) -> Result<Vec<u8>, String> {
    validate_preview_pixels(
        preview.width,
        preview.height,
        preview.format,
        preview.pixels.len(),
    )?;

    match preview.format {
        PreviewFormat::Gray8 => encode_gray8_bmp(preview.width, preview.height, &preview.pixels),
        PreviewFormat::Rgba8 => {
            encode_rgba8_as_bgr24_bmp(preview.width, preview.height, &preview.pixels)
        }
    }
}

// Defensive sanity-check before we trust the dimensions enough to write a
// BMP header. Catches three things: zero dims (which break BMP), usize
// overflow on huge inputs, and mismatch between declared dims and buffer
// length (which would otherwise produce a truncated/garbled image).
fn validate_preview_pixels(
    width: u32,
    height: u32,
    format: PreviewFormat,
    pixel_len: usize,
) -> Result<(), String> {
    if width == 0 || height == 0 {
        return Err("preview dimensions must be non-zero".to_string());
    }
    if width > i32::MAX as u32 || height > i32::MAX as u32 {
        return Err("BMP dimensions exceed signed 32-bit header limits".to_string());
    }
    let channels: usize = match format {
        PreviewFormat::Gray8 => 1,
        PreviewFormat::Rgba8 => 4,
    };
    // Use checked_mul instead of `as usize` arithmetic — on 32-bit targets a
    // 16k×16k Gray8 image would overflow silently otherwise.
    let expected = (width as usize)
        .checked_mul(height as usize)
        .and_then(|count| count.checked_mul(channels))
        .ok_or_else(|| "preview dimensions overflow".to_string())?;
    if pixel_len != expected {
        return Err(format!(
            "preview pixel length = {}, want {}",
            pixel_len, expected
        ));
    }

    Ok(())
}

// 8-bit indexed BMP encoder. The BMP format dates back to Windows 3.x and it
// shows: rows are 4-byte aligned, rows are stored bottom-up (because OS/2
// thought that was clever), and indexed BMPs need a 256-entry palette even
// if it's just the identity.
fn encode_gray8_bmp(width: u32, height: u32, pixels: &[u8]) -> Result<Vec<u8>, String> {
    let width_usize = width as usize;
    let height_usize = height as usize;
    // BMP rows must be padded out to a 4-byte boundary. div_ceil(4)*4 is the
    // standard idiom for "round up to a multiple of 4".
    let row_stride = width_usize.div_ceil(4) * 4;
    // Palette is 256 entries × 4 bytes (BGRA). We write an identity grayscale
    // palette below so viewers display it as gray, not as random colors.
    let palette_bytes: usize = 256 * 4;
    // 14 bytes file header + 40 bytes BITMAPINFOHEADER + palette = pixel start.
    let pixel_offset: usize = 14 + 40 + palette_bytes;
    let pixel_bytes = row_stride
        .checked_mul(height_usize)
        .ok_or_else(|| "BMP pixel data overflow".to_string())?;
    let file_size = pixel_offset
        .checked_add(pixel_bytes)
        .ok_or_else(|| "BMP file size overflow".to_string())?;
    // The BMP header stores file size as u32, so anything past 4 GB simply
    // can't be represented. We refuse rather than silently truncating.
    if file_size > u32::MAX as usize {
        return Err("BMP file exceeds 4 GB".to_string());
    }

    // Pre-reserve so all the extends below are zero-alloc.
    let mut bmp = Vec::with_capacity(file_size);
    write_file_header(&mut bmp, file_size as u32, pixel_offset as u32);
    write_info_header(&mut bmp, width, height, 8, pixel_bytes as u32, 256);

    // Identity grayscale palette: entry i maps to (i, i, i). BMP palette
    // entries are BGRA, last byte reserved (0).
    for index in 0..=255_u8 {
        bmp.extend_from_slice(&[index, index, index, 0]);
    }

    let padding = row_stride - width_usize;
    // BMP stores rows bottom-to-top, hence the .rev(). Don't "fix" this —
    // viewers expect the file in this order.
    for source_y in (0..height_usize).rev() {
        let row_start = source_y * width_usize;
        bmp.extend_from_slice(&pixels[row_start..row_start + width_usize]);
        // Pad each row up to the 4-byte stride with zeros.
        bmp.extend(std::iter::repeat_n(0, padding));
    }

    Ok(bmp)
}

// 24-bit BMP from RGBA8 input. Note BMP is BGR, not RGB — that's a common
// source of "why is everything blue?" bugs. We drop the alpha channel here
// because 24-bit BMP has no place to put it.
fn encode_rgba8_as_bgr24_bmp(width: u32, height: u32, pixels: &[u8]) -> Result<Vec<u8>, String> {
    let width_usize = width as usize;
    let height_usize = height as usize;
    // 3 bytes per BGR pixel.
    let row_bytes = width_usize
        .checked_mul(3)
        .ok_or_else(|| "BMP row size overflow".to_string())?;
    let row_stride = row_bytes.div_ceil(4) * 4;
    // No palette on 24-bit BMP, so the pixel data starts right after the
    // headers — 14 + 40 = 54.
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
    // colors_used=0 here — no palette on a 24-bit image.
    write_info_header(&mut bmp, width, height, 24, pixel_bytes as u32, 0);

    let padding = row_stride - row_bytes;
    // Bottom-up again. Per-pixel we read RGBA, write BGR (swap R and B,
    // drop A). offset+2 is B, offset+1 is G, offset is R in the source.
    for source_y in (0..height_usize).rev() {
        let row_start = source_y * width_usize * 4;
        for x in 0..width_usize {
            let offset = row_start + x * 4;
            bmp.extend_from_slice(&[pixels[offset + 2], pixels[offset + 1], pixels[offset]]);
        }
        bmp.extend(std::iter::repeat_n(0, padding));
    }

    Ok(bmp)
}

// 14-byte BITMAPFILEHEADER: "BM" magic (yes, just those two ASCII bytes),
// file size (u32 LE), 4 reserved bytes that have to be zero, offset to pixel
// data (u32 LE). Everything little-endian — BMP is effectively a Win32 struct
// poured onto disk.
fn write_file_header(output: &mut Vec<u8>, file_size: u32, pixel_offset: u32) {
    output.extend_from_slice(b"BM");
    output.extend_from_slice(&file_size.to_le_bytes());
    // Two u16 reserved fields, both zero.
    output.extend_from_slice(&[0, 0, 0, 0]);
    output.extend_from_slice(&pixel_offset.to_le_bytes());
}

// 40-byte BITMAPINFOHEADER (the "DIB header v3"). Layout is fixed; field
// order matters and is validated by anything that opens the file.
fn write_info_header(
    output: &mut Vec<u8>,
    width: u32,
    height: u32,
    bits_per_pixel: u16,
    image_size: u32,
    colors_used: u32,
) {
    // Size of *this* header (40). Some viewers use this to figure out which
    // DIB variant they're dealing with.
    output.extend_from_slice(&40_u32.to_le_bytes());
    // Width and height are signed i32 in the spec. Positive height = bottom-up.
    output.extend_from_slice(&(width as i32).to_le_bytes());
    output.extend_from_slice(&(height as i32).to_le_bytes());
    // Planes — must be 1.
    output.extend_from_slice(&1_u16.to_le_bytes());
    output.extend_from_slice(&bits_per_pixel.to_le_bytes());
    // Compression = 0 (BI_RGB, i.e. uncompressed).
    output.extend_from_slice(&0_u32.to_le_bytes());
    output.extend_from_slice(&image_size.to_le_bytes());
    // X/Y pixels-per-meter — we set both to 0 to mean "don't care".
    output.extend_from_slice(&0_i32.to_le_bytes());
    output.extend_from_slice(&0_i32.to_le_bytes());
    output.extend_from_slice(&colors_used.to_le_bytes());
    // Colors important — 0 means all colors are important.
    output.extend_from_slice(&0_u32.to_le_bytes());
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::bmp::render_grayscale_preview;

    // Round-trip: encode our own BMP, decode it through the bmp.rs path,
    // confirm dimensions survive. Catches header/byte-offset regressions.
    #[test]
    fn encode_gray_bmp_writes_bmp_signature_and_round_trips() {
        let pixels = vec![0, 128, 192, 255];
        let bmp = encode_gray_bmp(2, 2, &pixels).unwrap();

        // "BM" — the literal magic bytes for every BMP variant we care about.
        assert!(bmp.starts_with(b"BM"));
        let preview = render_grayscale_preview(&bmp).unwrap();
        assert_eq!(preview.width, 2);
        assert_eq!(preview.height, 2);
    }

    // 2×2 with only 3 pixels worth of data should fail loudly. If validation
    // ever loosens, we could write a truncated BMP that crashes viewers.
    #[test]
    fn encode_gray_bmp_rejects_wrong_pixel_count() {
        let error = encode_gray_bmp(2, 2, &[0, 1, 2]).unwrap_err();

        assert!(error.contains("preview pixel length"));
    }

    // Zero dim is the easy-to-forget edge case — division/iter logic above
    // assumes both dims are positive.
    #[test]
    fn encode_gray_bmp_rejects_zero_dimensions() {
        let error = encode_gray_bmp(0, 2, &[]).unwrap_err();

        assert!(error.contains("non-zero"));
    }

    #[test]
    fn encode_gray_bmp_rejects_dimensions_outside_signed_header_range() {
        let error = encode_gray_bmp(i32::MAX as u32 + 1, 1, &[]).unwrap_err();

        assert!(error.contains("signed 32-bit header limits"));
    }

    // The 4-byte row padding is the single most error-prone part of the BMP
    // format. This test pins down that we (a) flip bottom-up correctly and
    // (b) pad each row to 4 bytes with zeros.
    #[test]
    fn encode_gray_bmp_pads_rows_to_four_byte_boundary() {
        let pixels = vec![10, 20, 30, 40, 50, 60];
        let bmp = encode_gray_bmp(3, 2, &pixels).unwrap();

        let pixel_offset = u32::from_le_bytes([bmp[10], bmp[11], bmp[12], bmp[13]]) as usize;
        let stride = 4;
        // Bottom row gets written *first* — that's row index 1 of the input
        // (pixels 40,50,60).
        let last_row_start = pixel_offset;
        assert_eq!(&bmp[last_row_start..last_row_start + 3], &[40, 50, 60]);
        // Confirm the padding byte after the 3-wide row is zero.
        assert_eq!(bmp[last_row_start + 3], 0);

        // Top row of the input is written second.
        let first_row_start = pixel_offset + stride;
        assert_eq!(&bmp[first_row_start..first_row_start + 3], &[10, 20, 30]);
    }

    // 24-bit BGR path: confirm bit depth, pixel offset, and the RGB→BGR swap.
    // Pixel 0 is red (255,0,0,255) → bytes should be 255,0,0 (BGR of red).
    // Pixel 3 is white → 255,255,255.
    #[test]
    fn encode_rgba_preview_writes_24bit_bmp() {
        let pixels = vec![
            255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 255, 255, 255, 255,
        ];
        let preview = PreviewImage::rgba(2, 2, pixels);
        let bmp = encode_preview_bmp(&preview).unwrap();

        assert!(bmp.starts_with(b"BM"));
        // bits_per_pixel field lives at offset 28 in the BITMAPINFOHEADER.
        let bits_per_pixel = u16::from_le_bytes([bmp[28], bmp[29]]);
        assert_eq!(bits_per_pixel, 24);

        let pixel_offset = u32::from_le_bytes([bmp[10], bmp[11], bmp[12], bmp[13]]) as usize;
        // No palette on 24-bit BMP, so the offset is just the two header sizes.
        assert_eq!(pixel_offset, 14 + 40);

        let last_row = &bmp[pixel_offset..pixel_offset + 8];
        assert_eq!(&last_row[0..3], &[255, 0, 0]);
        assert_eq!(&last_row[3..6], &[255, 255, 255]);
    }
}
