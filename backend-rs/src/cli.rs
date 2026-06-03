// CLI entry. Two argv shapes are accepted:
//
//   * Modern subcommand form:  xrayview <subcommand> [args...]
//     e.g.  xrayview render-preview --input foo.bmp --output bar.bmp
//
//   * Legacy flag form:  xrayview --input foo.bmp --preset xray ...
//     A single positional-less command that does everything the old Go
//     binary did. Triggered when argv[0] starts with '-'. Kept around for
//     CI scripts and dental-suite integrations that haven't migrated.
//
// The two paths fork in `run`, share helpers only where shape is
// identical (BMP loading, processing dispatch). When you're editing this
// file, ask yourself: "does this belong in modern, legacy, or both?"

use std::{fs, io::Write, path::PathBuf};

use serde::Serialize;
use serde_json::json;

use crate::{
    analysis,
    bmp::{self, Metadata, RenderWindowMode, RenderedPreview},
    config::Config,
    contracts::{
        BACKEND_CONTRACT_VERSION, BackendError, MeasurementScale, PaletteName, ProcessStudyCommand,
        SERVICE_NAME, SUPPORTED_COMMANDS, default_processing_manifest,
    },
    processing::{self, GrayscaleControls},
    render::{self, PreviewImage},
};

// Sentinel value used as a CliError::Message payload to signal "the user
// asked for legacy --help; we printed it; return Ok". A real ControlFlow
// type would be cleaner but this is the legacy path — we make do.
const LEGACY_HELP_SENTINEL: &str = "__xrayview_legacy_help__";

// CLI error type — wraps everything that can go wrong into a single Result.
// Message is the catch-all "string error" variant the legacy code emits;
// the others are #[from] conversions so `?` works against all the upstream
// error types without manual mapping.
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

impl CliError {
    fn message(message: impl Into<String>) -> Self {
        Self::Message(message.into())
    }
}

impl From<String> for CliError {
    fn from(message: String) -> Self {
        Self::message(message)
    }
}

impl From<&str> for CliError {
    fn from(message: &str) -> Self {
        Self::message(message)
    }
}

impl From<BackendError> for CliError {
    fn from(error: BackendError) -> Self {
        Self::message(error.message)
    }
}

type CliResult<T> = Result<T, CliError>;

// Subcommand dispatcher. The legacy-vs-modern fork happens *before* the
// match — anything starting with `-` is legacy. Note --help is special-cased
// in both arms.
pub fn run(args: &[&str], stdout: &mut dyn Write, stderr: &mut dyn Write) -> CliResult<()> {
    // Some shell wrappers like to insert -- between argv[0] and the rest;
    // strip those before we look at args[0].
    let args = trim_leading_separators(args);
    if args.is_empty() {
        print_usage(stderr)?;
        return Err("expected a subcommand".to_string().into());
    }
    if args[0].starts_with('-') {
        return run_legacy_args(args, stdout, stderr);
    }

    match args[0] {
        "print-config" => print_config(stdout),
        "decode-source" => decode_source(&args[1..], stdout),
        "render-preview" => render_preview(&args[1..], stdout),
        "process-preview" => process_preview(&args[1..], stdout),
        "analyze-preview" => analyze_preview(&args[1..], stdout),
        "list-commands" => list_commands(stdout),
        "version" => {
            // Format must remain "<service> contract-v<n>" — external tools
            // grep for this exact shape.
            writeln!(
                stdout,
                "{SERVICE_NAME} contract-v{BACKEND_CONTRACT_VERSION}"
            )?;
            Ok(())
        }
        "help" | "-h" | "--help" => print_usage(stdout),
        command => {
            print_usage(stderr)?;
            Err(format!("unknown subcommand: {command}").into())
        }
    }
}

// Legacy entry — parse flags, then execute. The sentinel-match on
// LEGACY_HELP_SENTINEL is how we tell "user asked for help, parse short-
// circuited, all good" apart from a real error.
fn run_legacy_args(args: &[&str], stdout: &mut dyn Write, stderr: &mut dyn Write) -> CliResult<()> {
    let options = match parse_legacy_args(args, stderr) {
        Ok(options) => options,
        Err(CliError::Message(error)) if error == LEGACY_HELP_SENTINEL => return Ok(()),
        Err(error) => return Err(error),
    };
    execute_legacy(options, stdout)
}

// Manual flag parser. We don't use clap here because the legacy shape
// historically supports `--flag=value`, `--flag value`, and bare bool flags
// (`--invert` meaning `--invert true`), and clap configured to accept all
// three turned out to be more code than this loop. Hand-rolling argv parsing
// in 2026 is a choice, but it's the right one for this particular mess.
fn parse_legacy_args(args: &[&str], stderr: &mut dyn Write) -> CliResult<LegacyOptions> {
    let mut options = LegacyOptions {
        // Preset defaults to "default" so non-preset legacy invocations still
        // produce a sensible image.
        preset: "default".to_string(),
        ..LegacyOptions::default()
    };
    let mut index = 0;
    while index < args.len() {
        let arg = args[index];
        if matches!(arg, "-h" | "--help") {
            print_legacy_usage(stderr)?;
            // Surfaced as Ok() in the caller via the sentinel match.
            return Err(LEGACY_HELP_SENTINEL.into());
        }
        if !arg.starts_with('-') {
            return Err(format!("unexpected positional arguments: {arg}").into());
        }

        // split_flag_value handles "--flag=value" form; inline_value is Some
        // in that case, None when the value is in the next argv slot.
        let (flag, inline_value) = split_flag_value(arg);
        // canonical_legacy_flag normalizes -input and --input to --input,
        // accepts both single- and double-dash spellings.
        let flag = canonical_legacy_flag(flag);
        match flag.as_str() {
            "--input" => {
                options.input =
                    required_flag_value(args, &mut index, &flag, inline_value)?.to_string()
            }
            "--preview-output" => {
                options.preview_output =
                    required_flag_value(args, &mut index, &flag, inline_value)?.to_string()
            }
            "--describe-presets" => {
                options.describe_presets = parse_bool_flag(&flag, inline_value)?.unwrap_or(true)
            }
            "--describe-study" => {
                options.describe_study = parse_bool_flag(&flag, inline_value)?.unwrap_or(true)
            }
            "--preset" => {
                options.preset =
                    required_flag_value(args, &mut index, &flag, inline_value)?.to_string()
            }
            "--invert" => {
                options.invert = Some(parse_bool_flag(&flag, inline_value)?.unwrap_or(true))
            }
            "--brightness" => {
                let value = required_flag_value(args, &mut index, &flag, inline_value)?;
                options.brightness = Some(
                    value
                        .trim()
                        .parse::<i32>()
                        .map_err(|error| format!("parse --brightness {value:?}: {error}"))?,
                );
            }
            "--contrast" => {
                let value = required_flag_value(args, &mut index, &flag, inline_value)?;
                options.contrast = Some(
                    value
                        .trim()
                        .parse::<f64>()
                        .map_err(|error| format!("parse --contrast {value:?}: {error}"))?,
                );
            }
            "--equalize" => {
                options.equalize = Some(parse_bool_flag(&flag, inline_value)?.unwrap_or(true))
            }
            "--compare" => options.compare = parse_bool_flag(&flag, inline_value)?.unwrap_or(true),
            "--palette" => {
                options.palette =
                    required_flag_value(args, &mut index, &flag, inline_value)?.to_string()
            }
            unknown => return Err(format!("unknown workflow flag: {unknown}").into()),
        }
        index += 1;
    }

    Ok(options)
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
    let mode_count = [options.describe_presets, options.describe_study]
        .into_iter()
        .filter(|enabled| *enabled)
        .count();
    if mode_count > 1 {
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

// "--flag=value" → ("--flag", Some("value"))
// "--flag"       → ("--flag", None)
fn split_flag_value(arg: &str) -> (&str, Option<&str>) {
    arg.split_once('=')
        .map(|(flag, value)| (flag, Some(value)))
        .unwrap_or((arg, None))
}

// Normalize "-flag" → "--flag" so the match arm below doesn't have to list
// both spellings. Anything not starting with a dash passes through unchanged.
fn canonical_legacy_flag(flag: &str) -> String {
    if flag.starts_with("--") {
        flag.to_string()
    } else if let Some(name) = flag.strip_prefix('-') {
        format!("--{name}")
    } else {
        flag.to_string()
    }
}

// Read a flag's value from either inline (after =) or the next argv slot.
// Bumps `index` when it consumes the next slot — caller adds 1 more in the
// outer loop to step past it.
fn required_flag_value<'a>(
    args: &'a [&str],
    index: &mut usize,
    flag: &str,
    inline_value: Option<&'a str>,
) -> CliResult<&'a str> {
    if let Some(value) = inline_value {
        return Ok(value);
    }
    *index += 1;
    Ok(args
        .get(*index)
        .copied()
        .ok_or_else(|| format!("workflow flag {flag} requires a value"))?)
}

// Three-state bool: None (flag absent), Some(true) (--flag or --flag=true),
// Some(false) (--flag=false). Bare-flag-no-value is handled by the caller
// via `.unwrap_or(true)`.
fn parse_bool_flag(flag: &str, inline_value: Option<&str>) -> CliResult<Option<bool>> {
    Ok(inline_value
        .map(|value| {
            value
                .trim()
                .parse::<bool>()
                .map_err(|error| format!("parse workflow flag {flag} value {value:?}: {error}"))
        })
        .transpose()?)
}

// Strip any leading `--` separators (some wrappers insert these between
// the executable name and the real flags). Idempotent.
fn trim_leading_separators<'a>(mut args: &'a [&'a str]) -> &'a [&'a str] {
    while matches!(args.first(), Some(&"--")) {
        args = &args[1..];
    }
    args
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

fn decode_source(args: &[&str], stdout: &mut dyn Write) -> CliResult<()> {
    if args.len() != 1 {
        return Err("decode-source requires exactly one BMP path"
            .to_string()
            .into());
    }

    let metadata = bmp::read_file(args[0])?;
    let rendered = bmp::render_grayscale_preview_file(args[0])?;
    write_json(
        stdout,
        &DecodeSourceSummary::from_rendered_and_metadata(rendered, &metadata),
    )
}

fn render_preview(args: &[&str], stdout: &mut dyn Write) -> CliResult<()> {
    let options = parse_render_preview_args(args)?;
    let rendered = bmp::render_grayscale_preview_file_with_window_mode(
        &options.input_path,
        render_window_mode(options.full_range),
    )?;
    render::save_gray_bmp(
        &options.output_path,
        rendered.width,
        rendered.height,
        &rendered.pixels,
    )?;

    write_json(
        stdout,
        &RenderPreviewSummary {
            preview_output: options.output_path.display().to_string(),
            loaded_width: rendered.width,
            loaded_height: rendered.height,
            window_mode: if options.full_range {
                "full-range"
            } else {
                "default"
            },
            measurement_scale: rendered.measurement_scale,
            rendered_byte_count: rendered.pixels.len(),
        },
    )
}

fn process_preview(args: &[&str], stdout: &mut dyn Write) -> CliResult<()> {
    let options = parse_process_preview_args(args)?;
    let rendered = bmp::render_grayscale_preview_file_with_window_mode(
        &options.input_path,
        render_window_mode(options.full_range),
    )?;
    let source = rendered_preview_image(&rendered);
    let palette = processing::normalize_palette_name(&options.palette)?;
    let processed =
        processing::process_rendered_preview(source, options.controls, palette, options.compare)?;
    render::save_preview_bmp(&options.output_path, &processed.preview)?;

    write_json(
        stdout,
        &ProcessPreviewSummary {
            preview_output: options.output_path.display().to_string(),
            loaded_width: rendered.width,
            loaded_height: rendered.height,
            window_mode: if options.full_range {
                "full-range"
            } else {
                "default"
            },
            mode: processed.mode,
            palette: options.palette,
            compare: options.compare,
            measurement_scale: rendered.measurement_scale,
            rendered_byte_count: processed.preview.pixels.len(),
        },
    )
}

fn analyze_preview(args: &[&str], stdout: &mut dyn Write) -> CliResult<()> {
    let options = parse_analyze_preview_args(args)?;
    let rendered = bmp::render_grayscale_preview_file_for_tooth_analysis(&options.input_path)?;
    let source = rendered_preview_image(&rendered);
    let result = analysis::generate_tooth_overlay(&source)?;
    let preview = if options.filled {
        &result.filled_preview
    } else {
        &result.preview
    };
    render::save_preview_bmp(&options.output_path, preview)?;

    write_json(
        stdout,
        &AnalyzePreviewSummary {
            preview_output: options.output_path.display().to_string(),
            loaded_width: rendered.width,
            loaded_height: rendered.height,
            filled: options.filled,
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

fn parse_render_preview_args(args: &[&str]) -> CliResult<RenderPreviewOptions> {
    let mut full_range = false;
    let mut positional = Vec::with_capacity(2);
    for arg in args {
        match *arg {
            "--full-range" => full_range = true,
            value if value.starts_with('-') => {
                return Err(format!("unknown render-preview flag: {value}").into());
            }
            value => positional.push(value),
        }
    }

    if positional.len() != 2 {
        return Err(
            "render-preview requires INPUT_BMP OUTPUT_BMP and accepts optional --full-range"
                .to_string()
                .into(),
        );
    }

    Ok(RenderPreviewOptions {
        full_range,
        input_path: PathBuf::from(positional[0]),
        output_path: PathBuf::from(positional[1]),
    })
}

fn parse_process_preview_args(args: &[&str]) -> CliResult<ProcessPreviewOptions> {
    let mut options = ProcessPreviewOptions {
        full_range: false,
        controls: GrayscaleControls {
            invert: false,
            brightness: 0,
            contrast: 1.0,
            equalize: false,
        },
        palette: "none".to_string(),
        compare: false,
        input_path: PathBuf::new(),
        output_path: PathBuf::new(),
    };
    let mut positional = Vec::with_capacity(2);
    let mut index = 0;
    while index < args.len() {
        let arg = args[index];
        match arg {
            "--full-range" => options.full_range = true,
            "--invert" => options.controls.invert = true,
            "--equalize" => options.controls.equalize = true,
            "--compare" => options.compare = true,
            "--brightness" => {
                index += 1;
                let value = args.get(index).ok_or_else(|| {
                    "process-preview flag --brightness requires a value".to_string()
                })?;
                options.controls.brightness = value.parse::<i32>().map_err(|error| {
                    format!("parse process-preview brightness {value:?}: {error}")
                })?;
            }
            "--contrast" => {
                index += 1;
                let value = args.get(index).ok_or_else(|| {
                    "process-preview flag --contrast requires a value".to_string()
                })?;
                let parsed = value.parse::<f64>().map_err(|error| {
                    format!("parse process-preview contrast {value:?}: {error}")
                })?;
                if !parsed.is_finite() || parsed < 0.0 {
                    return Err(format!("contrast must be >= 0.0, got {parsed}").into());
                }
                options.controls.contrast = parsed;
            }
            "--palette" => {
                index += 1;
                options.palette = args
                    .get(index)
                    .ok_or_else(|| "process-preview flag --palette requires a value".to_string())?
                    .to_ascii_lowercase();
            }
            value if value.starts_with('-') => {
                return Err(format!("unknown process-preview flag: {value}").into());
            }
            value => positional.push(value),
        }
        index += 1;
    }

    if positional.len() != 2 {
        return Err("process-preview requires INPUT_BMP OUTPUT_BMP and accepts optional --full-range, --invert, --brightness, --contrast, --equalize, --palette, and --compare".to_string().into());
    }
    if !(-256..=256).contains(&options.controls.brightness) {
        return Err(format!(
            "brightness must be between -256 and 256, got {}",
            options.controls.brightness
        )
        .into());
    }

    options.input_path = PathBuf::from(positional[0]);
    options.output_path = PathBuf::from(positional[1]);
    Ok(options)
}

fn parse_analyze_preview_args(args: &[&str]) -> CliResult<AnalyzePreviewOptions> {
    let mut filled = false;
    let mut positional = Vec::with_capacity(2);
    for arg in args {
        match *arg {
            "--filled" => filled = true,
            value if value.starts_with('-') => {
                return Err(format!("unknown analyze-preview flag: {value}").into());
            }
            value => positional.push(value),
        }
    }
    if positional.len() != 2 {
        return Err(
            "analyze-preview requires INPUT_BMP OUTPUT_BMP and accepts optional --filled"
                .to_string()
                .into(),
        );
    }
    Ok(AnalyzePreviewOptions {
        filled,
        input_path: PathBuf::from(positional[0]),
        output_path: PathBuf::from(positional[1]),
    })
}

fn render_window_mode(full_range: bool) -> RenderWindowMode {
    if full_range {
        RenderWindowMode::FullRange
    } else {
        RenderWindowMode::Default
    }
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

fn print_usage(stream: &mut dyn Write) -> CliResult<()> {
    writeln!(stream, "usage: xrayview-backend-rs <subcommand>")?;
    writeln!(stream, "       xrayview-backend-rs [workflow flags]")?;
    writeln!(stream)?;
    writeln!(stream, "workflow flags:")?;
    writeln!(
        stream,
        "  --describe-presets                          print processing preset metadata as JSON"
    )?;
    writeln!(
        stream,
        "  --input <image.bmp> --describe-study       print image metadata as JSON"
    )?;
    writeln!(
        stream,
        "  --input <image.bmp> --preview-output <bmp> render a grayscale preview BMP"
    )?;
    writeln!(
        stream,
        "  --input <image.bmp> [processing flags]     write processed preview BMP"
    )?;
    writeln!(stream)?;
    writeln!(stream, "utility subcommands:")?;
    writeln!(
        stream,
        "  print-config             print resolved backend configuration as JSON"
    )?;
    writeln!(
        stream,
        "  decode-source            decode source pixels directly in Rust"
    )?;
    writeln!(
        stream,
        "  render-preview           render a grayscale BMP preview"
    )?;
    writeln!(
        stream,
        "  process-preview          render then run the Rust preview pipeline"
    )?;
    writeln!(
        stream,
        "  analyze-preview          render the analysis overlay preview"
    )?;
    writeln!(
        stream,
        "  list-commands            print supported command names"
    )?;
    writeln!(
        stream,
        "  version                  print service and contract version"
    )?;
    Ok(())
}

fn print_legacy_usage(stream: &mut dyn Write) -> CliResult<()> {
    writeln!(stream, "usage: xrayview-backend-rs [workflow flags]")?;
    writeln!(stream)?;
    writeln!(stream, "input / output:")?;
    writeln!(
        stream,
        "  --input <image.bmp>           path to the source BMP image"
    )?;
    writeln!(
        stream,
        "  --preview-output <image.bmp>  BMP preview output path"
    )?;
    writeln!(stream)?;
    writeln!(stream, "metadata:")?;
    writeln!(
        stream,
        "  --describe-presets            print processing preset metadata as JSON"
    )?;
    writeln!(
        stream,
        "  --describe-study              print study measurement metadata as JSON"
    )?;
    writeln!(stream)?;
    writeln!(stream, "processing:")?;
    writeln!(
        stream,
        "  --preset <id>                 default, xray, or high-contrast"
    )?;
    writeln!(stream, "  --invert[=true|false]         invert grayscale")?;
    writeln!(
        stream,
        "  --brightness <int>            brightness adjustment (-256 to 256)"
    )?;
    writeln!(
        stream,
        "  --contrast <float>            contrast multiplier (>= 0.0)"
    )?;
    writeln!(
        stream,
        "  --equalize[=true|false]       apply histogram equalization"
    )?;
    writeln!(
        stream,
        "  --compare                     show before/after comparison"
    )?;
    writeln!(stream, "  --palette <name>              none, hot, or bone")?;
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

// Parsed legacy flag state. invert/brightness/contrast/equalize are
// Option<T> so we can distinguish "user didn't set this" (None → fall back
// to preset) from "user explicitly set this to false/0". Without that we
// couldn't tell `--preset xray` from `--preset xray --equalize=false`.
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

struct RenderPreviewOptions {
    full_range: bool,
    input_path: PathBuf,
    output_path: PathBuf,
}

struct ProcessPreviewOptions {
    full_range: bool,
    controls: GrayscaleControls,
    palette: String,
    compare: bool,
    input_path: PathBuf,
    output_path: PathBuf,
}

struct AnalyzePreviewOptions {
    filled: bool,
    input_path: PathBuf,
    output_path: PathBuf,
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

    // Smoke test for the process-preview flag parser: every knob set on the
    // command line should land in the parsed options struct, with palette
    // case-insensitive ("BONE" → "bone").
    #[test]
    fn parse_process_preview_args_accepts_controls() {
        let options = parse_process_preview_args(&[
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

        assert!(options.full_range);
        assert!(options.controls.invert);
        assert!(options.controls.equalize);
        assert_eq!(options.controls.brightness, 10);
        assert_eq!(options.controls.contrast, 1.4);
        assert_eq!(options.palette, "bone");
        assert!(options.compare);
        assert_eq!(options.input_path, Path::new("input.bmp"));
        assert_eq!(options.output_path, Path::new("output.bmp"));
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
