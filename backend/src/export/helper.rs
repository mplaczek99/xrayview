use std::path::Path;

use anyhow::{Context, Result, bail};
use dicom_core::{DataElement, DicomValue, PrimitiveValue, Tag, VR};
use dicom_object::mem::InMemDicomObject;
use serde::Deserialize;

use crate::preview::{PreviewFormat, PreviewImage};
use crate::study::source_image::SourceMetadata;

use super::secondary_capture::export_secondary_capture;

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SecondaryCaptureExportRequest {
    pub preview: ExportPreviewImage,
    pub metadata: PreservedSourceMetadata,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ExportPreviewImage {
    pub width: u32,
    pub height: u32,
    pub format: String,
    pub pixels: Vec<u8>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PreservedSourceMetadata {
    pub study_instance_uid: String,
    pub preserved_elements: Vec<PreservedSourceElement>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PreservedSourceElement {
    pub tag_group: u16,
    pub tag_element: u16,
    pub vr: String,
    pub values: Vec<String>,
}

pub fn write_secondary_capture_request(
    request: SecondaryCaptureExportRequest,
    output_path: &Path,
) -> Result<()> {
    let preview = request.preview.into_preview_image()?;
    let metadata = request.metadata.into_source_metadata()?;

    export_secondary_capture(&preview, &metadata, output_path)
        .with_context(|| format!("write secondary capture: {}", output_path.display()))
}

impl ExportPreviewImage {
    fn into_preview_image(self) -> Result<PreviewImage> {
        if self.width == 0 || self.height == 0 {
            bail!(
                "preview image size must be non-zero, got {}x{}",
                self.width,
                self.height
            );
        }

        let format = match self.format.trim() {
            "gray8" => PreviewFormat::Gray8,
            "rgba8" => PreviewFormat::Rgba8,
            other => bail!("unsupported preview image format: {other}"),
        };

        let expected_len = expected_preview_len(self.width, self.height, format)?;
        if self.pixels.len() != expected_len {
            bail!(
                "preview image byte count {} does not match image size {}x{} with format {}",
                self.pixels.len(),
                self.width,
                self.height,
                self.format
            );
        }

        Ok(PreviewImage {
            width: self.width,
            height: self.height,
            pixels: self.pixels,
            format,
        })
    }
}

impl PreservedSourceMetadata {
    fn into_source_metadata(self) -> Result<SourceMetadata> {
        if self.study_instance_uid.trim().is_empty() {
            bail!("study instance uid is required");
        }

        let preserved_elements = self
            .preserved_elements
            .into_iter()
            .map(PreservedSourceElement::into_data_element)
            .collect::<Result<Vec<_>>>()?;

        Ok(SourceMetadata::from_preserved_elements(
            self.study_instance_uid,
            preserved_elements,
        ))
    }
}

impl PreservedSourceElement {
    fn into_data_element(self) -> Result<DataElement<InMemDicomObject>> {
        let vr = parse_supported_string_vr(&self.vr)?;
        let value = if self.values.len() == 1 {
            PrimitiveValue::Str(self.values.into_iter().next().unwrap_or_default())
        } else {
            PrimitiveValue::Strs(self.values.into())
        };

        Ok(DataElement::new(
            Tag(self.tag_group, self.tag_element),
            vr,
            DicomValue::Primitive(value),
        ))
    }
}

fn expected_preview_len(width: u32, height: u32, format: PreviewFormat) -> Result<usize> {
    let channels = match format {
        PreviewFormat::Gray8 => 1_u64,
        PreviewFormat::Rgba8 => 4_u64,
    };

    let len = u64::from(width)
        .checked_mul(u64::from(height))
        .and_then(|value| value.checked_mul(channels))
        .context("preview image dimensions overflow")?;

    usize::try_from(len).context("preview image byte count exceeds platform limits")
}

fn parse_supported_string_vr(vr: &str) -> Result<VR> {
    match vr.trim().to_ascii_uppercase().as_str() {
        "AE" => Ok(VR::AE),
        "AS" => Ok(VR::AS),
        "CS" => Ok(VR::CS),
        "DA" => Ok(VR::DA),
        "DS" => Ok(VR::DS),
        "DT" => Ok(VR::DT),
        "IS" => Ok(VR::IS),
        "LO" => Ok(VR::LO),
        "LT" => Ok(VR::LT),
        "PN" => Ok(VR::PN),
        "SH" => Ok(VR::SH),
        "ST" => Ok(VR::ST),
        "TM" => Ok(VR::TM),
        "UC" => Ok(VR::UC),
        "UI" => Ok(VR::UI),
        "UR" => Ok(VR::UR),
        "UT" => Ok(VR::UT),
        other => bail!("unsupported preserved element VR: {other}"),
    }
}

#[cfg(test)]
mod tests {
    use super::{
        ExportPreviewImage, PreservedSourceElement, PreservedSourceMetadata,
        SecondaryCaptureExportRequest, write_secondary_capture_request,
    };
    use crate::study::decode_helper::decode_source_study;

    #[test]
    fn write_secondary_capture_request_writes_round_trip_output() {
        let temp_dir = tempfile::TempDir::new().expect("temp dir");
        let output_path = temp_dir.path().join("secondary-capture.dcm");

        write_secondary_capture_request(
            SecondaryCaptureExportRequest {
                preview: ExportPreviewImage {
                    width: 2,
                    height: 2,
                    format: "gray8".to_string(),
                    pixels: vec![0, 64, 128, 255],
                },
                metadata: PreservedSourceMetadata {
                    study_instance_uid: "1.2.3.4.5".to_string(),
                    preserved_elements: vec![
                        PreservedSourceElement {
                            tag_group: 0x0010,
                            tag_element: 0x0010,
                            vr: "PN".to_string(),
                            values: vec!["Helper^Patient".to_string()],
                        },
                        PreservedSourceElement {
                            tag_group: 0x0028,
                            tag_element: 0x0030,
                            vr: "DS".to_string(),
                            values: vec!["0.20".to_string(), "0.30".to_string()],
                        },
                    ],
                },
            },
            &output_path,
        )
        .expect("write secondary capture request");

        let decoded = decode_source_study(&output_path).expect("decode helper output");
        assert_eq!(decoded.image.width, 2);
        assert_eq!(decoded.image.height, 2);
        assert_eq!(decoded.metadata.study_instance_uid, "1.2.3.4.5");
        assert_eq!(decoded.metadata.preserved_elements.len(), 2);
        assert_eq!(decoded.metadata.preserved_elements[0].vr, "PN");
        assert_eq!(
            decoded.metadata.preserved_elements[0].values,
            ["Helper^Patient"]
        );

        let scale = decoded.measurement_scale.expect("measurement scale");
        assert_eq!(scale.row_spacing_mm, 0.2);
        assert_eq!(scale.column_spacing_mm, 0.3);
    }

    #[test]
    fn write_secondary_capture_request_rejects_unknown_preview_format() {
        let temp_dir = tempfile::TempDir::new().expect("temp dir");
        let output_path = temp_dir.path().join("secondary-capture.dcm");

        let error = write_secondary_capture_request(
            SecondaryCaptureExportRequest {
                preview: ExportPreviewImage {
                    width: 1,
                    height: 1,
                    format: "gray16".to_string(),
                    pixels: vec![128],
                },
                metadata: PreservedSourceMetadata {
                    study_instance_uid: "1.2.3.4.5".to_string(),
                    preserved_elements: Vec::new(),
                },
            },
            &output_path,
        )
        .expect_err("unknown preview format should fail");

        assert!(
            error
                .to_string()
                .contains("unsupported preview image format"),
            "unexpected error: {error}"
        );
    }

    #[test]
    fn write_secondary_capture_request_rejects_unsupported_vr() {
        let temp_dir = tempfile::TempDir::new().expect("temp dir");
        let output_path = temp_dir.path().join("secondary-capture.dcm");

        let error = write_secondary_capture_request(
            SecondaryCaptureExportRequest {
                preview: ExportPreviewImage {
                    width: 1,
                    height: 1,
                    format: "gray8".to_string(),
                    pixels: vec![128],
                },
                metadata: PreservedSourceMetadata {
                    study_instance_uid: "1.2.3.4.5".to_string(),
                    preserved_elements: vec![PreservedSourceElement {
                        tag_group: 0x0028,
                        tag_element: 0x0100,
                        vr: "US".to_string(),
                        values: vec!["8".to_string()],
                    }],
                },
            },
            &output_path,
        )
        .expect_err("unsupported preserved element vr should fail");

        assert!(
            error
                .to_string()
                .contains("unsupported preserved element VR"),
            "unexpected error: {error}"
        );
    }
}
