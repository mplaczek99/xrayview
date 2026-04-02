use std::collections::hash_map::DefaultHasher;
use std::hash::{Hash, Hasher};
use std::path::PathBuf;
use std::sync::{Arc, Mutex};

use anyhow::{Context, anyhow};
use serde::Serialize;

use crate::api::{
    AnalyzeStudyCommand, AnalyzeStudyCommandResult, AnalyzeStudyRequest, JobCommand, JobKind,
    JobResult, JobSnapshot, JobState, OpenStudyCommandResult, ProcessStudyCommand,
    ProcessStudyCommandResult, ProcessStudyRequest, RenderPreviewRequest, RenderStudyCommand,
    RenderStudyCommandResult, StartedJob, StudyRecord,
};
use crate::cache::{DiskCache, MemoryCache};
use crate::error::{BackendError, BackendErrorCode, BackendResult};
use crate::jobs::{JobRegistry, StartJobOutcome};
use crate::persistence::StudyCatalogStore;
use crate::preview::save_preview_png;
use crate::processing::pipeline::process_source_image;
use crate::render::render_plan::{RenderPlan, render_source_image};
use crate::study::StudyRegistry;
use crate::study::source_image::load_source_study;
use crate::tooth_measurement::analyze_grayscale_pixels;

use super::{
    analyze_study, describe_study, export_study, process_study, render_preview,
    validate_input_file,
};

type JobPublisher = Arc<dyn Fn(JobSnapshot) + Send + Sync + 'static>;

#[derive(Debug, Clone)]
pub struct AppState {
    registry: Arc<Mutex<StudyRegistry>>,
    jobs: JobRegistry,
    cache: MemoryCache,
    disk_cache: DiskCache,
    catalog: StudyCatalogStore,
}

impl Default for AppState {
    fn default() -> Self {
        let disk_cache = DiskCache::default();
        let catalog = StudyCatalogStore::new(disk_cache.root().join("state").join("catalog.json"));

        Self {
            registry: Arc::new(Mutex::new(StudyRegistry::default())),
            jobs: JobRegistry::default(),
            cache: MemoryCache::default(),
            disk_cache,
            catalog,
        }
    }
}

impl AppState {
    pub fn open_study(&self, input_path: PathBuf) -> BackendResult<StudyRecord> {
        let description = describe_study(&input_path)?;
        let mut registry = self
            .registry
            .lock()
            .map_err(|_| anyhow!("study registry is unavailable"))?;
        let study = registry.open_study(input_path, description.measurement_scale);
        let _ = self.catalog.record_opened_study(&study);
        Ok(study)
    }

    pub fn open_study_command(
        &self,
        input_path: PathBuf,
    ) -> BackendResult<OpenStudyCommandResult> {
        self.open_study(input_path)
            .map(|study| OpenStudyCommandResult { study })
    }

    pub fn render_study(
        &self,
        request: RenderStudyCommand,
        preview_output: PathBuf,
    ) -> BackendResult<RenderStudyCommandResult> {
        let study = self.require_study(&request.study_id)?;
        let result = render_preview(RenderPreviewRequest {
            input_path: study.input_path.clone(),
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

    pub fn start_render_job(
        &self,
        request: RenderStudyCommand,
        publish: JobPublisher,
    ) -> BackendResult<StartedJob> {
        let study = self.require_study(&request.study_id)?;
        let fingerprint = fingerprint(
            "render-study-v1",
            &serde_json::json!({
                "inputPath": &study.input_path,
            }),
        )?;

        if let Some(result) = self.cache.get(&fingerprint)? {
            let snapshot =
                self.jobs
                    .create_cached_job(JobKind::RenderStudy, Some(study.study_id.clone()), result)?;
            publish_job(&publish, snapshot.clone());
            return Ok(StartedJob {
                job_id: snapshot.job_id,
            });
        }

        let preview_output = self
            .disk_cache
            .artifact_path("render", &fingerprint, "png")?;
        let snapshot = match self.jobs.start_job(
            JobKind::RenderStudy,
            Some(study.study_id.clone()),
            fingerprint.clone(),
        )? {
            StartJobOutcome::Created(snapshot) => snapshot,
            StartJobOutcome::Existing(snapshot) => {
                publish_job(&publish, snapshot.clone());
                return Ok(StartedJob {
                    job_id: snapshot.job_id,
                });
            }
        };
        publish_job(&publish, snapshot.clone());

        let state = self.clone();
        let job_id = snapshot.job_id.clone();
        std::thread::spawn(move || {
            state.execute_render_job(
                job_id,
                study,
                preview_output,
                fingerprint,
                publish,
            );
        });

        Ok(StartedJob {
            job_id: snapshot.job_id,
        })
    }

    pub fn start_process_job(
        &self,
        request: ProcessStudyCommand,
        publish: JobPublisher,
    ) -> BackendResult<StartedJob> {
        let study = self.require_study(&request.study_id)?;
        let preview_fingerprint = fingerprint(
            "process-study-v1",
            &serde_json::json!({
                "inputPath": &study.input_path,
                "outputPath": &request.output_path,
                "presetId": &request.preset_id,
                "invert": request.invert,
                "brightness": request.brightness,
                "contrast": request.contrast,
                "equalize": request.equalize,
                "compare": request.compare,
                "pipeline": &request.pipeline,
                "palette": &request.palette,
            }),
        )?;

        if let Some(result) = self.cache.get(&preview_fingerprint)? {
            let snapshot = self.jobs.create_cached_job(
                JobKind::ProcessStudy,
                Some(study.study_id.clone()),
                result,
            )?;
            publish_job(&publish, snapshot.clone());
            return Ok(StartedJob {
                job_id: snapshot.job_id,
            });
        }

        let preview_output = self
            .disk_cache
            .artifact_path("process", &preview_fingerprint, "png")?;
        let dicom_path = match request.output_path.clone() {
            Some(path) => path,
            None => self
                .disk_cache
                .artifact_path("process", &preview_fingerprint, "dcm")?,
        };
        let snapshot = match self.jobs.start_job(
            JobKind::ProcessStudy,
            Some(study.study_id.clone()),
            preview_fingerprint.clone(),
        )? {
            StartJobOutcome::Created(snapshot) => snapshot,
            StartJobOutcome::Existing(snapshot) => {
                publish_job(&publish, snapshot.clone());
                return Ok(StartedJob {
                    job_id: snapshot.job_id,
                });
            }
        };
        publish_job(&publish, snapshot.clone());

        let state = self.clone();
        let job_id = snapshot.job_id.clone();
        std::thread::spawn(move || {
            state.execute_process_job(
                job_id,
                study,
                request,
                preview_output,
                dicom_path,
                preview_fingerprint,
                publish,
            );
        });

        Ok(StartedJob {
            job_id: snapshot.job_id,
        })
    }

    pub fn start_analyze_job(
        &self,
        request: AnalyzeStudyCommand,
        publish: JobPublisher,
    ) -> BackendResult<StartedJob> {
        let study = self.require_study(&request.study_id)?;
        let fingerprint = fingerprint(
            "analyze-study-v1",
            &serde_json::json!({
                "inputPath": &study.input_path,
            }),
        )?;

        if let Some(result) = self.cache.get(&fingerprint)? {
            let snapshot = self.jobs.create_cached_job(
                JobKind::AnalyzeStudy,
                Some(study.study_id.clone()),
                result,
            )?;
            publish_job(&publish, snapshot.clone());
            return Ok(StartedJob {
                job_id: snapshot.job_id,
            });
        }

        let preview_output = self
            .disk_cache
            .artifact_path("analyze", &fingerprint, "png")?;
        let snapshot = match self.jobs.start_job(
            JobKind::AnalyzeStudy,
            Some(study.study_id.clone()),
            fingerprint.clone(),
        )? {
            StartJobOutcome::Created(snapshot) => snapshot,
            StartJobOutcome::Existing(snapshot) => {
                publish_job(&publish, snapshot.clone());
                return Ok(StartedJob {
                    job_id: snapshot.job_id,
                });
            }
        };
        publish_job(&publish, snapshot.clone());

        let state = self.clone();
        let job_id = snapshot.job_id.clone();
        std::thread::spawn(move || {
            state.execute_analyze_job(job_id, study, preview_output, fingerprint, publish);
        });

        Ok(StartedJob {
            job_id: snapshot.job_id,
        })
    }

    pub fn get_job(&self, request: JobCommand) -> BackendResult<JobSnapshot> {
        self.jobs.get(&request.job_id)
    }

    pub fn cancel_job(&self, request: JobCommand) -> BackendResult<JobSnapshot> {
        self.jobs.cancel(&request.job_id)
    }

    fn execute_render_job(
        &self,
        job_id: String,
        study: StudyRecord,
        preview_output: PathBuf,
        fingerprint: String,
        publish: JobPublisher,
    ) {
        let outcome = (|| -> BackendResult<RenderStudyCommandResult> {
            self.check_cancelled(&job_id, &publish, "queued")?;
            self.update_job_progress(
                &job_id,
                JobState::Running,
                10,
                "validating",
                "Validating source study",
                &publish,
            )?;
            validate_input_file(&study.input_path)?;
            self.check_cancelled(&job_id, &publish, "validating")?;

            self.update_job_progress(
                &job_id,
                JobState::Running,
                35,
                "loadingStudy",
                "Loading source study",
                &publish,
            )?;
            let source = load_source_study(&study.input_path)?;
            self.check_cancelled(&job_id, &publish, "loadingStudy")?;

            self.update_job_progress(
                &job_id,
                JobState::Running,
                75,
                "renderingPreview",
                "Rendering preview",
                &publish,
            )?;
            let preview = render_source_image(&source.image, &RenderPlan::default());
            self.check_cancelled(&job_id, &publish, "renderingPreview")?;

            self.update_job_progress(
                &job_id,
                JobState::Running,
                90,
                "writingPreview",
                "Writing preview",
                &publish,
            )?;
            save_preview_png(&preview_output, &preview)?;

            Ok(RenderStudyCommandResult {
                study_id: study.study_id,
                preview_path: preview_output,
                measurement_scale: source.measurement_scale,
            })
        })();

        self.finish_job(
            job_id,
            fingerprint,
            JobKind::RenderStudy,
            outcome.map(JobResult::RenderStudy),
            publish,
        );
    }

    fn execute_process_job(
        &self,
        job_id: String,
        study: StudyRecord,
        request: ProcessStudyCommand,
        preview_output: PathBuf,
        dicom_path: PathBuf,
        fingerprint: String,
        publish: JobPublisher,
    ) {
        let outcome = (|| -> BackendResult<ProcessStudyCommandResult> {
            self.check_cancelled(&job_id, &publish, "queued")?;
            self.update_job_progress(
                &job_id,
                JobState::Running,
                10,
                "validating",
                "Validating processing request",
                &publish,
            )?;
            validate_input_file(&study.input_path)?;
            let process_request = ProcessStudyRequest {
                input_path: study.input_path.clone(),
                output_path: Some(dicom_path.clone()),
                preview_output: Some(preview_output.clone()),
                preset: request.preset_id.clone(),
                invert: request.invert,
                brightness: request.brightness,
                contrast: request.contrast,
                equalize: request.equalize,
                compare: request.compare,
                pipeline: request.pipeline.clone().map(join_pipeline_steps),
                palette: request.palette.map(|palette| palette.as_str().to_string()),
            };
            super::validate_processing_request(&process_request)?;
            let resolved = super::resolve_processing(&process_request)?;
            self.check_cancelled(&job_id, &publish, "validating")?;

            self.update_job_progress(
                &job_id,
                JobState::Running,
                30,
                "loadingStudy",
                "Loading source pixels",
                &publish,
            )?;
            let source = load_source_study(&study.input_path)?;
            self.check_cancelled(&job_id, &publish, "loadingStudy")?;

            self.update_job_progress(
                &job_id,
                JobState::Running,
                65,
                "processingPixels",
                "Applying processing pipeline",
                &publish,
            )?;
            let output = process_source_image(
                &source.image,
                &resolved.controls,
                &resolved.palette,
                resolved.compare,
            )?;
            self.check_cancelled(&job_id, &publish, "processingPixels")?;

            self.update_job_progress(
                &job_id,
                JobState::Running,
                84,
                "writingPreview",
                "Writing processed preview",
                &publish,
            )?;
            save_preview_png(&preview_output, &output.preview)?;
            self.check_cancelled(&job_id, &publish, "writingPreview")?;

            self.update_job_progress(
                &job_id,
                JobState::Running,
                95,
                "writingDicom",
                "Writing processed DICOM",
                &publish,
            )?;
            export_study(&output.preview, &source.metadata, &dicom_path)?;

            Ok(ProcessStudyCommandResult {
                study_id: study.study_id,
                preview_path: preview_output,
                dicom_path,
                loaded_width: source.image.width,
                loaded_height: source.image.height,
                mode: output.mode,
                measurement_scale: source.measurement_scale,
            })
        })();

        self.finish_job(
            job_id,
            fingerprint,
            JobKind::ProcessStudy,
            outcome.map(JobResult::ProcessStudy),
            publish,
        );
    }

    fn execute_analyze_job(
        &self,
        job_id: String,
        study: StudyRecord,
        preview_output: PathBuf,
        fingerprint: String,
        publish: JobPublisher,
    ) {
        let outcome = (|| -> BackendResult<AnalyzeStudyCommandResult> {
            self.check_cancelled(&job_id, &publish, "queued")?;
            self.update_job_progress(
                &job_id,
                JobState::Running,
                10,
                "validating",
                "Validating analysis request",
                &publish,
            )?;
            validate_input_file(&study.input_path)?;
            self.check_cancelled(&job_id, &publish, "validating")?;

            self.update_job_progress(
                &job_id,
                JobState::Running,
                35,
                "loadingStudy",
                "Loading source study",
                &publish,
            )?;
            let source = load_source_study(&study.input_path)?;
            self.check_cancelled(&job_id, &publish, "loadingStudy")?;

            self.update_job_progress(
                &job_id,
                JobState::Running,
                65,
                "renderingPreview",
                "Rendering analysis preview",
                &publish,
            )?;
            let preview = render_source_image(&source.image, &RenderPlan::default());
            save_preview_png(&preview_output, &preview)?;
            self.check_cancelled(&job_id, &publish, "renderingPreview")?;

            self.update_job_progress(
                &job_id,
                JobState::Running,
                88,
                "measuringTooth",
                "Measuring tooth candidate",
                &publish,
            )?;
            let analysis = analyze_grayscale_pixels(
                preview.width,
                preview.height,
                &preview.pixels,
                source.measurement_scale,
            )?;

            Ok(AnalyzeStudyCommandResult {
                study_id: study.study_id,
                preview_path: preview_output,
                analysis,
            })
        })();

        self.finish_job(
            job_id,
            fingerprint,
            JobKind::AnalyzeStudy,
            outcome.map(JobResult::AnalyzeStudy),
            publish,
        );
    }

    fn finish_job(
        &self,
        job_id: String,
        fingerprint: String,
        _job_kind: JobKind,
        outcome: BackendResult<JobResult>,
        publish: JobPublisher,
    ) {
        match outcome {
            Ok(result) => {
                let _ = self.cache.insert(fingerprint, result.clone());
                if let Ok(snapshot) = self.jobs.complete(&job_id, result) {
                    publish_job(&publish, snapshot);
                }
            }
            Err(error) if error.code == BackendErrorCode::Cancelled => {}
            Err(error) => {
                if let Ok(snapshot) = self.jobs.fail(&job_id, error) {
                    publish_job(&publish, snapshot);
                }
            }
        }
    }

    fn update_job_progress(
        &self,
        job_id: &str,
        state: JobState,
        percent: u8,
        stage: &str,
        message: &str,
        publish: &JobPublisher,
    ) -> BackendResult<()> {
        let snapshot = self
            .jobs
            .update_progress(job_id, state, percent, stage, message)?;
        publish_job(publish, snapshot);
        Ok(())
    }

    fn check_cancelled(
        &self,
        job_id: &str,
        publish: &JobPublisher,
        stage: &str,
    ) -> BackendResult<()> {
        if self.jobs.is_cancellation_requested(job_id)? {
            let snapshot = self
                .jobs
                .mark_cancelled(job_id, stage, "Cancelled by user")?;
            publish_job(publish, snapshot);
            return Err(BackendError::cancelled("job cancelled"));
        }

        Ok(())
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
            .map_err(Into::into)
    }
}

fn publish_job(publish: &JobPublisher, snapshot: JobSnapshot) {
    (publish)(snapshot);
}

fn fingerprint<T>(namespace: &str, payload: &T) -> BackendResult<String>
where
    T: Serialize,
{
    let serialized = serde_json::to_string(payload)
        .map_err(|error| BackendError::internal(format!("serialize job fingerprint: {error}")))?;
    let mut hasher = DefaultHasher::new();
    namespace.hash(&mut hasher);
    serialized.hash(&mut hasher);
    Ok(format!("{:016x}", hasher.finish()))
}

fn join_pipeline_steps(steps: Vec<crate::api::ProcessingPipelineStep>) -> String {
    steps
        .into_iter()
        .map(|step| step.as_str())
        .collect::<Vec<_>>()
        .join(",")
}
