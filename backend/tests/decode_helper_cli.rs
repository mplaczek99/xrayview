use std::path::{Path, PathBuf};
use std::process::Command;

use serde::Deserialize;

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct DecodedSourceStudy {
    image: DecodedSourceImage,
    metadata: DecodedMetadata,
    measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct DecodedSourceImage {
    width: u32,
    height: u32,
    pixels: Vec<f32>,
    default_window: Option<DecodedWindowLevel>,
    invert: bool,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct DecodedWindowLevel {
    center: f32,
    width: f32,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct DecodedMetadata {
    study_instance_uid: String,
    preserved_elements: Vec<DecodedElement>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct DecodedElement {
    tag_group: u16,
    tag_element: u16,
    vr: String,
    values: Vec<String>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
#[allow(dead_code)]
struct MeasurementScale {
    row_spacing_mm: f64,
    column_spacing_mm: f64,
    source: String,
}

fn sample_dicom_path() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../images/sample-dental-radiograph.dcm")
}

#[test]
fn decode_helper_emits_normalized_source_study_json() {
    let sample = sample_dicom_path();
    assert!(
        sample.is_file(),
        "sample fixture missing: {}",
        sample.display()
    );

    let output = Command::new(env!("CARGO_BIN_EXE_xrayview-decode-helper"))
        .arg("--input")
        .arg(&sample)
        .output()
        .expect("run decode helper");

    assert!(
        output.status.success(),
        "decode helper failed\nstdout:\n{}\nstderr:\n{}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );

    let payload: DecodedSourceStudy =
        serde_json::from_slice(&output.stdout).expect("parse decode helper stdout as json");

    assert_eq!(payload.image.width, 2048);
    assert_eq!(payload.image.height, 1088);
    assert_eq!(payload.image.pixels.len(), (2048_u32 * 1088_u32) as usize);
    assert_eq!(
        payload
            .image
            .default_window
            .as_ref()
            .expect("default window")
            .center,
        127.5
    );
    assert_eq!(
        payload
            .image
            .default_window
            .as_ref()
            .expect("default window")
            .width,
        255.0
    );
    assert!(!payload.image.invert);
    assert!(payload.measurement_scale.is_none());
    assert!(!payload.metadata.study_instance_uid.is_empty());
    for element in &payload.metadata.preserved_elements {
        assert!(element.tag_group != 0 || element.tag_element != 0);
        assert!(!element.vr.is_empty());
        assert!(!element.values.is_empty());
    }
}
