use std::fs;
use std::path::{Path, PathBuf};

use serde_json::json;
use xrayview_backend::api::contracts::{
    AnnotationPointDto, AnnotationSourceDto, LineAnnotationDto, LineMeasurementDto,
};
use xrayview_backend::api::{
    BACKEND_CONTRACT_VERSION, JobKind, JobProgress, JobResult, JobSnapshot, JobState,
    MeasureLineAnnotationCommandResult, MeasurementScale, RenderStudyCommandResult,
    generated_typescript_contracts,
};
use xrayview_backend::error::BackendError;

#[test]
fn generated_typescript_contracts_match_committed_frontend_contracts() {
    let generated = generated_typescript_contracts();
    let committed_path = repo_root().join("frontend/src/lib/generated/contracts.ts");
    let committed = fs::read_to_string(&committed_path).unwrap_or_else(|error| {
        panic!(
            "read committed TypeScript contracts {}: {error}",
            committed_path.display()
        )
    });

    assert_eq!(
        generated,
        committed,
        "generated TypeScript contracts drifted from {}",
        committed_path.display()
    );
}

#[test]
fn backend_contract_version_is_v1() {
    assert_eq!(
        BACKEND_CONTRACT_VERSION, 1,
        "phase 2 freezes the desktop/backend contract at version 1"
    );
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
}

fn repo_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .expect("backend crate should live under the repo root")
        .to_path_buf()
}
