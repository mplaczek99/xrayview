use std::fs;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};

use serde_json::{Value, json};
use xrayview_backend::api::contracts::{
    AnnotationPointDto, AnnotationSourceDto, LineAnnotationDto, LineMeasurementDto,
};
use xrayview_backend::api::{
    BACKEND_CONTRACT_VERSION, JobKind, JobProgress, JobResult, JobSnapshot, JobState,
    MeasureLineAnnotationCommandResult, MeasurementScale, OpenStudyCommandResult,
    RenderStudyCommandResult, StudyRecord,
};
use xrayview_backend::app::processing_manifest;
use xrayview_backend::error::BackendError;

#[test]
fn schema_generated_contract_bindings_match_committed_files() {
    run_node_script(
        "contracts/scripts/generate-contract-bindings.mjs",
        &["--check"],
        None,
    );
}

#[test]
fn backend_contract_version_matches_schema_source_of_truth() {
    let schema_path = repo_root().join("contracts/backend-contract-v1.schema.json");
    let schema = fs::read_to_string(&schema_path)
        .unwrap_or_else(|error| panic!("read schema file {}: {error}", schema_path.display()));
    let value: Value = serde_json::from_str(&schema)
        .unwrap_or_else(|error| panic!("parse schema file {}: {error}", schema_path.display()));
    let schema_version = value
        .get("x-contract-version")
        .and_then(Value::as_u64)
        .unwrap_or_else(|| {
            panic!(
                "schema file {} is missing x-contract-version",
                schema_path.display()
            )
        });

    assert_eq!(
        BACKEND_CONTRACT_VERSION as u64, schema_version,
        "Rust must match the language-neutral contract version"
    );
}

#[test]
fn processing_manifest_validates_against_schema() {
    let manifest =
        serde_json::to_value(processing_manifest()).expect("serialize processing manifest");
    assert_schema_accepts("ProcessingManifest", &manifest);
}

#[test]
fn open_study_command_result_validates_against_schema() {
    let result = OpenStudyCommandResult {
        study: StudyRecord {
            study_id: String::from("study-1"),
            input_path: PathBuf::from("/tmp/sample-dental-radiograph.dcm"),
            input_name: String::from("sample-dental-radiograph.dcm"),
            measurement_scale: Some(MeasurementScale {
                row_spacing_mm: 0.1,
                column_spacing_mm: 0.1,
                source: "pixelSpacing",
            }),
        },
    };

    let value = serde_json::to_value(result).expect("serialize open study result");
    assert_schema_accepts("OpenStudyCommandResult", &value);
}

#[test]
fn backend_error_serialization_omits_empty_details() {
    let empty_details = serde_json::to_value(BackendError::invalid_input("bad input"))
        .expect("serialize backend error without details");
    assert_eq!(
        empty_details,
        json!({
            "code": "invalidInput",
            "message": "bad input",
            "recoverable": true
        })
    );
    assert_schema_accepts("BackendError", &empty_details);

    let populated_details =
        serde_json::to_value(BackendError::invalid_input("bad input").with_details(vec![
            String::from("brightness must be between -255 and 255"),
            String::from("contrast must be finite"),
        ]))
        .expect("serialize backend error with details");
    assert_eq!(
        populated_details,
        json!({
            "code": "invalidInput",
            "message": "bad input",
            "details": [
                "brightness must be between -255 and 255",
                "contrast must be finite"
            ],
            "recoverable": true
        })
    );
    assert_schema_accepts("BackendError", &populated_details);
}

#[test]
fn job_snapshot_serialization_uses_kind_payload_union_shape() {
    let snapshot = JobSnapshot {
        job_id: String::from("job-1"),
        job_kind: JobKind::RenderStudy,
        study_id: Some(String::from("study-1")),
        state: JobState::Completed,
        progress: JobProgress {
            percent: 100,
            stage: String::from("completed"),
            message: String::from("Preview ready"),
        },
        from_cache: false,
        result: Some(JobResult::RenderStudy(RenderStudyCommandResult {
            study_id: String::from("study-1"),
            preview_path: PathBuf::from("/tmp/render-preview.png"),
            loaded_width: 1200,
            loaded_height: 900,
            measurement_scale: Some(MeasurementScale {
                row_spacing_mm: 0.1,
                column_spacing_mm: 0.1,
                source: "pixelSpacing",
            }),
        })),
        error: None,
    };

    let value = serde_json::to_value(snapshot).expect("serialize completed job snapshot");
    assert_eq!(
        value,
        json!({
            "jobId": "job-1",
            "jobKind": "renderStudy",
            "studyId": "study-1",
            "state": "completed",
            "progress": {
                "percent": 100,
                "stage": "completed",
                "message": "Preview ready"
            },
            "fromCache": false,
            "result": {
                "kind": "renderStudy",
                "payload": {
                    "studyId": "study-1",
                    "previewPath": "/tmp/render-preview.png",
                    "loadedWidth": 1200,
                    "loadedHeight": 900,
                    "measurementScale": {
                        "rowSpacingMm": 0.1,
                        "columnSpacingMm": 0.1,
                        "source": "pixelSpacing"
                    }
                }
            }
        })
    );
    assert_schema_accepts("JobSnapshot", &value);
}

#[test]
fn measure_line_annotation_result_omits_absent_optional_fields() {
    let result = MeasureLineAnnotationCommandResult {
        study_id: String::from("study-1"),
        annotation: LineAnnotationDto {
            id: String::from("line-1"),
            label: String::from("Tooth width"),
            source: AnnotationSourceDto::Manual,
            start: AnnotationPointDto { x: 10.0, y: 20.0 },
            end: AnnotationPointDto { x: 110.0, y: 20.0 },
            editable: true,
            confidence: None,
            measurement: Some(LineMeasurementDto {
                pixel_length: 100.0,
                calibrated_length_mm: None,
            }),
        },
    };

    let value = serde_json::to_value(result).expect("serialize measurement result");
    assert_eq!(
        value,
        json!({
            "studyId": "study-1",
            "annotation": {
                "id": "line-1",
                "label": "Tooth width",
                "source": "manual",
                "start": { "x": 10.0, "y": 20.0 },
                "end": { "x": 110.0, "y": 20.0 },
                "editable": true,
                "measurement": {
                    "pixelLength": 100.0
                }
            }
        })
    );
    assert_schema_accepts("MeasureLineAnnotationCommandResult", &value);
}

fn assert_schema_accepts(definition_name: &str, value: &Value) {
    let input = serde_json::to_vec(value).expect("serialize JSON for schema validation");
    run_node_script(
        "contracts/scripts/validate-contract-value.mjs",
        &[definition_name],
        Some(&input),
    );
}

fn run_node_script(script_relative_path: &str, args: &[&str], stdin: Option<&[u8]>) {
    let repo_root = repo_root();
    let script_path = repo_root.join(script_relative_path);
    let mut command = Command::new("node");
    command
        .arg(&script_path)
        .args(args)
        .current_dir(&repo_root)
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());

    if stdin.is_some() {
        command.stdin(Stdio::piped());
    }

    let mut child = command
        .spawn()
        .unwrap_or_else(|error| panic!("spawn node script {}: {error}", script_path.display()));

    if let Some(stdin_bytes) = stdin {
        let mut handle = child
            .stdin
            .take()
            .unwrap_or_else(|| panic!("open stdin for {}", script_path.display()));
        handle
            .write_all(stdin_bytes)
            .unwrap_or_else(|error| panic!("write stdin for {}: {error}", script_path.display()));
    }

    let output = child
        .wait_with_output()
        .unwrap_or_else(|error| panic!("wait for node script {}: {error}", script_path.display()));

    if !output.status.success() {
        let stdout = String::from_utf8_lossy(&output.stdout);
        let stderr = String::from_utf8_lossy(&output.stderr);
        panic!(
            "node script {} failed with status {:?}\nstdout:\n{}\nstderr:\n{}",
            script_path.display(),
            output.status.code(),
            stdout.trim(),
            stderr.trim()
        );
    }
}

fn repo_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .expect("backend crate should live under the repo root")
        .to_path_buf()
}
