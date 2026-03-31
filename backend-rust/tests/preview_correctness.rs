use std::path::{Path, PathBuf};
use std::process::Command;

use tempfile::TempDir;

#[test]
fn plain_processed_pseudocolor_and_compare_preview_match_go() {
    let repo_root = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .expect("backend-rust lives under the repo root")
        .to_path_buf();
    let sample = repo_root.join("images/sample-dental-radiograph.dcm");
    let temp_dir = TempDir::new().expect("create temp dir");

    compare_preview_against_go(
        &repo_root,
        &sample,
        &[],
        &temp_dir.path().join("sample-go.png"),
        &temp_dir.path().join("sample-rust.png"),
    );

    compare_preview_against_go(
        &repo_root,
        &sample,
        &["-compare"],
        &temp_dir.path().join("sample-compare-go.png"),
        &temp_dir.path().join("sample-compare-rust.png"),
    );

    compare_preview_against_go(
        &repo_root,
        &sample,
        &[
            "-invert",
            "-brightness",
            "18",
            "-contrast",
            "1.35",
            "-equalize",
            "-pipeline",
            "contrast,invert,brightness,equalize",
        ],
        &temp_dir.path().join("sample-processed-go.png"),
        &temp_dir.path().join("sample-processed-rust.png"),
    );

    compare_preview_against_go(
        &repo_root,
        &sample,
        &[
            "-invert",
            "-brightness",
            "18",
            "-contrast",
            "1.35",
            "-equalize",
            "-pipeline",
            "contrast,invert,brightness,equalize",
            "-compare",
        ],
        &temp_dir.path().join("sample-processed-compare-go.png"),
        &temp_dir.path().join("sample-processed-compare-rust.png"),
    );

    compare_preview_against_go(
        &repo_root,
        &sample,
        &["-palette", "hot"],
        &temp_dir.path().join("sample-hot-go.png"),
        &temp_dir.path().join("sample-hot-rust.png"),
    );

    compare_preview_against_go(
        &repo_root,
        &sample,
        &["-preset", "xray"],
        &temp_dir.path().join("sample-xray-go.png"),
        &temp_dir.path().join("sample-xray-rust.png"),
    );

    compare_preview_against_go(
        &repo_root,
        &sample,
        &["-preset", "xray", "-compare"],
        &temp_dir.path().join("sample-xray-compare-go.png"),
        &temp_dir.path().join("sample-xray-compare-rust.png"),
    );

    let derived_grayscale_dicom = temp_dir.path().join("derived-high-contrast.dcm");
    run_command(
        Command::new("go")
            .current_dir(&repo_root)
            .arg("run")
            .arg("./cmd/xrayview")
            .arg("-input")
            .arg(&sample)
            .arg("-output")
            .arg(&derived_grayscale_dicom)
            .arg("-preset")
            .arg("high-contrast"),
    );

    compare_preview_against_go(
        &repo_root,
        &derived_grayscale_dicom,
        &[],
        &temp_dir.path().join("derived-go.png"),
        &temp_dir.path().join("derived-rust.png"),
    );

    compare_preview_against_go(
        &repo_root,
        &derived_grayscale_dicom,
        &["-compare"],
        &temp_dir.path().join("derived-compare-go.png"),
        &temp_dir.path().join("derived-compare-rust.png"),
    );

    compare_preview_against_go(
        &repo_root,
        &derived_grayscale_dicom,
        &[
            "-brightness",
            "7",
            "-contrast",
            "1.15",
            "-pipeline",
            "brightness,contrast",
        ],
        &temp_dir.path().join("derived-processed-go.png"),
        &temp_dir.path().join("derived-processed-rust.png"),
    );

    compare_preview_against_go(
        &repo_root,
        &derived_grayscale_dicom,
        &["-palette", "bone"],
        &temp_dir.path().join("derived-bone-go.png"),
        &temp_dir.path().join("derived-bone-rust.png"),
    );
}

/// Converts a single-dash Go-style flag to a double-dash Rust/clap flag.
/// Values (non-flag arguments) are passed through unchanged.
fn go_flag_to_clap(arg: &str) -> String {
    if arg.starts_with('-') && !arg.starts_with("--") {
        format!("-{arg}")
    } else {
        arg.to_string()
    }
}

fn compare_preview_against_go(
    repo_root: &Path,
    input: &Path,
    extra_args: &[&str],
    go_output: &Path,
    rust_output: &Path,
) {
    let mut go_command = Command::new("go");
    go_command
        .current_dir(repo_root)
        .arg("run")
        .arg("./cmd/xrayview")
        .arg("-input")
        .arg(input)
        .arg("-preview-output")
        .arg(go_output)
        .args(extra_args);
    run_command(&mut go_command);

    let rust_extra: Vec<String> = extra_args.iter().map(|a| go_flag_to_clap(a)).collect();
    let mut rust_command = Command::new(env!("CARGO_BIN_EXE_xrayview-backend-rust"));
    rust_command
        .current_dir(repo_root)
        .arg("--input")
        .arg(input)
        .arg("--preview-output")
        .arg(rust_output)
        .args(&rust_extra);
    run_command(&mut rust_command);

    let go_image = image::open(go_output)
        .unwrap_or_else(|error| panic!("decode Go preview {}: {error}", go_output.display()))
        .into_rgba8();
    let rust_image = image::open(rust_output)
        .unwrap_or_else(|error| panic!("decode Rust preview {}: {error}", rust_output.display()))
        .into_rgba8();

    assert_eq!(rust_image.dimensions(), go_image.dimensions());
    assert_eq!(rust_image.as_raw(), go_image.as_raw());
}

fn run_command(command: &mut Command) {
    let status = command.status().expect("spawn command");
    assert!(status.success(), "command failed with status {status}");
}
