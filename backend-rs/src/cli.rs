use std::{fs, io::Write, path::PathBuf};

use serde::Serialize;
use serde_json::json;

use crate::{
    analysis,
    config::Config,
    contracts::{
        BACKEND_CONTRACT_VERSION, MeasurementScale, PaletteName, ProcessStudyCommand, SERVICE_NAME,
        SUPPORTED_COMMANDS, default_processing_manifest,
    },
    bmp::{self, Metadata, RenderWindowMode, RenderedPreview},
    processing::{self, GrayscaleControls},
    render::{self, PreviewImage},
};

const LEGACY_HELP_SENTINEL: &str = "__xrayview_legacy_help__";

pub fn run(args: &[String], stdout: &mut dyn Write, stderr: &mut dyn Write) -> Result<(), String> {
    let args = args.iter().map(String::as_str).collect::<Vec<_>>();
    run_args(&args, stdout, stderr)
}

fn run_args(args: &[&str], stdout: &mut dyn Write, stderr: &mut dyn Write) -> Result<(), String> {
    let args = trim_leading_separators(args);
    if args.is_empty() {
        print_usage(stderr)?;
        return Err("expected a subcommand".to_string());
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
        "version" => writeln!(
            stdout,
            "{SERVICE_NAME} contract-v{BACKEND_CONTRACT_VERSION}"
        )
        .map_err(|error| error.to_string()),
        "help" | "-h" | "--help" => print_usage(stdout),
        command => {
            print_usage(stderr)?;
            Err(format!("unknown subcommand: {command}"))
        }
    }
}

fn run_legacy_args(
    args: &[&str],
    stdout: &mut dyn Write,
    stderr: &mut dyn Write,
) -> Result<(), String> {
    let options = match parse_legacy_args(args, stderr) {
        Ok(options) => options,
        Err(error) if error == LEGACY_HELP_SENTINEL => return Ok(()),
        Err(error) => return Err(error),
    };
    execute_legacy(options, stdout)
}

fn parse_legacy_args(args: &[&str], stderr: &mut dyn Write) -> Result<LegacyOptions, String> {
    let mut options = LegacyOptions {
        preset: "default".to_string(),
        ..LegacyOptions::default()
    };
    let mut index = 0;
    while index < args.len() {
        let arg = args[index];
        if matches!(arg, "-h" | "--help") {
            print_legacy_usage(stderr)?;
            return Err(LEGACY_HELP_SENTINEL.to_string());
        }
        if !arg.starts_with('-') {
            return Err(format!("unexpected positional arguments: {arg}"));
        }

        let (flag, inline_value) = split_flag_value(arg);
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
                options.invert =
                    OptionalBool::set(parse_bool_flag(&flag, inline_value)?.unwrap_or(true))
            }
            "--brightness" => {
                let value = required_flag_value(args, &mut index, &flag, inline_value)?;
                options.brightness = OptionalInt::set(
                    value
                        .trim()
                        .parse::<i32>()
                        .map_err(|error| format!("parse --brightness {value:?}: {error}"))?,
                );
            }
            "--contrast" => {
                let value = required_flag_value(args, &mut index, &flag, inline_value)?;
                options.contrast = OptionalFloat::set(
                    value
                        .trim()
                        .parse::<f64>()
                        .map_err(|error| format!("parse --contrast {value:?}: {error}"))?,
                );
            }
            "--equalize" => {
                options.equalize =
                    OptionalBool::set(parse_bool_flag(&flag, inline_value)?.unwrap_or(true))
            }
            "--compare" => options.compare = parse_bool_flag(&flag, inline_value)?.unwrap_or(true),
            "--palette" => {
                options.palette =
                    required_flag_value(args, &mut index, &flag, inline_value)?.to_string()
            }
            unknown => return Err(format!("unknown workflow flag: {unknown}")),
        }
        index += 1;
    }

    Ok(options)
}

fn execute_legacy(options: LegacyOptions, stdout: &mut dyn Write) -> Result<(), String> {
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

fn validate_legacy_mode_selection(options: &LegacyOptions) -> Result<(), String> {
    let mode_count = [options.describe_presets, options.describe_study]
        .into_iter()
        .filter(|enabled| *enabled)
        .count();
    if mode_count > 1 {
        return Err(
            "choose only one backend mode: --describe-presets or --describe-study".to_string(),
        );
    }
    Ok(())
}

fn required_input_path(options: &LegacyOptions) -> Result<String, String> {
    let input_path = options.input.trim();
    if input_path.is_empty() {
        return Err("--input is required".to_string());
    }
    validate_legacy_input_path(input_path)?;
    Ok(input_path.to_string())
}

fn validate_legacy_input_path(input_path: &str) -> Result<(), String> {
    let metadata = fs::metadata(input_path).map_err(|error| {
        if error.kind() == std::io::ErrorKind::NotFound {
            format!("input file does not exist: {input_path}")
        } else {
            format!("inspect input file {input_path}: {error}")
        }
    })?;
    if metadata.is_dir() {
        return Err(format!("input path must be a file: {input_path}"));
    }
    Ok(())
}

fn render_legacy_preview(
    input_path: &str,
    preview_output: &str,
    stdout: &mut dyn Write,
) -> Result<(), String> {
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
    )
    .map_err(|error| error.to_string())?;
    writeln!(stdout, "saved grayscale preview image: {preview_output}")
        .map_err(|error| error.to_string())
}

fn process_legacy_study(
    input_path: &str,
    preview_output: &str,
    options: &LegacyOptions,
    stdout: &mut dyn Write,
) -> Result<(), String> {
    let rendered = bmp::render_grayscale_preview_file(input_path)?;
    let command = legacy_process_command(options)?;
    let resolved =
        processing::resolve_process_study_command(&command).map_err(|error| error.message)?;
    let source = rendered_preview_image(&rendered);
    let processed = processing::process_rendered_preview(
        &source,
        resolved.controls,
        &resolved.palette,
        resolved.compare,
    )?;

    render::save_preview_bmp(preview_output, &processed.preview)?;

    writeln!(
        stdout,
        "loaded BMP image: {}x{}",
        rendered.width, rendered.height
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        stdout,
        "saved {} preview image: {}",
        processed.mode, preview_output
    )
    .map_err(|error| error.to_string())?;
    Ok(())
}

fn legacy_process_command(options: &LegacyOptions) -> Result<ProcessStudyCommand, String> {
    let mut command = ProcessStudyCommand {
        study_id: String::new(),
        preset_id: options.preset.clone(),
        invert: false,
        brightness: options.brightness.value,
        contrast: options.contrast.value,
        equalize: false,
        compare: options.compare,
        palette: parse_palette_name(&options.palette)?,
    };
    if let Some(controls) = legacy_preset_controls(&options.preset) {
        command.invert = controls.invert;
        command.equalize = controls.equalize;
    }
    if let Some(value) = options.invert.value {
        command.invert = value;
    }
    if let Some(value) = options.equalize.value {
        command.equalize = value;
    }
    Ok(command)
}

fn legacy_preset_controls(preset_id: &str) -> Option<crate::contracts::ProcessingControls> {
    let normalized = preset_id.trim().to_ascii_lowercase();
    default_processing_manifest()
        .presets
        .into_iter()
        .find(|preset| preset.id == normalized)
        .map(|preset| preset.controls)
}

fn parse_palette_name(value: &str) -> Result<Option<PaletteName>, String> {
    Ok(match value.trim().to_ascii_lowercase().as_str() {
        "" => None,
        "none" => Some(PaletteName::None),
        "hot" => Some(PaletteName::Hot),
        "bone" => Some(PaletteName::Bone),
        _ => return Err("palette must be one of: none, hot, bone".to_string()),
    })
}

fn is_plain_preview_request(options: &LegacyOptions) -> bool {
    !options.preview_output.trim().is_empty()
        && options.preset.trim().eq_ignore_ascii_case("default")
        && options.invert.value.is_none()
        && options.brightness.value.is_none()
        && options.contrast.value.is_none()
        && options.equalize.value.is_none()
        && !options.compare
        && options.palette.trim().is_empty()
}

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

fn split_flag_value(arg: &str) -> (&str, Option<&str>) {
    arg.split_once('=')
        .map(|(flag, value)| (flag, Some(value)))
        .unwrap_or((arg, None))
}

fn canonical_legacy_flag(flag: &str) -> String {
    if flag.starts_with("--") {
        flag.to_string()
    } else if let Some(name) = flag.strip_prefix('-') {
        format!("--{name}")
    } else {
        flag.to_string()
    }
}

fn required_flag_value<'a>(
    args: &'a [&str],
    index: &mut usize,
    flag: &str,
    inline_value: Option<&'a str>,
) -> Result<&'a str, String> {
    if let Some(value) = inline_value {
        return Ok(value);
    }
    *index += 1;
    args.get(*index)
        .copied()
        .ok_or_else(|| format!("workflow flag {flag} requires a value"))
}

fn parse_bool_flag(flag: &str, inline_value: Option<&str>) -> Result<Option<bool>, String> {
    inline_value
        .map(|value| {
            value
                .trim()
                .parse::<bool>()
                .map_err(|error| format!("parse workflow flag {flag} value {value:?}: {error}"))
        })
        .transpose()
}

fn trim_leading_separators<'a>(mut args: &'a [&'a str]) -> &'a [&'a str] {
    while matches!(args.first(), Some(&"--")) {
        args = &args[1..];
    }
    args
}

fn print_config(stdout: &mut dyn Write) -> Result<(), String> {
    let config = Config::load()?;
    write_json(
        stdout,
        &json!({
            "serviceName": config.service_name,
            "server": {
                "host": config.server.host,
                "port": config.server.port,
                "shutdownTimeout": format_duration(config.server.shutdown_timeout),
            },
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

fn decode_source(args: &[&str], stdout: &mut dyn Write) -> Result<(), String> {
    if args.len() != 1 {
        return Err("decode-source requires exactly one BMP path".to_string());
    }

    let metadata = bmp::read_file(args[0])?;
    let rendered = bmp::render_grayscale_preview_file(args[0])?;
    write_json(
        stdout,
        &DecodeSourceSummary::from_rendered_and_metadata(rendered, &metadata),
    )
}

fn render_preview(args: &[&str], stdout: &mut dyn Write) -> Result<(), String> {
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

fn process_preview(args: &[&str], stdout: &mut dyn Write) -> Result<(), String> {
    let options = parse_process_preview_args(args)?;
    let rendered = bmp::render_grayscale_preview_file_with_window_mode(
        &options.input_path,
        render_window_mode(options.full_range),
    )?;
    let source = rendered_preview_image(&rendered);
    let processed = processing::process_rendered_preview(
        &source,
        options.controls,
        &options.palette,
        options.compare,
    )?;
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

fn analyze_preview(args: &[&str], stdout: &mut dyn Write) -> Result<(), String> {
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

fn list_commands(stdout: &mut dyn Write) -> Result<(), String> {
    for command in SUPPORTED_COMMANDS {
        writeln!(stdout, "{command}").map_err(|error| error.to_string())?;
    }
    Ok(())
}

fn parse_render_preview_args(args: &[&str]) -> Result<RenderPreviewOptions, String> {
    let mut full_range = false;
    let mut positional = Vec::with_capacity(2);
    for arg in args {
        match *arg {
            "--full-range" => full_range = true,
            value if value.starts_with('-') => {
                return Err(format!("unknown render-preview flag: {value}"));
            }
            value => positional.push(value),
        }
    }

    if positional.len() != 2 {
        return Err(
            "render-preview requires INPUT_BMP OUTPUT_BMP and accepts optional --full-range"
                .to_string(),
        );
    }

    Ok(RenderPreviewOptions {
        full_range,
        input_path: PathBuf::from(positional[0]),
        output_path: PathBuf::from(positional[1]),
    })
}

fn parse_process_preview_args(args: &[&str]) -> Result<ProcessPreviewOptions, String> {
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
                    return Err(format!("contrast must be >= 0.0, got {parsed}"));
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
                return Err(format!("unknown process-preview flag: {value}"));
            }
            value => positional.push(value),
        }
        index += 1;
    }

    if positional.len() != 2 {
        return Err("process-preview requires INPUT_BMP OUTPUT_BMP and accepts optional --full-range, --invert, --brightness, --contrast, --equalize, --palette, and --compare".to_string());
    }
    if !(-256..=256).contains(&options.controls.brightness) {
        return Err(format!(
            "brightness must be between -256 and 256, got {}",
            options.controls.brightness
        ));
    }

    options.input_path = PathBuf::from(positional[0]);
    options.output_path = PathBuf::from(positional[1]);
    Ok(options)
}

fn parse_analyze_preview_args(args: &[&str]) -> Result<AnalyzePreviewOptions, String> {
    let mut filled = false;
    let mut positional = Vec::with_capacity(2);
    for arg in args {
        match *arg {
            "--filled" => filled = true,
            value if value.starts_with('-') => {
                return Err(format!("unknown analyze-preview flag: {value}"));
            }
            value => positional.push(value),
        }
    }
    if positional.len() != 2 {
        return Err(
            "analyze-preview requires INPUT_BMP OUTPUT_BMP and accepts optional --filled"
                .to_string(),
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

fn write_json(stdout: &mut dyn Write, value: &impl Serialize) -> Result<(), String> {
    serde_json::to_writer_pretty(&mut *stdout, value).map_err(|error| error.to_string())?;
    writeln!(stdout).map_err(|error| error.to_string())
}

fn write_json_compact(stdout: &mut dyn Write, value: &impl Serialize) -> Result<(), String> {
    serde_json::to_writer(&mut *stdout, value).map_err(|error| error.to_string())?;
    writeln!(stdout).map_err(|error| error.to_string())
}

fn print_usage(stream: &mut dyn Write) -> Result<(), String> {
    writeln!(stream, "usage: xrayview-backend-rs <subcommand>")
        .map_err(|error| error.to_string())?;
    writeln!(stream, "       xrayview-backend-rs [workflow flags]")
        .map_err(|error| error.to_string())?;
    writeln!(stream).map_err(|error| error.to_string())?;
    writeln!(stream, "workflow flags:").map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  --describe-presets                          print processing preset metadata as JSON"
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  --input <image.bmp> --describe-study       print image metadata as JSON"
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  --input <image.bmp> --preview-output <bmp> render a grayscale preview BMP"
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  --input <image.bmp> [processing flags]     write processed preview BMP"
    )
    .map_err(|error| error.to_string())?;
    writeln!(stream).map_err(|error| error.to_string())?;
    writeln!(stream, "utility subcommands:").map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  serve                    start the loopback HTTP backend"
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  print-config             print resolved backend configuration as JSON"
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  decode-source            decode source pixels directly in Rust"
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  render-preview           render a grayscale BMP preview"
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  process-preview          render then run the Rust preview pipeline"
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  analyze-preview          render the analysis overlay preview"
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  list-commands            print supported command names"
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  version                  print service and contract version"
    )
    .map_err(|error| error.to_string())
}

fn print_legacy_usage(stream: &mut dyn Write) -> Result<(), String> {
    writeln!(stream, "usage: xrayview-backend-rs [workflow flags]")
        .map_err(|error| error.to_string())?;
    writeln!(stream).map_err(|error| error.to_string())?;
    writeln!(stream, "input / output:").map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  --input <image.bmp>           path to the source BMP image"
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  --preview-output <image.bmp>  BMP preview output path"
    )
    .map_err(|error| error.to_string())?;
    writeln!(stream).map_err(|error| error.to_string())?;
    writeln!(stream, "metadata:").map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  --describe-presets            print processing preset metadata as JSON"
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  --describe-study              print study measurement metadata as JSON"
    )
    .map_err(|error| error.to_string())?;
    writeln!(stream).map_err(|error| error.to_string())?;
    writeln!(stream, "processing:").map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  --preset <id>                 default, xray, or high-contrast"
    )
    .map_err(|error| error.to_string())?;
    writeln!(stream, "  --invert[=true|false]         invert grayscale")
        .map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  --brightness <int>            brightness adjustment (-256 to 256)"
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  --contrast <float>            contrast multiplier (>= 0.0)"
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  --equalize[=true|false]       apply histogram equalization"
    )
    .map_err(|error| error.to_string())?;
    writeln!(
        stream,
        "  --compare                     show before/after comparison"
    )
    .map_err(|error| error.to_string())?;
    writeln!(stream, "  --palette <name>              none, hot, or bone")
        .map_err(|error| error.to_string())
}

fn format_duration(duration: std::time::Duration) -> String {
    if duration.as_millis().is_multiple_of(1000) {
        format!("{}s", duration.as_secs())
    } else {
        format!("{}ms", duration.as_millis())
    }
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
        let measurement_scale = rendered.measurement_scale.or_else(|| metadata.measurement_scale());
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

#[derive(Debug, Clone, Default, PartialEq)]
struct LegacyOptions {
    input: String,
    preview_output: String,
    describe_presets: bool,
    describe_study: bool,
    preset: String,
    invert: OptionalBool,
    brightness: OptionalInt,
    contrast: OptionalFloat,
    equalize: OptionalBool,
    compare: bool,
    palette: String,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
struct OptionalBool {
    value: Option<bool>,
}

impl OptionalBool {
    fn set(value: bool) -> Self {
        Self { value: Some(value) }
    }
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
struct OptionalInt {
    value: Option<i32>,
}

impl OptionalInt {
    fn set(value: i32) -> Self {
        Self { value: Some(value) }
    }
}

#[derive(Debug, Clone, Copy, Default, PartialEq)]
struct OptionalFloat {
    value: Option<f64>,
}

impl OptionalFloat {
    fn set(value: f64) -> Self {
        Self { value: Some(value) }
    }
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

    #[test]
    fn run_version_prints_contract_metadata() {
        let mut stdout = Vec::new();
        let mut stderr = Vec::new();

        run_args(&["version"], &mut stdout, &mut stderr).unwrap();

        assert_eq!(
            String::from_utf8(stdout).unwrap(),
            "xrayview-backend contract-v2\n"
        );
        assert!(stderr.is_empty());
    }

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
        run_args(
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
        run_args(
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

    #[test]
    fn decode_source_reports_bmp_metadata() {
        let root = unique_temp_dir("decode-source");
        fs::create_dir_all(&root).unwrap();
        let input = root.join("study.bmp");
        fs::write(&input, build_renderable_test_bmp()).unwrap();

        let mut stdout = Vec::new();
        let mut stderr = Vec::new();
        run_args(
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
    fn legacy_describe_commands_return_compact_json() {
        let root = unique_temp_dir("legacy-describe");
        fs::create_dir_all(&root).unwrap();
        let input = root.join("study.bmp");
        fs::write(&input, build_renderable_test_bmp()).unwrap();

        let mut stdout = Vec::new();
        let mut stderr = Vec::new();
        run_args(&["--describe-presets"], &mut stdout, &mut stderr).unwrap();
        let manifest_stdout = String::from_utf8(stdout.clone()).unwrap();
        assert!(manifest_stdout.contains(r#""defaultPresetId":"default""#));
        assert!(!manifest_stdout.contains('\n') || manifest_stdout.ends_with('\n'));
        let manifest: serde_json::Value = serde_json::from_slice(&stdout).unwrap();
        assert_eq!(manifest["presets"].as_array().unwrap().len(), 3);

        stdout.clear();
        run_args(
            &["--input", input.to_str().unwrap(), "--describe-study"],
            &mut stdout,
            &mut stderr,
        )
        .unwrap();
        let study: serde_json::Value = serde_json::from_slice(&stdout).unwrap();
        assert!(study.get("measurementScale").is_none());
        let _ = fs::remove_dir_all(root);
    }

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
        run_args(
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
        run_args(
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

    #[test]
    fn legacy_process_uses_default_preview_path_when_no_outputs_are_given() {
        let root = unique_temp_dir("legacy-default-output");
        fs::create_dir_all(&root).unwrap();
        let input = root.join("study.bmp");
        let output = root.join("study_processed.bmp");
        fs::write(&input, build_renderable_test_bmp()).unwrap();

        let mut stdout = Vec::new();
        let mut stderr = Vec::new();
        run_args(
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
            invert: OptionalBool::set(true),
            equalize: OptionalBool::set(false),
            ..LegacyOptions::default()
        })
        .unwrap();
        assert!(overridden.invert);
        assert!(!overridden.equalize);
    }

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
