#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use rfd::FileDialog;
use serde::{Deserialize, Serialize};
use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::sync::Mutex;
use tauri::Manager;
use tauri_plugin_shell::ShellExt;
use tempfile::Builder;
use tokio::sync::Semaphore;

/// Tracks temp files so they can be cleaned up when replaced or at exit.
struct TempFileState {
    paths: Mutex<Vec<PathBuf>>,
}

impl TempFileState {
    fn new() -> Self {
        Self {
            paths: Mutex::new(Vec::new()),
        }
    }

    fn track(&self, path: PathBuf) {
        if let Ok(mut paths) = self.paths.lock() {
            paths.push(path);
        }
    }

    fn cleanup_all(&self) {
        if let Ok(mut paths) = self.paths.lock() {
            for path in paths.drain(..) {
                let _ = fs::remove_file(&path);
            }
        }
    }
}

/// Single-user desktop app — serial execution is intentional. This also
/// closes the TempFileState race where cleanup_all() could delete files
/// still in use by an overlapping request.
struct BackendGate {
    semaphore: Semaphore,
}

impl BackendGate {
    fn new() -> Self {
        Self {
            semaphore: Semaphore::new(1),
        }
    }
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct ProcessingOptions {
    brightness: i32,
    contrast: f64,
    invert: bool,
    equalize: bool,
    palette: String,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct ProcessingPreset {
    id: String,
    controls: ProcessingOptions,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct ProcessingManifest {
    default_preset_id: String,
    presets: Vec<ProcessingPreset>,
}

#[derive(Debug, Deserialize, Serialize, Clone)]
#[serde(rename_all = "camelCase")]
struct MeasurementScale {
    row_spacing_mm: f64,
    column_spacing_mm: f64,
    source: String,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct StudyDescription {
    measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct ToothImageMetadata {
    width: u32,
    height: u32,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct ToothCalibration {
    pixel_units: String,
    measurement_scale: Option<MeasurementScale>,
    real_world_measurements_available: bool,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct ToothMeasurementValues {
    tooth_width: f64,
    tooth_height: f64,
    bounding_box_width: f64,
    bounding_box_height: f64,
    units: String,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct ToothMeasurementBundle {
    pixel: ToothMeasurementValues,
    calibrated: Option<ToothMeasurementValues>,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct Point {
    x: u32,
    y: u32,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct LineSegment {
    start: Point,
    end: Point,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct BoundingBox {
    x: u32,
    y: u32,
    width: u32,
    height: u32,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct ToothGeometry {
    bounding_box: BoundingBox,
    width_line: LineSegment,
    height_line: LineSegment,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct ToothCandidate {
    confidence: f64,
    mask_area_pixels: u32,
    measurements: ToothMeasurementBundle,
    geometry: ToothGeometry,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct ToothAnalysis {
    image: ToothImageMetadata,
    calibration: ToothCalibration,
    tooth: Option<ToothCandidate>,
    warnings: Vec<String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct PreviewResponse {
    preview_path: String,
    measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct ProcessResponse {
    preview_path: String,
    dicom_path: String,
    measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct ToothMeasurementResponse {
    preview_path: String,
    analysis: ToothAnalysis,
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
        .set_title("Open DICOM Study")
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
async fn run_backend_preview(
    app: tauri::AppHandle,
    input_path: String,
) -> Result<PreviewResponse, String> {
    let gate = app.state::<BackendGate>();
    let _permit = gate
        .semaphore
        .acquire()
        .await
        .map_err(|e| format!("backend gate closed: {e}"))?;

    let temp_state = app.state::<TempFileState>();
    temp_state.cleanup_all();

    let preview_path = create_temp_file(".png")?;
    temp_state.track(preview_path.clone());

    let args = vec![
        "--input".to_string(),
        input_path,
        "--preview-output".to_string(),
        path_to_string(preview_path.clone()),
    ];

    let _ = run_backend_command(&app, &args).await?;

    Ok(PreviewResponse {
        preview_path: path_to_string(preview_path),
        measurement_scale: describe_study_if_available(&app, &args[1]).await,
    })
}

#[tauri::command]
async fn run_backend_process(
    app: tauri::AppHandle,
    args: Vec<String>,
) -> Result<ProcessResponse, String> {
    let gate = app.state::<BackendGate>();
    let _permit = gate
        .semaphore
        .acquire()
        .await
        .map_err(|e| format!("backend gate closed: {e}"))?;

    let temp_state = app.state::<TempFileState>();
    temp_state.cleanup_all();

    let preview_path = create_temp_file(".png")?;
    temp_state.track(preview_path.clone());
    let mut command_args = args;
    let dicom_path = match find_flag_value(&command_args, "--output") {
        Some(path) => PathBuf::from(path),
        None => {
            let temp_dicom_path = create_temp_file(".dcm")?;
            temp_state.track(temp_dicom_path.clone());
            command_args.push("--output".to_string());
            command_args.push(path_to_string(temp_dicom_path.clone()));
            temp_dicom_path
        }
    };
    command_args.push("--preview-output".to_string());
    command_args.push(path_to_string(preview_path.clone()));

    let _ = run_backend_command(&app, &command_args).await?;
    let dicom_path_string = path_to_string(dicom_path.clone());

    Ok(ProcessResponse {
        preview_path: path_to_string(preview_path),
        dicom_path: path_to_string(dicom_path),
        measurement_scale: describe_study_if_available(&app, &dicom_path_string).await,
    })
}

#[tauri::command]
async fn run_backend_tooth_measurement(
    app: tauri::AppHandle,
    input_path: String,
) -> Result<ToothMeasurementResponse, String> {
    let gate = app.state::<BackendGate>();
    let _permit = gate
        .semaphore
        .acquire()
        .await
        .map_err(|e| format!("backend gate closed: {e}"))?;

    let temp_state = app.state::<TempFileState>();
    temp_state.cleanup_all();

    let preview_path = create_temp_file(".png")?;
    temp_state.track(preview_path.clone());
    let preview_path_string = path_to_string(preview_path.clone());

    let stdout = run_backend_command(
        &app,
        &[
            "--input".to_string(),
            input_path,
            "--preview-output".to_string(),
            preview_path_string.clone(),
            "--analyze-tooth".to_string(),
        ],
    )
    .await?;

    let analysis = serde_json::from_str(&stdout)
        .map_err(|error| format!("failed to parse backend tooth analysis: {error}"))?;

    Ok(ToothMeasurementResponse {
        preview_path: preview_path_string,
        analysis,
    })
}

#[tauri::command]
async fn get_processing_manifest(app: tauri::AppHandle) -> Result<ProcessingManifest, String> {
    let stdout = run_backend_command(&app, &["--describe-presets".to_string()]).await?;

    serde_json::from_str(&stdout)
        .map_err(|error| format!("failed to parse backend preset manifest: {error}"))
}

async fn describe_study(
    app: &tauri::AppHandle,
    input_path: &str,
) -> Result<StudyDescription, String> {
    let stdout = run_backend_command(
        app,
        &[
            "--input".to_string(),
            input_path.to_string(),
            "--describe-study".to_string(),
        ],
    )
    .await?;

    serde_json::from_str(&stdout)
        .map_err(|error| format!("failed to parse backend study description: {error}"))
}

async fn describe_study_if_available(
    app: &tauri::AppHandle,
    input_path: &str,
) -> Option<MeasurementScale> {
    describe_study(app, input_path)
        .await
        .ok()
        .and_then(|description| description.measurement_scale)
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

fn find_flag_value<'a>(args: &'a [String], flag: &str) -> Option<&'a str> {
    args.windows(2)
        .find_map(|window| (window[0] == flag).then_some(window[1].as_str()))
}

async fn run_backend_command(app: &tauri::AppHandle, args: &[String]) -> Result<String, String> {
    // Development should use the current backend source rather than any stale
    // sidecar or release binary left on disk from a previous build.
    if !cfg!(debug_assertions) {
        if let Ok(sidecar) = app.shell().sidecar("xrayview-backend") {
            let output = sidecar
                .args(args.iter().map(String::as_str))
                .output()
                .await
                .map_err(|error| format!("failed to start bundled backend: {error}"))?;

            return handle_backend_output(
                output.status.success(),
                format!("{:?}", output.status),
                output.stdout,
                output.stderr,
            );
        }
    }

    let backend = resolve_backend_spec()?;
    let mut command = Command::new(&backend.program);
    command.current_dir(&backend.working_directory);
    command.args(&backend.prefix_args);
    command.args(args);

    let output = command
        .output()
        .map_err(|error| format!("failed to start backend command: {error}"))?;

    handle_backend_output(
        output.status.success(),
        format!("{}", output.status),
        output.stdout,
        output.stderr,
    )
}

fn handle_backend_output(
    succeeded: bool,
    status_text: String,
    stdout_bytes: Vec<u8>,
    stderr_bytes: Vec<u8>,
) -> Result<String, String> {
    let stdout = String::from_utf8_lossy(&stdout_bytes).trim().to_string();
    if succeeded {
        return Ok(stdout);
    }

    let stderr = String::from_utf8_lossy(&stderr_bytes).trim().to_string();
    let message = if !stderr.is_empty() {
        stderr
    } else if !stdout.is_empty() {
        stdout
    } else {
        format!("backend exited with status {status_text}")
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

    if cfg!(debug_assertions) {
        if let Some(project_root) =
            find_project_root(&env::current_dir().unwrap_or_else(|_| PathBuf::from(".")))
        {
            return Ok(BackendSpec {
                program: "cargo".to_string(),
                prefix_args: vec![
                    "run".to_string(),
                    "--manifest-path".to_string(),
                    "backend/Cargo.toml".to_string(),
                    "--".to_string(),
                ],
                working_directory: project_root,
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
                program: "cargo".to_string(),
                prefix_args: vec![
                    "run".to_string(),
                    "--manifest-path".to_string(),
                    "backend/Cargo.toml".to_string(),
                    "--".to_string(),
                ],
                working_directory: project_root,
            });
        }
    }

    Err(
        "could not locate the Rust backend; set XRAYVIEW_BACKEND_PATH or run from the repository"
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
        // The frontend can be launched from several directories during dev, so
        // walk upward until we find the workspace that owns `backend/`.
        if current.join("backend").is_dir() {
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
        vec![project_root.join("backend/target/release/xrayview-backend.exe")]
    }

    #[cfg(not(target_os = "windows"))]
    {
        vec![project_root.join("backend/target/release/xrayview-backend")]
    }
}

fn path_to_string(path: PathBuf) -> String {
    path.to_string_lossy().to_string()
}

fn configure_linux_webkit_environment() {
    #[cfg(target_os = "linux")]
    {
        if env::var_os("WEBKIT_DISABLE_DMABUF_RENDERER").is_none() {
            env::set_var("WEBKIT_DISABLE_DMABUF_RENDERER", "1");
        }
    }
}

#[cfg(target_os = "linux")]
fn configure_linux_application_identity() {
    glib::set_prgname(Some("xrayview"));
    glib::set_application_name("XRayView");
}

#[cfg(not(target_os = "linux"))]
fn configure_linux_application_identity() {}

fn main() {
    configure_linux_webkit_environment();
    configure_linux_application_identity();

    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .manage(TempFileState::new())
        .manage(BackendGate::new())
        .setup(|app| {
            #[cfg(target_os = "linux")]
            {
                if let Some(window) = app.get_webview_window("main") {
                    window.set_decorations(false)?;
                }
            }

            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            pick_dicom_file,
            pick_save_dicom_path,
            get_processing_manifest,
            run_backend_preview,
            run_backend_process,
            run_backend_tooth_measurement,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
