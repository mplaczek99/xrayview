use std::{
    fs,
    io::{self, Cursor, Read, Seek, SeekFrom},
    path::Path,
    sync::atomic::{AtomicU64, Ordering},
    time::{SystemTime, UNIX_EPOCH},
};

use crate::{
    contracts::MeasurementScale,
    render::{PreviewFormat, PreviewImage},
};

const PART10_PREAMBLE_LENGTH: usize = 128;
const PART10_MAGIC: &[u8; 4] = b"DICM";
const IMPLICIT_LITTLE_ENDIAN_TRANSFER_SYNTAX: &str = "1.2.840.10008.1.2";
const EXPLICIT_LITTLE_ENDIAN_TRANSFER_SYNTAX: &str = "1.2.840.10008.1.2.1";
const EXPLICIT_BIG_ENDIAN_TRANSFER_SYNTAX: &str = "1.2.840.10008.1.2.2";
const DEFLATED_TRANSFER_SYNTAX: &str = "1.2.840.10008.1.2.1.99";
const SECONDARY_CAPTURE_SOP_CLASS_UID: &str = "1.2.840.10008.5.1.4.1.1.7";
const IMPLEMENTATION_CLASS_UID: &str = "2.25.302043790172249692526321623266752743501";
const IMPLEMENTATION_VERSION_NAME: &str = "XRAYVIEW_RS_1_0";
const UNDEFINED_LENGTH: u32 = u32::MAX;

const TAG_FILE_META_GROUP_LENGTH: Tag = Tag::new(0x0002, 0x0000);
const TAG_FILE_META_INFORMATION_VERSION: Tag = Tag::new(0x0002, 0x0001);
const TAG_MEDIA_STORAGE_SOP_CLASS_UID: Tag = Tag::new(0x0002, 0x0002);
const TAG_MEDIA_STORAGE_SOP_INSTANCE_UID: Tag = Tag::new(0x0002, 0x0003);
const TAG_TRANSFER_SYNTAX_UID: Tag = Tag::new(0x0002, 0x0010);
const TAG_IMPLEMENTATION_CLASS_UID: Tag = Tag::new(0x0002, 0x0012);
const TAG_IMPLEMENTATION_VERSION_NAME: Tag = Tag::new(0x0002, 0x0013);
const TAG_IMAGE_TYPE: Tag = Tag::new(0x0008, 0x0008);
const TAG_INSTANCE_CREATION_DATE: Tag = Tag::new(0x0008, 0x0012);
const TAG_INSTANCE_CREATION_TIME: Tag = Tag::new(0x0008, 0x0013);
const TAG_SOP_CLASS_UID: Tag = Tag::new(0x0008, 0x0016);
const TAG_SOP_INSTANCE_UID: Tag = Tag::new(0x0008, 0x0018);
const TAG_CONTENT_DATE: Tag = Tag::new(0x0008, 0x0023);
const TAG_CONTENT_TIME: Tag = Tag::new(0x0008, 0x0033);
const TAG_ACCESSION_NUMBER: Tag = Tag::new(0x0008, 0x0050);
const TAG_MODALITY: Tag = Tag::new(0x0008, 0x0060);
const TAG_CONVERSION_TYPE: Tag = Tag::new(0x0008, 0x0064);
const TAG_MANUFACTURER: Tag = Tag::new(0x0008, 0x0070);
const TAG_INSTITUTION_NAME: Tag = Tag::new(0x0008, 0x0080);
const TAG_REFERRING_PHYSICIAN_NAME: Tag = Tag::new(0x0008, 0x0090);
const TAG_STUDY_DATE: Tag = Tag::new(0x0008, 0x0020);
const TAG_STUDY_TIME: Tag = Tag::new(0x0008, 0x0030);
const TAG_STUDY_DESCRIPTION: Tag = Tag::new(0x0008, 0x1030);
const TAG_SERIES_DESCRIPTION: Tag = Tag::new(0x0008, 0x103e);
const TAG_MANUFACTURER_MODEL_NAME: Tag = Tag::new(0x0008, 0x1090);
const TAG_DERIVATION_DESCRIPTION: Tag = Tag::new(0x0008, 0x2111);
const TAG_SOFTWARE_VERSIONS: Tag = Tag::new(0x0018, 0x1020);
const TAG_STUDY_INSTANCE_UID: Tag = Tag::new(0x0020, 0x000d);
const TAG_STUDY_ID: Tag = Tag::new(0x0020, 0x0010);
const TAG_SERIES_INSTANCE_UID: Tag = Tag::new(0x0020, 0x000e);
const TAG_SERIES_NUMBER: Tag = Tag::new(0x0020, 0x0011);
const TAG_INSTANCE_NUMBER: Tag = Tag::new(0x0020, 0x0013);
const TAG_PATIENT_NAME: Tag = Tag::new(0x0010, 0x0010);
const TAG_PATIENT_ID: Tag = Tag::new(0x0010, 0x0020);
const TAG_PATIENT_BIRTH_DATE: Tag = Tag::new(0x0010, 0x0030);
const TAG_PATIENT_SEX: Tag = Tag::new(0x0010, 0x0040);
const TAG_SAMPLES_PER_PIXEL: Tag = Tag::new(0x0028, 0x0002);
const TAG_PHOTOMETRIC_INTERPRETATION: Tag = Tag::new(0x0028, 0x0004);
const TAG_NUMBER_OF_FRAMES: Tag = Tag::new(0x0028, 0x0008);
const TAG_ROWS: Tag = Tag::new(0x0028, 0x0010);
const TAG_COLUMNS: Tag = Tag::new(0x0028, 0x0011);
const TAG_PIXEL_SPACING: Tag = Tag::new(0x0028, 0x0030);
const TAG_PIXEL_SPACING_CALIBRATION_DESCRIPTION: Tag = Tag::new(0x0028, 0x0a02);
const TAG_PIXEL_SPACING_CALIBRATION_TYPE: Tag = Tag::new(0x0028, 0x0a04);
const TAG_BITS_ALLOCATED: Tag = Tag::new(0x0028, 0x0100);
const TAG_BITS_STORED: Tag = Tag::new(0x0028, 0x0101);
const TAG_HIGH_BIT: Tag = Tag::new(0x0028, 0x0102);
const TAG_PIXEL_REPRESENTATION: Tag = Tag::new(0x0028, 0x0103);
const TAG_PLANAR_CONFIGURATION: Tag = Tag::new(0x0028, 0x0006);
const TAG_WINDOW_CENTER: Tag = Tag::new(0x0028, 0x1050);
const TAG_WINDOW_WIDTH: Tag = Tag::new(0x0028, 0x1051);
const TAG_RESCALE_INTERCEPT: Tag = Tag::new(0x0028, 0x1052);
const TAG_RESCALE_SLOPE: Tag = Tag::new(0x0028, 0x1053);
const TAG_IMAGER_PIXEL_SPACING: Tag = Tag::new(0x0018, 0x1164);
const TAG_NOMINAL_SCANNED_PIXEL_SPACING: Tag = Tag::new(0x0018, 0x2010);
const TAG_PIXEL_DATA: Tag = Tag::new(0x7fe0, 0x0010);
const TAG_ITEM: Tag = Tag::new(0xfffe, 0xe000);
const TAG_ITEM_DELIMITATION: Tag = Tag::new(0xfffe, 0xe00d);
const TAG_SEQUENCE_DELIMITATION: Tag = Tag::new(0xfffe, 0xe0dd);

pub const PIXEL_DATA_ENCODING_MISSING: &str = "missing";
pub const PIXEL_DATA_ENCODING_NATIVE: &str = "native";
pub const PIXEL_DATA_ENCODING_ENCAPSULATED: &str = "encapsulated";

static SECONDARY_CAPTURE_UID_COUNTER: AtomicU64 = AtomicU64::new(1);

#[derive(Debug, Clone, PartialEq)]
pub struct SpacingPair {
    pub row_spacing_mm: f64,
    pub column_spacing_mm: f64,
}

#[derive(Debug, Clone, PartialEq, Default)]
pub struct Metadata {
    pub rows: u16,
    pub columns: u16,
    pub samples_per_pixel: u16,
    pub bits_allocated: u16,
    pub bits_stored: u16,
    pub pixel_representation: u16,
    pub planar_configuration: u16,
    pub number_of_frames: u32,
    pub pixel_data_encoding: String,
    pub photometric_interpretation: String,
    pub window_center: Option<f64>,
    pub window_width: Option<f64>,
    pub rescale_intercept: Option<f64>,
    pub rescale_slope: Option<f64>,
    pub pixel_spacing: Option<SpacingPair>,
    pub imager_pixel_spacing: Option<SpacingPair>,
    pub nominal_scanned_pixel_spacing: Option<SpacingPair>,
    pub transfer_syntax_uid: String,
    pub study_instance_uid: String,
    pub preserved_elements: Vec<PreservedElement>,
}

impl Metadata {
    pub fn measurement_scale(&self) -> Option<MeasurementScale> {
        [
            (self.pixel_spacing.as_ref(), "PixelSpacing"),
            (self.imager_pixel_spacing.as_ref(), "ImagerPixelSpacing"),
            (
                self.nominal_scanned_pixel_spacing.as_ref(),
                "NominalScannedPixelSpacing",
            ),
        ]
        .into_iter()
        .find_map(|(pair, source)| {
            let pair = pair?;
            if pair.row_spacing_mm <= 0.0 || pair.column_spacing_mm <= 0.0 {
                return None;
            }

            Some(MeasurementScale {
                row_spacing_mm: pair.row_spacing_mm,
                column_spacing_mm: pair.column_spacing_mm,
                source: source.to_string(),
            })
        })
    }
}

#[derive(Debug, Clone, PartialEq)]
pub struct RenderedPreview {
    pub width: u32,
    pub height: u32,
    pub pixels: Vec<u8>,
    pub measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct PreservedElement {
    pub tag_group: u16,
    pub tag_element: u16,
    pub vr: String,
    pub values: Vec<String>,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct SecondaryCaptureOptions {
    pub measurement_scale: Option<MeasurementScale>,
    pub study_instance_uid: Option<String>,
    pub preserved_elements: Vec<PreservedElement>,
}

impl SecondaryCaptureOptions {
    pub fn new(measurement_scale: Option<&MeasurementScale>) -> Self {
        Self {
            measurement_scale: measurement_scale.cloned(),
            study_instance_uid: None,
            preserved_elements: Vec::new(),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RenderWindowMode {
    Default,
    FullRange,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
struct Tag {
    group: u16,
    element: u16,
}

impl Tag {
    const fn new(group: u16, element: u16) -> Self {
        Self { group, element }
    }
}

impl std::fmt::Display for Tag {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(formatter, "({:04x},{:04x})", self.group, self.element)
    }
}

#[derive(Debug, Clone, Copy)]
enum ByteOrder {
    Little,
    Big,
}

impl ByteOrder {
    fn read_u16(self, value: &[u8]) -> u16 {
        match self {
            Self::Little => u16::from_le_bytes([value[0], value[1]]),
            Self::Big => u16::from_be_bytes([value[0], value[1]]),
        }
    }

    fn read_u32(self, value: &[u8]) -> u32 {
        match self {
            Self::Little => u32::from_le_bytes([value[0], value[1], value[2], value[3]]),
            Self::Big => u32::from_be_bytes([value[0], value[1], value[2], value[3]]),
        }
    }
}

#[derive(Debug, Clone, Copy)]
struct TransferSyntax {
    byte_order: ByteOrder,
    explicit: bool,
}

const FILE_META_TRANSFER_SYNTAX: TransferSyntax = TransferSyntax {
    byte_order: ByteOrder::Little,
    explicit: true,
};

#[derive(Debug, Clone, PartialEq, Eq)]
struct ElementHeader {
    tag: Tag,
    vr: String,
    length: u32,
}

#[derive(Debug, Clone, PartialEq, Eq)]
enum PixelData {
    Native(Vec<u8>),
    Encapsulated(Vec<u8>),
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct EncodedElement {
    tag: Tag,
    vr: String,
    value: Vec<u8>,
}

pub fn read_file(path: &str) -> Result<Metadata, String> {
    let bytes = fs::read(path).map_err(|error| format!("open source file: {error}"))?;
    match read(&bytes) {
        Ok(metadata) => Ok(metadata),
        Err(error) if supports_standalone_image_path(Path::new(path)) => {
            read_standalone_image_metadata(Path::new(path), &bytes)
                .map_err(|image_error| {
                    format!(
                        "read source metadata from {path}: {error}; fallback image decode failed: {image_error}"
                    )
                })
        }
        Err(error) => Err(format!("read source metadata from {path}: {error}")),
    }
}

pub fn read(bytes: &[u8]) -> Result<Metadata, String> {
    let (metadata, _) = read_internal(bytes, false)?;
    Ok(metadata)
}

pub fn render_grayscale_preview_file(path: impl AsRef<Path>) -> Result<RenderedPreview, String> {
    render_grayscale_preview_file_with_window_mode(path, RenderWindowMode::Default)
}

pub fn render_grayscale_preview_file_with_window_mode(
    path: impl AsRef<Path>,
    window_mode: RenderWindowMode,
) -> Result<RenderedPreview, String> {
    render_grayscale_preview_file_inner(path, window_mode, false)
}

pub fn render_grayscale_preview_file_for_tooth_analysis(
    path: impl AsRef<Path>,
) -> Result<RenderedPreview, String> {
    render_grayscale_preview_file_inner(path, RenderWindowMode::Default, true)
}

fn render_grayscale_preview_file_inner(
    path: impl AsRef<Path>,
    window_mode: RenderWindowMode,
    preserve_standalone_eight_bit_range: bool,
) -> Result<RenderedPreview, String> {
    let path = path.as_ref();
    let bytes = fs::read(path).map_err(|error| format!("open source file: {error}"))?;
    match render_grayscale_preview_with_window_mode(&bytes, window_mode) {
        Ok(preview) => Ok(preview),
        Err(error) if supports_standalone_image_path(path) => {
            render_standalone_image_preview_with_options(
                path,
                &bytes,
                preserve_standalone_eight_bit_range,
            )
            .map_err(|image_error| {
                format!(
                    "decode source study from {}: {error}; fallback image decode failed: {image_error}",
                    path.display()
                )
            })
        }
        Err(error) => Err(format!(
            "decode source study from {}: {error}",
            path.display()
        )),
    }
}

pub fn render_grayscale_preview(bytes: &[u8]) -> Result<RenderedPreview, String> {
    render_grayscale_preview_with_window_mode(bytes, RenderWindowMode::Default)
}

pub fn render_grayscale_preview_with_window_mode(
    bytes: &[u8],
    window_mode: RenderWindowMode,
) -> Result<RenderedPreview, String> {
    let (metadata, pixel_data) = read_internal(bytes, true)?;
    let pixel_data = pixel_data
        .ok_or_else(|| "invalid DICOM source: missing PixelData (7fe0,0010)".to_string())?;
    match pixel_data {
        PixelData::Native(pixel_data) => {
            let pixels = render_native_grayscale_pixels(&metadata, &pixel_data, window_mode)?;
            Ok(RenderedPreview {
                width: u32::from(metadata.columns),
                height: u32::from(metadata.rows),
                pixels,
                measurement_scale: metadata.measurement_scale(),
            })
        }
        PixelData::Encapsulated(pixel_data) => {
            render_encapsulated_compressed_preview(&metadata, &pixel_data, window_mode)
        }
    }
}

pub fn write_secondary_capture_file(
    path: impl AsRef<Path>,
    preview: &PreviewImage,
    measurement_scale: Option<&MeasurementScale>,
) -> Result<(), String> {
    let bytes = encode_secondary_capture(preview, measurement_scale)?;
    fs::write(path.as_ref(), bytes)
        .map_err(|error| format!("write processed DICOM {}: {error}", path.as_ref().display()))
}

pub fn write_secondary_capture_file_with_options(
    path: impl AsRef<Path>,
    preview: &PreviewImage,
    options: &SecondaryCaptureOptions,
) -> Result<(), String> {
    let bytes = encode_secondary_capture_with_options(preview, options)?;
    fs::write(path.as_ref(), bytes)
        .map_err(|error| format!("write processed DICOM {}: {error}", path.as_ref().display()))
}

pub fn encode_secondary_capture(
    preview: &PreviewImage,
    measurement_scale: Option<&MeasurementScale>,
) -> Result<Vec<u8>, String> {
    encode_secondary_capture_with_options(preview, &SecondaryCaptureOptions::new(measurement_scale))
}

pub fn encode_secondary_capture_with_options(
    preview: &PreviewImage,
    options: &SecondaryCaptureOptions,
) -> Result<Vec<u8>, String> {
    if preview.width == 0 || preview.height == 0 {
        return Err("secondary capture dimensions must be non-zero".to_string());
    }
    if preview.width > u16::MAX as u32 || preview.height > u16::MAX as u32 {
        return Err(format!(
            "secondary capture dimensions exceed DICOM US limits: {}x{}",
            preview.width, preview.height
        ));
    }

    let (samples_per_pixel, photometric, pixel_data) = match preview.format {
        PreviewFormat::Gray8 => {
            let expected = preview.width as usize * preview.height as usize;
            if preview.pixels.len() != expected {
                return Err(format!(
                    "preview pixel length = {}, want {}",
                    preview.pixels.len(),
                    expected
                ));
            }
            (1_u16, "MONOCHROME2", preview.pixels.clone())
        }
        PreviewFormat::Rgba8 => {
            let expected = preview.width as usize * preview.height as usize * 4;
            if preview.pixels.len() != expected {
                return Err(format!(
                    "preview pixel length = {}, want {}",
                    preview.pixels.len(),
                    expected
                ));
            }
            let mut rgb = Vec::with_capacity(preview.width as usize * preview.height as usize * 3);
            for rgba in preview.pixels.chunks_exact(4) {
                rgb.extend_from_slice(&rgba[..3]);
            }
            (3_u16, "RGB", rgb)
        }
    };

    let sop_instance_uid = generate_secondary_capture_uid();
    let study_instance_uid = options
        .study_instance_uid
        .as_deref()
        .map(str::trim)
        .filter(|uid| !uid.is_empty())
        .map(str::to_string)
        .unwrap_or_else(generate_secondary_capture_uid);
    let series_instance_uid = generate_secondary_capture_uid();
    let now = chrono::Utc::now();
    let date_value = now.format("%Y%m%d").to_string();
    let time_value = now.format("%H%M%S").to_string();

    let mut payload = Vec::new();
    payload.extend([0_u8; PART10_PREAMBLE_LENGTH]);
    payload.extend(PART10_MAGIC);
    write_secondary_capture_file_meta(&mut payload, &sop_instance_uid);

    let mut dataset_elements = Vec::with_capacity(32 + options.preserved_elements.len());
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_IMAGE_TYPE,
        "CS",
        encode_dicom_string("DERIVED\\SECONDARY", b' '),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_INSTANCE_CREATION_DATE,
        "DA",
        encode_dicom_string(&date_value, b' '),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_INSTANCE_CREATION_TIME,
        "TM",
        encode_dicom_string(&time_value, b' '),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_SOP_CLASS_UID,
        "UI",
        encode_dicom_string(SECONDARY_CAPTURE_SOP_CLASS_UID, 0),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_SOP_INSTANCE_UID,
        "UI",
        encode_dicom_string(&sop_instance_uid, 0),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_CONTENT_DATE,
        "DA",
        encode_dicom_string(&date_value, b' '),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_CONTENT_TIME,
        "TM",
        encode_dicom_string(&time_value, b' '),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_MODALITY,
        "CS",
        encode_dicom_string("OT", b' '),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_CONVERSION_TYPE,
        "CS",
        encode_dicom_string("WSD", b' '),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_MANUFACTURER,
        "LO",
        encode_dicom_string("XRayView", b' '),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_SERIES_DESCRIPTION,
        "LO",
        encode_dicom_string("XRayView Processed", b' '),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_MANUFACTURER_MODEL_NAME,
        "LO",
        encode_dicom_string("xrayview", b' '),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_DERIVATION_DESCRIPTION,
        "ST",
        encode_dicom_string("Processed by XRayView", b' '),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_SOFTWARE_VERSIONS,
        "LO",
        encode_dicom_string("xrayview", b' '),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_STUDY_INSTANCE_UID,
        "UI",
        encode_dicom_string(&study_instance_uid, 0),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_SERIES_INSTANCE_UID,
        "UI",
        encode_dicom_string(&series_instance_uid, 0),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_SERIES_NUMBER,
        "IS",
        encode_dicom_string("999", b' '),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_INSTANCE_NUMBER,
        "IS",
        encode_dicom_string("1", b' '),
    );

    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_SAMPLES_PER_PIXEL,
        "US",
        samples_per_pixel.to_le_bytes().to_vec(),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_PHOTOMETRIC_INTERPRETATION,
        "CS",
        encode_dicom_string(photometric, b' '),
    );
    if samples_per_pixel > 1 {
        insert_explicit_le_element(
            &mut dataset_elements,
            TAG_PLANAR_CONFIGURATION,
            "US",
            0_u16.to_le_bytes().to_vec(),
        );
    }
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_ROWS,
        "US",
        (preview.height as u16).to_le_bytes().to_vec(),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_COLUMNS,
        "US",
        (preview.width as u16).to_le_bytes().to_vec(),
    );
    if let Some(scale) = options.measurement_scale.as_ref() {
        insert_explicit_le_element(
            &mut dataset_elements,
            TAG_PIXEL_SPACING,
            "DS",
            encode_dicom_string(
                &format!("{}\\{}", scale.row_spacing_mm, scale.column_spacing_mm),
                b' ',
            ),
        );
    }
    for preserved in &options.preserved_elements {
        insert_encoded_element(&mut dataset_elements, encode_preserved_element(preserved)?);
    }
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_BITS_ALLOCATED,
        "US",
        8_u16.to_le_bytes().to_vec(),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_BITS_STORED,
        "US",
        8_u16.to_le_bytes().to_vec(),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_HIGH_BIT,
        "US",
        7_u16.to_le_bytes().to_vec(),
    );
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_PIXEL_REPRESENTATION,
        "US",
        0_u16.to_le_bytes().to_vec(),
    );
    if samples_per_pixel == 1 {
        insert_explicit_le_element(
            &mut dataset_elements,
            TAG_WINDOW_CENTER,
            "DS",
            encode_dicom_string("127.5", b' '),
        );
        insert_explicit_le_element(
            &mut dataset_elements,
            TAG_WINDOW_WIDTH,
            "DS",
            encode_dicom_string("255", b' '),
        );
    }

    let mut padded_pixel_data = pixel_data;
    if padded_pixel_data.len() % 2 != 0 {
        padded_pixel_data.push(0);
    }
    insert_explicit_le_element(
        &mut dataset_elements,
        TAG_PIXEL_DATA,
        "OB",
        padded_pixel_data,
    );
    for element in dataset_elements {
        write_explicit_le_element(&mut payload, element.tag, &element.vr, &element.value);
    }
    Ok(payload)
}

fn write_secondary_capture_file_meta(payload: &mut Vec<u8>, sop_instance_uid: &str) {
    let meta_elements = [
        (TAG_FILE_META_INFORMATION_VERSION, "OB", vec![0x00, 0x01]),
        (
            TAG_MEDIA_STORAGE_SOP_CLASS_UID,
            "UI",
            encode_dicom_string(SECONDARY_CAPTURE_SOP_CLASS_UID, 0),
        ),
        (
            TAG_MEDIA_STORAGE_SOP_INSTANCE_UID,
            "UI",
            encode_dicom_string(sop_instance_uid, 0),
        ),
        (
            TAG_TRANSFER_SYNTAX_UID,
            "UI",
            encode_dicom_string(EXPLICIT_LITTLE_ENDIAN_TRANSFER_SYNTAX, 0),
        ),
        (
            TAG_IMPLEMENTATION_CLASS_UID,
            "UI",
            encode_dicom_string(IMPLEMENTATION_CLASS_UID, 0),
        ),
        (
            TAG_IMPLEMENTATION_VERSION_NAME,
            "SH",
            encode_dicom_string(IMPLEMENTATION_VERSION_NAME, b' '),
        ),
    ];
    let group_length = meta_elements
        .iter()
        .map(|(_, vr, value)| explicit_element_encoded_len(vr, value.len()))
        .sum::<usize>() as u32;

    write_explicit_le_element(
        payload,
        TAG_FILE_META_GROUP_LENGTH,
        "UL",
        &group_length.to_le_bytes(),
    );
    for (tag, vr, value) in meta_elements {
        write_explicit_le_element(payload, tag, vr, &value);
    }
}

fn insert_explicit_le_element(
    elements: &mut Vec<EncodedElement>,
    tag: Tag,
    vr: &str,
    value: Vec<u8>,
) {
    insert_encoded_element(
        elements,
        EncodedElement {
            tag,
            vr: vr.to_string(),
            value,
        },
    );
}

fn insert_encoded_element(elements: &mut Vec<EncodedElement>, element: EncodedElement) {
    let index = elements
        .binary_search_by(|candidate| candidate.tag.cmp(&element.tag))
        .unwrap_or_else(|index| index);
    if index < elements.len() && elements[index].tag == element.tag {
        elements[index] = element;
    } else {
        elements.insert(index, element);
    }
}

fn encode_preserved_element(source: &PreservedElement) -> Result<EncodedElement, String> {
    let tag = Tag::new(source.tag_group, source.tag_element);
    let vr = source.vr.trim().to_ascii_uppercase();
    if tag.group == 0x0002 {
        return Err(format!(
            "preserved element {} cannot target file meta information",
            tag
        ));
    }
    if !is_supported_string_vr(&vr) {
        return Err(format!(
            "unsupported preserved element VR {:?} for {}",
            source.vr, tag
        ));
    }

    Ok(EncodedElement {
        tag,
        vr: vr.clone(),
        value: encode_dicom_values(&vr, &source.values),
    })
}

fn encode_dicom_values(vr: &str, values: &[String]) -> Vec<u8> {
    let joined = values.join("\\");
    let padding = if vr.eq_ignore_ascii_case("UI") {
        0
    } else {
        b' '
    };
    encode_dicom_string(&joined, padding)
}

fn is_supported_string_vr(vr: &str) -> bool {
    matches!(
        vr.trim().to_ascii_uppercase().as_str(),
        "AE" | "AS"
            | "CS"
            | "DA"
            | "DS"
            | "DT"
            | "IS"
            | "LO"
            | "LT"
            | "PN"
            | "SH"
            | "ST"
            | "TM"
            | "UC"
            | "UI"
            | "UR"
            | "UT"
    )
}

fn preserved_source_vr(tag: Tag) -> Option<&'static str> {
    match tag {
        TAG_PATIENT_NAME => Some("PN"),
        TAG_PATIENT_ID => Some("LO"),
        TAG_PATIENT_BIRTH_DATE => Some("DA"),
        TAG_PATIENT_SEX => Some("CS"),
        TAG_STUDY_ID => Some("SH"),
        TAG_STUDY_DATE => Some("DA"),
        TAG_STUDY_TIME => Some("TM"),
        TAG_ACCESSION_NUMBER => Some("SH"),
        TAG_STUDY_DESCRIPTION => Some("LO"),
        TAG_REFERRING_PHYSICIAN_NAME => Some("PN"),
        TAG_INSTITUTION_NAME => Some("LO"),
        TAG_PIXEL_SPACING => Some("DS"),
        TAG_IMAGER_PIXEL_SPACING => Some("DS"),
        TAG_NOMINAL_SCANNED_PIXEL_SPACING => Some("DS"),
        TAG_PIXEL_SPACING_CALIBRATION_TYPE => Some("CS"),
        TAG_PIXEL_SPACING_CALIBRATION_DESCRIPTION => Some("LO"),
        _ => None,
    }
}

fn preserved_source_order(tag: Tag) -> usize {
    const PRESERVED_SOURCE_TAG_ORDER: [Tag; 16] = [
        TAG_PATIENT_NAME,
        TAG_PATIENT_ID,
        TAG_PATIENT_BIRTH_DATE,
        TAG_PATIENT_SEX,
        TAG_STUDY_ID,
        TAG_STUDY_DATE,
        TAG_STUDY_TIME,
        TAG_ACCESSION_NUMBER,
        TAG_STUDY_DESCRIPTION,
        TAG_REFERRING_PHYSICIAN_NAME,
        TAG_INSTITUTION_NAME,
        TAG_PIXEL_SPACING,
        TAG_IMAGER_PIXEL_SPACING,
        TAG_NOMINAL_SCANNED_PIXEL_SPACING,
        TAG_PIXEL_SPACING_CALIBRATION_TYPE,
        TAG_PIXEL_SPACING_CALIBRATION_DESCRIPTION,
    ];

    PRESERVED_SOURCE_TAG_ORDER
        .iter()
        .position(|candidate| *candidate == tag)
        .unwrap_or(usize::MAX)
}

fn upsert_preserved_element(elements: &mut Vec<PreservedElement>, element: PreservedElement) {
    if let Some(existing) = elements.iter_mut().find(|existing| {
        existing.tag_group == element.tag_group && existing.tag_element == element.tag_element
    }) {
        *existing = element;
    } else {
        elements.push(element);
    }
}

fn sort_preserved_elements(elements: &mut [PreservedElement]) {
    elements.sort_by(|left, right| {
        let left_tag = Tag::new(left.tag_group, left.tag_element);
        let right_tag = Tag::new(right.tag_group, right.tag_element);
        preserved_source_order(left_tag)
            .cmp(&preserved_source_order(right_tag))
            .then_with(|| left_tag.cmp(&right_tag))
    });
}

fn generate_secondary_capture_uid() -> String {
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_nanos())
        .unwrap_or_default();
    let counter = u128::from(SECONDARY_CAPTURE_UID_COUNTER.fetch_add(1, Ordering::Relaxed));
    let pid = u128::from(std::process::id());
    format!(
        "2.25.{}",
        nanos
            .wrapping_mul(1_000_003)
            .wrapping_add(pid << 16)
            .wrapping_add(counter)
    )
}

fn explicit_element_encoded_len(vr: &str, value_len: usize) -> usize {
    if uses_32_bit_length(vr) {
        12 + value_len
    } else {
        8 + value_len
    }
}

fn read_internal(
    bytes: &[u8],
    capture_pixel_data: bool,
) -> Result<(Metadata, Option<PixelData>), String> {
    let mut source = Cursor::new(bytes);
    let transfer_syntax_uid = load_transfer_syntax_uid(&mut source)?;
    let syntax = syntax_from_uid(&transfer_syntax_uid)?;
    let mut metadata = Metadata {
        transfer_syntax_uid,
        ..Metadata::default()
    };

    let pixel_data = parse_dataset(&mut source, syntax, &mut metadata, capture_pixel_data)?;
    apply_decode_defaults(&mut metadata);
    sort_preserved_elements(&mut metadata.preserved_elements);
    if metadata.rows == 0 {
        return Err("invalid DICOM metadata: missing Rows (0028,0010)".to_string());
    }
    if metadata.columns == 0 {
        return Err("invalid DICOM metadata: missing Columns (0028,0011)".to_string());
    }

    Ok((metadata, pixel_data))
}

fn load_transfer_syntax_uid(source: &mut Cursor<&[u8]>) -> Result<String, String> {
    if !has_part10_magic(source.get_ref()) {
        source
            .seek(SeekFrom::Start(0))
            .map_err(|error| format!("seek raw DICOM dataset: {error}"))?;
        return Ok(IMPLICIT_LITTLE_ENDIAN_TRANSFER_SYNTAX.to_string());
    }

    source
        .seek(SeekFrom::Start(
            (PART10_PREAMBLE_LENGTH + PART10_MAGIC.len()) as u64,
        ))
        .map_err(|error| format!("seek file meta: {error}"))?;

    let mut transfer_syntax_uid = String::new();
    loop {
        let Some(next_group) = peek_group(source, ByteOrder::Little)? else {
            break;
        };
        if next_group != 0x0002 {
            break;
        }

        let header = read_element_header(source, FILE_META_TRANSFER_SYNTAX)
            .map_err(|error| format!("read file meta element: {error}"))?;
        if header.length == UNDEFINED_LENGTH {
            return Err(format!(
                "invalid DICOM file meta: undefined length on {}",
                header.tag
            ));
        }

        let value = read_value(source, header.length)
            .map_err(|error| format!("read file meta value for {}: {error}", header.tag))?;
        if header.tag == TAG_TRANSFER_SYNTAX_UID {
            transfer_syntax_uid = trim_string_value(&value);
        }
    }

    if transfer_syntax_uid.is_empty() {
        return Err("invalid DICOM file meta: missing TransferSyntaxUID (0002,0010)".to_string());
    }
    Ok(transfer_syntax_uid)
}

fn parse_dataset(
    source: &mut Cursor<&[u8]>,
    syntax: TransferSyntax,
    metadata: &mut Metadata,
    capture_pixel_data: bool,
) -> Result<Option<PixelData>, String> {
    loop {
        let header = match read_element_header(source, syntax) {
            Ok(header) => header,
            Err(error) if error.kind() == io::ErrorKind::UnexpectedEof => return Ok(None),
            Err(error) => return Err(format!("read dataset element: {error}")),
        };

        if header.tag == TAG_PIXEL_DATA {
            metadata.pixel_data_encoding = pixel_data_encoding_for_header(&header).to_string();
            if header.length == UNDEFINED_LENGTH {
                if capture_pixel_data {
                    let value =
                        read_encapsulated_pixel_data(source, syntax, metadata.number_of_frames)?;
                    return Ok(Some(PixelData::Encapsulated(value)));
                }
                return Ok(None);
            }
            if capture_pixel_data {
                let value = read_value(source, header.length)
                    .map_err(|error| format!("read value for {}: {error}", header.tag))?;
                return Ok(Some(PixelData::Native(value)));
            }
            return Ok(None);
        }

        if header.length == UNDEFINED_LENGTH {
            skip_undefined_value(source, syntax)
                .map_err(|error| format!("skip undefined-length {}: {error}", header.tag))?;
            continue;
        }

        if !is_tracked_tag(header.tag) {
            source
                .seek(SeekFrom::Current(i64::from(header.length)))
                .map_err(|error| format!("skip {} payload: {error}", header.tag))?;
            continue;
        }

        let value = read_value(source, header.length)
            .map_err(|error| format!("read value for {}: {error}", header.tag))?;
        apply_value(metadata, syntax, header, &value);
    }
}

fn read_encapsulated_pixel_data(
    source: &mut Cursor<&[u8]>,
    syntax: TransferSyntax,
    number_of_frames: u32,
) -> Result<Vec<u8>, String> {
    if number_of_frames > 1 {
        return Err(format!(
            "unsupported multi-frame encapsulated source decode: {number_of_frames} frames"
        ));
    }

    let basic_offset_table = read_element_header(source, syntax)
        .map_err(|error| format!("read encapsulated basic offset table: {error}"))?;
    if basic_offset_table.tag != TAG_ITEM {
        return Err(format!(
            "invalid encapsulated pixel data: expected item header, found {}",
            basic_offset_table.tag
        ));
    }
    if basic_offset_table.length == UNDEFINED_LENGTH {
        return Err(
            "invalid encapsulated pixel data: undefined basic offset table length".to_string(),
        );
    }
    source
        .seek(SeekFrom::Current(i64::from(basic_offset_table.length)))
        .map_err(|error| format!("skip encapsulated basic offset table: {error}"))?;

    let mut payload = Vec::new();
    loop {
        let header = read_element_header(source, syntax)
            .map_err(|error| format!("read encapsulated pixel data item: {error}"))?;
        match header.tag {
            TAG_ITEM => {
                if header.length == UNDEFINED_LENGTH {
                    return Err(
                        "invalid encapsulated pixel data: undefined fragment length".to_string()
                    );
                }
                let fragment = read_value(source, header.length)
                    .map_err(|error| format!("read encapsulated pixel data fragment: {error}"))?;
                payload.extend(fragment);
            }
            TAG_SEQUENCE_DELIMITATION => {
                if header.length > 0 {
                    source
                        .seek(SeekFrom::Current(i64::from(header.length)))
                        .map_err(|error| {
                            format!("skip encapsulated sequence delimiter payload: {error}")
                        })?;
                }
                if payload.is_empty() {
                    return Err(
                        "encapsulated pixel data did not contain any frame bytes".to_string()
                    );
                }
                return Ok(payload);
            }
            other => {
                return Err(format!(
                    "invalid encapsulated pixel data: expected item or sequence delimiter, found {other}"
                ));
            }
        }
    }
}

fn apply_value(
    metadata: &mut Metadata,
    syntax: TransferSyntax,
    header: ElementHeader,
    value: &[u8],
) {
    match header.tag {
        TAG_SAMPLES_PER_PIXEL => {
            if let Some(parsed) = parse_u16_value(value, syntax.byte_order) {
                metadata.samples_per_pixel = parsed;
            }
        }
        TAG_PHOTOMETRIC_INTERPRETATION => {
            metadata.photometric_interpretation = trim_string_value(value);
        }
        TAG_STUDY_INSTANCE_UID => {
            metadata.study_instance_uid = trim_string_value(value);
        }
        TAG_NUMBER_OF_FRAMES => {
            if let Some(parsed) = parse_u32_value(value, syntax.byte_order) {
                metadata.number_of_frames = parsed;
            }
        }
        TAG_ROWS => {
            if let Some(parsed) = parse_u16_value(value, syntax.byte_order) {
                metadata.rows = parsed;
            }
        }
        TAG_COLUMNS => {
            if let Some(parsed) = parse_u16_value(value, syntax.byte_order) {
                metadata.columns = parsed;
            }
        }
        TAG_PIXEL_SPACING => metadata.pixel_spacing = parse_spacing_pair(value),
        TAG_BITS_ALLOCATED => {
            if let Some(parsed) = parse_u16_value(value, syntax.byte_order) {
                metadata.bits_allocated = parsed;
            }
        }
        TAG_BITS_STORED => {
            if let Some(parsed) = parse_u16_value(value, syntax.byte_order) {
                metadata.bits_stored = parsed;
            }
        }
        TAG_PIXEL_REPRESENTATION => {
            if let Some(parsed) = parse_u16_value(value, syntax.byte_order) {
                metadata.pixel_representation = parsed;
            }
        }
        TAG_PLANAR_CONFIGURATION => {
            if let Some(parsed) = parse_u16_value(value, syntax.byte_order) {
                metadata.planar_configuration = parsed;
            }
        }
        TAG_IMAGER_PIXEL_SPACING => metadata.imager_pixel_spacing = parse_spacing_pair(value),
        TAG_NOMINAL_SCANNED_PIXEL_SPACING => {
            metadata.nominal_scanned_pixel_spacing = parse_spacing_pair(value);
        }
        TAG_WINDOW_CENTER => metadata.window_center = parse_float_value(value),
        TAG_WINDOW_WIDTH => metadata.window_width = parse_float_value(value),
        TAG_RESCALE_INTERCEPT => metadata.rescale_intercept = parse_float_value(value),
        TAG_RESCALE_SLOPE => metadata.rescale_slope = parse_float_value(value),
        _ => {}
    }

    if let Some(default_vr) = preserved_source_vr(header.tag) {
        let vr = if header.vr.trim().is_empty() {
            default_vr
        } else {
            header.vr.trim()
        };
        upsert_preserved_element(
            &mut metadata.preserved_elements,
            PreservedElement {
                tag_group: header.tag.group,
                tag_element: header.tag.element,
                vr: vr.to_ascii_uppercase(),
                values: parse_string_values(value),
            },
        );
    }
}

fn apply_decode_defaults(metadata: &mut Metadata) {
    if metadata.samples_per_pixel == 0 {
        metadata.samples_per_pixel = 1;
    }
    if metadata.number_of_frames == 0 {
        metadata.number_of_frames = 1;
    }
    if metadata.bits_allocated == 0 {
        metadata.bits_allocated = 8;
    }
    if metadata.bits_stored == 0 {
        metadata.bits_stored = metadata.bits_allocated;
    }
    if metadata.pixel_data_encoding.is_empty() {
        metadata.pixel_data_encoding = PIXEL_DATA_ENCODING_MISSING.to_string();
    }
}

fn supports_standalone_image_path(path: &Path) -> bool {
    matches!(
        path.extension()
            .and_then(|extension| extension.to_str())
            .map(str::to_ascii_lowercase)
            .as_deref(),
        Some("bmp" | "tif" | "tiff")
    )
}

fn read_standalone_image_metadata(path: &Path, bytes: &[u8]) -> Result<Metadata, String> {
    let image = decode_standalone_image(path, bytes)
        .map_err(|error| format!("decode {}: {error}", path.display()))?;
    let mut metadata = Metadata {
        rows: image.height as u16,
        columns: image.width as u16,
        samples_per_pixel: image.samples_per_pixel,
        bits_allocated: image.bits_allocated,
        bits_stored: image.bits_allocated,
        pixel_representation: 0,
        number_of_frames: 1,
        pixel_data_encoding: PIXEL_DATA_ENCODING_NATIVE.to_string(),
        photometric_interpretation: image.photometric_interpretation,
        ..Metadata::default()
    };
    apply_decode_defaults(&mut metadata);
    Ok(metadata)
}

fn render_standalone_image_preview_with_options(
    path: &Path,
    bytes: &[u8],
    preserve_eight_bit_range: bool,
) -> Result<RenderedPreview, String> {
    let image = decode_standalone_image(path, bytes)
        .map_err(|error| format!("decode {}: {error}", path.display()))?;
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

struct DecodedStandaloneImage {
    width: u32,
    height: u32,
    samples_per_pixel: u16,
    bits_allocated: u16,
    photometric_interpretation: String,
    pixels: Vec<f32>,
}

fn decode_standalone_image(path: &Path, bytes: &[u8]) -> Result<DecodedStandaloneImage, String> {
    match path
        .extension()
        .and_then(|extension| extension.to_str())
        .map(str::to_ascii_lowercase)
        .as_deref()
    {
        Some("bmp") => decode_bmp(bytes),
        Some("tif" | "tiff") => decode_tiff(bytes),
        _ => Err("unsupported standalone image extension".to_string()),
    }
}

fn decode_bmp(bytes: &[u8]) -> Result<DecodedStandaloneImage, String> {
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
    Ok(DecodedStandaloneImage {
        width: width as u32,
        height: height as u32,
        samples_per_pixel,
        bits_allocated: 8,
        photometric_interpretation: photometric_interpretation.to_string(),
        pixels,
    })
}

fn decode_tiff(bytes: &[u8]) -> Result<DecodedStandaloneImage, String> {
    if bytes.len() < 8 {
        return Err("truncated TIFF header".to_string());
    }
    let byte_order = match &bytes[..2] {
        b"II" => ByteOrder::Little,
        b"MM" => ByteOrder::Big,
        _ => return Err("unsupported TIFF byte order marker".to_string()),
    };
    if byte_order.read_u16(&bytes[2..4]) != 42 {
        return Err("unsupported TIFF magic".to_string());
    }
    let ifd_offset = byte_order.read_u32(&bytes[4..8]) as usize;
    let entries = read_tiff_ifd(bytes, byte_order, ifd_offset)?;

    let width = tiff_required_u32(&entries, bytes, byte_order, 256, "ImageWidth")?;
    let height = tiff_required_u32(&entries, bytes, byte_order, 257, "ImageLength")?;
    if width == 0 || height == 0 {
        return Err(format!("invalid TIFF dimensions: {width}x{height}"));
    }
    if width > u16::MAX as u32 || height > u16::MAX as u32 {
        return Err(format!(
            "TIFF dimensions exceed supported range: {width}x{height}"
        ));
    }

    let compression = tiff_optional_u32(&entries, bytes, byte_order, 259)?.unwrap_or(1);
    if compression != 1 {
        return Err(format!("unsupported TIFF compression: {compression}"));
    }
    let photometric = tiff_required_u32(
        &entries,
        bytes,
        byte_order,
        262,
        "PhotometricInterpretation",
    )?;
    let samples_per_pixel = tiff_optional_u32(&entries, bytes, byte_order, 277)?.unwrap_or(1);
    if !matches!(samples_per_pixel, 1 | 3) {
        return Err(format!(
            "unsupported TIFF SamplesPerPixel: {samples_per_pixel}"
        ));
    }
    if samples_per_pixel == 3 && photometric != 2 {
        return Err(format!(
            "unsupported TIFF RGB photometric interpretation: {photometric}"
        ));
    }
    if samples_per_pixel == 1 && !matches!(photometric, 0 | 1) {
        return Err(format!(
            "unsupported TIFF grayscale photometric interpretation: {photometric}"
        ));
    }
    let planar_config = tiff_optional_u32(&entries, bytes, byte_order, 284)?.unwrap_or(1);
    if planar_config != 1 {
        return Err(format!(
            "unsupported TIFF planar configuration: {planar_config}"
        ));
    }

    let bits_per_sample =
        tiff_required_u32_values(&entries, bytes, byte_order, 258, "BitsPerSample")?;
    if bits_per_sample.len() != samples_per_pixel as usize
        && !(samples_per_pixel == 1 && bits_per_sample.len() == 1)
    {
        return Err(format!(
            "TIFF BitsPerSample count = {}, want {samples_per_pixel}",
            bits_per_sample.len()
        ));
    }
    if bits_per_sample.iter().any(|value| *value != 8) {
        return Err(format!(
            "unsupported TIFF BitsPerSample values: {bits_per_sample:?}"
        ));
    }

    let strip_offsets = tiff_required_u32_values(&entries, bytes, byte_order, 273, "StripOffsets")?;
    let strip_byte_counts =
        tiff_required_u32_values(&entries, bytes, byte_order, 279, "StripByteCounts")?;
    if strip_offsets.len() != strip_byte_counts.len() {
        return Err(format!(
            "TIFF StripOffsets count {} does not match StripByteCounts count {}",
            strip_offsets.len(),
            strip_byte_counts.len()
        ));
    }

    let mut pixel_data = Vec::new();
    for (offset, byte_count) in strip_offsets.iter().zip(strip_byte_counts.iter()) {
        let offset = *offset as usize;
        let byte_count = *byte_count as usize;
        let end = offset
            .checked_add(byte_count)
            .ok_or_else(|| "TIFF strip range overflow".to_string())?;
        let strip = bytes
            .get(offset..end)
            .ok_or_else(|| format!("truncated TIFF strip at byte offset {offset}"))?;
        pixel_data.extend_from_slice(strip);
    }

    let pixel_count = width as usize * height as usize;
    let expected = pixel_count * samples_per_pixel as usize;
    if pixel_data.len() < expected {
        return Err(format!(
            "TIFF pixel data length = {}, want at least {expected}",
            pixel_data.len()
        ));
    }

    let mut pixels = Vec::with_capacity(pixel_count);
    if samples_per_pixel == 1 {
        let white_is_zero = photometric == 0;
        for value in &pixel_data[..pixel_count] {
            let value = if white_is_zero { 255 - *value } else { *value };
            pixels.push(f32::from(value));
        }
    } else {
        for chunk in pixel_data[..expected].chunks_exact(3) {
            pixels.push(f32::from(gray_from_rgb8(chunk[0], chunk[1], chunk[2])));
        }
    }

    Ok(DecodedStandaloneImage {
        width,
        height,
        samples_per_pixel: samples_per_pixel as u16,
        bits_allocated: 8,
        photometric_interpretation: if samples_per_pixel == 1 {
            "MONOCHROME2".to_string()
        } else {
            "RGB".to_string()
        },
        pixels,
    })
}

#[derive(Debug, Clone)]
struct TiffEntry {
    tag: u16,
    type_id: u16,
    count: u32,
    value_or_offset: u32,
    inline_value: [u8; 4],
}

fn read_tiff_ifd(
    bytes: &[u8],
    byte_order: ByteOrder,
    offset: usize,
) -> Result<Vec<TiffEntry>, String> {
    let entry_count = read_ordered_u16_at(bytes, byte_order, offset)? as usize;
    let entries_start = offset + 2;
    let entries_len = entry_count
        .checked_mul(12)
        .ok_or_else(|| "TIFF IFD entry size overflow".to_string())?;
    if bytes.len() < entries_start + entries_len {
        return Err("truncated TIFF IFD".to_string());
    }

    let mut entries = Vec::with_capacity(entry_count);
    for index in 0..entry_count {
        let entry_offset = entries_start + index * 12;
        let inline_value = [
            bytes[entry_offset + 8],
            bytes[entry_offset + 9],
            bytes[entry_offset + 10],
            bytes[entry_offset + 11],
        ];
        entries.push(TiffEntry {
            tag: read_ordered_u16_at(bytes, byte_order, entry_offset)?,
            type_id: read_ordered_u16_at(bytes, byte_order, entry_offset + 2)?,
            count: read_ordered_u32_at(bytes, byte_order, entry_offset + 4)?,
            value_or_offset: read_ordered_u32_at(bytes, byte_order, entry_offset + 8)?,
            inline_value,
        });
    }
    Ok(entries)
}

fn tiff_required_u32(
    entries: &[TiffEntry],
    bytes: &[u8],
    byte_order: ByteOrder,
    tag: u16,
    name: &str,
) -> Result<u32, String> {
    tiff_required_u32_values(entries, bytes, byte_order, tag, name)?
        .into_iter()
        .next()
        .ok_or_else(|| format!("missing TIFF tag {name} ({tag})"))
}

fn tiff_optional_u32(
    entries: &[TiffEntry],
    bytes: &[u8],
    byte_order: ByteOrder,
    tag: u16,
) -> Result<Option<u32>, String> {
    let Some(entry) = entries.iter().find(|entry| entry.tag == tag) else {
        return Ok(None);
    };
    Ok(tiff_entry_u32_values(entry, bytes, byte_order)?
        .into_iter()
        .next())
}

fn tiff_required_u32_values(
    entries: &[TiffEntry],
    bytes: &[u8],
    byte_order: ByteOrder,
    tag: u16,
    name: &str,
) -> Result<Vec<u32>, String> {
    let entry = entries
        .iter()
        .find(|entry| entry.tag == tag)
        .ok_or_else(|| format!("missing TIFF tag {name} ({tag})"))?;
    let values = tiff_entry_u32_values(entry, bytes, byte_order)?;
    if values.is_empty() {
        return Err(format!("empty TIFF tag {name} ({tag})"));
    }
    Ok(values)
}

fn tiff_entry_u32_values(
    entry: &TiffEntry,
    bytes: &[u8],
    byte_order: ByteOrder,
) -> Result<Vec<u32>, String> {
    let raw = tiff_entry_value_bytes(entry, bytes)?;
    match entry.type_id {
        1 => Ok(raw.into_iter().map(u32::from).collect()),
        3 => {
            let mut values = Vec::with_capacity(entry.count as usize);
            for chunk in raw.chunks_exact(2) {
                values.push(u32::from(byte_order.read_u16(chunk)));
            }
            Ok(values)
        }
        4 => {
            let mut values = Vec::with_capacity(entry.count as usize);
            for chunk in raw.chunks_exact(4) {
                values.push(byte_order.read_u32(chunk));
            }
            Ok(values)
        }
        other => Err(format!(
            "unsupported TIFF tag {} value type: {other}",
            entry.tag
        )),
    }
}

fn tiff_entry_value_bytes(entry: &TiffEntry, bytes: &[u8]) -> Result<Vec<u8>, String> {
    let unit_size = match entry.type_id {
        1 | 2 => 1_usize,
        3 => 2,
        4 => 4,
        other => {
            return Err(format!(
                "unsupported TIFF tag {} value type: {other}",
                entry.tag
            ));
        }
    };
    let byte_count = unit_size
        .checked_mul(entry.count as usize)
        .ok_or_else(|| format!("TIFF tag {} value size overflow", entry.tag))?;
    if byte_count <= 4 {
        return Ok(entry.inline_value[..byte_count].to_vec());
    }

    let offset = entry.value_or_offset as usize;
    let end = offset
        .checked_add(byte_count)
        .ok_or_else(|| format!("TIFF tag {} value range overflow", entry.tag))?;
    bytes
        .get(offset..end)
        .map(|value| value.to_vec())
        .ok_or_else(|| format!("truncated TIFF tag {} value", entry.tag))
}

fn render_native_grayscale_pixels(
    metadata: &Metadata,
    pixel_data: &[u8],
    window_mode: RenderWindowMode,
) -> Result<Vec<u8>, String> {
    let pixel_count = metadata.rows as usize * metadata.columns as usize;
    if pixel_count == 0 {
        return Err("source image dimensions must be non-zero".to_string());
    }

    let (values, apply_rescale) = match (metadata.samples_per_pixel, metadata.bits_allocated) {
        (1, 8) => (
            parse_8_bit_pixels(pixel_data, pixel_count, metadata.pixel_representation != 0)?,
            true,
        ),
        (1, 16) => (
            parse_16_bit_pixels(
                pixel_data,
                pixel_count,
                metadata.transfer_syntax()?,
                metadata.bits_stored,
                metadata.pixel_representation != 0,
            )?,
            true,
        ),
        (1, 32) => (
            parse_32_bit_pixels(
                pixel_data,
                pixel_count,
                metadata.transfer_syntax()?,
                metadata.bits_stored,
                metadata.pixel_representation != 0,
            )?,
            true,
        ),
        (3, 8) => (
            parse_8_bit_rgb_pixels(
                pixel_data,
                pixel_count,
                metadata.planar_configuration,
                &metadata.photometric_interpretation,
            )?,
            false,
        ),
        (3, 16) => {
            return Err("16-bit color DICOM source decode is not supported yet".to_string());
        }
        (1, bits) => {
            return Err(format!(
                "unsupported BitsAllocated for Rust render path: {bits}"
            ));
        }
        (3, bits) => {
            return Err(format!(
                "{bits}-bit color DICOM source decode is not supported yet"
            ));
        }
        (samples_per_pixel, _) => {
            return Err(format!(
                "unsupported SamplesPerPixel for Rust render path: {samples_per_pixel}"
            ));
        }
    };

    render_grayscale_values(metadata, values, apply_rescale, window_mode)
}

fn render_encapsulated_compressed_preview(
    metadata: &Metadata,
    pixel_data: &[u8],
    window_mode: RenderWindowMode,
) -> Result<RenderedPreview, String> {
    let decoded = image::load_from_memory(pixel_data).map_err(|error| {
        format!(
            "decode encapsulated pixel data for transfer syntax {}: {error}",
            metadata.transfer_syntax_uid
        )
    })?;
    if decoded.width() == 0 || decoded.height() == 0 {
        return Err(format!(
            "invalid encapsulated image dimensions: {}x{}",
            decoded.width(),
            decoded.height()
        ));
    }

    let gray = decoded.to_luma8();
    let values = gray
        .as_raw()
        .iter()
        .map(|value| f32::from(*value))
        .collect::<Vec<_>>();
    let pixels = render_grayscale_values(metadata, values, false, window_mode)?;
    Ok(RenderedPreview {
        width: decoded.width(),
        height: decoded.height(),
        pixels,
        measurement_scale: metadata.measurement_scale(),
    })
}

fn render_grayscale_values(
    metadata: &Metadata,
    mut values: Vec<f32>,
    apply_rescale: bool,
    window_mode: RenderWindowMode,
) -> Result<Vec<u8>, String> {
    if values.is_empty() {
        return Err("source image dimensions must be non-zero".to_string());
    }
    if apply_rescale {
        apply_modality_rescale(&mut values, metadata);
    }

    let mut min = values[0];
    let mut max = values[0];
    for value in &values[1..] {
        min = min.min(*value);
        max = max.max(*value);
    }

    let invert = metadata
        .photometric_interpretation
        .eq_ignore_ascii_case("MONOCHROME1");
    let window = resolve_window_transform(metadata, window_mode);
    let mut output = Vec::with_capacity(values.len());
    for value in values {
        let mut mapped = match window {
            Some(window) => window.map(value),
            None => map_linear(value, min, max),
        };
        if invert {
            mapped = 255 - mapped;
        }
        output.push(mapped);
    }
    Ok(output)
}

fn parse_8_bit_pixels(
    pixel_data: &[u8],
    pixel_count: usize,
    signed: bool,
) -> Result<Vec<f32>, String> {
    if pixel_data.len() < pixel_count {
        return Err(format!(
            "PixelData length = {}, want at least {}",
            pixel_data.len(),
            pixel_count
        ));
    }

    Ok(pixel_data[..pixel_count]
        .iter()
        .map(|value| {
            if signed {
                f32::from(i8::from_ne_bytes([*value]))
            } else {
                f32::from(*value)
            }
        })
        .collect())
}

fn parse_8_bit_rgb_pixels(
    pixel_data: &[u8],
    pixel_count: usize,
    planar_configuration: u16,
    photometric_interpretation: &str,
) -> Result<Vec<f32>, String> {
    let photometric = photometric_interpretation.trim().to_ascii_uppercase();
    if photometric != "RGB" {
        return Err(format!(
            "unsupported color photometric interpretation: {photometric}"
        ));
    }

    let expected = pixel_count * 3;
    if pixel_data.len() < expected {
        return Err(format!(
            "PixelData length = {}, want at least {}",
            pixel_data.len(),
            expected
        ));
    }

    let mut values = Vec::with_capacity(pixel_count);
    match planar_configuration {
        0 => {
            for chunk in pixel_data[..expected].chunks_exact(3) {
                values.push(f32::from(gray_from_rgb8(chunk[0], chunk[1], chunk[2])));
            }
        }
        1 => {
            let red = &pixel_data[..pixel_count];
            let green = &pixel_data[pixel_count..pixel_count * 2];
            let blue = &pixel_data[pixel_count * 2..expected];
            for index in 0..pixel_count {
                values.push(f32::from(gray_from_rgb8(
                    red[index],
                    green[index],
                    blue[index],
                )));
            }
        }
        other => return Err(format!("unsupported planar configuration: {other}")),
    }
    Ok(values)
}

fn gray_from_rgb8(red: u8, green: u8, blue: u8) -> u8 {
    let red = u32::from(red) | (u32::from(red) << 8);
    let green = u32::from(green) | (u32::from(green) << 8);
    let blue = u32::from(blue) | (u32::from(blue) << 8);
    ((19_595 * red + 38_470 * green + 7_471 * blue + (1 << 15)) >> 24) as u8
}

fn parse_16_bit_pixels(
    pixel_data: &[u8],
    pixel_count: usize,
    syntax: TransferSyntax,
    bits_stored: u16,
    signed: bool,
) -> Result<Vec<f32>, String> {
    let expected = pixel_count * 2;
    if pixel_data.len() < expected {
        return Err(format!(
            "PixelData length = {}, want at least {}",
            pixel_data.len(),
            expected
        ));
    }

    let mut values = Vec::with_capacity(pixel_count);
    for chunk in pixel_data[..expected].chunks_exact(2) {
        let raw = syntax.byte_order.read_u16(chunk);
        values.push(if signed {
            f32::from(sign_extend_stored_value(raw, bits_stored))
        } else {
            f32::from(mask_stored_value(raw, bits_stored))
        });
    }
    Ok(values)
}

fn parse_32_bit_pixels(
    pixel_data: &[u8],
    pixel_count: usize,
    syntax: TransferSyntax,
    bits_stored: u16,
    signed: bool,
) -> Result<Vec<f32>, String> {
    let expected = pixel_count * 4;
    if pixel_data.len() < expected {
        return Err(format!(
            "PixelData length = {}, want at least {}",
            pixel_data.len(),
            expected
        ));
    }

    let mut values = Vec::with_capacity(pixel_count);
    for chunk in pixel_data[..expected].chunks_exact(4) {
        let raw = syntax.byte_order.read_u32(chunk);
        values.push(if signed {
            sign_extend_stored_u32_value(raw, bits_stored) as f32
        } else {
            mask_stored_u32_value(raw, bits_stored) as f32
        });
    }
    Ok(values)
}

fn apply_modality_rescale(values: &mut [f32], metadata: &Metadata) {
    let slope = metadata.rescale_slope.unwrap_or(1.0) as f32;
    let intercept = metadata.rescale_intercept.unwrap_or(0.0) as f32;
    if slope == 1.0 && intercept == 0.0 {
        return;
    }

    for value in values {
        *value = *value * slope + intercept;
    }
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

fn resolve_window_transform(
    metadata: &Metadata,
    mode: RenderWindowMode,
) -> Option<WindowTransform> {
    match mode {
        RenderWindowMode::Default => WindowTransform::new(
            metadata.window_center? as f32,
            metadata.window_width? as f32,
        ),
        RenderWindowMode::FullRange => None,
    }
}

fn mask_stored_value(raw: u16, bits_stored: u16) -> u16 {
    if bits_stored == 0 || bits_stored >= 16 {
        raw
    } else {
        raw & ((1_u16 << bits_stored) - 1)
    }
}

fn mask_stored_u32_value(raw: u32, bits_stored: u16) -> u32 {
    if bits_stored == 0 || bits_stored >= 32 {
        raw
    } else {
        raw & ((1_u32 << bits_stored) - 1)
    }
}

fn sign_extend_stored_value(raw: u16, bits_stored: u16) -> i16 {
    let bits_stored = bits_stored.clamp(1, 16);
    let masked = i32::from(mask_stored_value(raw, bits_stored));
    let sign_bit = 1_i32 << (bits_stored - 1);
    let extended = if masked & sign_bit != 0 {
        masked | (!0_i32 << bits_stored)
    } else {
        masked
    };
    extended as i16
}

fn sign_extend_stored_u32_value(raw: u32, bits_stored: u16) -> i32 {
    let bits_stored = bits_stored.clamp(1, 32);
    let masked = mask_stored_u32_value(raw, bits_stored);
    if bits_stored == 32 {
        return masked as i32;
    }

    let shift = 32 - bits_stored;
    ((masked << shift) as i32) >> shift
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

impl Metadata {
    fn transfer_syntax(&self) -> Result<TransferSyntax, String> {
        syntax_from_uid(&self.transfer_syntax_uid)
    }
}

fn syntax_from_uid(uid: &str) -> Result<TransferSyntax, String> {
    match uid {
        IMPLICIT_LITTLE_ENDIAN_TRANSFER_SYNTAX => Ok(TransferSyntax {
            byte_order: ByteOrder::Little,
            explicit: false,
        }),
        EXPLICIT_LITTLE_ENDIAN_TRANSFER_SYNTAX => Ok(TransferSyntax {
            byte_order: ByteOrder::Little,
            explicit: true,
        }),
        EXPLICIT_BIG_ENDIAN_TRANSFER_SYNTAX => Ok(TransferSyntax {
            byte_order: ByteOrder::Big,
            explicit: true,
        }),
        DEFLATED_TRANSFER_SYNTAX => Err(format!(
            "unsupported deflated transfer syntax for metadata reader: {uid}"
        )),
        "" => Err("invalid DICOM metadata: empty transfer syntax UID".to_string()),
        _ => Ok(TransferSyntax {
            byte_order: ByteOrder::Little,
            explicit: true,
        }),
    }
}

fn has_part10_magic(bytes: &[u8]) -> bool {
    bytes.len() >= PART10_PREAMBLE_LENGTH + PART10_MAGIC.len()
        && &bytes[PART10_PREAMBLE_LENGTH..PART10_PREAMBLE_LENGTH + PART10_MAGIC.len()]
            == PART10_MAGIC
}

fn peek_group(source: &mut Cursor<&[u8]>, byte_order: ByteOrder) -> Result<Option<u16>, String> {
    let offset = source.position() as usize;
    let bytes = source.get_ref();
    if offset + 4 > bytes.len() {
        return Ok(None);
    }
    Ok(Some(byte_order.read_u16(&bytes[offset..offset + 2])))
}

fn read_element_header(
    source: &mut Cursor<&[u8]>,
    syntax: TransferSyntax,
) -> io::Result<ElementHeader> {
    let mut buf = [0_u8; 4];
    source.read_exact(&mut buf)?;
    let tag = Tag {
        group: syntax.byte_order.read_u16(&buf[..2]),
        element: syntax.byte_order.read_u16(&buf[2..]),
    };

    let mut header = ElementHeader {
        tag,
        vr: String::new(),
        length: 0,
    };

    if tag.group == 0xfffe {
        source.read_exact(&mut buf)?;
        header.length = syntax.byte_order.read_u32(&buf);
        return Ok(header);
    }

    if syntax.explicit {
        let mut vr = [0_u8; 2];
        source.read_exact(&mut vr)?;
        header.vr = String::from_utf8_lossy(&vr).to_string();

        if uses_32_bit_length(&header.vr) {
            let mut reserved = [0_u8; 2];
            source.read_exact(&mut reserved)?;
            source.read_exact(&mut buf)?;
            header.length = syntax.byte_order.read_u32(&buf);
            return Ok(header);
        }

        let mut length = [0_u8; 2];
        source.read_exact(&mut length)?;
        header.length = u32::from(syntax.byte_order.read_u16(&length));
        return Ok(header);
    }

    source.read_exact(&mut buf)?;
    header.length = syntax.byte_order.read_u32(&buf);
    Ok(header)
}

fn read_value(source: &mut Cursor<&[u8]>, length: u32) -> io::Result<Vec<u8>> {
    let mut value = vec![0; length as usize];
    if length > 0 {
        source.read_exact(&mut value)?;
    }
    Ok(value)
}

fn read_le_u16_at(bytes: &[u8], offset: usize) -> Result<u16, String> {
    let value = bytes
        .get(offset..offset + 2)
        .ok_or_else(|| format!("read little-endian u16 at byte offset {offset}"))?;
    Ok(u16::from_le_bytes([value[0], value[1]]))
}

fn read_le_u32_at(bytes: &[u8], offset: usize) -> Result<u32, String> {
    let value = bytes
        .get(offset..offset + 4)
        .ok_or_else(|| format!("read little-endian u32 at byte offset {offset}"))?;
    Ok(u32::from_le_bytes([value[0], value[1], value[2], value[3]]))
}

fn read_le_i32_at(bytes: &[u8], offset: usize) -> Result<i32, String> {
    let value = bytes
        .get(offset..offset + 4)
        .ok_or_else(|| format!("read little-endian i32 at byte offset {offset}"))?;
    Ok(i32::from_le_bytes([value[0], value[1], value[2], value[3]]))
}

fn read_ordered_u16_at(bytes: &[u8], byte_order: ByteOrder, offset: usize) -> Result<u16, String> {
    let value = bytes
        .get(offset..offset + 2)
        .ok_or_else(|| format!("read u16 at byte offset {offset}"))?;
    Ok(byte_order.read_u16(value))
}

fn read_ordered_u32_at(bytes: &[u8], byte_order: ByteOrder, offset: usize) -> Result<u32, String> {
    let value = bytes
        .get(offset..offset + 4)
        .ok_or_else(|| format!("read u32 at byte offset {offset}"))?;
    Ok(byte_order.read_u32(value))
}

fn write_explicit_le_element(payload: &mut Vec<u8>, tag: Tag, vr: &str, value: &[u8]) {
    payload.extend_from_slice(&tag.group.to_le_bytes());
    payload.extend_from_slice(&tag.element.to_le_bytes());
    payload.extend_from_slice(vr.as_bytes());
    if uses_32_bit_length(vr) {
        payload.extend_from_slice(&[0, 0]);
        payload.extend_from_slice(&(value.len() as u32).to_le_bytes());
    } else {
        payload.extend_from_slice(&(value.len() as u16).to_le_bytes());
    }
    payload.extend_from_slice(value);
}

fn encode_dicom_string(value: &str, padding: u8) -> Vec<u8> {
    let mut raw = value.as_bytes().to_vec();
    if raw.len() % 2 != 0 {
        raw.push(padding);
    }
    raw
}

fn skip_undefined_value(source: &mut Cursor<&[u8]>, syntax: TransferSyntax) -> io::Result<()> {
    let mut depth = 1;
    while depth > 0 {
        let header = read_element_header(source, syntax)?;
        match header.tag {
            TAG_ITEM_DELIMITATION | TAG_SEQUENCE_DELIMITATION => {
                if header.length > 0 {
                    source.seek(SeekFrom::Current(i64::from(header.length)))?;
                }
                depth -= 1;
            }
            _ if header.length == UNDEFINED_LENGTH => {
                depth += 1;
            }
            _ => {
                source.seek(SeekFrom::Current(i64::from(header.length)))?;
            }
        }
    }
    Ok(())
}

fn uses_32_bit_length(vr: &str) -> bool {
    matches!(
        vr,
        "OB" | "OD" | "OF" | "OL" | "OV" | "OW" | "SQ" | "UC" | "UR" | "UT" | "UN"
    )
}

fn is_tracked_tag(tag: Tag) -> bool {
    matches!(
        tag,
        TAG_SAMPLES_PER_PIXEL
            | TAG_PHOTOMETRIC_INTERPRETATION
            | TAG_STUDY_INSTANCE_UID
            | TAG_NUMBER_OF_FRAMES
            | TAG_ROWS
            | TAG_COLUMNS
            | TAG_PIXEL_SPACING
            | TAG_BITS_ALLOCATED
            | TAG_BITS_STORED
            | TAG_PIXEL_REPRESENTATION
            | TAG_PLANAR_CONFIGURATION
            | TAG_WINDOW_CENTER
            | TAG_WINDOW_WIDTH
            | TAG_RESCALE_INTERCEPT
            | TAG_RESCALE_SLOPE
            | TAG_IMAGER_PIXEL_SPACING
            | TAG_NOMINAL_SCANNED_PIXEL_SPACING
    ) || preserved_source_vr(tag).is_some()
}

fn pixel_data_encoding_for_header(header: &ElementHeader) -> &'static str {
    if header.length == UNDEFINED_LENGTH {
        PIXEL_DATA_ENCODING_ENCAPSULATED
    } else {
        PIXEL_DATA_ENCODING_NATIVE
    }
}

fn parse_u16_value(value: &[u8], byte_order: ByteOrder) -> Option<u16> {
    if value.len() == 2 {
        return Some(byte_order.read_u16(value));
    }

    first_component(&trim_string_value(value))
        .parse::<u16>()
        .ok()
}

fn parse_u32_value(value: &[u8], byte_order: ByteOrder) -> Option<u32> {
    if value.len() == 4 {
        return Some(byte_order.read_u32(value));
    }

    first_component(&trim_string_value(value))
        .parse::<u32>()
        .ok()
}

fn parse_float_value(value: &[u8]) -> Option<f64> {
    first_component(&trim_string_value(value))
        .parse::<f64>()
        .ok()
}

fn parse_spacing_pair(value: &[u8]) -> Option<SpacingPair> {
    let raw = trim_string_value(value);
    let mut parts = raw.split('\\');
    let row = parts.next()?.trim().parse::<f64>().ok()?;
    let column = parts.next()?.trim().parse::<f64>().ok()?;
    Some(SpacingPair {
        row_spacing_mm: row,
        column_spacing_mm: column,
    })
}

fn parse_string_values(value: &[u8]) -> Vec<String> {
    let raw = trim_string_value(value);
    if raw.is_empty() {
        return Vec::new();
    }

    raw.split('\\')
        .map(|part| part.trim().to_string())
        .collect()
}

fn trim_string_value(value: &[u8]) -> String {
    String::from_utf8_lossy(value)
        .trim_end_matches([' ', '\0'])
        .to_string()
}

fn first_component(raw: &str) -> &str {
    raw.split('\\').next().unwrap_or("").trim()
}

#[cfg(test)]
pub mod tests {
    use super::*;

    #[test]
    fn read_uses_pixel_spacing_measurement_scale_precedence() {
        let metadata = read(&build_test_dicom_with_options(BuildOptions {
            pixel_spacing: Some("0.20\\0.30"),
            imager_pixel_spacing: Some("0.40\\0.50"),
            nominal_scanned_pixel_spacing: Some("0.60\\0.70"),
            ..BuildOptions::default()
        }))
        .unwrap();

        assert_eq!(metadata.rows, 512);
        assert_eq!(metadata.columns, 1024);
        assert_eq!(
            metadata.measurement_scale(),
            Some(MeasurementScale {
                row_spacing_mm: 0.20,
                column_spacing_mm: 0.30,
                source: "PixelSpacing".to_string(),
            })
        );
    }

    #[test]
    fn read_falls_back_to_imager_spacing() {
        let metadata = read(&build_test_dicom_with_options(BuildOptions {
            imager_pixel_spacing: Some("0.40\\0.50"),
            nominal_scanned_pixel_spacing: Some("0.60\\0.70"),
            ..BuildOptions::default()
        }))
        .unwrap();

        assert_eq!(
            metadata.measurement_scale(),
            Some(MeasurementScale {
                row_spacing_mm: 0.40,
                column_spacing_mm: 0.50,
                source: "ImagerPixelSpacing".to_string(),
            })
        );
    }

    #[test]
    fn read_supports_raw_implicit_little_endian_dataset() {
        let metadata = read(&build_test_dicom_with_options(BuildOptions {
            with_part10: false,
            explicit: false,
            pixel_spacing: Some("0.20\\0.30"),
            ..BuildOptions::default()
        }))
        .unwrap();

        assert_eq!(
            metadata.transfer_syntax_uid,
            IMPLICIT_LITTLE_ENDIAN_TRANSFER_SYNTAX
        );
        assert_eq!(metadata.rows, 512);
        assert_eq!(metadata.columns, 1024);
        assert_eq!(
            metadata.measurement_scale().map(|scale| scale.source),
            Some("PixelSpacing".to_string())
        );
    }

    #[test]
    fn read_supports_explicit_big_endian_dataset() {
        let metadata = read(&build_test_dicom_with_options(BuildOptions {
            transfer_syntax_uid: EXPLICIT_BIG_ENDIAN_TRANSFER_SYNTAX,
            byte_order: ByteOrder::Big,
            pixel_spacing: Some("0.20\\0.30"),
            ..BuildOptions::default()
        }))
        .unwrap();

        assert_eq!(
            metadata.transfer_syntax_uid,
            EXPLICIT_BIG_ENDIAN_TRANSFER_SYNTAX
        );
        assert_eq!(metadata.rows, 512);
        assert_eq!(metadata.columns, 1024);
    }

    #[test]
    fn read_tracks_inspection_metadata_fields() {
        let metadata = read(&build_test_dicom_with_options(BuildOptions {
            samples_per_pixel: 3,
            planar_configuration: Some(2),
            number_of_frames: 4,
            photometric_interpretation: "RGB",
            window_center: Some("1200.5"),
            window_width: Some("2401.25"),
            rescale_intercept: Some("-1024"),
            rescale_slope: Some("2"),
            pixel_data: b"\x00\x20\x40\xff\x80\x60\xc0\xe0",
            ..BuildOptions::default()
        }))
        .unwrap();

        assert_eq!(metadata.samples_per_pixel, 3);
        assert_eq!(metadata.planar_configuration, 2);
        assert_eq!(metadata.number_of_frames, 4);
        assert_eq!(metadata.pixel_data_encoding, PIXEL_DATA_ENCODING_NATIVE);
        assert_eq!(metadata.photometric_interpretation, "RGB");
        assert_eq!(metadata.window_center, Some(1200.5));
        assert_eq!(metadata.window_width, Some(2401.25));
        assert_eq!(metadata.rescale_intercept, Some(-1024.0));
        assert_eq!(metadata.rescale_slope, Some(2.0));
    }

    #[test]
    fn read_tracks_preserved_source_elements() {
        const EXTRA_PRESERVED: &[(Tag, &str, &str)] = &[
            (TAG_PATIENT_ID, "LO", "P123"),
            (TAG_STUDY_DATE, "DA", "20260408"),
            (TAG_ACCESSION_NUMBER, "SH", "A456"),
            (TAG_IMAGER_PIXEL_SPACING, "DS", "0.50\\0.60"),
            (TAG_PIXEL_SPACING_CALIBRATION_TYPE, "CS", "GEOMETRY"),
        ];
        let metadata = read(&build_test_dicom_with_options(BuildOptions {
            study_instance_uid: Some("1.2.3.4.5"),
            patient_name: Some("Test^Patient"),
            pixel_spacing: Some("0.25\\0.40"),
            preserved_elements: EXTRA_PRESERVED,
            ..BuildOptions::default()
        }))
        .unwrap();

        assert_eq!(metadata.study_instance_uid, "1.2.3.4.5");
        assert_eq!(
            metadata.preserved_elements,
            vec![
                PreservedElement {
                    tag_group: 0x0010,
                    tag_element: 0x0010,
                    vr: "PN".to_string(),
                    values: vec!["Test^Patient".to_string()],
                },
                PreservedElement {
                    tag_group: 0x0010,
                    tag_element: 0x0020,
                    vr: "LO".to_string(),
                    values: vec!["P123".to_string()],
                },
                PreservedElement {
                    tag_group: 0x0008,
                    tag_element: 0x0020,
                    vr: "DA".to_string(),
                    values: vec!["20260408".to_string()],
                },
                PreservedElement {
                    tag_group: 0x0008,
                    tag_element: 0x0050,
                    vr: "SH".to_string(),
                    values: vec!["A456".to_string()],
                },
                PreservedElement {
                    tag_group: 0x0028,
                    tag_element: 0x0030,
                    vr: "DS".to_string(),
                    values: vec!["0.25".to_string(), "0.40".to_string()],
                },
                PreservedElement {
                    tag_group: 0x0018,
                    tag_element: 0x1164,
                    vr: "DS".to_string(),
                    values: vec!["0.50".to_string(), "0.60".to_string()],
                },
                PreservedElement {
                    tag_group: 0x0028,
                    tag_element: 0x0a04,
                    vr: "CS".to_string(),
                    values: vec!["GEOMETRY".to_string()],
                },
            ]
        );
    }

    #[test]
    fn read_marks_undefined_length_pixel_data_as_encapsulated() {
        let metadata = read(&build_test_dicom_with_options(BuildOptions {
            pixel_data_undefined_length: true,
            number_of_frames: 2,
            ..BuildOptions::default()
        }))
        .unwrap();

        assert_eq!(
            metadata.pixel_data_encoding,
            PIXEL_DATA_ENCODING_ENCAPSULATED
        );
        assert_eq!(metadata.number_of_frames, 2);
    }

    #[test]
    fn render_grayscale_preview_decodes_encapsulated_fragments() {
        let png = crate::render::encode_gray_png(2, 2, &[0, 85, 170, 255]).unwrap();
        let (first_fragment, second_fragment) = png.split_at(png.len() / 2);
        let mut dicom = build_test_dicom_with_options(BuildOptions {
            transfer_syntax_uid: "1.2.840.10008.1.2.4.50",
            rows: 2,
            columns: 2,
            pixel_spacing: Some("0.20\\0.30"),
            pixel_data_undefined_length: true,
            ..BuildOptions::default()
        });
        append_encapsulated_pixel_data(&mut dicom, &[first_fragment, second_fragment]);

        let preview = render_grayscale_preview(&dicom).unwrap();

        assert_eq!(preview.width, 2);
        assert_eq!(preview.height, 2);
        assert_eq!(preview.pixels, vec![0, 85, 170, 255]);
        assert_eq!(
            preview.measurement_scale,
            Some(MeasurementScale {
                row_spacing_mm: 0.20,
                column_spacing_mm: 0.30,
                source: "PixelSpacing".to_string(),
            })
        );

        let metadata = read(&dicom).unwrap();
        assert_eq!(
            metadata.pixel_data_encoding,
            PIXEL_DATA_ENCODING_ENCAPSULATED
        );
    }

    #[test]
    fn render_grayscale_preview_decodes_encapsulated_jpeg() {
        let mut jpeg = Vec::new();
        image::codecs::jpeg::JpegEncoder::new_with_quality(&mut jpeg, 100)
            .encode(&[0, 255], 2, 1, image::ExtendedColorType::L8)
            .unwrap();
        let mut dicom = build_test_dicom_with_options(BuildOptions {
            transfer_syntax_uid: "1.2.840.10008.1.2.4.50",
            rows: 1,
            columns: 2,
            pixel_data_undefined_length: true,
            ..BuildOptions::default()
        });
        append_encapsulated_pixel_data(&mut dicom, &[&jpeg]);

        let preview =
            render_grayscale_preview_with_window_mode(&dicom, RenderWindowMode::FullRange).unwrap();

        assert_eq!(preview.width, 2);
        assert_eq!(preview.height, 1);
        assert_eq!(preview.pixels, vec![0, 255]);
    }

    #[test]
    fn render_grayscale_preview_rejects_encapsulated_pixel_data_without_basic_offset_table() {
        let mut dicom = build_test_dicom_with_options(BuildOptions {
            transfer_syntax_uid: "1.2.840.10008.1.2.4.50",
            rows: 2,
            columns: 2,
            pixel_data_undefined_length: true,
            ..BuildOptions::default()
        });
        write_special_header(&mut dicom, ByteOrder::Little, TAG_SEQUENCE_DELIMITATION, 0);

        let error = render_grayscale_preview(&dicom).unwrap_err();

        assert!(error.contains("invalid encapsulated pixel data: expected item header"));
    }

    #[test]
    fn render_grayscale_preview_rejects_undefined_basic_offset_table_length() {
        let mut dicom = build_test_dicom_with_options(BuildOptions {
            transfer_syntax_uid: "1.2.840.10008.1.2.4.50",
            rows: 2,
            columns: 2,
            pixel_data_undefined_length: true,
            ..BuildOptions::default()
        });
        write_special_header(&mut dicom, ByteOrder::Little, TAG_ITEM, UNDEFINED_LENGTH);

        let error = render_grayscale_preview(&dicom).unwrap_err();

        assert!(
            error.contains("invalid encapsulated pixel data: undefined basic offset table length")
        );
    }

    #[test]
    fn render_grayscale_preview_rejects_undefined_encapsulated_fragment_length() {
        let mut dicom = build_test_dicom_with_options(BuildOptions {
            transfer_syntax_uid: "1.2.840.10008.1.2.4.50",
            rows: 2,
            columns: 2,
            pixel_data_undefined_length: true,
            ..BuildOptions::default()
        });
        write_special_header(&mut dicom, ByteOrder::Little, TAG_ITEM, 0);
        write_special_header(&mut dicom, ByteOrder::Little, TAG_ITEM, UNDEFINED_LENGTH);

        let error = render_grayscale_preview(&dicom).unwrap_err();

        assert!(error.contains("invalid encapsulated pixel data: undefined fragment length"));
    }

    #[test]
    fn render_grayscale_preview_rejects_empty_encapsulated_payload() {
        let mut dicom = build_test_dicom_with_options(BuildOptions {
            transfer_syntax_uid: "1.2.840.10008.1.2.4.50",
            rows: 2,
            columns: 2,
            pixel_data_undefined_length: true,
            ..BuildOptions::default()
        });
        write_special_header(&mut dicom, ByteOrder::Little, TAG_ITEM, 0);
        write_special_header(&mut dicom, ByteOrder::Little, TAG_SEQUENCE_DELIMITATION, 0);

        let error = render_grayscale_preview(&dicom).unwrap_err();

        assert!(error.contains("encapsulated pixel data did not contain any frame bytes"));
    }

    #[test]
    fn render_grayscale_preview_rejects_invalid_encapsulated_payload() {
        let mut dicom = build_test_dicom_with_options(BuildOptions {
            transfer_syntax_uid: "1.2.840.10008.1.2.4.50",
            rows: 2,
            columns: 2,
            pixel_data_undefined_length: true,
            ..BuildOptions::default()
        });
        append_encapsulated_pixel_data(&mut dicom, &[b"nope"]);

        let error = render_grayscale_preview(&dicom).unwrap_err();

        assert!(error.contains("decode encapsulated pixel data for transfer syntax"));
    }

    #[test]
    fn render_grayscale_preview_rejects_multi_frame_encapsulated_pixel_data() {
        let dicom = build_test_dicom_with_options(BuildOptions {
            transfer_syntax_uid: "1.2.840.10008.1.2.4.50",
            rows: 2,
            columns: 2,
            number_of_frames: 2,
            pixel_data_undefined_length: true,
            ..BuildOptions::default()
        });

        let error = render_grayscale_preview(&dicom).unwrap_err();

        assert!(error.contains("unsupported multi-frame encapsulated source decode: 2 frames"));
    }

    #[test]
    fn read_rejects_missing_rows() {
        let error = read(&build_test_dicom_with_options(BuildOptions {
            rows: 0,
            ..BuildOptions::default()
        }))
        .unwrap_err();

        assert!(error.contains("missing Rows"));
    }

    #[test]
    fn render_grayscale_preview_maps_native_pixels() {
        let preview = render_grayscale_preview(&build_test_dicom_with_options(BuildOptions {
            rows: 2,
            columns: 4,
            pixel_spacing: Some("0.20\\0.30"),
            pixel_data: b"\x00\x20\x40\xff\x80\x60\xc0\xe0",
            ..BuildOptions::default()
        }))
        .unwrap();

        assert_eq!(preview.width, 4);
        assert_eq!(preview.height, 2);
        assert_eq!(preview.pixels, vec![0, 32, 64, 255, 128, 96, 192, 224]);
        assert_eq!(
            preview.measurement_scale,
            Some(MeasurementScale {
                row_spacing_mm: 0.20,
                column_spacing_mm: 0.30,
                source: "PixelSpacing".to_string(),
            })
        );
    }

    #[test]
    fn render_grayscale_preview_uses_embedded_default_window() {
        let preview = render_grayscale_preview(&build_test_dicom_with_options(BuildOptions {
            rows: 1,
            columns: 3,
            window_center: Some("128"),
            window_width: Some("256"),
            pixel_data: b"\x00\x40\x80",
            ..BuildOptions::default()
        }))
        .unwrap();

        assert_eq!(preview.pixels, vec![0, 64, 128]);
    }

    #[test]
    fn render_grayscale_preview_full_range_ignores_embedded_window() {
        let preview = render_grayscale_preview_with_window_mode(
            &build_test_dicom_with_options(BuildOptions {
                rows: 1,
                columns: 3,
                window_center: Some("128"),
                window_width: Some("256"),
                pixel_data: b"\x00\x40\x80",
                ..BuildOptions::default()
            }),
            RenderWindowMode::FullRange,
        )
        .unwrap();

        assert_eq!(preview.pixels, vec![0, 128, 255]);
    }

    #[test]
    fn render_grayscale_preview_invalid_window_falls_back_to_full_range() {
        let preview = render_grayscale_preview(&build_test_dicom_with_options(BuildOptions {
            rows: 1,
            columns: 3,
            window_center: Some("128"),
            window_width: Some("1"),
            pixel_data: b"\x00\x40\x80",
            ..BuildOptions::default()
        }))
        .unwrap();

        assert_eq!(preview.pixels, vec![0, 128, 255]);
    }

    #[test]
    fn render_grayscale_preview_applies_rescale_before_windowing() {
        let preview = render_grayscale_preview(&build_test_dicom_with_options(BuildOptions {
            rows: 1,
            columns: 3,
            window_center: Some("100"),
            window_width: Some("200"),
            rescale_intercept: Some("-10"),
            rescale_slope: Some("2"),
            pixel_data: b"\x00\x37\x6e",
            ..BuildOptions::default()
        }))
        .unwrap();

        assert_eq!(preview.pixels, vec![0, 128, 255]);
    }

    #[test]
    fn render_grayscale_preview_rescale_updates_full_range_values() {
        let preview = render_grayscale_preview(&build_test_dicom_with_options(BuildOptions {
            rows: 1,
            columns: 3,
            rescale_intercept: Some("255"),
            rescale_slope: Some("-1"),
            pixel_data: b"\x00\x7f\xff",
            ..BuildOptions::default()
        }))
        .unwrap();

        assert_eq!(preview.pixels, vec![255, 128, 0]);
    }

    #[test]
    fn render_grayscale_preview_decodes_32_bit_unsigned_pixels() {
        let preview = render_grayscale_preview(&build_test_dicom_with_options(BuildOptions {
            rows: 1,
            columns: 3,
            bits_allocated: 32,
            bits_stored: 12,
            pixel_data: b"\x00\x00\x00\x00\x00\x08\x00\x00\xff\x0f\x00\x00",
            ..BuildOptions::default()
        }))
        .unwrap();

        assert_eq!(preview.pixels, vec![0, 128, 255]);
    }

    #[test]
    fn render_grayscale_preview_decodes_32_bit_signed_pixels() {
        let preview = render_grayscale_preview(&build_test_dicom_with_options(BuildOptions {
            rows: 1,
            columns: 3,
            bits_allocated: 32,
            bits_stored: 12,
            pixel_representation: 1,
            pixel_data: b"\x00\x08\x00\x00\x00\x00\x00\x00\xff\x07\x00\x00",
            ..BuildOptions::default()
        }))
        .unwrap();

        assert_eq!(preview.pixels, vec![0, 128, 255]);
    }

    #[test]
    fn render_grayscale_preview_decodes_32_bit_big_endian_pixels() {
        let preview = render_grayscale_preview(&build_test_dicom_with_options(BuildOptions {
            transfer_syntax_uid: EXPLICIT_BIG_ENDIAN_TRANSFER_SYNTAX,
            byte_order: ByteOrder::Big,
            rows: 1,
            columns: 2,
            bits_allocated: 32,
            bits_stored: 32,
            pixel_data: b"\x00\x00\x00\x00\x00\x00\x00\xff",
            ..BuildOptions::default()
        }))
        .unwrap();

        assert_eq!(preview.pixels, vec![0, 255]);
    }

    #[test]
    fn render_grayscale_preview_decodes_interleaved_rgb_pixels() {
        let rgb = &[
            255, 0, 0, // red
            0, 255, 0, // green
            0, 0, 255, // blue
            255, 255, 255, // white
        ];
        let preview = render_grayscale_preview(&build_test_dicom_with_options(BuildOptions {
            rows: 1,
            columns: 4,
            samples_per_pixel: 3,
            planar_configuration: Some(0),
            photometric_interpretation: "RGB",
            pixel_data: rgb,
            ..BuildOptions::default()
        }))
        .unwrap();

        let raw = [
            gray_from_rgb8(255, 0, 0),
            gray_from_rgb8(0, 255, 0),
            gray_from_rgb8(0, 0, 255),
            gray_from_rgb8(255, 255, 255),
        ];
        assert_eq!(preview.pixels, full_range_mapped_u8(&raw));
    }

    #[test]
    fn render_grayscale_preview_decodes_planar_rgb_pixels() {
        let rgb = &[
            255, 0, 0, // red plane
            0, 255, 0, // green plane
            0, 0, 255, // blue plane
        ];
        let preview = render_grayscale_preview(&build_test_dicom_with_options(BuildOptions {
            rows: 1,
            columns: 3,
            samples_per_pixel: 3,
            planar_configuration: Some(1),
            photometric_interpretation: "RGB",
            pixel_data: rgb,
            ..BuildOptions::default()
        }))
        .unwrap();

        let raw = [
            gray_from_rgb8(255, 0, 0),
            gray_from_rgb8(0, 255, 0),
            gray_from_rgb8(0, 0, 255),
        ];
        assert_eq!(preview.pixels, full_range_mapped_u8(&raw));
    }

    #[test]
    fn render_grayscale_preview_round_trips_rgb_secondary_capture() {
        let dicom = encode_secondary_capture(
            &PreviewImage::rgba(2, 1, vec![255, 0, 0, 255, 0, 255, 0, 255]),
            None,
        )
        .unwrap();

        let preview = render_grayscale_preview(&dicom).unwrap();

        assert_eq!(
            preview.pixels,
            full_range_mapped_u8(&[gray_from_rgb8(255, 0, 0), gray_from_rgb8(0, 255, 0)])
        );
    }

    #[test]
    fn encode_secondary_capture_sets_default_window_for_grayscale() {
        let dicom =
            encode_secondary_capture(&PreviewImage::gray(3, 1, vec![0, 127, 255]), None).unwrap();

        let metadata = read(&dicom).unwrap();

        assert_eq!(metadata.photometric_interpretation, "MONOCHROME2");
        assert_eq!(metadata.samples_per_pixel, 1);
        assert_eq!(metadata.window_center, Some(127.5));
        assert_eq!(metadata.window_width, Some(255.0));
    }

    #[test]
    fn encode_secondary_capture_writes_identity_metadata() {
        let dicom = encode_secondary_capture(&PreviewImage::gray(1, 1, vec![127]), None).unwrap();
        let elements = read_test_elements(&dicom);

        assert_eq!(
            test_element_string(&elements, TAG_MEDIA_STORAGE_SOP_CLASS_UID),
            SECONDARY_CAPTURE_SOP_CLASS_UID
        );
        assert_eq!(
            test_element_string(&elements, TAG_SOP_CLASS_UID),
            SECONDARY_CAPTURE_SOP_CLASS_UID
        );
        assert_eq!(
            test_element_string(&elements, TAG_TRANSFER_SYNTAX_UID),
            EXPLICIT_LITTLE_ENDIAN_TRANSFER_SYNTAX
        );
        assert_eq!(
            test_element_string(&elements, TAG_IMPLEMENTATION_CLASS_UID),
            IMPLEMENTATION_CLASS_UID
        );
        assert_eq!(
            test_element_string(&elements, TAG_IMPLEMENTATION_VERSION_NAME),
            IMPLEMENTATION_VERSION_NAME
        );
        assert_eq!(test_element_string(&elements, TAG_MODALITY), "OT");
        assert_eq!(
            test_element_string(&elements, TAG_IMAGE_TYPE),
            "DERIVED\\SECONDARY"
        );
        assert_eq!(test_element_string(&elements, TAG_CONVERSION_TYPE), "WSD");
        assert_eq!(test_element_string(&elements, TAG_MANUFACTURER), "XRayView");
        assert_eq!(
            test_element_string(&elements, TAG_SERIES_DESCRIPTION),
            "XRayView Processed"
        );
        assert_eq!(
            test_element_string(&elements, TAG_DERIVATION_DESCRIPTION),
            "Processed by XRayView"
        );
        assert_eq!(test_element_string(&elements, TAG_SERIES_NUMBER), "999");
        assert_eq!(test_element_string(&elements, TAG_INSTANCE_NUMBER), "1");

        let media_sop_uid = test_element_string(&elements, TAG_MEDIA_STORAGE_SOP_INSTANCE_UID);
        assert_eq!(
            test_element_string(&elements, TAG_SOP_INSTANCE_UID),
            media_sop_uid
        );
        assert!(media_sop_uid.starts_with("2.25."));
        assert!(test_element_string(&elements, TAG_STUDY_INSTANCE_UID).starts_with("2.25."));
        assert!(test_element_string(&elements, TAG_SERIES_INSTANCE_UID).starts_with("2.25."));
        assert_eq!(test_element_string(&elements, TAG_CONTENT_DATE).len(), 8);
        assert_eq!(test_element_string(&elements, TAG_CONTENT_TIME).len(), 6);
    }

    #[test]
    fn encode_secondary_capture_preserves_source_study_uid_and_measurement_scale() {
        let dicom = encode_secondary_capture_with_options(
            &PreviewImage::gray(2, 1, vec![0, 255]),
            &SecondaryCaptureOptions {
                measurement_scale: Some(MeasurementScale {
                    row_spacing_mm: 0.25,
                    column_spacing_mm: 0.40,
                    source: "PixelSpacing".to_string(),
                }),
                study_instance_uid: Some("1.2.3.4.5".to_string()),
                preserved_elements: vec![
                    PreservedElement {
                        tag_group: 0x0010,
                        tag_element: 0x0010,
                        vr: "PN".to_string(),
                        values: vec!["Test^Patient".to_string()],
                    },
                    PreservedElement {
                        tag_group: 0x0018,
                        tag_element: 0x1164,
                        vr: "DS".to_string(),
                        values: vec!["0.50".to_string(), "0.60".to_string()],
                    },
                ],
            },
        )
        .unwrap();

        let metadata = read(&dicom).unwrap();
        let elements = read_test_elements(&dicom);

        assert_eq!(metadata.study_instance_uid, "1.2.3.4.5");
        assert_eq!(
            test_element_string(&elements, TAG_STUDY_INSTANCE_UID),
            "1.2.3.4.5"
        );
        assert_eq!(
            test_element_string(&elements, TAG_PATIENT_NAME),
            "Test^Patient"
        );
        assert_eq!(
            test_element_string(&elements, TAG_IMAGER_PIXEL_SPACING),
            "0.50\\0.60"
        );
        assert_eq!(
            metadata.measurement_scale(),
            Some(MeasurementScale {
                row_spacing_mm: 0.25,
                column_spacing_mm: 0.40,
                source: "PixelSpacing".to_string(),
            })
        );
    }

    #[test]
    fn encode_secondary_capture_omits_default_window_for_color() {
        let dicom = encode_secondary_capture(
            &PreviewImage::rgba(2, 1, vec![10, 20, 30, 255, 40, 50, 60, 128]),
            None,
        )
        .unwrap();

        let metadata = read(&dicom).unwrap();

        assert_eq!(metadata.photometric_interpretation, "RGB");
        assert_eq!(metadata.samples_per_pixel, 3);
        assert_eq!(metadata.planar_configuration, 0);
        assert_eq!(metadata.window_center, None);
        assert_eq!(metadata.window_width, None);
    }

    #[test]
    fn render_grayscale_preview_rejects_unsupported_rgb_variants() {
        let ybr_error = render_grayscale_preview(&build_test_dicom_with_options(BuildOptions {
            rows: 1,
            columns: 1,
            samples_per_pixel: 3,
            planar_configuration: Some(0),
            photometric_interpretation: "YBR_FULL",
            pixel_data: b"\x00\x00\x00",
            ..BuildOptions::default()
        }))
        .unwrap_err();
        assert!(ybr_error.contains("unsupported color photometric interpretation: YBR_FULL"));

        let planar_error = render_grayscale_preview(&build_test_dicom_with_options(BuildOptions {
            rows: 1,
            columns: 1,
            samples_per_pixel: 3,
            planar_configuration: Some(2),
            photometric_interpretation: "RGB",
            pixel_data: b"\x00\x00\x00",
            ..BuildOptions::default()
        }))
        .unwrap_err();
        assert!(planar_error.contains("unsupported planar configuration: 2"));
    }

    fn full_range_mapped_u8(values: &[u8]) -> Vec<u8> {
        let min = f32::from(*values.iter().min().unwrap());
        let max = f32::from(*values.iter().max().unwrap());
        values
            .iter()
            .map(|value| map_linear(f32::from(*value), min, max))
            .collect()
    }

    fn read_test_elements(bytes: &[u8]) -> Vec<(Tag, String, Vec<u8>)> {
        let mut source = Cursor::new(bytes);
        source
            .seek(SeekFrom::Start(
                (PART10_PREAMBLE_LENGTH + PART10_MAGIC.len()) as u64,
            ))
            .unwrap();
        let mut elements = Vec::new();
        loop {
            let header = match read_element_header(&mut source, FILE_META_TRANSFER_SYNTAX) {
                Ok(header) => header,
                Err(error) if error.kind() == io::ErrorKind::UnexpectedEof => break,
                Err(error) => panic!("read test element header: {error}"),
            };
            if header.length == UNDEFINED_LENGTH {
                panic!("unexpected undefined-length test element {}", header.tag);
            }
            let value = read_value(&mut source, header.length).unwrap();
            elements.push((header.tag, header.vr, value));
        }
        elements
    }

    fn test_element_string(elements: &[(Tag, String, Vec<u8>)], tag: Tag) -> String {
        let value = elements
            .iter()
            .find(|(element_tag, _, _)| *element_tag == tag)
            .unwrap_or_else(|| panic!("missing test element {tag}"));
        trim_string_value(&value.2)
    }

    #[test]
    fn read_file_falls_back_to_standalone_bmp_metadata() {
        let path = unique_temp_path("standalone", "bmp");
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
    fn render_grayscale_preview_file_falls_back_to_standalone_bmp_pixels() {
        let path = unique_temp_path("standalone-render", "bmp");
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
    fn render_tooth_analysis_preview_preserves_standalone_8bit_range() {
        let path = unique_temp_path("standalone-analysis-render", "bmp");
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
    fn render_standalone_bmp_supports_palette_pixels() {
        let bmp = build_bmp_8_palette(2, 1, &[(0, 0, 0), (255, 255, 255)], &[0, 1]);
        let preview =
            render_standalone_image_preview_with_options(Path::new("palette.bmp"), &bmp, false)
                .unwrap();

        assert_eq!(preview.pixels, vec![0, 255]);
    }

    #[test]
    fn read_file_falls_back_to_standalone_tiff_metadata() {
        let path = unique_temp_path("standalone", "tiff");
        std::fs::write(&path, build_tiff_rgb(2, 1, &[(255, 0, 0), (0, 255, 0)])).unwrap();

        let metadata = read_file(path.to_str().unwrap()).unwrap();

        assert_eq!(metadata.rows, 1);
        assert_eq!(metadata.columns, 2);
        assert_eq!(metadata.samples_per_pixel, 3);
        assert_eq!(metadata.bits_allocated, 8);
        assert_eq!(metadata.photometric_interpretation, "RGB");
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn render_grayscale_preview_file_falls_back_to_standalone_tiff_pixels() {
        let path = unique_temp_path("standalone-render", "tif");
        std::fs::write(&path, build_tiff_rgb(2, 1, &[(255, 0, 0), (0, 255, 0)])).unwrap();

        let preview = render_grayscale_preview_file(&path).unwrap();

        assert_eq!(preview.width, 2);
        assert_eq!(preview.height, 1);
        assert_eq!(
            preview.pixels,
            full_range_mapped_u8(&[gray_from_rgb8(255, 0, 0), gray_from_rgb8(0, 255, 0)])
        );
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn render_standalone_tiff_supports_white_is_zero_grayscale() {
        let tiff = build_tiff_gray(2, 1, 0, &[0, 255]);
        let preview = render_standalone_image_preview_with_options(
            Path::new("white-is-zero.tif"),
            &tiff,
            false,
        )
        .unwrap();

        assert_eq!(preview.pixels, vec![255, 0]);
    }

    fn unique_temp_path(name: &str, extension: &str) -> std::path::PathBuf {
        let nanos = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        std::env::temp_dir().join(format!(
            "xrayview-rs-dicom-{name}-{}-{nanos}.{extension}",
            std::process::id()
        ))
    }

    fn build_bmp_32(width: u32, height: u32, rgb_top_down: &[(u8, u8, u8)]) -> Vec<u8> {
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

    fn build_bmp_8_palette(
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

    fn build_tiff_gray(width: u32, height: u32, photometric: u16, pixels: &[u8]) -> Vec<u8> {
        assert_eq!(pixels.len(), width as usize * height as usize);
        let entry_count = 9_u16;
        let pixel_offset = 8 + 2 + usize::from(entry_count) * 12 + 4;
        let mut tiff = tiff_header(entry_count);
        write_tiff_long_entry(&mut tiff, 256, width);
        write_tiff_long_entry(&mut tiff, 257, height);
        write_tiff_short_entry(&mut tiff, 258, 8);
        write_tiff_short_entry(&mut tiff, 259, 1);
        write_tiff_short_entry(&mut tiff, 262, photometric);
        write_tiff_long_entry(&mut tiff, 273, pixel_offset as u32);
        write_tiff_short_entry(&mut tiff, 277, 1);
        write_tiff_long_entry(&mut tiff, 278, height);
        write_tiff_long_entry(&mut tiff, 279, pixels.len() as u32);
        tiff.extend_from_slice(&0_u32.to_le_bytes());
        tiff.extend_from_slice(pixels);
        tiff
    }

    fn build_tiff_rgb(width: u32, height: u32, rgb_top_down: &[(u8, u8, u8)]) -> Vec<u8> {
        assert_eq!(rgb_top_down.len(), width as usize * height as usize);
        let entry_count = 10_u16;
        let ifd_end = 8 + 2 + usize::from(entry_count) * 12 + 4;
        let bits_offset = ifd_end;
        let pixel_offset = bits_offset + 6;
        let pixel_byte_count = rgb_top_down.len() * 3;
        let mut tiff = tiff_header(entry_count);
        write_tiff_long_entry(&mut tiff, 256, width);
        write_tiff_long_entry(&mut tiff, 257, height);
        write_tiff_offset_entry(&mut tiff, 258, 3, 3, bits_offset as u32);
        write_tiff_short_entry(&mut tiff, 259, 1);
        write_tiff_short_entry(&mut tiff, 262, 2);
        write_tiff_long_entry(&mut tiff, 273, pixel_offset as u32);
        write_tiff_short_entry(&mut tiff, 277, 3);
        write_tiff_long_entry(&mut tiff, 278, height);
        write_tiff_long_entry(&mut tiff, 279, pixel_byte_count as u32);
        write_tiff_short_entry(&mut tiff, 284, 1);
        tiff.extend_from_slice(&0_u32.to_le_bytes());
        tiff.extend_from_slice(&8_u16.to_le_bytes());
        tiff.extend_from_slice(&8_u16.to_le_bytes());
        tiff.extend_from_slice(&8_u16.to_le_bytes());
        for &(red, green, blue) in rgb_top_down {
            tiff.extend_from_slice(&[red, green, blue]);
        }
        tiff
    }

    fn tiff_header(entry_count: u16) -> Vec<u8> {
        let mut tiff = Vec::new();
        tiff.extend_from_slice(b"II");
        tiff.extend_from_slice(&42_u16.to_le_bytes());
        tiff.extend_from_slice(&8_u32.to_le_bytes());
        tiff.extend_from_slice(&entry_count.to_le_bytes());
        tiff
    }

    fn write_tiff_short_entry(tiff: &mut Vec<u8>, tag: u16, value: u16) {
        tiff.extend_from_slice(&tag.to_le_bytes());
        tiff.extend_from_slice(&3_u16.to_le_bytes());
        tiff.extend_from_slice(&1_u32.to_le_bytes());
        tiff.extend_from_slice(&value.to_le_bytes());
        tiff.extend_from_slice(&0_u16.to_le_bytes());
    }

    fn write_tiff_long_entry(tiff: &mut Vec<u8>, tag: u16, value: u32) {
        tiff.extend_from_slice(&tag.to_le_bytes());
        tiff.extend_from_slice(&4_u16.to_le_bytes());
        tiff.extend_from_slice(&1_u32.to_le_bytes());
        tiff.extend_from_slice(&value.to_le_bytes());
    }

    fn write_tiff_offset_entry(
        tiff: &mut Vec<u8>,
        tag: u16,
        type_id: u16,
        count: u32,
        offset: u32,
    ) {
        tiff.extend_from_slice(&tag.to_le_bytes());
        tiff.extend_from_slice(&type_id.to_le_bytes());
        tiff.extend_from_slice(&count.to_le_bytes());
        tiff.extend_from_slice(&offset.to_le_bytes());
    }

    pub fn build_test_dicom(pixel_spacing: Option<&'static str>) -> Vec<u8> {
        build_test_dicom_with_options(BuildOptions {
            pixel_spacing,
            ..BuildOptions::default()
        })
    }

    pub fn build_renderable_test_dicom(pixel_spacing: Option<&'static str>) -> Vec<u8> {
        build_test_dicom_with_options(BuildOptions {
            rows: 2,
            columns: 4,
            pixel_spacing,
            pixel_data: b"\x00\x20\x40\xff\x80\x60\xc0\xe0",
            ..BuildOptions::default()
        })
    }

    pub fn build_renderable_test_dicom_with_study_uid(
        pixel_spacing: Option<&'static str>,
        study_instance_uid: &'static str,
    ) -> Vec<u8> {
        build_test_dicom_with_options(BuildOptions {
            rows: 2,
            columns: 4,
            pixel_spacing,
            study_instance_uid: Some(study_instance_uid),
            pixel_data: b"\x00\x20\x40\xff\x80\x60\xc0\xe0",
            ..BuildOptions::default()
        })
    }

    pub fn build_renderable_test_dicom_with_source_metadata(
        pixel_spacing: Option<&'static str>,
        study_instance_uid: &'static str,
        patient_name: &'static str,
    ) -> Vec<u8> {
        build_test_dicom_with_options(BuildOptions {
            rows: 2,
            columns: 4,
            pixel_spacing,
            study_instance_uid: Some(study_instance_uid),
            patient_name: Some(patient_name),
            pixel_data: b"\x00\x20\x40\xff\x80\x60\xc0\xe0",
            ..BuildOptions::default()
        })
    }

    pub fn build_renderable_test_dicom_with_pixels(
        width: u16,
        height: u16,
        pixel_spacing: Option<&'static str>,
        pixel_data: &'static [u8],
    ) -> Vec<u8> {
        build_test_dicom_with_options(BuildOptions {
            rows: height,
            columns: width,
            pixel_spacing,
            pixel_data,
            ..BuildOptions::default()
        })
    }

    pub fn build_windowed_renderable_test_dicom(pixel_data: &'static [u8]) -> Vec<u8> {
        build_test_dicom_with_options(BuildOptions {
            rows: 1,
            columns: pixel_data.len() as u16,
            window_center: Some("128"),
            window_width: Some("256"),
            pixel_data,
            ..BuildOptions::default()
        })
    }

    #[derive(Clone, Copy)]
    struct BuildOptions {
        with_part10: bool,
        transfer_syntax_uid: &'static str,
        byte_order: ByteOrder,
        explicit: bool,
        rows: u16,
        columns: u16,
        samples_per_pixel: u16,
        planar_configuration: Option<u16>,
        number_of_frames: u32,
        bits_allocated: u16,
        bits_stored: u16,
        pixel_representation: u16,
        photometric_interpretation: &'static str,
        study_instance_uid: Option<&'static str>,
        patient_name: Option<&'static str>,
        preserved_elements: &'static [(Tag, &'static str, &'static str)],
        window_center: Option<&'static str>,
        window_width: Option<&'static str>,
        rescale_intercept: Option<&'static str>,
        rescale_slope: Option<&'static str>,
        pixel_spacing: Option<&'static str>,
        imager_pixel_spacing: Option<&'static str>,
        nominal_scanned_pixel_spacing: Option<&'static str>,
        pixel_data: &'static [u8],
        pixel_data_undefined_length: bool,
    }

    impl Default for BuildOptions {
        fn default() -> Self {
            Self {
                with_part10: true,
                transfer_syntax_uid: EXPLICIT_LITTLE_ENDIAN_TRANSFER_SYNTAX,
                byte_order: ByteOrder::Little,
                explicit: true,
                rows: 512,
                columns: 1024,
                samples_per_pixel: 1,
                planar_configuration: None,
                number_of_frames: 1,
                bits_allocated: 8,
                bits_stored: 8,
                pixel_representation: 0,
                photometric_interpretation: "MONOCHROME2",
                study_instance_uid: None,
                patient_name: None,
                preserved_elements: &[],
                window_center: None,
                window_width: None,
                rescale_intercept: None,
                rescale_slope: None,
                pixel_spacing: None,
                imager_pixel_spacing: None,
                nominal_scanned_pixel_spacing: None,
                pixel_data: &[],
                pixel_data_undefined_length: false,
            }
        }
    }

    fn build_test_dicom_with_options(options: BuildOptions) -> Vec<u8> {
        let mut payload = Vec::new();
        if options.with_part10 {
            payload.extend([0_u8; PART10_PREAMBLE_LENGTH]);
            payload.extend(PART10_MAGIC);
            write_element(
                &mut payload,
                ByteOrder::Little,
                true,
                TAG_TRANSFER_SYNTAX_UID,
                "UI",
                &encode_string(options.transfer_syntax_uid, 0),
            );
        }

        if options.rows != 0 {
            write_element(
                &mut payload,
                options.byte_order,
                options.explicit,
                TAG_SAMPLES_PER_PIXEL,
                "US",
                &encode_u16(options.byte_order, options.samples_per_pixel),
            );
            if let Some(planar_configuration) = options.planar_configuration {
                write_element(
                    &mut payload,
                    options.byte_order,
                    options.explicit,
                    TAG_PLANAR_CONFIGURATION,
                    "US",
                    &encode_u16(options.byte_order, planar_configuration),
                );
            }
            if options.number_of_frames > 0 {
                write_element(
                    &mut payload,
                    options.byte_order,
                    options.explicit,
                    TAG_NUMBER_OF_FRAMES,
                    "IS",
                    &encode_string(&options.number_of_frames.to_string(), b' '),
                );
            }
            write_element(
                &mut payload,
                options.byte_order,
                options.explicit,
                TAG_ROWS,
                "US",
                &encode_u16(options.byte_order, options.rows),
            );
        }
        write_element(
            &mut payload,
            options.byte_order,
            options.explicit,
            TAG_COLUMNS,
            "US",
            &encode_u16(options.byte_order, options.columns),
        );
        write_element(
            &mut payload,
            options.byte_order,
            options.explicit,
            TAG_PHOTOMETRIC_INTERPRETATION,
            "CS",
            &encode_string(options.photometric_interpretation, b' '),
        );
        if let Some(study_instance_uid) = options.study_instance_uid {
            write_element(
                &mut payload,
                options.byte_order,
                options.explicit,
                TAG_STUDY_INSTANCE_UID,
                "UI",
                &encode_string(study_instance_uid, 0),
            );
        }
        if let Some(patient_name) = options.patient_name {
            write_element(
                &mut payload,
                options.byte_order,
                options.explicit,
                TAG_PATIENT_NAME,
                "PN",
                &encode_string(patient_name, b' '),
            );
        }
        for (tag, vr, value) in options.preserved_elements {
            write_element(
                &mut payload,
                options.byte_order,
                options.explicit,
                *tag,
                vr,
                &encode_string(
                    value,
                    if vr.eq_ignore_ascii_case("UI") {
                        0
                    } else {
                        b' '
                    },
                ),
            );
        }
        write_element(
            &mut payload,
            options.byte_order,
            options.explicit,
            TAG_BITS_ALLOCATED,
            "US",
            &encode_u16(options.byte_order, options.bits_allocated),
        );
        write_element(
            &mut payload,
            options.byte_order,
            options.explicit,
            TAG_BITS_STORED,
            "US",
            &encode_u16(options.byte_order, options.bits_stored),
        );
        write_element(
            &mut payload,
            options.byte_order,
            options.explicit,
            TAG_PIXEL_REPRESENTATION,
            "US",
            &encode_u16(options.byte_order, options.pixel_representation),
        );
        if let Some(pixel_spacing) = options.pixel_spacing {
            write_element(
                &mut payload,
                options.byte_order,
                options.explicit,
                TAG_PIXEL_SPACING,
                "DS",
                &encode_string(pixel_spacing, b' '),
            );
        }
        if let Some(window_center) = options.window_center {
            write_element(
                &mut payload,
                options.byte_order,
                options.explicit,
                TAG_WINDOW_CENTER,
                "DS",
                &encode_string(window_center, b' '),
            );
        }
        if let Some(window_width) = options.window_width {
            write_element(
                &mut payload,
                options.byte_order,
                options.explicit,
                TAG_WINDOW_WIDTH,
                "DS",
                &encode_string(window_width, b' '),
            );
        }
        if let Some(rescale_intercept) = options.rescale_intercept {
            write_element(
                &mut payload,
                options.byte_order,
                options.explicit,
                TAG_RESCALE_INTERCEPT,
                "DS",
                &encode_string(rescale_intercept, b' '),
            );
        }
        if let Some(rescale_slope) = options.rescale_slope {
            write_element(
                &mut payload,
                options.byte_order,
                options.explicit,
                TAG_RESCALE_SLOPE,
                "DS",
                &encode_string(rescale_slope, b' '),
            );
        }
        if let Some(imager_pixel_spacing) = options.imager_pixel_spacing {
            write_element(
                &mut payload,
                options.byte_order,
                options.explicit,
                TAG_IMAGER_PIXEL_SPACING,
                "DS",
                &encode_string(imager_pixel_spacing, b' '),
            );
        }
        if let Some(nominal_scanned_pixel_spacing) = options.nominal_scanned_pixel_spacing {
            write_element(
                &mut payload,
                options.byte_order,
                options.explicit,
                TAG_NOMINAL_SCANNED_PIXEL_SPACING,
                "DS",
                &encode_string(nominal_scanned_pixel_spacing, b' '),
            );
        }
        if options.pixel_data_undefined_length {
            write_element_header(
                &mut payload,
                options.byte_order,
                options.explicit,
                TAG_PIXEL_DATA,
                "OB",
                UNDEFINED_LENGTH,
            );
        } else {
            write_element(
                &mut payload,
                options.byte_order,
                options.explicit,
                TAG_PIXEL_DATA,
                "OB",
                options.pixel_data,
            );
        }
        payload
    }

    fn write_element(
        payload: &mut Vec<u8>,
        byte_order: ByteOrder,
        explicit: bool,
        tag: Tag,
        vr: &str,
        value: &[u8],
    ) {
        write_u16(payload, byte_order, tag.group);
        write_u16(payload, byte_order, tag.element);

        if explicit {
            payload.extend(vr.as_bytes());
            if uses_32_bit_length(vr) {
                payload.extend([0, 0]);
                write_u32(payload, byte_order, value.len() as u32);
            } else {
                write_u16(payload, byte_order, value.len() as u16);
            }
        } else {
            write_u32(payload, byte_order, value.len() as u32);
        }

        payload.extend(value);
    }

    fn write_element_header(
        payload: &mut Vec<u8>,
        byte_order: ByteOrder,
        explicit: bool,
        tag: Tag,
        vr: &str,
        length: u32,
    ) {
        write_u16(payload, byte_order, tag.group);
        write_u16(payload, byte_order, tag.element);

        if explicit {
            payload.extend(vr.as_bytes());
            if uses_32_bit_length(vr) {
                payload.extend([0, 0]);
                write_u32(payload, byte_order, length);
            } else {
                write_u16(payload, byte_order, length as u16);
            }
        } else {
            write_u32(payload, byte_order, length);
        }
    }

    fn append_encapsulated_pixel_data(payload: &mut Vec<u8>, fragments: &[&[u8]]) {
        write_special_header(payload, ByteOrder::Little, TAG_ITEM, 0);
        for fragment in fragments {
            write_special_header(payload, ByteOrder::Little, TAG_ITEM, fragment.len() as u32);
            payload.extend_from_slice(fragment);
        }
        write_special_header(payload, ByteOrder::Little, TAG_SEQUENCE_DELIMITATION, 0);
    }

    fn write_special_header(payload: &mut Vec<u8>, byte_order: ByteOrder, tag: Tag, length: u32) {
        write_u16(payload, byte_order, tag.group);
        write_u16(payload, byte_order, tag.element);
        write_u32(payload, byte_order, length);
    }

    fn write_u16(payload: &mut Vec<u8>, byte_order: ByteOrder, value: u16) {
        match byte_order {
            ByteOrder::Little => payload.extend(value.to_le_bytes()),
            ByteOrder::Big => payload.extend(value.to_be_bytes()),
        }
    }

    fn write_u32(payload: &mut Vec<u8>, byte_order: ByteOrder, value: u32) {
        match byte_order {
            ByteOrder::Little => payload.extend(value.to_le_bytes()),
            ByteOrder::Big => payload.extend(value.to_be_bytes()),
        }
    }

    fn encode_u16(byte_order: ByteOrder, value: u16) -> Vec<u8> {
        match byte_order {
            ByteOrder::Little => value.to_le_bytes().to_vec(),
            ByteOrder::Big => value.to_be_bytes().to_vec(),
        }
    }

    fn encode_string(value: &str, padding: u8) -> Vec<u8> {
        let mut raw = value.as_bytes().to_vec();
        if raw.len() % 2 != 0 {
            raw.push(padding);
        }
        raw
    }
}
