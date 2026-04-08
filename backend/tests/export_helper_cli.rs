use std::path::PathBuf;
use std::process::{Command, Stdio};

use dicom_dictionary_std::tags;
use dicom_object::OpenFileOptions;
use serde_json::json;

#[test]
fn export_helper_reads_request_from_stdin_and_writes_secondary_capture() {
    let temp_dir = tempfile::TempDir::new().expect("temp dir");
    let output_path: PathBuf = temp_dir.path().join("helper-output.dcm");
    let request = json!({
        "preview": {
            "width": 2,
            "height": 2,
            "format": "gray8",
            "pixels": [0, 64, 128, 255]
        },
        "metadata": {
            "studyInstanceUid": "1.2.3.4.5",
            "preservedElements": [
                {
                    "tagGroup": 0x0010,
                    "tagElement": 0x0010,
                    "vr": "PN",
                    "values": ["Helper^Patient"]
                }
            ]
        }
    });

    let mut child = Command::new(env!("CARGO_BIN_EXE_xrayview-export-helper"))
        .arg("--output")
        .arg(&output_path)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .expect("spawn export helper");

    serde_json::to_writer(child.stdin.take().as_mut().expect("helper stdin"), &request)
        .expect("write request");

    let output = child.wait_with_output().expect("wait for export helper");
    assert!(
        output.status.success(),
        "export helper failed\nstdout:\n{}\nstderr:\n{}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );
    assert!(
        output.stdout.is_empty(),
        "export helper should not emit stdout on success: {}",
        String::from_utf8_lossy(&output.stdout)
    );

    let object = OpenFileOptions::new()
        .open_file(&output_path)
        .expect("open helper output");

    let patient_name = object
        .element(tags::PATIENT_NAME)
        .expect("patient name")
        .to_str()
        .expect("patient name string");
    assert_eq!(patient_name.trim(), "Helper^Patient");

    let study_instance_uid = object
        .element(tags::STUDY_INSTANCE_UID)
        .expect("study instance uid")
        .to_str()
        .expect("study instance uid string");
    assert_eq!(study_instance_uid.trim(), "1.2.3.4.5");
}
