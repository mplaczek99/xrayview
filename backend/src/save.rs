use std::path::Path;

use anyhow::{Context, Result};
use chrono::Utc;
use dicom_core::{DataElement, DicomValue, PrimitiveValue, Tag, VR};
use dicom_dictionary_std::tags;
use dicom_object::mem::InMemDicomObject;
use dicom_object::{FileDicomObject, meta::FileMetaTableBuilder};
use image::DynamicImage;
use uuid::Uuid;

use crate::study::source_image::SourceMetadata;

const SECONDARY_CAPTURE_SOP_CLASS_UID: &str = "1.2.840.10008.5.1.4.1.1.7";
const EXPLICIT_VR_LITTLE_ENDIAN: &str = "1.2.840.10008.1.2.1";
const IMPLEMENTATION_CLASS_UID: &str = "2.25.302043790172249692526321623266752743501";
const IMPLEMENTATION_VERSION_NAME: &str = "XRAYVIEW_1_0";
const DEFAULT_PROCESSED_SERIES_DESCRIPTION: &str = "XRayView Processed";

fn generate_uid() -> String {
    // 2.25.<uuid-as-u128> is a valid DICOM UID form and avoids maintaining a
    // private implementation root just for generated instances.
    let uuid = Uuid::new_v4();
    let value = u128::from_be_bytes(*uuid.as_bytes());
    format!("2.25.{value}")
}

pub(crate) fn save_dicom(
    img: &DynamicImage,
    source_meta: &SourceMetadata,
    output_path: &Path,
) -> Result<()> {
    let now = Utc::now();
    let sop_instance_uid = generate_uid();
    let series_instance_uid = generate_uid();

    let meta = FileMetaTableBuilder::new()
        .media_storage_sop_class_uid(SECONDARY_CAPTURE_SOP_CLASS_UID)
        .media_storage_sop_instance_uid(&sop_instance_uid)
        .transfer_syntax(EXPLICIT_VR_LITTLE_ENDIAN)
        .implementation_class_uid(IMPLEMENTATION_CLASS_UID)
        .implementation_version_name(IMPLEMENTATION_VERSION_NAME)
        .build()
        .context("build file meta table")?;

    let mut file_obj = FileDicomObject::new_empty_with_meta(meta);

    // Core identification elements for the secondary capture dataset.
    put_str(
        &mut file_obj,
        tags::SOP_CLASS_UID,
        VR::UI,
        SECONDARY_CAPTURE_SOP_CLASS_UID,
    );
    put_str(
        &mut file_obj,
        tags::SOP_INSTANCE_UID,
        VR::UI,
        &sop_instance_uid,
    );
    put_str(&mut file_obj, tags::MODALITY, VR::CS, "OT");
    put_strs(
        &mut file_obj,
        tags::IMAGE_TYPE,
        VR::CS,
        &["DERIVED", "SECONDARY"],
    );
    put_str(&mut file_obj, tags::CONVERSION_TYPE, VR::CS, "WSD");

    let date_str = now.format("%Y%m%d").to_string();
    let time_str = now.format("%H%M%S").to_string();
    put_str(
        &mut file_obj,
        tags::INSTANCE_CREATION_DATE,
        VR::DA,
        &date_str,
    );
    put_str(
        &mut file_obj,
        tags::INSTANCE_CREATION_TIME,
        VR::TM,
        &time_str,
    );
    put_str(&mut file_obj, tags::CONTENT_DATE, VR::DA, &date_str);
    put_str(&mut file_obj, tags::CONTENT_TIME, VR::TM, &time_str);

    put_str(
        &mut file_obj,
        tags::SERIES_DESCRIPTION,
        VR::LO,
        DEFAULT_PROCESSED_SERIES_DESCRIPTION,
    );
    put_str(
        &mut file_obj,
        tags::DERIVATION_DESCRIPTION,
        VR::ST,
        "Processed by XRayView",
    );
    put_str(&mut file_obj, tags::MANUFACTURER, VR::LO, "XRayView");
    put_str(
        &mut file_obj,
        tags::MANUFACTURER_MODEL_NAME,
        VR::LO,
        "xrayview",
    );
    put_str(&mut file_obj, tags::SOFTWARE_VERSIONS, VR::LO, "xrayview");
    put_str(
        &mut file_obj,
        tags::STUDY_INSTANCE_UID,
        VR::UI,
        source_meta.study_instance_uid(),
    );
    put_str(
        &mut file_obj,
        tags::SERIES_INSTANCE_UID,
        VR::UI,
        &series_instance_uid,
    );
    put_str(&mut file_obj, tags::SERIES_NUMBER, VR::IS, "999");
    put_str(&mut file_obj, tags::INSTANCE_NUMBER, VR::IS, "1");

    for elem in source_meta.preserved_elements() {
        file_obj.put(elem.clone());
    }

    // Encode pixel data.
    match img {
        DynamicImage::ImageLuma8(gray) => {
            let (width, height) = gray.dimensions();
            put_gray_image_elements(&mut file_obj, width as u16, height as u16);
            let pixel_elem: DataElement<InMemDicomObject> = DataElement::new(
                tags::PIXEL_DATA,
                VR::OW,
                DicomValue::Primitive(PrimitiveValue::U8(gray.as_raw().clone().into())),
            );
            file_obj.put(pixel_elem);
        }
        _ => {
            let rgba = img.to_rgba8();
            let (width, height) = rgba.dimensions();
            let rgb = rgba_to_rgb(rgba.as_raw());
            put_rgb_image_elements(&mut file_obj, width as u16, height as u16);
            let pixel_elem: DataElement<InMemDicomObject> = DataElement::new(
                tags::PIXEL_DATA,
                VR::OW,
                DicomValue::Primitive(PrimitiveValue::U8(rgb.into())),
            );
            file_obj.put(pixel_elem);
        }
    }

    file_obj
        .write_to_file(output_path)
        .with_context(|| format!("encode output image: {}", output_path.display()))?;

    Ok(())
}

fn put_str(obj: &mut FileDicomObject<InMemDicomObject>, tag: Tag, vr: VR, value: &str) {
    let elem: DataElement<InMemDicomObject> = DataElement::new(
        tag,
        vr,
        DicomValue::Primitive(PrimitiveValue::Str(value.to_string())),
    );
    obj.put(elem);
}

fn put_strs(obj: &mut FileDicomObject<InMemDicomObject>, tag: Tag, vr: VR, values: &[&str]) {
    let elem: DataElement<InMemDicomObject> = DataElement::new(
        tag,
        vr,
        DicomValue::Primitive(PrimitiveValue::Strs(
            values.iter().map(|s| s.to_string()).collect(),
        )),
    );
    obj.put(elem);
}

fn put_u16(obj: &mut FileDicomObject<InMemDicomObject>, tag: Tag, value: u16) {
    let elem: DataElement<InMemDicomObject> = DataElement::new(
        tag,
        VR::US,
        DicomValue::Primitive(PrimitiveValue::from(value)),
    );
    obj.put(elem);
}

fn put_gray_image_elements(obj: &mut FileDicomObject<InMemDicomObject>, width: u16, height: u16) {
    put_u16(obj, tags::ROWS, height);
    put_u16(obj, tags::COLUMNS, width);
    put_u16(obj, tags::SAMPLES_PER_PIXEL, 1);
    put_str(obj, tags::PHOTOMETRIC_INTERPRETATION, VR::CS, "MONOCHROME2");
    put_u16(obj, tags::BITS_ALLOCATED, 8);
    put_u16(obj, tags::BITS_STORED, 8);
    put_u16(obj, tags::HIGH_BIT, 7);
    put_u16(obj, tags::PIXEL_REPRESENTATION, 0);
    put_str(obj, tags::WINDOW_CENTER, VR::DS, "127.5");
    put_str(obj, tags::WINDOW_WIDTH, VR::DS, "255");
}

fn put_rgb_image_elements(obj: &mut FileDicomObject<InMemDicomObject>, width: u16, height: u16) {
    put_u16(obj, tags::ROWS, height);
    put_u16(obj, tags::COLUMNS, width);
    put_u16(obj, tags::SAMPLES_PER_PIXEL, 3);
    put_str(obj, tags::PHOTOMETRIC_INTERPRETATION, VR::CS, "RGB");
    put_u16(obj, tags::PLANAR_CONFIGURATION, 0);
    put_u16(obj, tags::BITS_ALLOCATED, 8);
    put_u16(obj, tags::BITS_STORED, 8);
    put_u16(obj, tags::HIGH_BIT, 7);
    put_u16(obj, tags::PIXEL_REPRESENTATION, 0);
}

fn rgba_to_rgb(rgba: &[u8]) -> Vec<u8> {
    let pixel_count = rgba.len() / 4;
    let mut rgb = Vec::with_capacity(pixel_count * 3);
    // Secondary Capture stores packed RGB triples; alpha only matters to the
    // UI preview path and is intentionally dropped when writing DICOM.
    for chunk in rgba.chunks_exact(4) {
        rgb.push(chunk[0]);
        rgb.push(chunk[1]);
        rgb.push(chunk[2]);
    }
    rgb
}

#[cfg(test)]
mod tests {
    use super::*;
    use dicom_object::{DefaultDicomObject, DicomAttribute, DicomObject, OpenFileOptions};
    use image::{DynamicImage, GrayImage, Luma};
    use tempfile::TempDir;

    #[test]
    fn round_trip_grayscale_preserves_metadata() {
        let temp_dir = TempDir::new().unwrap();
        let output_path = temp_dir.path().join("test_output.dcm");

        // Create a synthetic 64x64 gray image.
        let gray = GrayImage::from_fn(64, 64, |x, y| Luma([((x + y) % 256) as u8]));
        let img = DynamicImage::ImageLuma8(gray);

        // Create a minimal in-memory source DICOM with PatientName and StudyInstanceUID.
        let meta = FileMetaTableBuilder::new()
            .transfer_syntax(EXPLICIT_VR_LITTLE_ENDIAN)
            .build()
            .unwrap();
        let mut source: DefaultDicomObject = FileDicomObject::new_empty_with_meta(meta);
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

        let source_meta = SourceMetadata::extract(&source);
        save_dicom(&img, &source_meta, &output_path).expect("save_dicom should succeed");

        // Reopen and verify.
        let reopened = OpenFileOptions::new()
            .open_file(&output_path)
            .expect("reopen saved DICOM");

        assert_eq!(reopened.attr(tags::ROWS).unwrap().to_u16().unwrap(), 64);
        assert_eq!(reopened.attr(tags::COLUMNS).unwrap().to_u16().unwrap(), 64);
        assert_eq!(
            reopened
                .attr(tags::PHOTOMETRIC_INTERPRETATION)
                .unwrap()
                .to_str()
                .unwrap()
                .trim(),
            "MONOCHROME2"
        );
        assert_eq!(
            reopened
                .attr(tags::PATIENT_NAME)
                .unwrap()
                .to_str()
                .unwrap()
                .trim(),
            "Test^Patient"
        );
        assert_eq!(
            reopened
                .attr(tags::SAMPLES_PER_PIXEL)
                .unwrap()
                .to_u16()
                .unwrap(),
            1
        );
        assert_eq!(
            reopened
                .attr(tags::STUDY_INSTANCE_UID)
                .unwrap()
                .to_str()
                .unwrap()
                .trim(),
            "1.2.3.4.5.6.7.8.9"
        );
        assert_eq!(
            reopened
                .attr(tags::SOP_CLASS_UID)
                .unwrap()
                .to_str()
                .unwrap()
                .trim(),
            SECONDARY_CAPTURE_SOP_CLASS_UID
        );
    }
}
