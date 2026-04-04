pub mod state;

use std::path::{Path, PathBuf};

use anyhow::Context;
use dicom_dictionary_std::tags;
use dicom_object::OpenFileOptions;

use crate::api::{
    AnalyzeStudyRequest, AnalyzeStudyResult, PaletteName, ProcessStudyRequest, ProcessStudyResult,
    ProcessingControls, ProcessingManifest, ProcessingPreset, RenderPreviewRequest,
    RenderPreviewResult, StudyDescription,
};
use crate::error::{BackendError, BackendResult};
use crate::export::secondary_capture::export_secondary_capture;
use crate::preview::{PreviewImage, save_preview_png};
use crate::processing::GrayscaleControls;
use crate::processing::pipeline::process_source_image;
use crate::render::render_plan::{RenderPlan, render_source_image};
use crate::study::source_image::{load_source_study, measurement_scale_from_obj};
use crate::tooth_measurement::analyze_grayscale_pixels;

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

    let source = load_source_study(&request.input_path)?;
    let preview = render_source_image(&source.image, &RenderPlan::default());
    save_preview_png(&request.preview_output, &preview)?;

    Ok(RenderPreviewResult {
        loaded_width: source.image.width,
        loaded_height: source.image.height,
        preview_output: request.preview_output,
        measurement_scale: source.measurement_scale,
    })
}

pub fn process_study(mut request: ProcessStudyRequest) -> BackendResult<ProcessStudyResult> {
    validate_input_file(&request.input_path)?;

    if request.output_path.is_none() && request.preview_output.is_none() {
        request.output_path = Some(default_output_path(&request.input_path));
    }

    if request.output_path.is_none() && request.preview_output.is_none() {
        return Err(BackendError::invalid_input(
            "either --output or --preview-output must be set",
        ));
    }

    validate_processing_request(&request)?;
    let resolved = resolve_processing(&request)?;
    let source = load_source_study(&request.input_path)?;
    let loaded_width = source.image.width;
    let loaded_height = source.image.height;
    let measurement_scale = source.measurement_scale;
    let output = process_source_image(
        &source.image,
        &resolved.controls,
        &resolved.palette,
        resolved.compare,
    )?;

    if let Some(ref preview_path) = request.preview_output {
        save_preview_png(preview_path, &output.preview)?;
    }

    if let Some(ref output_path) = request.output_path {
        export_study(&output.preview, &source.metadata, output_path)?;
    }

    Ok(ProcessStudyResult {
        loaded_width,
        loaded_height,
        mode: output.mode,
        preview_output: request.preview_output,
        output_path: request.output_path,
        measurement_scale,
    })
}

pub fn analyze_study(request: AnalyzeStudyRequest) -> BackendResult<AnalyzeStudyResult> {
    validate_input_file(&request.input_path)?;

    let source = load_source_study(&request.input_path)?;
    let preview = render_source_image(&source.image, &RenderPlan::default());

    if let Some(ref preview_output) = request.preview_output {
        save_preview_png(preview_output, &preview)?;
    }

    let analysis = analyze_grayscale_pixels(
        preview.width,
        preview.height,
        &preview.pixels,
        source.measurement_scale,
    )?;

    Ok(AnalyzeStudyResult {
        preview_output: request.preview_output,
        analysis,
    })
}

pub(crate) fn export_study(
    preview: &PreviewImage,
    source_meta: &crate::study::source_image::SourceMetadata,
    output_path: &Path,
) -> BackendResult<()> {
    Ok(export_secondary_capture(preview, source_meta, output_path)?)
}

pub fn validate_input_file(path: &Path) -> BackendResult<()> {
    if !path.exists() {
        return Err(BackendError::not_found(format!(
            "input file does not exist: {}",
            path.display()
        )));
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
        return Err(BackendError::invalid_input(format!(
            "brightness must be between -256 and 256, got {brightness}"
        )));
    }
    if let Some(contrast) = request.contrast
        && contrast < 0.0
    {
        return Err(BackendError::invalid_input(format!(
            "contrast must be >= 0.0, got {contrast}"
        )));
    }
    if let Some(ref palette) = request.palette {
        match palette.to_ascii_lowercase().as_str() {
            "none" | "hot" | "bone" => {}
            _ => {
                return Err(BackendError::invalid_input(
                    "palette must be one of: none, hot, bone",
                ));
            }
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
        return Err(BackendError::invalid_input(
            "contrast must be a finite value greater than or equal to 0",
        ));
    }
    match palette.as_str() {
        "none" | "hot" | "bone" => {}
        _ => {
            return Err(BackendError::invalid_input(
                "palette must be one of: none, hot, bone",
            ));
        }
    }
    Ok(ResolvedProcessing {
        controls: GrayscaleControls {
            invert,
            brightness,
            contrast,
            equalize,
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

#[cfg(test)]
mod tests {
    use std::fs;

    use tempfile::TempDir;

    use super::*;

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
