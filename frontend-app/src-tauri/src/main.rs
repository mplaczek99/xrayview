#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use rfd::FileDialog;
use serde::{Deserialize, Serialize};
use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;
use tauri::Manager;
use tempfile::Builder;

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct ProcessingOptions {
    brightness: i32,
    contrast: f64,
    invert: bool,
    equalize: bool,
    palette: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct PreviewResponse {
    preview_path: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct ProcessResponse {
    preview_path: String,
    dicom_path: String,
}

#[derive(Debug)]
struct BackendSpec {
    program: String,
    prefix_args: Vec<String>,
    working_directory: PathBuf,
}

#[tauri::command]
fn pick_dicom_file() -> Option<String> {
    FileDialog::new()
        .add_filter("DICOM", &["dcm", "dicom"])
        .pick_file()
        .map(path_to_string)
}

#[tauri::command]
fn pick_save_dicom_path(default_name: Option<String>) -> Option<String> {
    let mut dialog = FileDialog::new().add_filter("DICOM", &["dcm", "dicom"]);
    if let Some(name) = default_name {
        dialog = dialog.set_file_name(&name);
    }

    dialog.save_file().map(path_to_string)
}

#[tauri::command]
fn run_backend_preview(input_path: String) -> Result<PreviewResponse, String> {
    let preview_path = create_temp_file(".png")?;

    let mut args = vec![
        "-input".to_string(),
        input_path,
        "-preview-output".to_string(),
        path_to_string(preview_path.clone()),
    ];

    run_backend_command(&mut args)?;

    Ok(PreviewResponse {
        preview_path: path_to_string(preview_path),
    })
}

#[tauri::command]
fn run_backend_process(
    input_path: String,
    options: ProcessingOptions,
) -> Result<ProcessResponse, String> {
    let preview_path = create_temp_file(".png")?;
    let dicom_path = create_temp_file(".dcm")?;

    let mut args = vec![
        "-input".to_string(),
        input_path,
        "-output".to_string(),
        path_to_string(dicom_path.clone()),
        "-preview-output".to_string(),
        path_to_string(preview_path.clone()),
        format!("-invert={}", options.invert),
        format!("-brightness={}", options.brightness),
        format!("-contrast={}", options.contrast),
        format!("-equalize={}", options.equalize),
        format!("-palette={}", options.palette),
    ];

    run_backend_command(&mut args)?;

    Ok(ProcessResponse {
        preview_path: path_to_string(preview_path),
        dicom_path: path_to_string(dicom_path),
    })
}

#[tauri::command]
fn copy_processed_output(source_path: String, destination_path: String) -> Result<String, String> {
    let source = PathBuf::from(&source_path);
    let destination = PathBuf::from(&destination_path);

    fs::copy(&source, &destination)
        .map_err(|error| format!("failed to copy processed DICOM: {error}"))?;

    Ok(path_to_string(destination))
}

fn create_temp_file(suffix: &str) -> Result<PathBuf, String> {
    let temp_file = Builder::new()
        .prefix("xrayview-frontend-")
        .suffix(suffix)
        .tempfile()
        .map_err(|error| format!("failed to create temporary file: {error}"))?;

    let (_file, path) = temp_file
        .keep()
        .map_err(|error| format!("failed to persist temporary file: {error}"))?;

    Ok(path)
}

fn run_backend_command(args: &mut Vec<String>) -> Result<(), String> {
    let backend = resolve_backend_spec()?;
    let mut command = Command::new(&backend.program);
    command.current_dir(&backend.working_directory);
    command.args(&backend.prefix_args);
    command.args(args);

    let output = command
        .output()
        .map_err(|error| format!("failed to start backend command: {error}"))?;

    if output.status.success() {
        return Ok(());
    }

    let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
    let stdout = String::from_utf8_lossy(&output.stdout).trim().to_string();
    let message = if !stderr.is_empty() {
        stderr
    } else if !stdout.is_empty() {
        stdout
    } else {
        format!("backend exited with status {}", output.status)
    };

    Err(message)
}

fn resolve_backend_spec() -> Result<BackendSpec, String> {
    if let Ok(configured_path) = env::var("XRAYVIEW_BACKEND_PATH") {
        let binary = PathBuf::from(configured_path);
        if binary.is_file() {
            let working_directory =
                find_project_root(&env::current_dir().unwrap_or_else(|_| PathBuf::from(".")))
                    .unwrap_or_else(|| env::current_dir().unwrap_or_else(|_| PathBuf::from(".")));

            return Ok(BackendSpec {
                program: path_to_string(binary),
                prefix_args: Vec::new(),
                working_directory,
            });
        }
    }

    let search_roots = vec![
        env::current_dir().unwrap_or_else(|_| PathBuf::from(".")),
        env::current_exe()
            .ok()
            .and_then(|path| path.parent().map(Path::to_path_buf))
            .unwrap_or_else(|| PathBuf::from(".")),
    ];

    for root in search_roots {
        if let Some(project_root) = find_project_root(&root) {
            for candidate in backend_binary_candidates(&project_root) {
                if candidate.is_file() {
                    return Ok(BackendSpec {
                        program: path_to_string(candidate),
                        prefix_args: Vec::new(),
                        working_directory: project_root,
                    });
                }
            }

            return Ok(BackendSpec {
                program: "go".to_string(),
                prefix_args: vec!["run".to_string(), "./cmd/xrayview".to_string()],
                working_directory: project_root,
            });
        }
    }

    Err(
        "could not locate the Go backend; set XRAYVIEW_BACKEND_PATH or run from the repository"
            .to_string(),
    )
}

fn find_project_root(start: &Path) -> Option<PathBuf> {
    let mut current = if start.is_file() {
        start.parent().map(Path::to_path_buf)?
    } else {
        start.to_path_buf()
    };

    loop {
        if current.join("cmd").join("xrayview").is_dir() {
            return Some(current);
        }

        if !current.pop() {
            return None;
        }
    }
}

fn backend_binary_candidates(project_root: &Path) -> Vec<PathBuf> {
    #[cfg(target_os = "windows")]
    {
        vec![project_root.join("xrayview.exe")]
    }

    #[cfg(not(target_os = "windows"))]
    {
        vec![project_root.join("xrayview")]
    }
}

fn path_to_string(path: PathBuf) -> String {
    path.to_string_lossy().to_string()
}

fn main() {
    tauri::Builder::default()
        .setup(|app| {
            let _window = app.get_webview_window("main");
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            pick_dicom_file,
            pick_save_dicom_path,
            run_backend_preview,
            run_backend_process,
            copy_processed_output,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
