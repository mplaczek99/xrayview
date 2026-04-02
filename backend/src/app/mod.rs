use std::path::{Path, PathBuf};

use anyhow::{Context, bail};
use dicom_dictionary_std::tags;
use dicom_object::{DicomAttribute, DicomObject, OpenFileOptions};
use image::DynamicImage;

use crate::api::{
    AnalyzeStudyRequest, AnalyzeStudyResult, MeasurementScale, PaletteName, ProcessStudyRequest,
    ProcessStudyResult, ProcessingControls, ProcessingManifest, ProcessingPreset,
    RenderPreviewRequest, RenderPreviewResult, StudyDescription,
};
use crate::compare::combine_comparison;
use crate::error::BackendResult;
use crate::palette::apply_named_palette;
use crate::preview::{load_dicom, save_preview_png};
use crate::processing::{GrayscaleControls, process_grayscale_pixels, validate_pipeline};
use crate::save::{SourceMetadata, save_dicom};
use crate::tooth_measurement::analyze_preview;

const DEFAULT_PRESET_ID: &str = "default";

const PROCESSING_PRESETS: [ProcessingPreset; 3] = [
    ProcessingPreset {
        id: DEFAULT_PRESET_ID,
        controls: ProcessingControls {
            brightness: 0,
            contrast: 1.0,
            invert: false,
            equalize: false,
            palette: PaletteName::None,
        },
    },
    ProcessingPreset {
        id: "xray",
        controls: ProcessingControls {
            brightness: 10,
            contrast: 1.4,
            invert: false,
            equalize: true,
            palette: PaletteName::Bone,
        },
    },
    ProcessingPreset {
        id: "high-contrast",
        controls: ProcessingControls {
            brightness: 0,
            contrast: 1.8,
            invert: false,
            equalize: true,
            palette: PaletteName::None,
        },
    },
];

struct ResolvedProcessing {
    controls: GrayscaleControls,
    palette: String,
    compare: bool,
}

pub fn processing_manifest() -> ProcessingManifest {
    ProcessingManifest {
        default_preset_id: DEFAULT_PRESET_ID,
        presets: PROCESSING_PRESETS.to_vec(),
    }
}

pub fn describe_study(path: &Path) -> BackendResult<StudyDescription> {
    validate_input_file(path)?;

    let obj = OpenFileOptions::new()
        .read_until(tags::PIXEL_DATA)
        .open_file(path)
        .with_context(|| format!("decode DICOM: {}", path.display()))?;

    Ok(StudyDescription {
        measurement_scale: measurement_scale_from_obj(&obj),
    })
}

pub fn render_preview(request: RenderPreviewRequest) -> BackendResult<RenderPreviewResult> {
    validate_input_file(&request.input_path)?;

    let (preview, source_dataset) = load_dicom(&request.input_path)?;
    save_preview_png(&request.preview_output, &preview)?;

    Ok(RenderPreviewResult {
        loaded_width: preview.width,
        loaded_height: preview.height,
        preview_output: request.preview_output,
        measurement_scale: measurement_scale_from_obj(&source_dataset),
    })
}

pub fn process_study(mut request: ProcessStudyRequest) -> BackendResult<ProcessStudyResult> {
    validate_input_file(&request.input_path)?;

    if request.output_path.is_none() && request.preview_output.is_none() {
        request.output_path = Some(default_output_path(&request.input_path));
    }

    if request.output_path.is_none() && request.preview_output.is_none() {
        bail!("either --output or --preview-output must be set");
    }

    validate_processing_request(&request)?;
    let resolved = resolve_processing(&request)?;
    let (mut preview, source_dataset) = load_dicom(&request.input_path)?;
    let loaded_width = preview.width;
    let loaded_height = preview.height;
    let measurement_scale = measurement_scale_from_obj(&source_dataset);

    // Extract the lightweight metadata we need for save_dicom, then drop the
    // heavy DefaultDicomObject (which holds the full pixel buffer) to free
    // 8-16 MB before pixel processing begins.
    let source_meta = if request.output_path.is_some() {
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

    if let Some(ref preview_path) = request.preview_output {
        save_preview_png(preview_path, &preview)?;
    }

    if let Some(ref output_path) = request.output_path {
        let dynamic_img = preview.into_dynamic_image();
        export_study(
            &dynamic_img,
            source_meta
                .as_ref()
                .expect("source metadata is required when exporting a DICOM"),
            output_path,
        )?;
    }

    Ok(ProcessStudyResult {
        loaded_width,
        loaded_height,
        mode,
        preview_output: request.preview_output,
        output_path: request.output_path,
        measurement_scale,
    })
}

pub fn analyze_study(request: AnalyzeStudyRequest) -> BackendResult<AnalyzeStudyResult> {
    validate_input_file(&request.input_path)?;

    let (preview, source_dataset) = load_dicom(&request.input_path)?;

    if let Some(ref preview_output) = request.preview_output {
        save_preview_png(preview_output, &preview)?;
    }

    let analysis = analyze_preview(&preview, measurement_scale_from_obj(&source_dataset))?;

    Ok(AnalyzeStudyResult {
        preview_output: request.preview_output,
        analysis,
    })
}

pub(crate) fn export_study(
    img: &DynamicImage,
    source_meta: &SourceMetadata,
    output_path: &Path,
) -> BackendResult<()> {
    save_dicom(img, source_meta, output_path)
}

pub fn validate_input_file(path: &Path) -> BackendResult<()> {
    if !path.exists() {
        bail!("input file does not exist: {}", path.display());
    }
    Ok(())
}

fn default_output_path(input: &Path) -> PathBuf {
    let stem = input.file_stem().unwrap_or_default();
    let mut name = stem.to_os_string();
    name.push("_processed.dcm");
    input.with_file_name(name)
}

fn validate_processing_request(request: &ProcessStudyRequest) -> BackendResult<()> {
    if let Some(brightness) = request.brightness
        && !(-256..=256).contains(&brightness)
    {
        bail!("brightness must be between -256 and 256, got {brightness}");
    }
    if let Some(contrast) = request.contrast
        && contrast < 0.0
    {
        bail!("contrast must be >= 0.0, got {contrast}");
    }
    if let Some(ref palette) = request.palette {
        match palette.to_ascii_lowercase().as_str() {
            "none" | "hot" | "bone" => {}
            _ => bail!("palette must be one of: none, hot, bone"),
        }
    }
    Ok(())
}

fn resolve_processing(request: &ProcessStudyRequest) -> BackendResult<ResolvedProcessing> {
    let preset_name = request.preset.to_ascii_lowercase();
    let preset = lookup_preset(&preset_name)
        .with_context(|| format!("preset must be one of: {}", supported_preset_list()))?;

    // Presets provide the baseline, and explicit flags only override the
    // fields the caller specified instead of replacing the preset wholesale.
    let invert = request.invert || preset.controls.invert;
    let brightness = request.brightness.unwrap_or(preset.controls.brightness);
    let contrast = request.contrast.unwrap_or(preset.controls.contrast);
    let equalize = request.equalize || preset.controls.equalize;
    let palette = request
        .palette
        .as_deref()
        .unwrap_or(preset.controls.palette.as_str())
        .to_ascii_lowercase();

    if !contrast.is_finite() || contrast < 0.0 {
        bail!("contrast must be a finite value greater than or equal to 0");
    }
    match palette.as_str() {
        "none" | "hot" | "bone" => {}
        _ => bail!("palette must be one of: none, hot, bone"),
    }
    validate_pipeline(request.pipeline.as_deref())?;

    Ok(ResolvedProcessing {
        controls: GrayscaleControls {
            invert,
            brightness,
            contrast,
            equalize,
            pipeline: request.pipeline.clone(),
        },
        palette,
        compare: request.compare,
    })
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

pub fn measurement_scale_from_obj<O>(obj: &O) -> Option<MeasurementScale>
where
    O: DicomObject,
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
    O: DicomObject,
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

#[cfg(test)]
mod tests {
    use std::fs;

    use tempfile::TempDir;

    use super::*;

    #[test]
    fn parse_float_pair_accepts_dicom_pair() {
        assert_eq!(parse_float_pair("0.4\\0.6"), Some((0.4, 0.6)));
    }

    #[test]
    fn manifest_has_expected_defaults() {
        let manifest = processing_manifest();
        assert_eq!(manifest.default_preset_id, DEFAULT_PRESET_ID);
        assert_eq!(manifest.presets.len(), 3);
        assert_eq!(manifest.presets[1].id, "xray");
        assert_eq!(manifest.presets[1].controls.palette, PaletteName::Bone);
    }

    #[test]
    fn validate_input_file_accepts_non_dcm_extensions() {
        let temp_dir = TempDir::new().expect("create temp dir");
        let dicom_path = temp_dir.path().join("study.dicom");
        let no_extension_path = temp_dir.path().join("study");

        fs::write(&dicom_path, b"placeholder").expect("write .dicom file");
        fs::write(&no_extension_path, b"placeholder").expect("write extensionless file");

        assert!(validate_input_file(&dicom_path).is_ok());
        assert!(validate_input_file(&no_extension_path).is_ok());
    }
}
