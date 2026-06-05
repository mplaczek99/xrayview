// CLI entry. One clap-derived parser covers both argv shapes:
//
//   * Subcommand form:  xrayview <subcommand> [args...]
//     e.g.  xrayview render-preview input.bmp output.bmp
//
//   * Legacy flag form: xrayview --input foo.bmp --preset xray ...
//     A single positional-less command that does everything the old Go
//     binary did. Kept around for CI scripts and dental-suite integrations
//     that haven't migrated. When no subcommand is matched, the top-level
//     flags execute the legacy workflow.
//
// Subcommands and the legacy flag set are declared on the same `Cli`
// struct; `args_conflicts_with_subcommands` means you can use one or the
// other, never both.

use std::{fs, io::Write, path::PathBuf};

use clap::{Args, CommandFactory, Parser, Subcommand};
use serde::Serialize;
use serde_json::json;

use crate::{
    analysis,
    bmp::{self, Metadata, RenderedPreview},
    config::Config,
    contracts::{
        BACKEND_CONTRACT_VERSION, BackendError, MeasurementScale, PaletteName, ProcessStudyCommand,
        SERVICE_NAME, SUPPORTED_COMMANDS, default_processing_manifest,
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
    disable_version_flag = true,
    args_conflicts_with_subcommands = true,
)]
struct Cli {
    #[command(subcommand)]
    command: Option<Command>,

    #[command(flatten)]
    legacy: LegacyArgs,
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

// Top-level (legacy) flags. All Option<T> so we can tell "user didn't
// pass this" from "user passed default-shaped value" — important for the
// preset/override merge in legacy_process_command.
#[derive(Args, Debug, Default)]
struct LegacyArgs {
    /// Path to the source BMP image.
    #[arg(long)]
    input: Option<String>,

    /// BMP preview output path.
    #[arg(long = "preview-output")]
    preview_output: Option<String>,

    /// Print processing preset metadata as JSON.
    #[arg(long = "describe-presets", num_args = 0..=1, default_missing_value = "true")]
    describe_presets: Option<bool>,

    /// Print study measurement metadata as JSON.
    #[arg(long = "describe-study", num_args = 0..=1, default_missing_value = "true")]
    describe_study: Option<bool>,

    /// Processing preset: default, xray, or high-contrast.
    #[arg(long)]
    preset: Option<String>,

    /// Invert grayscale.
    #[arg(long, num_args = 0..=1, default_missing_value = "true")]
    invert: Option<bool>,

    /// Brightness adjustment (-256 to 256).
    #[arg(long, allow_hyphen_values = true)]
    brightness: Option<i32>,

    /// Contrast multiplier (>= 0.0).
    #[arg(long)]
    contrast: Option<f64>,

    /// Apply histogram equalization.
    #[arg(long, num_args = 0..=1, default_missing_value = "true")]
    equalize: Option<bool>,

    /// Show before/after comparison.
    #[arg(long, num_args = 0..=1, default_missing_value = "true")]
    compare: Option<bool>,

    /// Palette name: none, hot, or bone.
    #[arg(long)]
    palette: Option<String>,
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
        Some(Command::PrintConfig) => print_config(stdout),
        Some(Command::DecodeSource { path }) => decode_source(&path, stdout),
        Some(Command::RenderPreview(args)) => render_preview(args, stdout),
        Some(Command::ProcessPreview(args)) => process_preview(args, stdout),
        Some(Command::AnalyzePreview(args)) => analyze_preview(args, stdout),
        Some(Command::ListCommands) => list_commands(stdout),
        Some(Command::Version) => {
            // Format must remain "<service> contract-v<n>" — external
            // tools grep for this exact shape.
            writeln!(
                stdout,
                "{SERVICE_NAME} contract-v{BACKEND_CONTRACT_VERSION}"
            )?;
            Ok(())
        }
        None => {
            if legacy_args_empty(&cli.legacy) {
                let help = Cli::command().render_help();
                write!(stderr, "{help}")?;
                return Err("expected a subcommand or workflow flags".into());
            }
            execute_legacy(legacy_options_from(cli.legacy), stdout)
        }
    }
}

fn legacy_args_empty(args: &LegacyArgs) -> bool {
    args.input.is_none()
        && args.preview_output.is_none()
        && args.describe_presets.is_none()
        && args.describe_study.is_none()
        && args.preset.is_none()
        && args.invert.is_none()
        && args.brightness.is_none()
        && args.contrast.is_none()
        && args.equalize.is_none()
        && args.compare.is_none()
        && args.palette.is_none()
}

fn legacy_options_from(args: LegacyArgs) -> LegacyOptions {
    LegacyOptions {
        input: args.input.unwrap_or_default(),
        preview_output: args.preview_output.unwrap_or_default(),
        describe_presets: args.describe_presets.unwrap_or(false),
        describe_study: args.describe_study.unwrap_or(false),
        // Preset defaults to "default" so non-preset legacy invocations
        // still produce a sensible image.
        preset: args.preset.unwrap_or_else(|| "default".to_string()),
        invert: args.invert,
        brightness: args.brightness,
        contrast: args.contrast,
        equalize: args.equalize,
        compare: args.compare.unwrap_or(false),
        palette: args.palette.unwrap_or_default(),
    }
}

fn execute_legacy(options: LegacyOptions, stdout: &mut dyn Write) -> CliResult<()> {
    validate_legacy_mode_selection(&options)?;
    if options.describe_presets {
        return write_json_compact(stdout, &default_processing_manifest());
    }

    let input_path = required_input_path(&options)?;
    if options.describe_study {
        let metadata = bmp::read_file(&input_path)?;
        return write_json_compact(
            stdout,
            &LegacyStudyDescription {
                measurement_scale: metadata.measurement_scale(),
            },
        );
    }

    if is_plain_preview_request(&options) {
        return render_legacy_preview(&input_path, options.preview_output.trim(), stdout);
    }

    let mut preview_output = options.preview_output.trim().to_string();
    if preview_output.is_empty() {
        preview_output = default_legacy_preview_output_path(&input_path);
    }

    process_legacy_study(&input_path, &preview_output, &options, stdout)
}

fn validate_legacy_mode_selection(options: &LegacyOptions) -> CliResult<()> {
    if options.describe_presets && options.describe_study {
        return Err(
            "choose only one backend mode: --describe-presets or --describe-study"
                .to_string()
                .into(),
        );
    }
    Ok(())
}

fn required_input_path(options: &LegacyOptions) -> CliResult<String> {
    let input_path = options.input.trim();
    if input_path.is_empty() {
        return Err("--input is required".to_string().into());
    }
    validate_legacy_input_path(input_path)?;
    Ok(input_path.to_string())
}

fn validate_legacy_input_path(input_path: &str) -> CliResult<()> {
    let metadata = fs::metadata(input_path).map_err(|error| {
        if error.kind() == std::io::ErrorKind::NotFound {
            format!("input file does not exist: {input_path}")
        } else {
            format!("inspect input file {input_path}: {error}")
        }
    })?;
    if metadata.is_dir() {
        return Err(format!("input path must be a file: {input_path}").into());
    }
    Ok(())
}

fn render_legacy_preview(
    input_path: &str,
    preview_output: &str,
    stdout: &mut dyn Write,
) -> CliResult<()> {
    let rendered = bmp::render_grayscale_preview_file(input_path)?;
    render::save_gray_bmp(
        preview_output,
        rendered.width,
        rendered.height,
        &rendered.pixels,
    )?;
    writeln!(
        stdout,
        "loaded BMP image: {}x{}",
        rendered.width, rendered.height
    )?;
    writeln!(stdout, "saved grayscale preview image: {preview_output}")?;
    Ok(())
}

fn process_legacy_study(
    input_path: &str,
    preview_output: &str,
    options: &LegacyOptions,
    stdout: &mut dyn Write,
) -> CliResult<()> {
    let rendered = bmp::render_grayscale_preview_file(input_path)?;
    let command = legacy_process_command(options)?;
    let resolved = processing::resolve_process_study_command(&command)?;
    let source = rendered_preview_image(&rendered);
    let processed = processing::process_rendered_preview(
        source,
        resolved.controls,
        resolved.palette,
        resolved.compare,
    )?;

    render::save_preview_bmp(preview_output, &processed.preview)?;

    writeln!(
        stdout,
        "loaded BMP image: {}x{}",
        rendered.width, rendered.height
    )?;
    writeln!(
        stdout,
        "saved {} preview image: {}",
        processed.mode, preview_output
    )?;
    Ok(())
}

// Translate parsed legacy flags into the modern ProcessStudyCommand shape.
// The merge order is important and matches the manifest semantics:
//   1. Start with empty defaults.
//   2. Layer in the preset's defaults (so e.g. --preset xray brings invert/equalize).
//   3. Layer in any explicit user overrides on top.
// This ordering means a user passing `--preset xray --invert false` wins
// even though the preset would have set invert=true.
fn legacy_process_command(options: &LegacyOptions) -> CliResult<ProcessStudyCommand> {
    let mut command = ProcessStudyCommand {
        // Legacy doesn't have study_ids — synthesized when needed downstream.
        study_id: String::new(),
        preset_id: options.preset.clone(),
        invert: false,
        // Option<i32>/Option<f64> carry the user's explicit value (or None for "use preset").
        brightness: options.brightness,
        contrast: options.contrast,
        equalize: false,
        compare: options.compare,
        palette: parse_palette_name(&options.palette)?,
    };
    // Preset defaults for the bool-typed knobs.
    if let Some(controls) = legacy_preset_controls(&options.preset) {
        command.invert = controls.invert;
        command.equalize = controls.equalize;
    }
    // User overrides win over the preset defaults set above.
    if let Some(value) = options.invert {
        command.invert = value;
    }
    if let Some(value) = options.equalize {
        command.equalize = value;
    }
    Ok(command)
}

fn legacy_preset_controls(preset_id: &str) -> Option<crate::contracts::ProcessingControls> {
    let preset_id = preset_id.trim();
    default_processing_manifest()
        .presets
        .into_iter()
        .find(|preset| preset.id.eq_ignore_ascii_case(preset_id))
        .map(|preset| preset.controls)
}

fn parse_palette_name(value: &str) -> CliResult<Option<PaletteName>> {
    Ok(match value.trim().to_ascii_lowercase().as_str() {
        "" => None,
        "none" => Some(PaletteName::None),
        "hot" => Some(PaletteName::Hot),
        "bone" => Some(PaletteName::Bone),
        _ => return Err("palette must be one of: none, hot, bone".to_string().into()),
    })
}

// "Plain preview" = the user only wants render_grayscale_preview (no
// processing applied). True if every knob is at its default. Lets us
// take the faster path that skips the processing pipeline entirely.
fn is_plain_preview_request(options: &LegacyOptions) -> bool {
    !options.preview_output.trim().is_empty()
        && options.preset.trim().eq_ignore_ascii_case("default")
        && options.invert.is_none()
        && options.brightness.is_none()
        && options.contrast.is_none()
        && options.equalize.is_none()
        && !options.compare
        && options.palette.trim().is_empty()
}

// When --preview-output isn't given but processing was requested, fall back
// to `<stem>_processed.bmp` next to the input. The .or_else gymnastics handle
// weird path shapes (dotfiles, no-extension, etc.).
fn default_legacy_preview_output_path(input_path: &str) -> String {
    let path = std::path::Path::new(input_path);
    let stem = path
        .file_stem()
        .filter(|stem| !stem.is_empty())
        .or_else(|| path.file_name())
        .map(|stem| stem.to_string_lossy().into_owned())
        .unwrap_or_else(|| "image".to_string());
    path.parent()
        .unwrap_or_else(|| std::path::Path::new(""))
        .join(format!("{stem}_processed.bmp"))
        .display()
        .to_string()
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

fn write_json_compact(stdout: &mut dyn Write, value: &impl Serialize) -> CliResult<()> {
    serde_json::to_writer(&mut *stdout, value)?;
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

// Legacy flag state after defaults are baked in. invert/brightness/contrast/
// equalize stay Option<T> so we can distinguish "user didn't set this"
// (None → fall back to preset) from "user explicitly set this to false/0".
// Without that we couldn't tell `--preset xray` from `--preset xray --equalize=false`.
#[derive(Debug, Clone, Default, PartialEq)]
struct LegacyOptions {
    input: String,
    preview_output: String,
    describe_presets: bool,
    describe_study: bool,
    preset: String,
    invert: Option<bool>,
    brightness: Option<i32>,
    contrast: Option<f64>,
    equalize: Option<bool>,
    compare: bool,
    palette: String,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct LegacyStudyDescription {
    #[serde(skip_serializing_if = "Option::is_none")]
    measurement_scale: Option<MeasurementScale>,
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

        let Some(Command::ProcessPreview(args)) = cli.command else {
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
    // when there isn't one (this BMP has no PixelSpacing).
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

    // Legacy --describe-* paths emit compact JSON (one line). The assertion
    // on the single newline-or-EOF is the "compact, not pretty" guard.
    #[test]
    fn legacy_describe_commands_return_compact_json() {
        let root = unique_temp_dir("legacy-describe");
        fs::create_dir_all(&root).unwrap();
        let input = root.join("study.bmp");
        fs::write(&input, build_renderable_test_bmp()).unwrap();

        let mut stdout = Vec::new();
        let mut stderr = Vec::new();
        run(&["--describe-presets"], &mut stdout, &mut stderr).unwrap();
        let manifest_stdout = String::from_utf8(stdout.clone()).unwrap();
        assert!(manifest_stdout.contains(r#""defaultPresetId":"default""#));
        assert!(!manifest_stdout.contains('\n') || manifest_stdout.ends_with('\n'));
        let manifest: serde_json::Value = serde_json::from_slice(&stdout).unwrap();
        assert_eq!(manifest["presets"].as_array().unwrap().len(), 3);

        stdout.clear();
        run(
            &["--input", input.to_str().unwrap(), "--describe-study"],
            &mut stdout,
            &mut stderr,
        )
        .unwrap();
        let study: serde_json::Value = serde_json::from_slice(&stdout).unwrap();
        assert!(study.get("measurementScale").is_none());
        let _ = fs::remove_dir_all(root);
    }

    // End-to-end legacy: bare --input/--preview-output writes a plain
    // grayscale preview; adding --preset xray --invert=false runs the full
    // processing pipeline. Verifies stdout has the expected human-readable
    // lines too (some scripts grep these).
    #[test]
    fn legacy_preview_and_process_write_expected_artifacts() {
        let root = unique_temp_dir("legacy-artifacts");
        fs::create_dir_all(&root).unwrap();
        let input = root.join("study.bmp");
        let preview_output = root.join("preview.bmp");
        let processed_preview_output = root.join("processed-preview.bmp");
        fs::write(&input, build_renderable_test_bmp()).unwrap();

        let mut stdout = Vec::new();
        let mut stderr = Vec::new();
        run(
            &[
                "--input",
                input.to_str().unwrap(),
                "--preview-output",
                preview_output.to_str().unwrap(),
            ],
            &mut stdout,
            &mut stderr,
        )
        .unwrap();
        let preview_stdout = String::from_utf8(stdout.clone()).unwrap();
        assert!(fs::read(&preview_output).unwrap().starts_with(b"BM"));
        assert!(preview_stdout.contains("loaded BMP image: 4x2"));
        assert!(preview_stdout.contains("saved grayscale preview image:"));

        stdout.clear();
        run(
            &[
                "--input",
                input.to_str().unwrap(),
                "--preview-output",
                processed_preview_output.to_str().unwrap(),
                "--preset",
                "xray",
                "--invert=false",
            ],
            &mut stdout,
            &mut stderr,
        )
        .unwrap();
        let process_stdout = String::from_utf8(stdout.clone()).unwrap();
        assert!(
            fs::read(&processed_preview_output)
                .unwrap()
                .starts_with(b"BM")
        );
        assert!(process_stdout.contains("loaded BMP image: 4x2"));
        assert!(process_stdout.contains("preview image:"));
        let _ = fs::remove_dir_all(root);
    }

    // Pins the default output naming: <stem>_processed.bmp next to the
    // input. Tested in isolation because it's easy to break when refactoring
    // default_legacy_preview_output_path.
    #[test]
    fn legacy_process_uses_default_preview_path_when_no_outputs_are_given() {
        let root = unique_temp_dir("legacy-default-output");
        fs::create_dir_all(&root).unwrap();
        let input = root.join("study.bmp");
        let output = root.join("study_processed.bmp");
        fs::write(&input, build_renderable_test_bmp()).unwrap();

        let mut stdout = Vec::new();
        let mut stderr = Vec::new();
        run(
            &["--input", input.to_str().unwrap()],
            &mut stdout,
            &mut stderr,
        )
        .unwrap();

        assert!(output.is_file());
        assert!(
            String::from_utf8(stdout)
                .unwrap()
                .contains(output.to_str().unwrap())
        );
        let _ = fs::remove_dir_all(root);
    }

    // Pins the override-on-top-of-preset merge order. xray preset gives
    // invert=false + equalize=true; user-supplied Some(...) overrides should
    // overwrite both. The two-call structure is "preset defaults" vs
    // "preset + overrides" and exercises both sides of legacy_process_command.
    #[test]
    fn legacy_process_command_preserves_and_overrides_preset_booleans() {
        let default = legacy_process_command(&LegacyOptions {
            preset: "xray".to_string(),
            ..LegacyOptions::default()
        })
        .unwrap();
        assert!(!default.invert);
        assert!(default.equalize);

        let overridden = legacy_process_command(&LegacyOptions {
            preset: "xray".to_string(),
            invert: Some(true),
            equalize: Some(false),
            ..LegacyOptions::default()
        })
        .unwrap();
        assert!(overridden.invert);
        assert!(!overridden.equalize);
    }

    #[test]
    fn legacy_preset_controls_trim_and_match_case_insensitively() {
        let controls = legacy_preset_controls(" XRAY ").unwrap();

        assert!(!controls.invert);
        assert!(controls.equalize);
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
