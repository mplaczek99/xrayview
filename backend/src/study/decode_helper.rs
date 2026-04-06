use std::path::Path;

use anyhow::{Result, bail};
use dicom_core::header::Header;
use dicom_core::{DicomValue, PrimitiveValue};
use serde::Serialize;

use crate::api::MeasurementScale;
use crate::render::windowing::WindowLevel;

use super::source_image::{SourceMetadata, SourceStudy, load_source_study};

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DecodedSourceStudy {
    pub image: DecodedSourceImage,
    pub metadata: PreservedSourceMetadata,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DecodedSourceImage {
    pub width: u32,
    pub height: u32,
    pub pixels: Vec<f32>,
    pub min_value: f32,
    pub max_value: f32,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub default_window: Option<DecodedWindowLevel>,
    pub invert: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DecodedWindowLevel {
    pub center: f32,
    pub width: f32,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct PreservedSourceMetadata {
    pub study_instance_uid: String,
    pub preserved_elements: Vec<PreservedSourceElement>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct PreservedSourceElement {
    pub tag_group: u16,
    pub tag_element: u16,
    pub vr: String,
    pub values: Vec<String>,
}

impl From<WindowLevel> for DecodedWindowLevel {
    fn from(value: WindowLevel) -> Self {
        Self {
            center: value.center,
            width: value.width,
        }
    }
}

pub fn decode_source_study(path: &Path) -> Result<DecodedSourceStudy> {
    DecodedSourceStudy::from_source_study(load_source_study(path)?)
}

impl DecodedSourceStudy {
    pub fn from_source_study(source: SourceStudy) -> Result<Self> {
        let image = DecodedSourceImage {
            width: source.image.width,
            height: source.image.height,
            pixels: source.image.pixels,
            min_value: source.image.min_value,
            max_value: source.image.max_value,
            default_window: source.image.default_window.map(Into::into),
            invert: source.image.invert,
        };

        let metadata = PreservedSourceMetadata::from_source_metadata(&source.metadata)?;

        Ok(Self {
            image,
            metadata,
            measurement_scale: source.measurement_scale,
        })
    }
}

impl PreservedSourceMetadata {
    fn from_source_metadata(metadata: &SourceMetadata) -> Result<Self> {
        let preserved_elements = metadata
            .preserved_elements()
            .iter()
            .map(PreservedSourceElement::from_source_element)
            .collect::<Result<Vec<_>>>()?;

        Ok(Self {
            study_instance_uid: metadata.study_instance_uid().to_string(),
            preserved_elements,
        })
    }
}

impl PreservedSourceElement {
    fn from_source_element(
        element: &dicom_core::DataElement<dicom_object::mem::InMemDicomObject>,
    ) -> Result<Self> {
        let values = match element.value() {
            DicomValue::Primitive(value) => collect_primitive_strings(value),
            DicomValue::PixelSequence(_) | DicomValue::Sequence(_) => {
                bail!("preserved source metadata cannot contain nested DICOM values")
            }
        };

        Ok(Self {
            tag_group: element.tag().group(),
            tag_element: element.tag().element(),
            vr: format!("{}", element.vr()),
            values,
        })
    }
}

fn collect_primitive_strings(value: &PrimitiveValue) -> Vec<String> {
    value
        .to_multi_str()
        .iter()
        .map(|item| item.to_string())
        .collect()
}

#[cfg(test)]
mod tests {
    use std::path::Path;

    use dicom_core::{DataElement, DicomValue, PrimitiveValue, VR};
    use dicom_dictionary_std::tags;
    use dicom_object::{FileDicomObject, meta::FileMetaTableBuilder};

    use super::decode_source_study;

    const EXPLICIT_VR_LITTLE_ENDIAN: &str = "1.2.840.10008.1.2.1";

    #[test]
    fn decoded_source_study_serializes_pixels_and_preserved_metadata() {
        let temp_dir = tempfile::TempDir::new().expect("temp dir");
        let input_path = temp_dir.path().join("helper-sample.dcm");
        write_test_dicom(&input_path);

        let decoded = decode_source_study(&input_path).expect("decode source study");

        assert_eq!(decoded.image.width, 2);
        assert_eq!(decoded.image.height, 2);
        assert_eq!(decoded.image.pixels, vec![0.0, 64.0, 128.0, 255.0]);
        assert_eq!(decoded.image.min_value, 0.0);
        assert_eq!(decoded.image.max_value, 255.0);
        assert_eq!(
            decoded.image.default_window,
            Some(super::DecodedWindowLevel {
                center: 127.5,
                width: 255.0,
            })
        );
        assert!(!decoded.image.invert);

        let measurement_scale = decoded.measurement_scale.expect("measurement scale");
        assert_eq!(measurement_scale.row_spacing_mm, 0.25);
        assert_eq!(measurement_scale.column_spacing_mm, 0.40);
        assert_eq!(measurement_scale.source, "PixelSpacing");

        assert_eq!(decoded.metadata.study_instance_uid, "1.2.3.4.5.6.7.8.9");
        assert_eq!(decoded.metadata.preserved_elements.len(), 2);
        assert_eq!(decoded.metadata.preserved_elements[0].tag_group, 0x0010);
        assert_eq!(decoded.metadata.preserved_elements[0].tag_element, 0x0010);
        assert_eq!(decoded.metadata.preserved_elements[0].vr, "PN");
        assert_eq!(
            decoded.metadata.preserved_elements[0].values,
            ["Test^Patient"]
        );
        assert_eq!(decoded.metadata.preserved_elements[1].tag_group, 0x0028);
        assert_eq!(decoded.metadata.preserved_elements[1].tag_element, 0x0030);
        assert_eq!(decoded.metadata.preserved_elements[1].vr, "DS");
        assert_eq!(
            decoded.metadata.preserved_elements[1].values,
            ["0.25", "0.40"]
        );
    }

    fn write_test_dicom(path: &Path) {
        let meta = FileMetaTableBuilder::new()
            .transfer_syntax(EXPLICIT_VR_LITTLE_ENDIAN)
            .build()
            .expect("file meta");
        let mut source = FileDicomObject::new_empty_with_meta(meta);

        source.put(DataElement::new(
            tags::PATIENT_NAME,
            VR::PN,
            DicomValue::Primitive(PrimitiveValue::Str("Test^Patient".to_string())),
        ));
        source.put(DataElement::new(
            tags::STUDY_INSTANCE_UID,
            VR::UI,
            DicomValue::Primitive(PrimitiveValue::Str("1.2.3.4.5.6.7.8.9".to_string())),
        ));
        source.put(DataElement::new(
            tags::ROWS,
            VR::US,
            DicomValue::Primitive(PrimitiveValue::from(2_u16)),
        ));
        source.put(DataElement::new(
            tags::COLUMNS,
            VR::US,
            DicomValue::Primitive(PrimitiveValue::from(2_u16)),
        ));
        source.put(DataElement::new(
            tags::PHOTOMETRIC_INTERPRETATION,
            VR::CS,
            DicomValue::Primitive(PrimitiveValue::Str("MONOCHROME2".to_string())),
        ));
        source.put(DataElement::new(
            tags::BITS_ALLOCATED,
            VR::US,
            DicomValue::Primitive(PrimitiveValue::from(8_u16)),
        ));
        source.put(DataElement::new(
            tags::BITS_STORED,
            VR::US,
            DicomValue::Primitive(PrimitiveValue::from(8_u16)),
        ));
        source.put(DataElement::new(
            tags::PIXEL_REPRESENTATION,
            VR::US,
            DicomValue::Primitive(PrimitiveValue::from(0_u16)),
        ));
        source.put(DataElement::new(
            tags::WINDOW_CENTER,
            VR::DS,
            DicomValue::Primitive(PrimitiveValue::Str("127.5".to_string())),
        ));
        source.put(DataElement::new(
            tags::WINDOW_WIDTH,
            VR::DS,
            DicomValue::Primitive(PrimitiveValue::Str("255".to_string())),
        ));
        source.put(DataElement::new(
            tags::PIXEL_SPACING,
            VR::DS,
            DicomValue::Primitive(PrimitiveValue::Strs(
                vec!["0.25".to_string(), "0.40".to_string()].into(),
            )),
        ));
        source.put(DataElement::new(
            tags::PIXEL_DATA,
            VR::OB,
            DicomValue::Primitive(PrimitiveValue::U8(vec![0_u8, 64, 128, 255].into())),
        ));

        source.write_to_file(path).expect("write helper fixture");
    }
}
