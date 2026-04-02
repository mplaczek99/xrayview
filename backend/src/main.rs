mod compare;
mod palette;
mod preview;
mod processing;
mod save;

use std::io::{self, Write};
use std::path::{Path, PathBuf};

use anyhow::{bail, Context, Result};
use clap::Parser;
use dicom_dictionary_std::tags;
use dicom_object::{DicomAttribute, OpenFileOptions};
use serde::Serialize;

use crate::compare::combine_comparison;
use crate::palette::apply_named_palette;
use crate::preview::{load_dicom, save_preview_png};
use crate::processing::{process_grayscale_pixels, validate_pipeline, GrayscaleControls};
use crate::save::{SourceMetadata, save_dicom};

#[derive(Parser, Debug)]
#[command(name = "xrayview")]
struct Cli {
    /// Input DICOM path
    #[arg(long)]
    input: Option<PathBuf>,

    /// Output DICOM path
    #[arg(long)]
    output: Option<PathBuf>,

    /// Preview PNG path
    #[arg(long = "preview-output")]
    preview_output: Option<PathBuf>,

    /// Print processing preset metadata as JSON
    #[arg(long)]
    describe_presets: bool,

    /// Print study measurement metadata as JSON
    #[arg(long = "describe-study")]
    describe_study: bool,

    /// Processing preset
    #[arg(long, default_value = "default")]
    preset: String,

    /// Invert grayscale
    #[arg(long)]
    invert: bool,

    /// Brightness adjustment (-256 to 256)
    #[arg(long)]
    brightness: Option<i32>,

    /// Contrast multiplier (>= 0.0)
    #[arg(long)]
    contrast: Option<f64>,

    /// Apply histogram equalization
    #[arg(long)]
    equalize: bool,

    /// Show before/after comparison
    #[arg(long)]
    compare: bool,

    /// Comma-separated processing pipeline
    #[arg(long)]
    pipeline: Option<String>,

    /// Color palette (none, hot, bone)
    #[arg(long)]
    palette: Option<String>,
}

#[derive(Debug, Clone, Copy, Serialize)]
struct ProcessingControls {
    brightness: i32,
    contrast: f64,
    invert: bool,
    equalize: bool,
    palette: &'static str,
}

#[derive(Debug, Clone, Copy, Serialize)]
struct ProcessingPreset {
    id: &'static str,
    controls: ProcessingControls,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct ProcessingManifest {
    default_preset_id: &'static str,
    presets: Vec<ProcessingPreset>,
}

#[derive(Debug, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
struct MeasurementScale {
    row_spacing_mm: f64,
    column_spacing_mm: f64,
    source: &'static str,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct StudyDescription {
    #[serde(skip_serializing_if = "Option::is_none")]
    measurement_scale: Option<MeasurementScale>,
}

const DEFAULT_PRESET_ID: &str = "default";

const PROCESSING_PRESETS: [ProcessingPreset; 3] = [
    ProcessingPreset {
        id: DEFAULT_PRESET_ID,
        controls: ProcessingControls {
            brightness: 0,
            contrast: 1.0,
            invert: false,
            equalize: false,
            palette: "none",
        },
    },
    ProcessingPreset {
        id: "xray",
        controls: ProcessingControls {
            brightness: 10,
            contrast: 1.4,
            invert: false,
            equalize: true,
            palette: "bone",
        },
    },
    ProcessingPreset {
        id: "high-contrast",
        controls: ProcessingControls {
            brightness: 0,
            contrast: 1.8,
            invert: false,
            equalize: true,
            palette: "none",
        },
    },
];

fn main() {
    if let Err(error) = run() {
        eprintln!("error: {error}");
        std::process::exit(1);
    }
}

fn run() -> Result<()> {
    let mut cli = Cli::parse();

    if cli.describe_presets {
        serde_json::to_writer(io::stdout(), &processing_manifest())?;
        writeln!(io::stdout())?;
        return Ok(());
    }

    if cli.describe_study {
        let input_path = cli.input.as_ref().context("--input is required")?;
        validate_input_file(input_path)?;
        let description = describe_study(input_path)?;
        serde_json::to_writer(io::stdout(), &description)?;
        writeln!(io::stdout())?;
        return Ok(());
    }

    let input_path = cli.input.as_ref().context("--input is required")?;
    validate_input_file(input_path)?;

    if cli.output.is_none() {
        cli.output = Some(default_output_path(input_path));
    }

    if cli.preview_output.is_none() && cli.output.is_none() {
        bail!("either --output or --preview-output must be set")
    }

    validate_processing_args(&cli)?;
    let resolved = resolve_processing(&cli)?;
    let (mut preview, source_dataset) = load_dicom(input_path)?;
    let loaded_width = preview.width;
    let loaded_height = preview.height;

    // Extract the lightweight metadata we need for save_dicom, then drop the
    // heavy DefaultDicomObject (which holds the full pixel buffer) to free
    // 8-16 MB before pixel processing begins.
    let source_meta = if cli.output.is_some() {
        Some(SourceMetadata::extract(&source_dataset))
    } else {
        None
    };
    drop(source_dataset);

    let original_preview = if resolved.compare {
        Some(preview.clone())
    } else {
        None
    };
    let mut mode = process_grayscale_pixels(&mut preview.pixels, &resolved.controls)?;
    if resolved.palette != "none" {
        preview = apply_named_palette(&preview, &resolved.palette)?;
        mode = format!("{mode} with {} palette", resolved.palette);
    }
    if let Some(ref original) = original_preview {
        preview = combine_comparison(original, &preview)?;
        mode = format!("comparison of grayscale and {mode}");
    }

    if let Some(ref preview_path) = cli.preview_output {
        save_preview_png(preview_path, &preview)?;
    }

    if let Some(ref output_path) = cli.output {
        let dynamic_img = preview.into_dynamic_image();
        save_dicom(&dynamic_img, source_meta.as_ref().unwrap(), output_path)?;
    }

    println!("loaded dicom image: {loaded_width}x{loaded_height}");
    if let Some(ref preview_path) = cli.preview_output {
        println!("saved {mode} preview image: {}", preview_path.display());
    }
    if let Some(ref output_path) = cli.output {
        println!("saved {mode} dicom image: {}", output_path.display());
    }

    Ok(())
}

fn validate_input_file(path: &Path) -> Result<()> {
    if !path.exists() {
        bail!("input file does not exist: {}", path.display());
    }
    match path.extension().and_then(|e| e.to_str()) {
        Some(ext) if ext.eq_ignore_ascii_case("dcm") => {}
        _ => bail!("input file must have a .dcm extension: {}", path.display()),
    }
    Ok(())
}

fn default_output_path(input: &Path) -> PathBuf {
    let stem = input.file_stem().unwrap_or_default();
    let mut name = stem.to_os_string();
    name.push("_processed.dcm");
    input.with_file_name(name)
}

fn validate_processing_args(cli: &Cli) -> Result<()> {
    if let Some(b) = cli.brightness
        && !(-256..=256).contains(&b)
    {
        bail!("brightness must be between -256 and 256, got {b}");
    }
    if let Some(c) = cli.contrast
        && c < 0.0
    {
        bail!("contrast must be >= 0.0, got {c}");
    }
    if let Some(ref p) = cli.palette {
        match p.to_ascii_lowercase().as_str() {
            "none" | "hot" | "bone" => {}
            _ => bail!("palette must be one of: none, hot, bone"),
        }
    }
    Ok(())
}

struct ResolvedProcessing {
    controls: GrayscaleControls,
    palette: String,
    compare: bool,
}

fn resolve_processing(cli: &Cli) -> Result<ResolvedProcessing> {
    let preset_name = cli.preset.to_ascii_lowercase();
    let preset = lookup_preset(&preset_name)
        .with_context(|| format!("preset must be one of: {}", supported_preset_list()))?;

    // Presets provide the baseline, and explicit flags only override the
    // fields the caller specified instead of replacing the preset wholesale.
    let invert = cli.invert || preset.controls.invert;
    let brightness = cli.brightness.unwrap_or(preset.controls.brightness);
    let contrast = cli.contrast.unwrap_or(preset.controls.contrast);
    let equalize = cli.equalize || preset.controls.equalize;
    let palette = cli
        .palette
        .as_deref()
        .unwrap_or(preset.controls.palette)
        .to_ascii_lowercase();

    if !contrast.is_finite() || contrast < 0.0 {
        bail!("contrast must be a finite value greater than or equal to 0")
    }
    match palette.as_str() {
        "none" | "hot" | "bone" => {}
        _ => bail!("palette must be one of: none, hot, bone"),
    }
    validate_pipeline(cli.pipeline.as_deref())?;

    Ok(ResolvedProcessing {
        controls: GrayscaleControls {
            invert,
            brightness,
            contrast,
            equalize,
            pipeline: cli.pipeline.clone(),
        },
        palette,
        compare: cli.compare,
    })
}

fn processing_manifest() -> ProcessingManifest {
    ProcessingManifest {
        default_preset_id: DEFAULT_PRESET_ID,
        presets: PROCESSING_PRESETS.to_vec(),
    }
}

fn describe_study(path: &Path) -> Result<StudyDescription> {
    let obj = OpenFileOptions::new()
        .read_until(tags::PIXEL_DATA)
        .open_file(path)
        .with_context(|| format!("decode DICOM: {}", path.display()))?;

    Ok(StudyDescription {
        measurement_scale: measurement_scale_from_obj(&obj),
    })
}

fn measurement_scale_from_obj<O>(obj: &O) -> Option<MeasurementScale>
where
    O: dicom_object::DicomObject,
{
    // Prefer the most directly meaningful spacing tags first, then fall back
    // to scanner-derived alternatives when the study omits PixelSpacing.
    [
        (tags::PIXEL_SPACING, "PixelSpacing"),
        (tags::IMAGER_PIXEL_SPACING, "ImagerPixelSpacing"),
        (
            tags::NOMINAL_SCANNED_PIXEL_SPACING,
            "NominalScannedPixelSpacing",
        ),
    ]
    .into_iter()
    .find_map(|(tag, source)| {
        lookup_float_pair(obj, tag).and_then(|(row_spacing_mm, column_spacing_mm)| {
            (row_spacing_mm > 0.0 && column_spacing_mm > 0.0).then_some(MeasurementScale {
                row_spacing_mm,
                column_spacing_mm,
                source,
            })
        })
    })
}

fn lookup_float_pair<O>(obj: &O, tag: dicom_object::Tag) -> Option<(f64, f64)>
where
    O: dicom_object::DicomObject,
{
    let attr = obj.attr(tag).ok()?;
    let raw = attr.to_str().ok()?;
    parse_float_pair(raw.as_ref())
}

fn parse_float_pair(raw: &str) -> Option<(f64, f64)> {
    let mut parts = raw
        .split('\\')
        .map(str::trim)
        .filter(|part| !part.is_empty());
    let first = parts.next()?.parse().ok()?;
    let second = parts.next()?.parse().ok()?;
    Some((first, second))
}

fn lookup_preset(id: &str) -> Option<ProcessingPreset> {
    PROCESSING_PRESETS
        .iter()
        .copied()
        .find(|preset| preset.id == id)
}

fn supported_preset_list() -> String {
    PROCESSING_PRESETS
        .iter()
        .map(|preset| preset.id)
        .collect::<Vec<_>>()
        .join(", ")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_float_pair_accepts_dicom_pair() {
        assert_eq!(parse_float_pair("0.4\\0.6"), Some((0.4, 0.6)));
    }

    #[test]
    fn clap_parses_double_dash_flags() {
        let cli = Cli::try_parse_from([
            "xrayview",
            "--input",
            "study.dcm",
            "--describe-study",
            "--equalize",
        ])
        .expect("parse args");

        assert_eq!(cli.input, Some(PathBuf::from("study.dcm")));
        assert!(cli.describe_study);
        assert!(cli.equalize);
    }

    #[test]
    fn clap_parses_tauri_sidecar_invocations() {
        // xrayview --describe-presets
        let cli = Cli::try_parse_from(["xrayview", "--describe-presets"]).expect("describe-presets");
        assert!(cli.describe_presets);

        // xrayview --input foo.dcm --preview-output /tmp/out.png
        let cli = Cli::try_parse_from([
            "xrayview",
            "--input",
            "foo.dcm",
            "--preview-output",
            "/tmp/out.png",
        ])
        .expect("input + preview-output");
        assert_eq!(cli.input, Some(PathBuf::from("foo.dcm")));
        assert_eq!(cli.preview_output, Some(PathBuf::from("/tmp/out.png")));

        // xrayview --input foo.dcm --output bar.dcm --invert --brightness 20 --contrast 1.5 --equalize --palette bone
        let cli = Cli::try_parse_from([
            "xrayview",
            "--input",
            "foo.dcm",
            "--output",
            "bar.dcm",
            "--invert",
            "--brightness",
            "20",
            "--contrast",
            "1.5",
            "--equalize",
            "--palette",
            "bone",
        ])
        .expect("full processing flags");
        assert_eq!(cli.input, Some(PathBuf::from("foo.dcm")));
        assert_eq!(cli.output, Some(PathBuf::from("bar.dcm")));
        assert!(cli.invert);
        assert_eq!(cli.brightness, Some(20));
        assert_eq!(cli.contrast, Some(1.5));
        assert!(cli.equalize);
        assert_eq!(cli.palette, Some("bone".to_string()));
    }

    #[test]
    fn manifest_has_expected_defaults() {
        let manifest = processing_manifest();
        assert_eq!(manifest.default_preset_id, DEFAULT_PRESET_ID);
        assert_eq!(manifest.presets.len(), 3);
        assert_eq!(manifest.presets[1].id, "xray");
        assert_eq!(manifest.presets[1].controls.palette, "bone");
    }
}
