// BMP decode + grayscale render. The file is the heart of the input pipeline:
// every study eventually passes through here. We support uncompressed BMPs in
// 8-bit (indexed or grayscale), 24-bit BGR, and 32-bit BGRA. Anything else is
// rejected at parse time. Compressed BMPs (RLE) are out of scope — bitewing
// captures are always raw uncompressed.
//
// "Source preview" vs "rendered preview":
//   * source = the raw 8-bit grayscale (or grayscale-projected from RGB) as
//     decoded from the file. This is what the cache stores.
//   * rendered = the source after a linear min/max stretch to fill the 0..255
//     range. Better contrast for display; not what analysis wants.
// The `preserve_eight_bit_range` / "tooth analysis" variant skips the stretch
// because the analyzer depends on the raw values being comparable across
// images.

use std::{fs, io::Read, path::Path, sync::Arc};

use byteorder::{ByteOrder, LittleEndian};

use crate::contracts::MeasurementScale;

const MIN_BMP_HEADER_BYTES: usize = 54;
const MAX_DECODED_PREVIEW_PIXELS: usize = 128 * 1024 * 1024;

// Header metadata exposed to the rest of the backend. BMP files do not carry
// pixel spacing, so measurement_scale() always returns None for now.
#[derive(Debug, Clone, PartialEq, Default)]
pub struct Metadata {
    pub width: u32,
    pub height: u32,
    pub color_channel_count: u16,
    pub bits_per_channel: u16,
    pub color_model: String,
}

impl Metadata {
    // BMP has no pixel-spacing metadata. Left in place so callers can use the
    // same measurement abstraction for all open studies.
    #[must_use]
    pub fn measurement_scale(&self) -> Option<MeasurementScale> {
        None
    }
}

// The cached, decoded source pixels. Arc<[u8]> for cheap fan-out into
// processing/analysis without copying.
#[derive(Debug, Clone, PartialEq)]
pub struct DecodedSourcePreview {
    pub width: u32,
    pub height: u32,
    pub pixels: Arc<[u8]>,
    pub measurement_scale: Option<MeasurementScale>,
}

// Render-ready preview — same shape as DecodedSourcePreview but the pixels
// have been (potentially) stretched to fill the 0..255 range.
#[derive(Debug, Clone, PartialEq)]
pub struct RenderedPreview {
    pub width: u32,
    pub height: u32,
    pub pixels: Arc<[u8]>,
    pub measurement_scale: Option<MeasurementScale>,
}

// Path-based public entry — reads just enough of the file to fill out
// Metadata. Doesn't decode pixel data, so it's fast on huge files.
pub fn read_file(path: &str) -> Result<Metadata, String> {
    let path = Path::new(path);
    if !supports_bmp_path(path) {
        return Err(format!(
            "unsupported source image extension for {}; expected .bmp",
            path.display()
        ));
    }
    let mut file = fs::File::open(path).map_err(|error| format!("open source file: {error}"))?;
    read_header_prefix(&mut file)
        .map_err(|error| format!("read BMP metadata from {}: {error}", path.display()))
}

fn read_header_prefix(reader: &mut impl Read) -> Result<Metadata, String> {
    let mut bytes = Vec::with_capacity(MIN_BMP_HEADER_BYTES);
    reader
        .take(MIN_BMP_HEADER_BYTES as u64)
        .read_to_end(&mut bytes)
        .map_err(|error| format!("read BMP header: {error}"))?;
    read_header(&bytes)
}

// The actual metadata extractor. We report 8-bit channels because these are
// the only BMP depths this decoder normalizes into source-preview pixels.
pub fn read_header(bytes: &[u8]) -> Result<Metadata, String> {
    let header = parse_bmp_header(bytes)?;
    Ok(Metadata {
        width: header.width as u32,
        height: header.height as u32,
        color_channel_count: header.color_channel_count(),
        bits_per_channel: 8,
        color_model: header.color_model().to_string(),
    })
}

// Default rendering: read file, decode, stretch.
pub fn render_grayscale_preview_file(path: impl AsRef<Path>) -> Result<RenderedPreview, String> {
    render_grayscale_preview_file_inner(path, false)
}

// "For tooth analysis" = preserve the raw 8-bit range. The analyzer uses
// absolute pixel values for thresholding, so a stretched preview would break
// the cross-image consistency assumption.
pub fn render_grayscale_preview_file_for_tooth_analysis(
    path: impl AsRef<Path>,
) -> Result<RenderedPreview, String> {
    render_grayscale_preview_file_inner(path, true)
}

// Decode without stretching — this is what gets put in the source preview cache.
// Stretched variants are derived from this on demand.
pub fn decode_source_preview_file(path: impl AsRef<Path>) -> Result<DecodedSourcePreview, String> {
    let path = path.as_ref();
    if !supports_bmp_path(path) {
        return Err(format!(
            "unsupported source image extension for {}; expected .bmp",
            path.display()
        ));
    }
    let bytes = fs::read(path).map_err(|error| format!("open source file: {error}"))?;
    decode_source_preview(&bytes)
        .map_err(|error| format!("decode BMP image from {}: {error}", path.display()))
}

// Shared file-loading prelude for the two file-based render variants.
fn render_grayscale_preview_file_inner(
    path: impl AsRef<Path>,
    preserve_eight_bit_range: bool,
) -> Result<RenderedPreview, String> {
    let path = path.as_ref();
    if !supports_bmp_path(path) {
        return Err(format!(
            "unsupported source image extension for {}; expected .bmp",
            path.display()
        ));
    }
    let bytes = fs::read(path).map_err(|error| format!("open source file: {error}"))?;
    render_grayscale_preview_with_options(&bytes, preserve_eight_bit_range)
        .map_err(|error| format!("decode BMP image from {}: {error}", path.display()))
}

// In-memory render entry point — same dispatch as the file version but
// taking pre-loaded bytes.
pub fn render_grayscale_preview(bytes: &[u8]) -> Result<RenderedPreview, String> {
    render_grayscale_preview_with_options(bytes, false)
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

// Decode-only path: hand the raw DecodedSourcePreview back to the caller
// without applying any tone mapping. App uses this to populate the source
// preview cache.
pub fn decode_source_preview(bytes: &[u8]) -> Result<DecodedSourcePreview, String> {
    let image = decode_bmp(bytes)?;
    Ok(DecodedSourcePreview {
        width: image.width,
        height: image.height,
        // Convert Vec<u8> → Arc<[u8]> once at the boundary — every consumer
        // beyond this point just clones the Arc.
        pixels: image.pixels.into(),
        measurement_scale: None,
    })
}

// Public render-from-decoded entry. Used when the source is already in the
// cache and we don't want to re-decode.
#[must_use]
pub fn render_grayscale_preview_from_source(source: &DecodedSourcePreview) -> RenderedPreview {
    render_grayscale_preview_from_source_with_options(source, false)
}

// Tooth-analysis variant: skip the auto-stretch and Arc::clone the source
// pixels straight through. Zero-copy fast path.
#[must_use]
pub fn render_grayscale_preview_from_source_for_tooth_analysis(
    source: &DecodedSourcePreview,
) -> RenderedPreview {
    render_grayscale_preview_from_source_with_options(source, true)
}

// The actual implementation. Two paths:
//   * preserve_eight_bit_range=true: Arc::clone, no work, no allocation.
//   * default: scan min/max, build a 256-entry stretch LUT, apply.
// LUT is 256 entries because input is u8 — we only need a tiny table.
fn render_grayscale_preview_from_source_with_options(
    source: &DecodedSourcePreview,
    preserve_eight_bit_range: bool,
) -> RenderedPreview {
    if preserve_eight_bit_range {
        return RenderedPreview {
            width: source.width,
            height: source.height,
            // Free clone — just bumps the refcount on the underlying buffer.
            pixels: Arc::clone(&source.pixels),
            measurement_scale: source.measurement_scale.clone(),
        };
    }

    // Find min/max in one pass. We initialize from pixels[0] (safe — caller
    // never hands us an empty preview because BMP enforces non-zero dims).
    let mut min = source.pixels[0];
    let mut max = source.pixels[0];
    for value in &source.pixels[1..] {
        min = min.min(*value);
        max = max.max(*value);
    }

    // Build a small LUT then map every pixel through it. Way cheaper than
    // calling map_linear once per pixel — 256 calls vs N (typically millions).
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

// Same stretch logic but operating on the post-decode owned Vec<u8> (not
// Arc'd yet). The duplication with above is intentional — this variant gets
// to consume the buffer with `into_iter`, no allocation past the result.
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

// Reject anything that isn't *.bmp at the path level. We do an explicit
// extension check rather than just trying to parse the bytes so the error
// message points the user at the obvious problem.
fn supports_bmp_path(path: &Path) -> bool {
    matches!(
        path.extension()
            .and_then(|extension| extension.to_str())
            .map(str::to_ascii_lowercase)
            .as_deref(),
        Some("bmp")
    )
}

// Decoder's intermediate representation. Always 8-bit grayscale (palette/BGR
// inputs get grayscale-projected during decode).
struct DecodedBmp {
    width: u32,
    height: u32,
    pixels: Vec<u8>,
}

// Parsed BMP header fields we care about.
//   top_down=true is the rare positive-height-equivalent (BMP signals it as
//   a negative height in the header) — in that case we iterate rows in
//   source order. Otherwise (the common case) we flip bottom-up to top-down.
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
    // 8-bit means one grayscale value per pixel after palette expansion.
    // 24/32-bit means three color channels before grayscale projection.
    fn color_channel_count(self) -> u16 {
        if self.bits_per_pixel == 8 { 1 } else { 3 }
    }

    fn color_model(self) -> &'static str {
        if self.color_channel_count() == 1 {
            "grayscale"
        } else {
            "rgb"
        }
    }

    // 1, 3, or 4 — depending on bit depth. Used for row_stride computation.
    fn bytes_per_pixel(self) -> usize {
        usize::from(self.bits_per_pixel / 8)
    }

    // BMP rows are always padded out to a 4-byte boundary. div_ceil(4)*4
    // is the standard idiom; overflow-checked because width*bpp can be huge.
    fn row_stride(self) -> Result<usize, String> {
        self.width
            .checked_mul(self.bytes_per_pixel())
            .ok_or_else(|| "BMP row size overflow".to_string())
            .and_then(|row_bytes| {
                let padding = (4 - row_bytes % 4) % 4;
                row_bytes
                    .checked_add(padding)
                    .ok_or_else(|| "BMP row size overflow".to_string())
            })
    }
}

// The pixel-format dispatch. Palette8 carries the resolved palette → gray LUT
// so we don't recompute it per row. Boxed because [u8; 256] on the stack
// every recursion would be a lot of stack traffic.
enum BmpPixelFormat {
    Gray8,
    Palette8(Box<[u8; 256]>),
    Bgr24,
    Bgra32,
}

// Parse the 14-byte file header + the (variable but ≥40 byte) DIB header.
// We refuse:
//   * Anything not starting "BM" (BMP magic).
//   * DIB headers < 40 bytes (the old OS/2 format — not supported).
//   * Compressed BMPs (compression != 0, i.e. anything but BI_RGB).
//   * Bit depths other than 8/24/32.
//   * Width/height beyond u16::MAX — that's our internal dimension cap.
fn parse_bmp_header(bytes: &[u8]) -> Result<BmpHeader, String> {
    // 54 = 14 (file header) + 40 (minimum DIB header). Anything smaller
    // can't possibly be a valid BMP.
    if bytes.len() < MIN_BMP_HEADER_BYTES || &bytes[..2] != b"BM" {
        return Err("unsupported BMP header".to_string());
    }

    let pixel_offset = LittleEndian::read_u32(&bytes[10..14]) as usize;
    let dib_size = LittleEndian::read_u32(&bytes[14..18]);
    if dib_size < 40 {
        return Err(format!("unsupported BMP DIB header size: {dib_size}"));
    }
    let dib_size_usize =
        usize::try_from(dib_size).map_err(|_| "BMP DIB header size overflow".to_string())?;
    let dib_end = 14_usize
        .checked_add(dib_size_usize)
        .ok_or_else(|| "BMP DIB header size overflow".to_string())?;
    let minimum_pixel_offset = dib_end;
    if pixel_offset < minimum_pixel_offset {
        return Err(format!(
            "invalid BMP pixel data offset: {pixel_offset}, want at least {minimum_pixel_offset}"
        ));
    }

    // Width is i32 in the spec but a negative value is invalid (unlike height).
    // Height is the *signed* trick: negative = top-down row order.
    let width = LittleEndian::read_i32(&bytes[18..22]);
    let raw_height = LittleEndian::read_i32(&bytes[22..26]);
    if width <= 0 || raw_height == 0 {
        return Err(format!("invalid BMP dimensions: {width}x{raw_height}"));
    }
    let height = raw_height.unsigned_abs();
    if width > u16::MAX as i32 || height > u16::MAX as u32 {
        return Err(format!(
            "BMP dimensions exceed supported range: {width}x{raw_height}"
        ));
    }
    let pixel_count = (width as usize)
        .checked_mul(height as usize)
        .ok_or_else(|| "BMP pixel count overflow".to_string())?;
    if pixel_count > MAX_DECODED_PREVIEW_PIXELS {
        return Err(format!(
            "BMP pixel count {pixel_count} exceeds supported limit {MAX_DECODED_PREVIEW_PIXELS}"
        ));
    }

    // Planes must always be 1 per the spec — multi-plane BMPs were never
    // really a thing in practice.
    let planes = LittleEndian::read_u16(&bytes[26..28]);
    if planes != 1 {
        return Err(format!("unsupported BMP plane count: {planes}"));
    }
    let bits_per_pixel = LittleEndian::read_u16(&bytes[28..30]);
    let compression = LittleEndian::read_u32(&bytes[30..34]);
    // 0 = BI_RGB (uncompressed). RLE and BITFIELDS variants aren't supported.
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
        // unsigned_abs handles negative height (top-down) and zero (already
        // rejected above) without wrapping.
        height: height as usize,
        top_down: raw_height < 0,
        bits_per_pixel,
    })
}

// Top-level decode: parse header → check buffer length → dispatch on pixel
// format. Always produces an 8-bit grayscale buffer regardless of input
// format (color sources get grayscale-projected via gray_from_rgb8).
fn decode_bmp(bytes: &[u8]) -> Result<DecodedBmp, String> {
    let header = parse_bmp_header(bytes)?;
    let width = header.width;
    let height = header.height;
    let row_stride = header.row_stride()?;
    // Confirm the buffer is long enough to hold the pixel data we expect.
    // checked_add/mul guard against any malformed header trying to coax us
    // into a huge slice index.
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
    // Pre-allocated grayscale buffer — every decoder branch fills this in.
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

// Dispatch on bit depth. 8-bit needs a palette inspection (it could be
// indexed-color, indexed-gray, or "identity gray"); 24/32 are straightforward.
// unreachable!() is safe because parse_bmp_header already filtered the values.
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

// Read + analyze the palette for an 8-bit BMP.
//
// The fast-path optimization: if every palette entry maps to its own index
// (palette[i] = (i,i,i)), we can skip the palette LUT entirely and treat
// it as plain Gray8. Saves a per-pixel indirection on the (very common)
// identity-grayscale-palette case.
fn bmp_8bit_pixel_format(
    bytes: &[u8],
    header: BmpHeader,
    row_stride: usize,
) -> Result<BmpPixelFormat, String> {
    let palette_start = 14 + header.dib_size as usize;
    // BMP stores "colors used" at offset 46. 0 means "all 256". parse_bmp_header
    // already ensured bytes.len() >= 14 + dib_size >= 54, so bytes[46..50] is
    // always in-bounds here.
    let color_count = LittleEndian::read_u32(&bytes[46..50]);
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
    // identity_gray starts true only if we have all 256 entries — a truncated
    // palette can't be the identity (some indexes would be unmapped).
    let mut identity_gray = palette_entries == 256;
    for index in 0..palette_entries {
        let offset = index * 4;
        // Palette entries are stored BGRA (last byte reserved/alpha).
        let gray = gray_from_rgb8(palette[offset + 2], palette[offset + 1], palette[offset]);
        palette_lut[index] = gray;
        // Track whether every entry is gray(i,i,i) — short-circuits below.
        identity_gray &= gray == index as u8;
    }

    // Partial palette → validate that every pixel index actually points to a
    // valid entry. Skipped when full because indexing into a 256-entry LUT
    // is always in bounds.
    if palette_entries < 256 {
        validate_8bit_palette_indexes(bytes, header, row_stride, palette_entries)?;
    }

    if identity_gray {
        Ok(BmpPixelFormat::Gray8)
    } else {
        Ok(BmpPixelFormat::Palette8(palette_lut))
    }
}

// For palettes with fewer than 256 entries, walk every row and confirm no
// pixel byte references an out-of-bounds index. Done up-front so the row
// decoders can use the LUT without bounds checks.
fn validate_8bit_palette_indexes(
    bytes: &[u8],
    header: BmpHeader,
    row_stride: usize,
    palette_entries: usize,
) -> Result<(), String> {
    for output_y in 0..header.height {
        // BMP row order: bottom-up unless top_down, in which case as-stored.
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

// Four row-decoder variants below — Gray8, Palette8, BGR24, BGRA32. They
// share the same shape: walk output rows, compute the corresponding source
// row (handling bottom-up vs top-down), and fill in pixel bytes.
//
// All of them are hot paths for big bitewing files. Inner loops avoid bounds
// checks via iter pairs (.zip(.iter_mut()).chunks_exact) where possible.

// Trivial: source bytes are already grayscale, just copy. row_start/output
// math handles the row-flip.
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

// Palette indexed: every source byte is an index into palette_lut, which we
// pre-computed in bmp_8bit_pixel_format. The LUT is [u8; 256] so bounds
// checks should optimize away.
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

// 24-bit BGR (no alpha). chunks_exact(3) gives us [B,G,R] triples; we pass
// them to gray_from_rgb8 in R,G,B order (note the source[2], [1], [0]).
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

// 32-bit BGRA — same as BGR24 but with an alpha byte we ignore. chunks_exact(4)
// instead of (3). We deliberately don't honor alpha — bitewings don't use it.
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

// Rec. 601 luma weights (approximate): 0.299 R + 0.587 G + 0.114 B, scaled
// up to 16-bit and shifted back. The `(1 << 15)` adds half a bit for
// round-to-nearest. Integer math throughout for speed and bitwise determinism.
//
// The `u32::from(red) | (u32::from(red) << 8)` trick doubles the 8-bit input
// into a 16-bit value (so 0xFF → 0xFFFF), keeping the multiply precision tight.
fn gray_from_rgb8(red: u8, green: u8, blue: u8) -> u8 {
    let red = u32::from(red) | (u32::from(red) << 8);
    let green = u32::from(green) | (u32::from(green) << 8);
    let blue = u32::from(blue) | (u32::from(blue) << 8);
    ((19_595 * red + 38_470 * green + 7_471 * blue + (1 << 15)) >> 24) as u8
}

// Linear stretch: maps `value` from [min, max] to [0, 255]. When max == min
// the input is flat — return 0 rather than divide-by-zero.
fn map_linear(value: f32, min: f32, max: f32) -> u8 {
    if max <= min {
        return 0;
    }

    clamp_to_byte((value - min) * (255.0 / (max - min)))
}

// f32 → u8 with saturation. `+ 0.5` before the cast gives round-to-nearest
// (since `as u8` truncates toward zero on positives).
fn clamp_to_byte(value: f32) -> u8 {
    if value <= 0.0 {
        0
    } else if value >= 255.0 {
        255
    } else {
        (value + 0.5) as u8
    }
}

#[cfg(test)]
pub mod tests {
    use super::*;

    // End-to-end: write a 2×2 32-bit BMP, parse the metadata back. Pins
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

        assert_eq!(metadata.width, 2);
        assert_eq!(metadata.height, 2);
        assert_eq!(metadata.color_channel_count, 3);
        assert_eq!(metadata.bits_per_channel, 8);
        assert_eq!(metadata.color_model, "rgb");
        let _ = std::fs::remove_file(path);
    }

    // Confirms read_file only touches the header — a 54-byte truncated
    // file still gives us valid dimensions. Important: lets us probe huge
    // files quickly without paging in pixel data.
    #[test]
    fn read_file_reads_metadata_without_pixel_data() {
        let path = unique_temp_path("metadata-header-only", "bmp");
        let pixels = vec![(0, 0, 0); 854_usize * 1200_usize];
        let mut bmp = build_bmp_32(854, 1200, &pixels);
        bmp.truncate(54);
        std::fs::write(&path, bmp).unwrap();

        let metadata = read_file(path.to_str().unwrap()).unwrap();

        assert_eq!(metadata.width, 854);
        assert_eq!(metadata.height, 1200);
        assert_eq!(metadata.color_channel_count, 3);
        assert_eq!(metadata.bits_per_channel, 8);
        assert_eq!(metadata.color_model, "rgb");
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn read_header_prefix_stops_before_pixel_data() {
        struct HeaderOnlyReader {
            bytes: Vec<u8>,
            position: usize,
        }

        impl std::io::Read for HeaderOnlyReader {
            fn read(&mut self, output: &mut [u8]) -> std::io::Result<usize> {
                if self.position >= MIN_BMP_HEADER_BYTES {
                    return Err(std::io::Error::other("pixel data was read"));
                }
                let remaining_header = MIN_BMP_HEADER_BYTES - self.position;
                let count = remaining_header.min(output.len());
                output[..count].copy_from_slice(&self.bytes[self.position..self.position + count]);
                self.position += count;
                Ok(count)
            }
        }

        let mut reader = HeaderOnlyReader {
            bytes: build_bmp_32(
                2,
                2,
                &[(0, 0, 0), (255, 0, 0), (0, 255, 0), (255, 255, 255)],
            ),
            position: 0,
        };

        let metadata = read_header_prefix(&mut reader).unwrap();

        assert_eq!(metadata.width, 2);
        assert_eq!(metadata.height, 2);
        assert_eq!(reader.position, MIN_BMP_HEADER_BYTES);
    }

    // Symmetric to the above — the *render* path must NOT accept a
    // header-only file; it needs real pixels.
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
    fn render_rejects_pixel_offset_inside_header() {
        let mut bmp = build_bmp_32(1, 1, &[(255, 255, 255)]);
        bmp[10..14].copy_from_slice(&14_u32.to_le_bytes());

        let error = render_grayscale_preview(&bmp).unwrap_err();

        assert!(error.contains("invalid BMP pixel data offset"));
    }

    #[test]
    fn render_rejects_absurd_dib_header_size_without_panicking() {
        let mut bmp = build_bmp_32(1, 1, &[(255, 255, 255)]);
        bmp[14..18].copy_from_slice(&u32::MAX.to_le_bytes());

        let error = render_grayscale_preview(&bmp).unwrap_err();

        assert!(
            error.contains("BMP DIB header size overflow")
                || error.contains("truncated BMP DIB header")
                || error.contains("invalid BMP pixel data offset")
        );
    }

    #[test]
    fn render_rejects_absurd_pixel_count_before_allocation() {
        let mut bmp = build_bmp_32_header(65_535, 65_535);

        let error = render_grayscale_preview(&bmp).unwrap_err();

        assert!(error.contains("BMP pixel count"));
        assert!(error.contains("exceeds supported limit"));
        bmp.truncate(54);
        assert!(read_header(&bmp).unwrap_err().contains("BMP pixel count"));
    }

    #[test]
    fn row_stride_rejects_padding_overflow() {
        let header = BmpHeader {
            pixel_offset: 54,
            dib_size: 40,
            width: usize::MAX,
            height: 1,
            top_down: false,
            bits_per_pixel: 8,
        };

        let error = header.row_stride().unwrap_err();

        assert!(error.contains("BMP row size overflow"));
    }

    // Full render pipeline on a tiny 2×2 color BMP. Compares against the
    // full-range-stretched grayscale we expect after gray_from_rgb8 + linear
    // stretch.
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

    // Direct comparison: default (stretched) vs tooth-analysis (raw) on the
    // same input. Default maps the [10,40] range to [0,255]; tooth-analysis
    // keeps the original values verbatim.
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

    // Algebraic identity: rendering from a decoded source == rendering from
    // the raw bytes, for both default and tooth-analysis variants. Proves
    // we don't drift between the two code paths.
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

    // Palette path: 2-entry palette mapping black→0, white→255, source
    // pixels are indices [0, 1]. Output should be [0, 255].
    #[test]
    fn render_bmp_supports_palette_pixels() {
        let bmp = build_bmp_8_palette(2, 1, &[(0, 0, 0), (255, 255, 255)], &[0, 1]);
        let preview = render_grayscale_preview(&bmp).unwrap();

        assert_eq!(preview.pixels.as_ref(), [0, 255]);
    }

    #[test]
    fn render_bmp_supports_top_down_rows() {
        let bmp = build_bmp_32_top_down(
            2,
            2,
            &[(0, 0, 0), (32, 32, 32), (160, 160, 160), (255, 255, 255)],
        );
        let preview = render_grayscale_preview_with_options(&bmp, true).unwrap();

        assert_eq!(preview.pixels.as_ref(), [0, 32, 160, 255]);
    }

    #[test]
    fn render_bmp_rejects_partial_palette_index_out_of_range() {
        let bmp = build_bmp_8_palette(2, 1, &[(0, 0, 0), (255, 255, 255)], &[0, 2]);

        let error = render_grayscale_preview(&bmp).unwrap_err();

        assert!(error.contains("BMP palette index 2 exceeds 2 entries"));
    }

    // Extension guard: even if the bytes are a valid BMP, a non-BMP suffix
    // means we refuse.
    #[test]
    fn rejects_non_bmp_extension() {
        let path = unique_temp_path("metadata", "png");
        std::fs::write(&path, build_bmp_32(1, 1, &[(0, 0, 0)])).unwrap();

        let error = read_file(path.to_str().unwrap()).unwrap_err();
        let decode_error = decode_source_preview_file(&path).unwrap_err();
        let render_error = render_grayscale_preview_file(&path).unwrap_err();

        assert!(error.contains("expected .bmp"));
        assert!(decode_error.contains("expected .bmp"));
        assert!(render_error.contains("expected .bmp"));
        let _ = std::fs::remove_file(path);
    }

    // Temp path generator: PID + nanos so concurrent test runs don't collide.
    // Pub because example benches in ../examples/ pull from this module.
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

    // Hand-build a 32-bit BGRA BMP from a top-down row-major RGB list.
    // Note we reverse the rows on write — BMP files store bottom-up.
    pub fn build_bmp_32(width: u32, height: u32, rgb_top_down: &[(u8, u8, u8)]) -> Vec<u8> {
        assert_eq!(rgb_top_down.len(), width as usize * height as usize);
        let row_stride = width as usize * 4;
        let pixel_bytes = row_stride * height as usize;
        let file_size = 54 + pixel_bytes;
        let mut bmp = build_bmp_32_header(width, height);
        bmp.reserve(pixel_bytes);
        for output_y in (0..height as usize).rev() {
            let row = &rgb_top_down[output_y * width as usize..(output_y + 1) * width as usize];
            for &(red, green, blue) in row {
                bmp.extend_from_slice(&[blue, green, red, 255]);
            }
        }
        debug_assert_eq!(bmp.len(), file_size);
        bmp
    }

    fn build_bmp_32_top_down(width: u32, height: u32, rgb_top_down: &[(u8, u8, u8)]) -> Vec<u8> {
        assert_eq!(rgb_top_down.len(), width as usize * height as usize);
        let mut bmp = build_bmp_32_header(width, height);
        bmp[22..26].copy_from_slice(&(-(height as i32)).to_le_bytes());
        for row in rgb_top_down.chunks_exact(width as usize) {
            for &(red, green, blue) in row {
                bmp.extend_from_slice(&[blue, green, red, 255]);
            }
        }
        bmp
    }

    fn build_bmp_32_header(width: u32, height: u32) -> Vec<u8> {
        let row_stride = width as usize * 4;
        let pixel_bytes = row_stride.saturating_mul(height as usize);
        let file_size = 54_usize.saturating_add(pixel_bytes);
        let mut bmp = Vec::with_capacity(54);
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
        bmp
    }

    // Build an 8-bit palette BMP. palette_rgb's length is the entry count
    // (stored in the "colors used" header field), indexes_top_down is the
    // pixel-index grid. Both row stride padding and the bottom-up flip are
    // applied to match what real BMPs look like on disk.
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

    // Reference impl of the full-range linear stretch used by the renderer.
    // Tests reach for this to compute the expected output without going
    // through the production map_linear path.
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
