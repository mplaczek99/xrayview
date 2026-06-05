// CLI entry. The desktop app talks to the backend in-process through Tauri IPC;
// this binary is only a headless inspection and batch utility over the same
// library code.

use std::{io::Write, path::PathBuf};

use clap::{Args, Parser, Subcommand};
use serde::Serialize;
use serde_json::json;

use crate::{
    analysis,
    bmp::{self, Metadata, RenderedPreview},
    config::Config,
    contracts::{
        BACKEND_CONTRACT_VERSION, BackendError, MeasurementScale, SERVICE_NAME, SUPPORTED_COMMANDS,
        default_processing_manifest,
    },
    processing::{self, GrayscaleControls},
    render::{self, PreviewImage},
};

#[derive(Debug, thiserror::Error)]
pub enum CliError {
    #[error("{0}")]
    Message(String),
    #[error(transparent)]
    Io(#[from] std::io::Error),
    #[error(transparent)]
    Json(#[from] serde_json::Error),
    #[error(transparent)]
    Processing(#[from] processing::ProcessingError),
}

impl From<String> for CliError {
    fn from(message: String) -> Self {
        Self::Message(message)
    }
}

impl From<&str> for CliError {
    fn from(message: &str) -> Self {
        Self::Message(message.to_string())
    }
}

impl From<BackendError> for CliError {
    fn from(error: BackendError) -> Self {
        Self::Message(error.message)
    }
}

type CliResult<T> = Result<T, CliError>;

#[derive(Parser, Debug)]
#[command(
    name = "xrayview-backend-rs",
    about = "X-ray BMP preview / processing / analysis CLI",
    disable_version_flag = true
)]
struct Cli {
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand, Debug)]
enum Command {
    /// Print resolved backend configuration as JSON.
    PrintConfig,
    /// Decode source pixels directly in Rust.
    DecodeSource {
        /// Path to the source BMP image.
        path: PathBuf,
    },
    /// Print processing presets as JSON.
    ProcessingManifest,
    /// Print BMP study metadata as JSON without decoding pixel data.
    DescribeStudy {
        /// Path to the source BMP image.
        path: PathBuf,
    },
    /// Render a grayscale BMP preview.
    RenderPreview(RenderPreviewArgs),
    /// Render then run the Rust preview pipeline.
    ProcessPreview(ProcessPreviewArgs),
    /// Render the analysis overlay preview.
    AnalyzePreview(AnalyzePreviewArgs),
    /// Print supported command names.
    ListCommands,
    /// Print service and contract version.
    Version,
}

#[derive(Args, Debug)]
struct RenderPreviewArgs {
    /// Bypass the min/max stretch and emit the full source range.
    #[arg(long = "full-range")]
    full_range: bool,
    input_path: PathBuf,
    output_path: PathBuf,
}

#[derive(Args, Debug)]
struct ProcessPreviewArgs {
    #[arg(long = "full-range")]
    full_range: bool,
    #[arg(long)]
    invert: bool,
    #[arg(long, allow_hyphen_values = true, default_value_t = 0)]
    brightness: i32,
    #[arg(long, default_value_t = 1.0)]
    contrast: f64,
    #[arg(long)]
    equalize: bool,
    #[arg(long, default_value = "none")]
    palette: String,
    #[arg(long)]
    compare: bool,
    input_path: PathBuf,
    output_path: PathBuf,
}

#[derive(Args, Debug)]
struct AnalyzePreviewArgs {
    #[arg(long)]
    filled: bool,
    input_path: PathBuf,
    output_path: PathBuf,
}

pub fn run(args: &[&str], stdout: &mut dyn Write, stderr: &mut dyn Write) -> CliResult<()> {
    // Some shell wrappers insert literal `--` between argv[0] and the rest;
    // clap would treat that as an end-of-options marker, so strip leading
    // `--`s ourselves before handing argv off.
    let mut start = 0;
    while args.get(start).copied() == Some("--") {
        start += 1;
    }
    let argv = std::iter::once("xrayview-backend-rs").chain(args[start..].iter().copied());

    let cli = match Cli::try_parse_from(argv) {
        Ok(cli) => cli,
        Err(error) => {
            // clap routes help/version through Error too — those should
            // exit successfully and print to stdout. Real parse errors go
            // to stderr and bubble up.
            if error.use_stderr() {
                write!(stderr, "{error}")?;
                return Err(error.to_string().into());
            }
            write!(stdout, "{error}")?;
            return Ok(());
        }
    };

    match cli.command {
        Command::PrintConfig => print_config(stdout),
        Command::DecodeSource { path } => decode_source(&path, stdout),
        Command::ProcessingManifest => processing_manifest(stdout),
        Command::DescribeStudy { path } => describe_study(&path, stdout),
        Command::RenderPreview(args) => render_preview(args, stdout),
        Command::ProcessPreview(args) => process_preview(args, stdout),
        Command::AnalyzePreview(args) => analyze_preview(args, stdout),
        Command::ListCommands => list_commands(stdout),
        Command::Version => {
            // Format must remain "<service> contract-v<n>" — external
            // tools grep for this exact shape.
            writeln!(
                stdout,
                "{SERVICE_NAME} contract-v{BACKEND_CONTRACT_VERSION}"
            )?;
            Ok(())
        }
    }
}

fn print_config(stdout: &mut dyn Write) -> CliResult<()> {
    let config = Config::load()?;
    write_json(
        stdout,
        &json!({
            "serviceName": config.service_name,
            "logging": {
                "level": config.logging.level,
            },
            "paths": {
                "baseDir": config.paths.base_dir,
                "cacheDir": config.paths.cache_dir,
                "persistenceDir": config.paths.persistence_dir,
            },
        }),
    )
}

fn decode_source(path: &std::path::Path, stdout: &mut dyn Write) -> CliResult<()> {
    let path_str = path
        .to_str()
        .ok_or_else(|| "decode-source path must be valid UTF-8".to_string())?;
    let metadata = bmp::read_file(path_str)?;
    let rendered = bmp::render_grayscale_preview_file(path_str)?;
    write_json(
        stdout,
        &DecodeSourceSummary::from_rendered_and_metadata(rendered, &metadata),
    )
}

fn processing_manifest(stdout: &mut dyn Write) -> CliResult<()> {
    write_json(stdout, &default_processing_manifest())
}

fn describe_study(path: &std::path::Path, stdout: &mut dyn Write) -> CliResult<()> {
    let path_str = path
        .to_str()
        .ok_or_else(|| "describe-study path must be valid UTF-8".to_string())?;
    let metadata = bmp::read_file(path_str)?;
    write_json(stdout, &StudyDescription::from_metadata(&metadata))
}

fn render_preview(args: RenderPreviewArgs, stdout: &mut dyn Write) -> CliResult<()> {
    let rendered = bmp::render_grayscale_preview_file(&args.input_path)?;
    render::save_gray_bmp(
        &args.output_path,
        rendered.width,
        rendered.height,
        &rendered.pixels,
    )?;

    write_json(
        stdout,
        &RenderPreviewSummary {
            preview_output: args.output_path.display().to_string(),
            loaded_width: rendered.width,
            loaded_height: rendered.height,
            window_mode: if args.full_range {
                "full-range"
            } else {
                "default"
            },
            measurement_scale: rendered.measurement_scale,
            rendered_byte_count: rendered.pixels.len(),
        },
    )
}

fn process_preview(args: ProcessPreviewArgs, stdout: &mut dyn Write) -> CliResult<()> {
    if !(-256..=256).contains(&args.brightness) {
        return Err(format!(
            "brightness must be between -256 and 256, got {}",
            args.brightness
        )
        .into());
    }
    if !args.contrast.is_finite() || args.contrast < 0.0 {
        return Err(format!("contrast must be >= 0.0, got {}", args.contrast).into());
    }

    let palette_name = args.palette.to_ascii_lowercase();
    let rendered = bmp::render_grayscale_preview_file(&args.input_path)?;
    let source = rendered_preview_image(&rendered);
    let palette = processing::normalize_palette_name(&palette_name)?;
    let controls = GrayscaleControls {
        invert: args.invert,
        brightness: args.brightness,
        contrast: args.contrast,
        equalize: args.equalize,
    };
    let processed = processing::process_rendered_preview(source, controls, palette, args.compare)?;
    render::save_preview_bmp(&args.output_path, &processed.preview)?;

    write_json(
        stdout,
        &ProcessPreviewSummary {
            preview_output: args.output_path.display().to_string(),
            loaded_width: rendered.width,
            loaded_height: rendered.height,
            window_mode: if args.full_range {
                "full-range"
            } else {
                "default"
            },
            mode: processed.mode,
            palette: palette_name,
            compare: args.compare,
            measurement_scale: rendered.measurement_scale,
            rendered_byte_count: processed.preview.pixels.len(),
        },
    )
}

fn analyze_preview(args: AnalyzePreviewArgs, stdout: &mut dyn Write) -> CliResult<()> {
    let rendered = bmp::render_grayscale_preview_file_for_tooth_analysis(&args.input_path)?;
    let source = rendered_preview_image(&rendered);
    let result = analysis::generate_tooth_overlay(&source)?;
    let preview = if args.filled {
        &result.filled_preview
    } else {
        &result.preview
    };
    render::save_preview_bmp(&args.output_path, preview)?;

    write_json(
        stdout,
        &AnalyzePreviewSummary {
            preview_output: args.output_path.display().to_string(),
            loaded_width: rendered.width,
            loaded_height: rendered.height,
            filled: args.filled,
            mode: result.mode,
            tooth_pixels: result.tooth_pixels,
            bone_pixels: result.bone_pixels,
            coverage: result.coverage,
            candidate_count: result.candidate_count,
        },
    )
}

fn list_commands(stdout: &mut dyn Write) -> CliResult<()> {
    for command in SUPPORTED_COMMANDS {
        writeln!(stdout, "{command}")?;
    }
    Ok(())
}

fn rendered_preview_image(rendered: &RenderedPreview) -> PreviewImage {
    PreviewImage::gray(rendered.width, rendered.height, rendered.pixels.clone())
}

fn write_json(stdout: &mut dyn Write, value: &impl Serialize) -> CliResult<()> {
    serde_json::to_writer_pretty(&mut *stdout, value)?;
    writeln!(stdout)?;
    Ok(())
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct DecodeSourceSummary {
    width: u32,
    height: u32,
    format: &'static str,
    pixel_count: usize,
    min_value: u8,
    max_value: u8,
    invert: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    measurement_scale: Option<MeasurementScale>,
}

impl DecodeSourceSummary {
    fn from_rendered_and_metadata(rendered: RenderedPreview, metadata: &Metadata) -> Self {
        let min_value = rendered.pixels.iter().copied().min().unwrap_or_default();
        let max_value = rendered.pixels.iter().copied().max().unwrap_or_default();
        let measurement_scale = rendered
            .measurement_scale
            .or_else(|| metadata.measurement_scale());
        Self {
            width: rendered.width,
            height: rendered.height,
            format: "gray8",
            pixel_count: rendered.pixels.len(),
            min_value,
            max_value,
            invert: false,
            measurement_scale,
        }
    }
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct StudyDescription {
    width: u32,
    height: u32,
    color_channel_count: u16,
    bits_per_channel: u16,
    color_model: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    measurement_scale: Option<MeasurementScale>,
}

impl StudyDescription {
    fn from_metadata(metadata: &Metadata) -> Self {
        Self {
            width: metadata.width,
            height: metadata.height,
            color_channel_count: metadata.color_channel_count,
            bits_per_channel: metadata.bits_per_channel,
            color_model: metadata.color_model.clone(),
            measurement_scale: metadata.measurement_scale(),
        }
    }
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct RenderPreviewSummary {
    preview_output: String,
    loaded_width: u32,
    loaded_height: u32,
    window_mode: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    measurement_scale: Option<MeasurementScale>,
    rendered_byte_count: usize,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct ProcessPreviewSummary {
    preview_output: String,
    loaded_width: u32,
    loaded_height: u32,
    window_mode: &'static str,
    mode: String,
    palette: String,
    compare: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    measurement_scale: Option<MeasurementScale>,
    rendered_byte_count: usize,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct AnalyzePreviewSummary {
    preview_output: String,
    loaded_width: u32,
    loaded_height: u32,
    filled: bool,
    mode: String,
    tooth_pixels: usize,
    bone_pixels: usize,
    coverage: f64,
    candidate_count: usize,
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{
        fs,
        path::Path,
        time::{SystemTime, UNIX_EPOCH},
    };

    // Pins the exact output format of `xrayview version` — external scripts
    // grep for this string, so a typo here is a breaking change.
    #[test]
    fn run_version_prints_contract_metadata() {
        let mut stdout = Vec::new();
        let mut stderr = Vec::new();

        run(&["version"], &mut stdout, &mut stderr).unwrap();

        assert_eq!(
            String::from_utf8(stdout).unwrap(),
            "xrayview-backend contract-v2\n"
        );
        assert!(stderr.is_empty());
    }

    #[test]
    fn help_flag_prints_usage_and_exits_successfully() {
        let mut stdout = Vec::new();
        let mut stderr = Vec::new();

        run(&["--help"], &mut stdout, &mut stderr).unwrap();

        assert!(stderr.is_empty());
        let stdout = String::from_utf8(stdout).unwrap();
        assert!(stdout.contains("Usage:"));
        assert!(stdout.contains("xrayview-backend-rs"));
    }

    // Smoke test for the process-preview flag parser: every knob set on the
    // command line should land in the parsed args, with palette left as
    // typed (the handler lower-cases it before normalization).
    #[test]
    fn parse_process_preview_args_accepts_controls() {
        let cli = Cli::try_parse_from([
            "xrayview-backend-rs",
            "process-preview",
            "--full-range",
            "--invert",
            "--brightness",
            "10",
            "--contrast",
            "1.4",
            "--equalize",
            "--palette",
            "BONE",
            "--compare",
            "input.bmp",
            "output.bmp",
        ])
        .unwrap();

        let Command::ProcessPreview(args) = cli.command else {
            panic!("expected process-preview subcommand");
        };

        assert!(args.full_range);
        assert!(args.invert);
        assert!(args.equalize);
        assert_eq!(args.brightness, 10);
        assert_eq!(args.contrast, 1.4);
        assert_eq!(args.palette, "BONE");
        assert!(args.compare);
        assert_eq!(args.input_path, Path::new("input.bmp"));
        assert_eq!(args.output_path, Path::new("output.bmp"));
    }

    // End-to-end: run render-preview and process-preview, confirm both
    // write valid BMP files (b"BM" magic) and that process-preview emits
    // the expected JSON summary on stdout.
    #[test]
    fn render_and_process_preview_write_bmps() {
        let root = unique_temp_dir("preview");
        fs::create_dir_all(&root).unwrap();
        let input = root.join("study.bmp");
        let render_output = root.join("render.bmp");
        let process_output = root.join("process.bmp");
        fs::write(&input, build_renderable_test_bmp()).unwrap();

        let mut stdout = Vec::new();
        let mut stderr = Vec::new();
        run(
            &[
                "render-preview",
                input.to_str().unwrap(),
                render_output.to_str().unwrap(),
            ],
            &mut stdout,
            &mut stderr,
        )
        .unwrap();
        assert!(fs::read(&render_output).unwrap().starts_with(b"BM"));

        stdout.clear();
        run(
            &[
                "process-preview",
                "--invert",
                "--palette",
                "hot",
                input.to_str().unwrap(),
                process_output.to_str().unwrap(),
            ],
            &mut stdout,
            &mut stderr,
        )
        .unwrap();
        assert!(fs::read(&process_output).unwrap().starts_with(b"BM"));

        let summary: serde_json::Value = serde_json::from_slice(&stdout).unwrap();
        assert_eq!(summary["palette"], "hot");
        let _ = fs::remove_dir_all(root);
    }

    // decode-source should round-trip width/height + omit measurementScale
    // when the BMP has no calibration metadata.
    #[test]
    fn decode_source_reports_bmp_metadata() {
        let root = unique_temp_dir("decode-source");
        fs::create_dir_all(&root).unwrap();
        let input = root.join("study.bmp");
        fs::write(&input, build_renderable_test_bmp()).unwrap();

        let mut stdout = Vec::new();
        let mut stderr = Vec::new();
        run(
            &["decode-source", input.to_str().unwrap()],
            &mut stdout,
            &mut stderr,
        )
        .unwrap();

        let summary: serde_json::Value = serde_json::from_slice(&stdout).unwrap();
        assert_eq!(summary["width"], 4);
        assert_eq!(summary["height"], 2);
        assert!(summary.get("measurementScale").is_none());
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn inspection_subcommands_return_manifest_and_study_metadata() {
        let root = unique_temp_dir("inspection");
        fs::create_dir_all(&root).unwrap();
        let input = root.join("study.bmp");
        fs::write(&input, build_renderable_test_bmp()).unwrap();

        let mut stdout = Vec::new();
        let mut stderr = Vec::new();
        run(&["processing-manifest"], &mut stdout, &mut stderr).unwrap();
        let manifest: serde_json::Value = serde_json::from_slice(&stdout).unwrap();
        assert_eq!(manifest["defaultPresetId"], "default");
        assert_eq!(manifest["presets"].as_array().unwrap().len(), 3);

        stdout.clear();
        run(
            &["describe-study", input.to_str().unwrap()],
            &mut stdout,
            &mut stderr,
        )
        .unwrap();
        let study: serde_json::Value = serde_json::from_slice(&stdout).unwrap();
        assert_eq!(study["width"], 4);
        assert_eq!(study["height"], 2);
        assert_eq!(study["colorChannelCount"], 3);
        assert_eq!(study["bitsPerChannel"], 8);
        assert_eq!(study["colorModel"], "rgb");
        assert!(study.get("measurementScale").is_none());
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn top_level_workflow_flags_are_rejected() {
        let mut stdout = Vec::new();
        let mut stderr = Vec::new();

        let error = run(&["--input", "study.bmp"], &mut stdout, &mut stderr).unwrap_err();

        assert!(stdout.is_empty());
        assert!(error.to_string().contains("unexpected argument '--input'"));
        assert!(String::from_utf8(stderr).unwrap().contains("Usage:"));
    }

    // PID + nanos so parallel cargo-test runs don't collide on the same dir.
    fn unique_temp_dir(name: &str) -> PathBuf {
        let nanos = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        std::env::temp_dir().join(format!(
            "xrayview-rs-cli-{name}-{}-{nanos}",
            std::process::id()
        ))
    }

    // Build a tiny 4×2 grayscale ramp BMP that the renderer can chew on.
    // Pulled from the bmp::tests module rather than duplicating the BMP
    // construction code here.
    fn build_renderable_test_bmp() -> Vec<u8> {
        crate::bmp::tests::build_bmp_32(
            4,
            2,
            &[
                (0, 0, 0),
                (36, 36, 36),
                (72, 72, 72),
                (108, 108, 108),
                (144, 144, 144),
                (180, 180, 180),
                (216, 216, 216),
                (255, 255, 255),
            ],
        )
    }
}
