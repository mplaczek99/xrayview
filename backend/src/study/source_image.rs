use std::path::Path;

use anyhow::{Context, Result, bail};
use dicom_core::{DataElement, DicomValue, PrimitiveValue, Tag};
use dicom_dictionary_std::tags;
use dicom_object::mem::InMemDicomObject;
use dicom_object::{DefaultDicomObject, DicomAttribute, DicomObject, OpenFileOptions};
use dicom_pixeldata::PixelDecoder;
use image::DynamicImage;
use uuid::Uuid;

use crate::api::MeasurementScale;
use crate::render::windowing::WindowLevel;

const PRESERVED_SOURCE_TAGS: &[Tag] = &[
    tags::PATIENT_NAME,
    tags::PATIENT_ID,
    tags::PATIENT_BIRTH_DATE,
    tags::PATIENT_SEX,
    tags::STUDY_ID,
    tags::STUDY_DATE,
    tags::STUDY_TIME,
    tags::ACCESSION_NUMBER,
    tags::STUDY_DESCRIPTION,
    tags::REFERRING_PHYSICIAN_NAME,
    tags::INSTITUTION_NAME,
    tags::PIXEL_SPACING,
    tags::IMAGER_PIXEL_SPACING,
    tags::NOMINAL_SCANNED_PIXEL_SPACING,
    Tag(0x0028, 0x0A04), // PixelSpacingCalibrationType
    Tag(0x0028, 0x0A02), // PixelSpacingCalibrationDescription
];

#[derive(Debug, Clone)]
pub struct SourceStudy {
    pub image: SourceImage,
    pub metadata: SourceMetadata,
    pub measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Clone)]
pub struct SourceImage {
    pub width: u32,
    pub height: u32,
    pub pixels: Vec<f32>,
    pub min_value: f32,
    pub max_value: f32,
    pub default_window: Option<WindowLevel>,
    pub invert: bool,
}

#[derive(Debug, Clone)]
pub struct SourceMetadata {
    study_instance_uid: String,
    preserved_elements: Vec<DataElement<InMemDicomObject>>,
}

#[derive(Debug, Clone, Copy)]
struct SourceDecodeConfig {
    bits_stored: u16,
    pixel_representation: u16,
    slope: f32,
    intercept: f32,
    default_window: Option<WindowLevel>,
    invert: bool,
}

impl SourceImage {
    pub fn len(&self) -> usize {
        self.pixels.len()
    }
}

impl SourceMetadata {
    pub fn extract(source: &DefaultDicomObject) -> Self {
        let study_instance_uid = source
            .element(tags::STUDY_INSTANCE_UID)
            .ok()
            .and_then(|element| element.to_str().ok())
            .map(|value| value.trim().to_string())
            .filter(|value| !value.is_empty())
            .unwrap_or_else(generate_uid);

        let preserved_elements = PRESERVED_SOURCE_TAGS
            .iter()
            .filter_map(|&tag| source.element(tag).ok().cloned())
            .collect();

        Self {
            study_instance_uid,
            preserved_elements,
        }
    }

    pub fn from_preserved_elements(
        study_instance_uid: String,
        preserved_elements: Vec<DataElement<InMemDicomObject>>,
    ) -> Self {
        Self {
            study_instance_uid,
            preserved_elements,
        }
    }

    pub fn study_instance_uid(&self) -> &str {
        &self.study_instance_uid
    }

    pub fn preserved_elements(&self) -> &[DataElement<InMemDicomObject>] {
        &self.preserved_elements
    }
}

pub fn load_source_study(path: &Path) -> Result<SourceStudy> {
    let obj = OpenFileOptions::new()
        .open_file(path)
        .with_context(|| format!("decode DICOM: {}", path.display()))?;

    Ok(SourceStudy {
        image: decode_source_image(&obj)?,
        metadata: SourceMetadata::extract(&obj),
        measurement_scale: measurement_scale_from_obj(&obj),
    })
}

pub fn measurement_scale_from_obj<O>(obj: &O) -> Option<MeasurementScale>
where
    O: DicomObject,
{
    [
        (tags::PIXEL_SPACING, "PixelSpacing"),
        (tags::IMAGER_PIXEL_SPACING, "ImagerPixelSpacing"),
        (
            tags::NOMINAL_SCANNED_PIXEL_SPACING,
            "NominalScannedPixelSpacing",
        ),
    ]
    .into_iter()
    .find_map(|(tag, source)| {
        lookup_float_pair(obj, tag).and_then(|(row_spacing_mm, column_spacing_mm)| {
            (row_spacing_mm > 0.0 && column_spacing_mm > 0.0).then_some(MeasurementScale {
                row_spacing_mm,
                column_spacing_mm,
                source,
            })
        })
    })
}

fn decode_source_image(obj: &DefaultDicomObject) -> Result<SourceImage> {
    let width = usize::from(required_u16_attr(obj, tags::COLUMNS, "Columns")?);
    let height = usize::from(required_u16_attr(obj, tags::ROWS, "Rows")?);
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

    match pixel_data.value() {
        DicomValue::Primitive(prim) => decode_native_primitive(
            obj,
            prim,
            bits_allocated,
            frame_pixels,
            frame_sample_count,
            samples_per_pixel,
        ),
        DicomValue::PixelSequence(_) => decode_encapsulated_frame(obj),
        DicomValue::Sequence(_) => bail!("unsupported pixel data representation"),
    }
}

fn decode_encapsulated_frame(obj: &DefaultDicomObject) -> Result<SourceImage> {
    let ts_uid = obj.meta().transfer_syntax().trim();
    let decoded = obj
        .decode_pixel_data()
        .with_context(|| format!("decode encapsulated pixel data (transfer syntax {ts_uid})"))?;
    let dynamic_image = decoded
        .to_dynamic_image(0)
        .context("convert decoded pixel data to image")?;
    let cfg = resolve_decode_config(obj, 8);
    source_image_from_dynamic(dynamic_image, cfg.default_window, cfg.invert)
}

fn source_image_from_dynamic(
    image: DynamicImage,
    default_window: Option<WindowLevel>,
    invert: bool,
) -> Result<SourceImage> {
    match image {
        DynamicImage::ImageLuma8(gray) => build_source_image(
            gray.width(),
            gray.height(),
            gray.into_raw().into_iter().map(f32::from).collect(),
            default_window,
            invert,
        ),
        DynamicImage::ImageLuma16(gray) => build_source_image(
            gray.width(),
            gray.height(),
            gray.into_raw().into_iter().map(f32::from).collect(),
            default_window,
            invert,
        ),
        other => {
            let rgb = other.to_rgb8();
            let (width, height) = rgb.dimensions();
            let pixels = rgb
                .as_raw()
                .chunks_exact(3)
                .map(|chunk| f32::from(gray_from_rgb8(chunk[0], chunk[1], chunk[2])))
                .collect();
            build_source_image(width, height, pixels, None, false)
        }
    }
}

fn decode_native_primitive(
    obj: &DefaultDicomObject,
    prim: &PrimitiveValue,
    bits_allocated: u16,
    frame_pixels: usize,
    frame_sample_count: usize,
    samples_per_pixel: usize,
) -> Result<SourceImage> {
    let width = required_u16_attr(obj, tags::COLUMNS, "Columns")? as u32;
    let height = required_u16_attr(obj, tags::ROWS, "Rows")? as u32;

    match bits_allocated {
        8 => {
            let raw = prim.to_bytes();
            ensure_frame_len(raw.len(), frame_sample_count)?;
            let samples = &raw[..frame_sample_count];
            let cfg = resolve_decode_config(obj, 8);
            let pixels = if samples_per_pixel == 1 {
                decode_u8_monochrome(samples, cfg)
            } else {
                decode_u8_color(obj, samples, frame_pixels, samples_per_pixel)?
            };
            build_source_image(width, height, pixels, cfg.default_window, cfg.invert)
        }
        16 => {
            if samples_per_pixel != 1 {
                bail!("16-bit color DICOM source decode is not supported yet")
            }
            let cfg = resolve_decode_config(obj, 16);
            let pixels = match prim {
                PrimitiveValue::U16(values) => {
                    ensure_frame_len(values.len(), frame_sample_count)?;
                    decode_u16_monochrome(&values[..frame_sample_count], cfg)
                }
                _ => {
                    let raw = prim.to_bytes();
                    ensure_frame_len(raw.len(), frame_sample_count * 2)?;
                    let samples = read_u16_samples(&raw[..frame_sample_count * 2]);
                    decode_u16_monochrome(&samples, cfg)
                }
            };
            build_source_image(width, height, pixels, cfg.default_window, cfg.invert)
        }
        32 => {
            if samples_per_pixel != 1 {
                bail!("32-bit color DICOM source decode is not supported yet")
            }
            let cfg = resolve_decode_config(obj, 32);
            let pixels = match prim {
                PrimitiveValue::U32(values) => {
                    ensure_frame_len(values.len(), frame_sample_count)?;
                    decode_u32_monochrome(&values[..frame_sample_count], cfg)
                }
                _ => {
                    let raw = prim.to_bytes();
                    ensure_frame_len(raw.len(), frame_sample_count * 4)?;
                    let samples = read_u32_samples(&raw[..frame_sample_count * 4]);
                    decode_u32_monochrome(&samples, cfg)
                }
            };
            build_source_image(width, height, pixels, cfg.default_window, cfg.invert)
        }
        other => bail!("unsupported BitsAllocated for source decode: {other}"),
    }
}

fn build_source_image(
    width: u32,
    height: u32,
    pixels: Vec<f32>,
    default_window: Option<WindowLevel>,
    invert: bool,
) -> Result<SourceImage> {
    let expected = width as usize * height as usize;
    if pixels.len() != expected {
        bail!(
            "decoded source pixel count {} does not match dimensions {}x{}",
            pixels.len(),
            width,
            height
        );
    }

    let (min_value, max_value) = pixels.iter().copied().fold(
        (f32::INFINITY, f32::NEG_INFINITY),
        |(min_value, max_value), value| (min_value.min(value), max_value.max(value)),
    );
    let (min_value, max_value) = if pixels.is_empty() {
        (0.0, 0.0)
    } else {
        (min_value, max_value)
    };

    Ok(SourceImage {
        width,
        height,
        pixels,
        min_value,
        max_value,
        default_window,
        invert,
    })
}

fn decode_u8_monochrome(samples: &[u8], cfg: SourceDecodeConfig) -> Vec<f32> {
    samples
        .iter()
        .copied()
        .map(|value| scaled_stored_pixel_value(u32::from(value), cfg))
        .collect()
}

fn decode_u16_monochrome(samples: &[u16], cfg: SourceDecodeConfig) -> Vec<f32> {
    samples
        .iter()
        .copied()
        .map(|value| scaled_stored_pixel_value(u32::from(value), cfg))
        .collect()
}

fn decode_u32_monochrome(samples: &[u32], cfg: SourceDecodeConfig) -> Vec<f32> {
    samples
        .iter()
        .copied()
        .map(|value| scaled_stored_pixel_value(value, cfg))
        .collect()
}

fn decode_u8_color(
    obj: &DefaultDicomObject,
    samples: &[u8],
    frame_pixels: usize,
    samples_per_pixel: usize,
) -> Result<Vec<f32>> {
    if samples_per_pixel != 3 {
        bail!("unsupported SamplesPerPixel for color source decode: {samples_per_pixel}")
    }

    let photometric = optional_str_attr(obj, tags::PHOTOMETRIC_INTERPRETATION)
        .unwrap_or_default()
        .trim()
        .to_ascii_uppercase();
    if photometric != "RGB" {
        bail!("unsupported color photometric interpretation: {photometric}")
    }

    let planar_configuration = optional_u16_attr(obj, tags::PLANAR_CONFIGURATION).unwrap_or(0);
    let mut pixels = vec![0_f32; frame_pixels];

    match planar_configuration {
        0 => {
            for (index, chunk) in samples.chunks_exact(3).enumerate() {
                pixels[index] = f32::from(gray_from_rgb8(chunk[0], chunk[1], chunk[2]));
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
                pixels[index] = f32::from(gray_from_rgb8(red[index], green[index], blue[index]));
            }
        }
        other => bail!("unsupported planar configuration: {other}"),
    }

    Ok(pixels)
}

fn resolve_decode_config(obj: &DefaultDicomObject, default_bits_stored: u16) -> SourceDecodeConfig {
    let bits_stored = optional_u16_attr(obj, tags::BITS_STORED)
        .filter(|value| *value > 0)
        .unwrap_or(default_bits_stored);
    let pixel_representation = optional_u16_attr(obj, tags::PIXEL_REPRESENTATION).unwrap_or(0);
    let slope = optional_f64_attr(obj, tags::RESCALE_SLOPE).unwrap_or(1.0) as f32;
    let intercept = optional_f64_attr(obj, tags::RESCALE_INTERCEPT).unwrap_or(0.0) as f32;
    let default_window = match (
        optional_f64_attr(obj, tags::WINDOW_CENTER),
        optional_f64_attr(obj, tags::WINDOW_WIDTH),
    ) {
        (Some(center), Some(width)) if width > 1.0 => Some(WindowLevel {
            center: center as f32,
            width: width as f32,
        }),
        _ => None,
    };
    let invert = optional_str_attr(obj, tags::PHOTOMETRIC_INTERPRETATION)
        .map(|value| value.eq_ignore_ascii_case("MONOCHROME1"))
        .unwrap_or(false);

    SourceDecodeConfig {
        bits_stored,
        pixel_representation,
        slope,
        intercept,
        default_window,
        invert,
    }
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

fn scaled_stored_pixel_value(raw_value: u32, cfg: SourceDecodeConfig) -> f32 {
    decode_stored_pixel_value(raw_value, cfg.bits_stored, cfg.pixel_representation) as f32
        * cfg.slope
        + cfg.intercept
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

fn lookup_float_pair<O>(obj: &O, tag: dicom_object::Tag) -> Option<(f64, f64)>
where
    O: DicomObject,
{
    let attr = obj.attr(tag).ok()?;
    let raw = attr.to_str().ok()?;
    parse_float_pair(raw.as_ref())
}

fn parse_float_pair(raw: &str) -> Option<(f64, f64)> {
    let mut parts = raw
        .split('\\')
        .map(str::trim)
        .filter(|part| !part.is_empty());
    let first = parts.next()?.parse().ok()?;
    let second = parts.next()?.parse().ok()?;
    Some((first, second))
}

fn generate_uid() -> String {
    let uuid = Uuid::new_v4();
    let value = u128::from_be_bytes(*uuid.as_bytes());
    format!("2.25.{value}")
}

#[cfg(test)]
mod tests {
    use dicom_core::{DataElement, DicomValue, PrimitiveValue, VR};
    use dicom_dictionary_std::tags;
    use dicom_object::{DefaultDicomObject, FileDicomObject, meta::FileMetaTableBuilder};

    use super::{
        SourceMetadata, decode_stored_pixel_value, measurement_scale_from_obj, parse_float_pair,
    };

    const EXPLICIT_VR_LITTLE_ENDIAN: &str = "1.2.840.10008.1.2.1";

    #[test]
    fn decode_stored_pixel_value_sign_extends() {
        assert_eq!(decode_stored_pixel_value(0x0fff, 12, 1), -1);
        assert_eq!(decode_stored_pixel_value(0x07ff, 12, 1), 2047);
    }

    #[test]
    fn parse_float_pair_accepts_dicom_pair() {
        assert_eq!(parse_float_pair("0.4\\0.6"), Some((0.4, 0.6)));
    }

    #[test]
    fn measurement_scale_reads_pixel_spacing() {
        let meta = FileMetaTableBuilder::new()
            .transfer_syntax(EXPLICIT_VR_LITTLE_ENDIAN)
            .build()
            .expect("file meta");
        let mut source: DefaultDicomObject = FileDicomObject::new_empty_with_meta(meta);
        source.put(DataElement::new(
            tags::PIXEL_SPACING,
            VR::DS,
            DicomValue::Primitive(PrimitiveValue::Str("0.2\\0.3".to_string())),
        ));

        let scale = measurement_scale_from_obj(&source).expect("measurement scale");

        assert_eq!(scale.row_spacing_mm, 0.2);
        assert_eq!(scale.column_spacing_mm, 0.3);
        assert_eq!(scale.source, "PixelSpacing");
    }

    #[test]
    fn source_metadata_generates_uid_when_study_uid_missing() {
        let meta = FileMetaTableBuilder::new()
            .transfer_syntax(EXPLICIT_VR_LITTLE_ENDIAN)
            .build()
            .expect("file meta");
        let source: DefaultDicomObject = FileDicomObject::new_empty_with_meta(meta);

        let metadata = SourceMetadata::extract(&source);

        assert!(metadata.study_instance_uid().starts_with("2.25."));
        assert!(metadata.preserved_elements().is_empty());
    }
}
