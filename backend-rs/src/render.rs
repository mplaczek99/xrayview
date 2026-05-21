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

pub fn save_preview_png(path: impl AsRef<Path>, preview: &PreviewImage) -> Result<(), String> {
    let encoded = encode_preview_png(preview)?;
    fs::write(path.as_ref(), encoded)
        .map_err(|error| format!("write preview PNG {}: {error}", path.as_ref().display()))
}

pub fn save_gray_png(
    path: impl AsRef<Path>,
    width: u32,
    height: u32,
    pixels: &[u8],
) -> Result<(), String> {
    save_preview_png(path, &PreviewImage::gray(width, height, pixels.to_vec()))
}

pub fn encode_gray_png(width: u32, height: u32, pixels: &[u8]) -> Result<Vec<u8>, String> {
    encode_preview_png(&PreviewImage::gray(width, height, pixels.to_vec()))
}

pub fn encode_preview_png(preview: &PreviewImage) -> Result<Vec<u8>, String> {
    let channels = match preview.format {
        PreviewFormat::Gray8 => 1,
        PreviewFormat::Rgba8 => 4,
    };
    encode_png(
        preview.width,
        preview.height,
        preview.format,
        channels,
        &preview.pixels,
    )
}

fn encode_png(
    width: u32,
    height: u32,
    format: PreviewFormat,
    channels: usize,
    pixels: &[u8],
) -> Result<Vec<u8>, String> {
    if width == 0 || height == 0 {
        return Err("preview dimensions must be non-zero".to_string());
    }
    let expected = width as usize * height as usize * channels;
    if pixels.len() != expected {
        return Err(format!(
            "preview pixel length = {}, want {}",
            pixels.len(),
            expected
        ));
    }

    let row_bytes = width as usize * channels;
    let mut raw = Vec::with_capacity((row_bytes + 1) * height as usize);
    for row in 0..height as usize {
        raw.push(0); // PNG filter method 0: None.
        let start = row * row_bytes;
        raw.extend_from_slice(&pixels[start..start + row_bytes]);
    }

    let mut png = Vec::new();
    png.extend_from_slice(b"\x89PNG\r\n\x1a\n");

    let mut ihdr = Vec::with_capacity(13);
    ihdr.extend_from_slice(&width.to_be_bytes());
    ihdr.extend_from_slice(&height.to_be_bytes());
    ihdr.push(8); // bit depth
    ihdr.push(match format {
        PreviewFormat::Gray8 => 0,
        PreviewFormat::Rgba8 => 6,
    });
    ihdr.push(0); // deflate
    ihdr.push(0); // adaptive filtering
    ihdr.push(0); // no interlace
    write_chunk(&mut png, b"IHDR", &ihdr);

    let compressed = zlib_store(&raw);
    write_chunk(&mut png, b"IDAT", &compressed);
    write_chunk(&mut png, b"IEND", &[]);

    Ok(png)
}

fn write_chunk(output: &mut Vec<u8>, name: &[u8; 4], data: &[u8]) {
    output.extend_from_slice(&(data.len() as u32).to_be_bytes());
    output.extend_from_slice(name);
    output.extend_from_slice(data);

    let mut crc_input = Vec::with_capacity(name.len() + data.len());
    crc_input.extend_from_slice(name);
    crc_input.extend_from_slice(data);
    output.extend_from_slice(&crc32(&crc_input).to_be_bytes());
}

fn zlib_store(raw: &[u8]) -> Vec<u8> {
    let mut output = Vec::with_capacity(raw.len() + raw.len() / 65_535 * 5 + 16);
    output.extend_from_slice(&[0x78, 0x01]);

    if raw.is_empty() {
        output.extend_from_slice(&[0x01, 0x00, 0x00, 0xff, 0xff]);
    } else {
        for (index, block) in raw.chunks(65_535).enumerate() {
            let final_block = index == (raw.len() - 1) / 65_535;
            output.push(if final_block { 0x01 } else { 0x00 });
            let length = block.len() as u16;
            output.extend_from_slice(&length.to_le_bytes());
            output.extend_from_slice(&(!length).to_le_bytes());
            output.extend_from_slice(block);
        }
    }

    output.extend_from_slice(&adler32(raw).to_be_bytes());
    output
}

fn adler32(bytes: &[u8]) -> u32 {
    const MOD: u32 = 65_521;
    let mut a = 1_u32;
    let mut b = 0_u32;
    for byte in bytes {
        a = (a + u32::from(*byte)) % MOD;
        b = (b + a) % MOD;
    }
    (b << 16) | a
}

fn crc32(bytes: &[u8]) -> u32 {
    let mut crc = 0xffff_ffff_u32;
    for byte in bytes {
        crc ^= u32::from(*byte);
        for _ in 0..8 {
            let mask = 0_u32.wrapping_sub(crc & 1);
            crc = (crc >> 1) ^ (0xedb8_8320 & mask);
        }
    }
    !crc
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn encode_gray_png_writes_png_signature_and_chunks() {
        let png = encode_gray_png(2, 2, &[0, 128, 192, 255]).unwrap();

        assert!(png.starts_with(b"\x89PNG\r\n\x1a\n"));
        assert!(png.windows(4).any(|window| window == b"IHDR"));
        assert!(png.windows(4).any(|window| window == b"IDAT"));
        assert!(png.windows(4).any(|window| window == b"IEND"));
    }

    #[test]
    fn encode_gray_png_rejects_wrong_pixel_count() {
        let error = encode_gray_png(2, 2, &[0, 1, 2]).unwrap_err();

        assert!(error.contains("preview pixel length"));
    }
}
