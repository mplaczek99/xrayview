use std::path::PathBuf;
use std::sync::{Arc, Mutex};

use anyhow::{Context, anyhow};

use crate::api::{
    AnalyzeStudyCommand, AnalyzeStudyCommandResult, AnalyzeStudyRequest, ProcessStudyCommand,
    ProcessStudyCommandResult, ProcessStudyRequest, RenderPreviewRequest, RenderStudyCommand,
    RenderStudyCommandResult, StudyRecord,
};
use crate::error::BackendResult;
use crate::study::StudyRegistry;

use super::{analyze_study, describe_study, process_study, render_preview};

#[derive(Debug, Clone, Default)]
pub struct AppState {
    registry: Arc<Mutex<StudyRegistry>>,
}

impl AppState {
    pub fn open_study(&self, input_path: PathBuf) -> BackendResult<StudyRecord> {
        let description = describe_study(&input_path)?;
        let mut registry = self
            .registry
            .lock()
            .map_err(|_| anyhow!("study registry is unavailable"))?;

        Ok(registry.open_study(input_path, description.measurement_scale))
    }

    pub fn render_study(
        &self,
        request: RenderStudyCommand,
        preview_output: PathBuf,
    ) -> BackendResult<RenderStudyCommandResult> {
        let study = self.require_study(&request.study_id)?;
        let result = render_preview(RenderPreviewRequest {
            input_path: study.input_path,
            preview_output,
        })?;

        Ok(RenderStudyCommandResult {
            study_id: study.study_id,
            preview_path: result.preview_output,
            measurement_scale: result.measurement_scale,
        })
    }

    pub fn process_study(
        &self,
        request: ProcessStudyCommand,
        preview_output: PathBuf,
        dicom_path: PathBuf,
    ) -> BackendResult<ProcessStudyCommandResult> {
        let study = self.require_study(&request.study_id)?;
        let study_id = study.study_id.clone();
        let result = process_study(ProcessStudyRequest {
            input_path: study.input_path,
            output_path: Some(dicom_path.clone()),
            preview_output: Some(preview_output.clone()),
            preset: request.preset_id,
            invert: request.invert,
            brightness: request.brightness,
            contrast: request.contrast,
            equalize: request.equalize,
            compare: request.compare,
            pipeline: request.pipeline.map(join_pipeline_steps),
            palette: request.palette.map(|palette| palette.as_str().to_string()),
        })?;

        Ok(ProcessStudyCommandResult {
            study_id,
            preview_path: preview_output,
            dicom_path,
            loaded_width: result.loaded_width,
            loaded_height: result.loaded_height,
            mode: result.mode,
            measurement_scale: result.measurement_scale,
        })
    }

    pub fn analyze_study(
        &self,
        request: AnalyzeStudyCommand,
        preview_output: PathBuf,
    ) -> BackendResult<AnalyzeStudyCommandResult> {
        let study = self.require_study(&request.study_id)?;
        let study_id = study.study_id.clone();
        let result = analyze_study(AnalyzeStudyRequest {
            input_path: study.input_path,
            preview_output: Some(preview_output.clone()),
        })?;

        Ok(AnalyzeStudyCommandResult {
            study_id,
            preview_path: preview_output,
            analysis: result.analysis,
        })
    }

    fn require_study(&self, study_id: &str) -> BackendResult<StudyRecord> {
        let registry = self
            .registry
            .lock()
            .map_err(|_| anyhow!("study registry is unavailable"))?;

        registry
            .get(study_id)
            .cloned()
            .with_context(|| format!("study not found: {study_id}"))
    }
}

fn join_pipeline_steps(steps: Vec<crate::api::ProcessingPipelineStep>) -> String {
    steps
        .into_iter()
        .map(|step| step.as_str())
        .collect::<Vec<_>>()
        .join(",")
}
