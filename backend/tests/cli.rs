use std::ffi::OsString;
use std::path::{Path, PathBuf};
use std::process::Command;

use serde_json::Value;
use tempfile::TempDir;

fn sample_dicom_path() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../images/sample-dental-radiograph.dcm")
}

fn run_backend(args: Vec<OsString>) -> String {
    let output = Command::new(env!("CARGO_BIN_EXE_xrayview-backend"))
        .args(args)
        .output()
        .expect("run backend binary");

    assert!(
        output.status.success(),
        "backend command failed\nstdout:\n{}\nstderr:\n{}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );

    String::from_utf8(output.stdout).expect("backend stdout should be valid utf-8")
}

#[test]
fn describe_commands_return_json() {
    let sample = sample_dicom_path();
    assert!(
        sample.is_file(),
        "sample fixture missing: {}",
        sample.display()
    );

    let manifest_stdout = run_backend(vec![OsString::from("--describe-presets")]);
    let manifest: Value = serde_json::from_str(&manifest_stdout).expect("parse preset manifest");
    assert_eq!(manifest["defaultPresetId"], "default");
    assert_eq!(manifest["presets"].as_array().map(Vec::len), Some(3));

    let study_stdout = run_backend(vec![
        OsString::from("--input"),
        sample.into_os_string(),
        OsString::from("--describe-study"),
    ]);
    let study: Value = serde_json::from_str(&study_stdout).expect("parse study description");
    assert!(
        study.is_object(),
        "study description should be a JSON object"
    );
}

#[test]
fn preview_and_process_commands_write_expected_artifacts() {
    let sample = sample_dicom_path();
    let temp_dir = TempDir::new().expect("create temp dir");
    let preview_path = temp_dir.path().join("preview.png");
    let processed_preview_path = temp_dir.path().join("processed-preview.png");
    let output_path = temp_dir.path().join("processed.dcm");

    let preview_stdout = run_backend(vec![
        OsString::from("--input"),
        sample.clone().into_os_string(),
        OsString::from("--preview-output"),
        preview_path.clone().into_os_string(),
    ]);
    assert!(preview_path.is_file(), "preview png should be written");
    assert!(preview_stdout.contains("loaded dicom image:"));
    assert!(preview_stdout.contains("saved grayscale preview image:"));

    let process_stdout = run_backend(vec![
        OsString::from("--input"),
        sample.into_os_string(),
        OsString::from("--preview-output"),
        processed_preview_path.clone().into_os_string(),
        OsString::from("--output"),
        output_path.clone().into_os_string(),
        OsString::from("--preset"),
        OsString::from("xray"),
    ]);
    assert!(
        processed_preview_path.is_file(),
        "processed preview png should be written"
    );
    assert!(output_path.is_file(), "processed dicom should be written");
    assert!(process_stdout.contains("loaded dicom image:"));
    assert!(process_stdout.contains("preview image:"));
    assert!(process_stdout.contains("dicom image:"));
}

#[test]
fn analyze_tooth_command_returns_json_and_preview() {
    let sample = sample_dicom_path();
    let temp_dir = TempDir::new().expect("create temp dir");
    let preview_path = temp_dir.path().join("analysis-preview.png");

    let stdout = run_backend(vec![
        OsString::from("--input"),
        sample.into_os_string(),
        OsString::from("--preview-output"),
        preview_path.clone().into_os_string(),
        OsString::from("--analyze-tooth"),
    ]);

    assert!(
        preview_path.is_file(),
        "analysis preview png should be written"
    );

    let analysis: Value = serde_json::from_str(&stdout).expect("parse tooth analysis");
    assert!(analysis["image"]["width"].is_number());
    assert!(analysis["image"]["height"].is_number());
    assert!(analysis["warnings"].is_array());
    assert!(
        analysis.get("tooth").is_some(),
        "analysis should include a tooth field even when null"
    );
}
