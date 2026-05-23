#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;
use std::{
    collections::HashMap,
    fs,
    path::Path,
    sync::{
        Arc, Mutex,
        atomic::{AtomicU64, Ordering},
        mpsc::{Receiver, SyncSender, TrySendError, sync_channel},
    },
    thread,
    time::UNIX_EPOCH,
};

#[cfg(test)]
use crate::cache::SourcePreviewCacheStats;
use chrono::{DateTime, Utc};
use serde::Serialize;

use crate::{
    analysis, annotations,
    bmp,
    cache::{SourcePreviewCache, Store},
    config::Config,
    contracts::{
        AnalyzeStudyCommand, AnalyzeStudyCommandResult, BackendError, GetJobsCommand, JobCommand,
        JobKind, JobProgress, JobResult, JobSnapshot, JobState, MeasureLineAnnotationCommand,
        MeasureLineAnnotationCommandResult, MeasurementScale, OpenStudyCommand,
        OpenStudyCommandResult, ProcessStudyCommand, ProcessStudyCommandResult, ProcessingManifest,
        RenderStudyCommand, RenderStudyCommandResult, StartedJob, StudyRecord,
        default_processing_manifest,
    },
    persistence, processing, render,
};

pub struct App {
    config: Config,
    started_at: DateTime<Utc>,
    cache_session_id: String,
    cache: Store,
    studies: Mutex<HashMap<String, StudyRecord>>,
    jobs: Mutex<HashMap<String, JobSnapshot>>,
    active_fingerprints: Mutex<HashMap<String, String>>,
    result_cache: Mutex<HashMap<String, JobResult>>,
    source_preview_cache: SourcePreviewCache,
    job_update_subscribers: Mutex<HashMap<u64, SyncSender<JobSnapshot>>>,
    persistence: persistence::Catalog,
    next_study_number: AtomicU64,
    next_job_number: AtomicU64,
    next_subscriber_number: AtomicU64,
}

pub const JOB_UPDATE_BUFFER_SIZE: usize = 16;
const MAX_ARTIFACT_BYTES: u64 = 256 * 1024 * 1024;
const MAX_TERMINAL_JOBS: usize = 64;

pub struct JobUpdateSubscription {
    pub id: u64,
    pub receiver: Receiver<JobSnapshot>,
}

enum AsyncJobReservation {
    Existing(String),
    Created(String),
}

impl App {
    pub fn new(config: Config) -> Result<Self, BackendError> {
        let started_at = Utc::now();
        let cache_session_id = format!(
            "{}-{}",
            std::process::id(),
            started_at.timestamp_nanos_opt().unwrap_or_default()
        );
        let cache = Store::new_with_paths(&config.paths.cache_dir, &config.paths.persistence_dir);
        let persistence = persistence::Catalog::new(&config.paths.persistence_dir);
        Ok(Self {
            config,
            started_at,
            cache_session_id,
            cache,
            studies: Mutex::new(HashMap::new()),
            jobs: Mutex::new(HashMap::new()),
            active_fingerprints: Mutex::new(HashMap::new()),
            result_cache: Mutex::new(HashMap::new()),
            source_preview_cache: SourcePreviewCache::default_session_cache(),
            job_update_subscribers: Mutex::new(HashMap::new()),
            persistence,
            next_study_number: AtomicU64::new(1),
            next_job_number: AtomicU64::new(1),
            next_subscriber_number: AtomicU64::new(1),
        })
    }

    pub fn config(&self) -> &Config {
        &self.config
    }

    pub fn started_at(&self) -> DateTime<Utc> {
        self.started_at
    }

    pub fn prepare(&self) -> Result<(), BackendError> {
        self.cache.ensure()?;
        self.persistence.ensure()
    }

    pub fn study_count(&self) -> usize {
        self.studies
            .lock()
            .expect("study registry mutex poisoned")
            .len()
    }

    pub fn supported_job_kinds(&self) -> [&'static str; 3] {
        ["renderStudy", "analyzeStudy", "processStudy"]
    }

    pub fn get_processing_manifest(&self) -> ProcessingManifest {
        default_processing_manifest()
    }

    #[cfg(test)]
    fn source_preview_cache_stats(&self) -> SourcePreviewCacheStats {
        self.source_preview_cache.stats()
    }

    pub fn subscribe_job_updates(&self) -> JobUpdateSubscription {
        let id = self.next_subscriber_number.fetch_add(1, Ordering::Relaxed);
        let (sender, receiver) = sync_channel(JOB_UPDATE_BUFFER_SIZE);
        self.job_update_subscribers
            .lock()
            .expect("job update subscriber mutex poisoned")
            .insert(id, sender);
        JobUpdateSubscription { id, receiver }
    }

    pub fn unsubscribe_job_updates(&self, subscription_id: u64) {
        self.job_update_subscribers
            .lock()
            .expect("job update subscriber mutex poisoned")
            .remove(&subscription_id);
    }

    pub fn start_render_job(
        &self,
        command: RenderStudyCommand,
    ) -> Result<StartedJob, BackendError> {
        let study_id = command.study_id.trim();
        if study_id.is_empty() {
            return Err(BackendError::invalid_input("studyId is required"));
        }

        let study = self
            .studies
            .lock()
            .expect("study registry mutex poisoned")
            .get(study_id)
            .cloned()
            .ok_or_else(|| BackendError::not_found(format!("study not found: {study_id}")))?;

        let fingerprint = self.render_fingerprint(&study)?;
        if let Some(started) =
            self.start_cached_job(&fingerprint, JobKind::RenderStudy, study.study_id.clone())?
        {
            return Ok(started);
        }

        let job_id = format!(
            "job-{}",
            self.next_job_number.fetch_add(1, Ordering::Relaxed)
        );

        let snapshot = match self.render_study_job(&job_id, &fingerprint, &study) {
            Ok(snapshot) => snapshot,
            Err(error) => failed_job_snapshot(
                job_id.clone(),
                JobKind::RenderStudy,
                Some(study.study_id.clone()),
                error,
            ),
        };
        let should_evict = self.store_completed_result(&fingerprint, &snapshot);
        self.store_job_snapshot(snapshot.clone());
        self.publish_job_update(&snapshot);
        if should_evict {
            self.evict_artifacts_if_needed();
        }

        Ok(StartedJob { job_id })
    }

    pub fn start_render_job_async(
        self: &Arc<Self>,
        command: RenderStudyCommand,
    ) -> Result<StartedJob, BackendError> {
        let study_id = command.study_id.trim();
        if study_id.is_empty() {
            return Err(BackendError::invalid_input("studyId is required"));
        }

        let study = self
            .studies
            .lock()
            .expect("study registry mutex poisoned")
            .get(study_id)
            .cloned()
            .ok_or_else(|| BackendError::not_found(format!("study not found: {study_id}")))?;

        let fingerprint = self.render_fingerprint(&study)?;
        if let Some(started) =
            self.start_cached_job(&fingerprint, JobKind::RenderStudy, study.study_id.clone())?
        {
            return Ok(started);
        }

        match self.reserve_async_job(&fingerprint, JobKind::RenderStudy, study.study_id.clone())? {
            AsyncJobReservation::Existing(job_id) => Ok(StartedJob { job_id }),
            AsyncJobReservation::Created(job_id) => {
                let app = Arc::clone(self);
                let worker_job_id = job_id.clone();
                thread::spawn(move || {
                    app.run_render_job_async(worker_job_id, fingerprint, study);
                });
                Ok(StartedJob { job_id })
            }
        }
    }

    pub fn start_process_job(
        &self,
        command: ProcessStudyCommand,
    ) -> Result<StartedJob, BackendError> {
        let study_id = command.study_id.trim();
        if study_id.is_empty() {
            return Err(BackendError::invalid_input("studyId is required"));
        }

        let study = self
            .studies
            .lock()
            .expect("study registry mutex poisoned")
            .get(study_id)
            .cloned()
            .ok_or_else(|| BackendError::not_found(format!("study not found: {study_id}")))?;

        let fingerprint = self.process_fingerprint(&study, &command)?;
        if let Some(started) =
            self.start_cached_job(&fingerprint, JobKind::ProcessStudy, study.study_id.clone())?
        {
            return Ok(started);
        }

        let job_id = format!(
            "job-{}",
            self.next_job_number.fetch_add(1, Ordering::Relaxed)
        );

        let snapshot = match self.process_study_job(&job_id, &fingerprint, &study, &command) {
            Ok(snapshot) => snapshot,
            Err(error) => failed_job_snapshot(
                job_id.clone(),
                JobKind::ProcessStudy,
                Some(study.study_id.clone()),
                error,
            ),
        };
        let should_evict = self.store_completed_result(&fingerprint, &snapshot);
        self.store_job_snapshot(snapshot.clone());
        self.publish_job_update(&snapshot);
        if should_evict {
            self.evict_artifacts_if_needed();
        }

        Ok(StartedJob { job_id })
    }

    pub fn start_process_job_async(
        self: &Arc<Self>,
        command: ProcessStudyCommand,
    ) -> Result<StartedJob, BackendError> {
        let study_id = command.study_id.trim();
        if study_id.is_empty() {
            return Err(BackendError::invalid_input("studyId is required"));
        }

        let study = self
            .studies
            .lock()
            .expect("study registry mutex poisoned")
            .get(study_id)
            .cloned()
            .ok_or_else(|| BackendError::not_found(format!("study not found: {study_id}")))?;

        let fingerprint = self.process_fingerprint(&study, &command)?;
        if let Some(started) =
            self.start_cached_job(&fingerprint, JobKind::ProcessStudy, study.study_id.clone())?
        {
            return Ok(started);
        }

        match self.reserve_async_job(&fingerprint, JobKind::ProcessStudy, study.study_id.clone())? {
            AsyncJobReservation::Existing(job_id) => Ok(StartedJob { job_id }),
            AsyncJobReservation::Created(job_id) => {
                let app = Arc::clone(self);
                let worker_job_id = job_id.clone();
                thread::spawn(move || {
                    app.run_process_job_async(worker_job_id, fingerprint, study, command);
                });
                Ok(StartedJob { job_id })
            }
        }
    }

    pub fn start_analyze_job(
        &self,
        command: AnalyzeStudyCommand,
    ) -> Result<StartedJob, BackendError> {
        let study_id = command.study_id.trim();
        if study_id.is_empty() {
            return Err(BackendError::invalid_input("studyId is required"));
        }

        let study = self
            .studies
            .lock()
            .expect("study registry mutex poisoned")
            .get(study_id)
            .cloned()
            .ok_or_else(|| BackendError::not_found(format!("study not found: {study_id}")))?;

        let fingerprint = self.analyze_fingerprint(&study)?;
        if let Some(started) =
            self.start_cached_job(&fingerprint, JobKind::AnalyzeStudy, study.study_id.clone())?
        {
            return Ok(started);
        }

        let job_id = format!(
            "job-{}",
            self.next_job_number.fetch_add(1, Ordering::Relaxed)
        );

        let snapshot = match self.analyze_study_job(&job_id, &fingerprint, &study) {
            Ok(snapshot) => snapshot,
            Err(error) => failed_job_snapshot(
                job_id.clone(),
                JobKind::AnalyzeStudy,
                Some(study.study_id.clone()),
                error,
            ),
        };
        let should_evict = self.store_completed_result(&fingerprint, &snapshot);
        self.store_job_snapshot(snapshot.clone());
        self.publish_job_update(&snapshot);
        if should_evict {
            self.evict_artifacts_if_needed();
        }

        Ok(StartedJob { job_id })
    }

    pub fn start_analyze_job_async(
        self: &Arc<Self>,
        command: AnalyzeStudyCommand,
    ) -> Result<StartedJob, BackendError> {
        let study_id = command.study_id.trim();
        if study_id.is_empty() {
            return Err(BackendError::invalid_input("studyId is required"));
        }

        let study = self
            .studies
            .lock()
            .expect("study registry mutex poisoned")
            .get(study_id)
            .cloned()
            .ok_or_else(|| BackendError::not_found(format!("study not found: {study_id}")))?;

        let fingerprint = self.analyze_fingerprint(&study)?;
        if let Some(started) =
            self.start_cached_job(&fingerprint, JobKind::AnalyzeStudy, study.study_id.clone())?
        {
            return Ok(started);
        }

        match self.reserve_async_job(&fingerprint, JobKind::AnalyzeStudy, study.study_id.clone())? {
            AsyncJobReservation::Existing(job_id) => Ok(StartedJob { job_id }),
            AsyncJobReservation::Created(job_id) => {
                let app = Arc::clone(self);
                let worker_job_id = job_id.clone();
                thread::spawn(move || {
                    app.run_analyze_job_async(worker_job_id, fingerprint, study);
                });
                Ok(StartedJob { job_id })
            }
        }
    }

    pub fn get_job(&self, command: JobCommand) -> Result<JobSnapshot, BackendError> {
        let job_id = command.job_id.trim();
        if job_id.is_empty() {
            return Err(BackendError::invalid_input("jobId is required"));
        }

        self.jobs
            .lock()
            .expect("job registry mutex poisoned")
            .get(job_id)
            .cloned()
            .ok_or_else(|| BackendError::not_found(format!("job not found: {job_id}")))
    }

    pub fn get_jobs(&self, command: GetJobsCommand) -> Result<Vec<JobSnapshot>, BackendError> {
        let jobs = self.jobs.lock().expect("job registry mutex poisoned");
        Ok(command
            .job_ids
            .into_iter()
            .filter_map(|job_id| {
                let job_id = job_id.trim();
                if job_id.is_empty() {
                    return None;
                }
                jobs.get(job_id).cloned()
            })
            .collect())
    }

    pub fn cancel_job(&self, command: JobCommand) -> Result<JobSnapshot, BackendError> {
        let job_id = command.job_id.trim();
        if job_id.is_empty() {
            return Err(BackendError::invalid_input("jobId is required"));
        }

        let (snapshot, changed) = {
            let mut jobs = self.jobs.lock().expect("job registry mutex poisoned");
            let snapshot = jobs
                .get_mut(job_id)
                .ok_or_else(|| BackendError::not_found(format!("job not found: {job_id}")))?;
            let changed = if snapshot.state == JobState::Queued {
                snapshot.state = JobState::Cancelled;
                snapshot.progress.message = "Cancelled before start".to_string();
                snapshot.result = None;
                snapshot.error = None;
                self.active_fingerprints
                    .lock()
                    .expect("active job fingerprint mutex poisoned")
                    .retain(|_, active_job_id| active_job_id != job_id);
                true
            } else if snapshot.state == JobState::Running || snapshot.state == JobState::Cancelling
            {
                snapshot.state = JobState::Cancelling;
                snapshot.progress.message = "Cancellation requested".to_string();
                snapshot.result = None;
                snapshot.error = None;
                true
            } else {
                false
            };
            (snapshot.clone(), changed)
        };
        if changed {
            self.publish_job_update(&snapshot);
        }
        Ok(snapshot)
    }

    pub fn open_study(
        &self,
        command: OpenStudyCommand,
    ) -> Result<OpenStudyCommandResult, BackendError> {
        if command.input_path.is_empty() {
            return Err(BackendError::invalid_input("inputPath is required"));
        }

        let metadata = fs::metadata(&command.input_path).map_err(|error| {
            if error.kind() == std::io::ErrorKind::NotFound {
                BackendError::not_found(format!(
                    "input file does not exist: {}",
                    command.input_path
                ))
            } else {
                BackendError::internal(format!(
                    "failed to inspect input file {}: {error}",
                    command.input_path
                ))
            }
        })?;
        if metadata.is_dir() {
            return Err(BackendError::invalid_input(format!(
                "input path must be a file: {}",
                command.input_path
            )));
        }

        let metadata = bmp::read_file(&command.input_path).map_err(|error| {
            BackendError::invalid_input(format!("failed to read study metadata: {error}"))
        })?;

        let study = self.register_study(command.input_path, metadata.measurement_scale())?;
        let _ = self.persistence.record_opened_study(&study);
        Ok(OpenStudyCommandResult { study })
    }

    pub fn measure_line_annotation(
        &self,
        command: MeasureLineAnnotationCommand,
    ) -> Result<MeasureLineAnnotationCommandResult, BackendError> {
        let study_id = command.study_id.trim();
        if study_id.is_empty() {
            return Err(BackendError::invalid_input("studyId is required"));
        }

        let study = self
            .studies
            .lock()
            .expect("study registry mutex poisoned")
            .get(study_id)
            .cloned()
            .ok_or_else(|| BackendError::not_found(format!("study not found: {study_id}")))?;

        Ok(MeasureLineAnnotationCommandResult {
            study_id: study.study_id,
            annotation: annotations::measure_line_annotation(
                command.annotation,
                study.measurement_scale.as_ref(),
            ),
        })
    }

    pub fn register_study(
        &self,
        input_path: impl Into<String>,
        measurement_scale: Option<MeasurementScale>,
    ) -> Result<StudyRecord, BackendError> {
        let input_path = input_path.into();
        let input_name = Path::new(&input_path)
            .file_name()
            .and_then(|name| name.to_str())
            .unwrap_or(&input_path)
            .to_string();
        let study_id = format!(
            "study-{}",
            self.next_study_number.fetch_add(1, Ordering::Relaxed)
        );
        let study = StudyRecord {
            study_id: study_id.clone(),
            input_path,
            input_name,
            measurement_scale,
        };

        self.studies
            .lock()
            .expect("study registry mutex poisoned")
            .insert(study_id, study.clone());
        Ok(study)
    }

    fn render_study_job(
        &self,
        job_id: &str,
        fingerprint: &str,
        study: &StudyRecord,
    ) -> Result<JobSnapshot, BackendError> {
        let preview = self.load_source_preview(study)?;

        let preview_path = self.cache.artifact_path("render", fingerprint, "bmp")?;
        render::save_gray_bmp(
            &preview_path,
            preview.width,
            preview.height,
            &preview.pixels,
        )
        .map_err(BackendError::internal)?;
        self.track_artifact_bytes(&preview_path);

        let result = RenderStudyCommandResult {
            study_id: study.study_id.clone(),
            preview_path: preview_path.display().to_string(),
            loaded_width: preview.width,
            loaded_height: preview.height,
            measurement_scale: preview
                .measurement_scale
                .or_else(|| study.measurement_scale.clone()),
        };

        Ok(completed_job_snapshot(
            job_id.to_string(),
            JobKind::RenderStudy,
            Some(study.study_id.clone()),
            JobResult {
                kind: JobKind::RenderStudy,
                payload: serde_json::to_value(result).map_err(|error| {
                    BackendError::internal(format!("serialize render result: {error}"))
                })?,
            },
        ))
    }

    fn process_study_job(
        &self,
        job_id: &str,
        fingerprint: &str,
        study: &StudyRecord,
        command: &ProcessStudyCommand,
    ) -> Result<JobSnapshot, BackendError> {
        let resolved = processing::resolve_process_study_command(command)?;
        let source = self.load_source_preview(study)?;
        let source_preview =
            render::PreviewImage::gray(source.width, source.height, source.pixels.clone());
        let output = processing::process_rendered_preview(
            &source_preview,
            resolved.controls,
            &resolved.palette,
            resolved.compare,
        )
        .map_err(|error| BackendError::internal(format!("process source preview: {error}")))?;

        let preview_path = self.cache.artifact_path("process", fingerprint, "bmp")?;

        render::save_preview_bmp(&preview_path, &output.preview).map_err(BackendError::internal)?;
        self.track_artifact_bytes(&preview_path);

        let result = ProcessStudyCommandResult {
            study_id: study.study_id.clone(),
            preview_path: preview_path.display().to_string(),
            loaded_width: source.width,
            loaded_height: source.height,
            mode: output.mode,
            measurement_scale: source
                .measurement_scale
                .or_else(|| study.measurement_scale.clone()),
        };

        Ok(completed_job_snapshot(
            job_id.to_string(),
            JobKind::ProcessStudy,
            Some(study.study_id.clone()),
            JobResult {
                kind: JobKind::ProcessStudy,
                payload: serde_json::to_value(result).map_err(|error| {
                    BackendError::internal(format!("serialize process result: {error}"))
                })?,
            },
        ))
    }

    fn analyze_study_job(
        &self,
        job_id: &str,
        fingerprint: &str,
        study: &StudyRecord,
    ) -> Result<JobSnapshot, BackendError> {
        let source = self.load_analysis_preview(study)?;
        let source_preview =
            render::PreviewImage::gray(source.width, source.height, source.pixels.clone());
        let output = analysis::generate_tooth_overlay(&source_preview)
            .map_err(|error| BackendError::internal(format!("analyze source preview: {error}")))?;

        let preview_path = self.cache.artifact_path("analyze", fingerprint, "bmp")?;
        let filled_preview_path = self
            .cache
            .artifact_path("analyze-filled", fingerprint, "bmp")?;
        render::save_preview_bmp(&preview_path, &output.preview).map_err(BackendError::internal)?;
        self.track_artifact_bytes(&preview_path);
        render::save_preview_bmp(&filled_preview_path, &output.filled_preview)
            .map_err(BackendError::internal)?;
        self.track_artifact_bytes(&filled_preview_path);

        let result = AnalyzeStudyCommandResult {
            study_id: study.study_id.clone(),
            preview_path: preview_path.display().to_string(),
            filled_preview_path: filled_preview_path.display().to_string(),
            loaded_width: source.width,
            loaded_height: source.height,
            mode: output.mode,
            measurement_scale: source
                .measurement_scale
                .or_else(|| study.measurement_scale.clone()),
        };

        Ok(completed_job_snapshot(
            job_id.to_string(),
            JobKind::AnalyzeStudy,
            Some(study.study_id.clone()),
            JobResult {
                kind: JobKind::AnalyzeStudy,
                payload: serde_json::to_value(result).map_err(|error| {
                    BackendError::internal(format!("serialize analyze result: {error}"))
                })?,
            },
        ))
    }

    fn publish_job_update(&self, snapshot: &JobSnapshot) {
        let mut subscribers = self
            .job_update_subscribers
            .lock()
            .expect("job update subscriber mutex poisoned");
        subscribers.retain(|_, sender| match sender.try_send(snapshot.clone()) {
            Ok(()) | Err(TrySendError::Full(_)) => true,
            Err(TrySendError::Disconnected(_)) => false,
        });
    }

    fn load_source_preview(
        &self,
        study: &StudyRecord,
    ) -> Result<bmp::RenderedPreview, BackendError> {
        let cache_key = input_cache_key(&study.input_path);
        self.source_preview_cache
            .get_or_try_insert_with(cache_key, || {
                bmp::render_grayscale_preview_file(&study.input_path).map_err(|error| {
                    BackendError::invalid_input(format!("failed to render study: {error}"))
                })
            })
    }

    fn load_analysis_preview(
        &self,
        study: &StudyRecord,
    ) -> Result<bmp::RenderedPreview, BackendError> {
        let cache_key = format!("analysis\0{}", input_cache_key(&study.input_path));
        self.source_preview_cache
            .get_or_try_insert_with(cache_key, || {
                bmp::render_grayscale_preview_file_for_tooth_analysis(&study.input_path).map_err(
                    |error| BackendError::invalid_input(format!("failed to render study: {error}")),
                )
            })
    }

    fn render_fingerprint(&self, study: &StudyRecord) -> Result<String, BackendError> {
        fingerprint_json(&serde_json::json!({
            "namespace": "render-study",
            "sessionId": self.cache_session_id,
            "inputPath": study.input_path,
            "inputIdentity": current_input_file_identity(&study.input_path),
        }))
    }

    fn analyze_fingerprint(&self, study: &StudyRecord) -> Result<String, BackendError> {
        fingerprint_json(&serde_json::json!({
            "namespace": "analyze-study",
            "outputVersion": "sections-reference-mask-v16",
            "sessionId": self.cache_session_id,
            "inputPath": study.input_path,
            "inputIdentity": current_input_file_identity(&study.input_path),
        }))
    }

    fn process_fingerprint(
        &self,
        study: &StudyRecord,
        command: &ProcessStudyCommand,
    ) -> Result<String, BackendError> {
        fingerprint_json(&serde_json::json!({
            "namespace": "process-study",
            "sessionId": self.cache_session_id,
            "inputPath": study.input_path,
            "inputIdentity": current_input_file_identity(&study.input_path),
            "presetId": command.preset_id,
            "invert": command.invert,
            "brightness": command.brightness,
            "contrast": command.contrast,
            "equalize": command.equalize,
            "compare": command.compare,
            "palette": command.palette,
        }))
    }

    fn start_cached_job(
        &self,
        fingerprint: &str,
        job_kind: JobKind,
        study_id: String,
    ) -> Result<Option<StartedJob>, BackendError> {
        let cached = self
            .result_cache
            .lock()
            .expect("result cache mutex poisoned")
            .get(fingerprint)
            .cloned();
        let Some(result) = cached else {
            return Ok(None);
        };
        if !result_artifacts_exist(&result) {
            self.result_cache
                .lock()
                .expect("result cache mutex poisoned")
                .remove(fingerprint);
            return Ok(None);
        }

        let job_id = format!(
            "job-{}",
            self.next_job_number.fetch_add(1, Ordering::Relaxed)
        );
        let snapshot = cached_job_snapshot(job_id.clone(), job_kind, Some(study_id), result);
        self.store_job_snapshot(snapshot);
        Ok(Some(StartedJob { job_id }))
    }

    fn reserve_async_job(
        &self,
        fingerprint: &str,
        job_kind: JobKind,
        study_id: String,
    ) -> Result<AsyncJobReservation, BackendError> {
        let mut jobs = self.jobs.lock().expect("job registry mutex poisoned");
        let mut active = self
            .active_fingerprints
            .lock()
            .expect("active job fingerprint mutex poisoned");

        if let Some(job_id) = active.get(fingerprint).cloned() {
            if jobs
                .get(&job_id)
                .is_some_and(|snapshot| !is_terminal_state(&snapshot.state))
            {
                return Ok(AsyncJobReservation::Existing(job_id));
            }
            active.remove(fingerprint);
        }

        let job_id = format!(
            "job-{}",
            self.next_job_number.fetch_add(1, Ordering::Relaxed)
        );
        let snapshot = queued_job_snapshot(job_id.clone(), job_kind, Some(study_id));
        jobs.insert(job_id.clone(), snapshot.clone());
        evict_old_terminal_jobs(&mut jobs, &job_id);
        active.insert(fingerprint.to_string(), job_id.clone());
        drop(active);
        drop(jobs);
        self.publish_job_update(&snapshot);

        Ok(AsyncJobReservation::Created(job_id))
    }

    fn store_job_snapshot(&self, snapshot: JobSnapshot) {
        let job_id = snapshot.job_id.clone();
        let mut jobs = self.jobs.lock().expect("job registry mutex poisoned");
        jobs.insert(job_id.clone(), snapshot);
        evict_old_terminal_jobs(&mut jobs, &job_id);
    }

    fn run_render_job_async(
        self: Arc<Self>,
        job_id: String,
        fingerprint: String,
        study: StudyRecord,
    ) {
        let Some(validated) = self.run_job_stage(
            &job_id,
            &fingerprint,
            10,
            "validating",
            "Validating source study",
            &[],
            || validate_input_file(&study.input_path),
        ) else {
            return;
        };
        if let Err(error) = validated {
            self.finish_async_job(
                &job_id,
                &fingerprint,
                failed_job_snapshot(
                    job_id.clone(),
                    JobKind::RenderStudy,
                    Some(study.study_id.clone()),
                    error,
                ),
            );
            return;
        }

        let source = match self.run_job_stage(
            &job_id,
            &fingerprint,
            35,
            "loadingStudy",
            "Loading source study",
            &[],
            || self.load_source_preview(&study),
        ) {
            Some(Ok(source)) => source,
            Some(Err(error)) => {
                self.finish_async_job(
                    &job_id,
                    &fingerprint,
                    failed_job_snapshot(
                        job_id.clone(),
                        JobKind::RenderStudy,
                        Some(study.study_id.clone()),
                        error,
                    ),
                );
                return;
            }
            None => return,
        };

        let Some(rendered) = self.run_job_stage(
            &job_id,
            &fingerprint,
            75,
            "renderingPreview",
            "Rendering preview",
            &[],
            || Ok(()),
        ) else {
            return;
        };
        if let Err(error) = rendered {
            self.finish_async_job(
                &job_id,
                &fingerprint,
                failed_job_snapshot(
                    job_id.clone(),
                    JobKind::RenderStudy,
                    Some(study.study_id.clone()),
                    error,
                ),
            );
            return;
        }

        let mut preview_path = None;
        let written = self.run_job_stage(
            &job_id,
            &fingerprint,
            90,
            "writingPreview",
            "Writing preview",
            &[],
            || {
                let path = self.cache.artifact_path("render", &fingerprint, "bmp")?;
                preview_path = Some(path.clone());
                render::save_gray_bmp(&path, source.width, source.height, &source.pixels)
                    .map_err(BackendError::internal)?;
                self.track_artifact_bytes(&path);
                Ok(path)
            },
        );
        let preview_path = match written {
            Some(Ok(path)) => path,
            Some(Err(error)) => {
                if let Some(path) = preview_path.as_deref() {
                    cleanup_file(path);
                }
                self.finish_async_job(
                    &job_id,
                    &fingerprint,
                    failed_job_snapshot(
                        job_id.clone(),
                        JobKind::RenderStudy,
                        Some(study.study_id.clone()),
                        error,
                    ),
                );
                return;
            }
            None => {
                if let Some(path) = preview_path.as_deref() {
                    cleanup_file(path);
                }
                return;
            }
        };

        let result = RenderStudyCommandResult {
            study_id: study.study_id.clone(),
            preview_path: preview_path.display().to_string(),
            loaded_width: source.width,
            loaded_height: source.height,
            measurement_scale: source
                .measurement_scale
                .clone()
                .or_else(|| study.measurement_scale.clone()),
        };
        let snapshot = match serde_json::to_value(result)
            .map_err(|error| BackendError::internal(format!("serialize render result: {error}")))
        {
            Ok(payload) => completed_job_snapshot(
                job_id.clone(),
                JobKind::RenderStudy,
                Some(study.study_id.clone()),
                JobResult {
                    kind: JobKind::RenderStudy,
                    payload,
                },
            ),
            Err(error) => failed_job_snapshot(
                job_id.clone(),
                JobKind::RenderStudy,
                Some(study.study_id.clone()),
                error,
            ),
        };
        self.finish_async_job(&job_id, &fingerprint, snapshot);
    }

    fn run_process_job_async(
        self: Arc<Self>,
        job_id: String,
        fingerprint: String,
        study: StudyRecord,
        command: ProcessStudyCommand,
    ) {
        let prepared = match self.run_job_stage(
            &job_id,
            &fingerprint,
            10,
            "validating",
            "Validating processing request",
            &[],
            || {
                validate_input_file(&study.input_path)?;
                let resolved = processing::resolve_process_study_command(&command)?;
                let preview_path = self.cache.artifact_path("process", &fingerprint, "bmp")?;
                Ok((resolved, preview_path))
            },
        ) {
            Some(Ok(prepared)) => prepared,
            Some(Err(error)) => {
                self.finish_async_job(
                    &job_id,
                    &fingerprint,
                    failed_job_snapshot(
                        job_id.clone(),
                        JobKind::ProcessStudy,
                        Some(study.study_id.clone()),
                        error,
                    ),
                );
                return;
            }
            None => return,
        };
        let (resolved, preview_path) = prepared;

        let source = match self.run_job_stage(
            &job_id,
            &fingerprint,
            30,
            "loadingStudy",
            "Loading source pixels",
            &[preview_path.as_path()],
            || self.load_source_preview(&study),
        ) {
            Some(Ok(source)) => source,
            Some(Err(error)) => {
                cleanup_file(&preview_path);
                self.finish_async_job(
                    &job_id,
                    &fingerprint,
                    failed_job_snapshot(
                        job_id.clone(),
                        JobKind::ProcessStudy,
                        Some(study.study_id.clone()),
                        error,
                    ),
                );
                return;
            }
            None => return,
        };

        let output = match self.run_job_stage(
            &job_id,
            &fingerprint,
            65,
            "processingPixels",
            "Applying processing pipeline",
            &[preview_path.as_path()],
            || {
                let source_preview =
                    render::PreviewImage::gray(source.width, source.height, source.pixels.clone());
                processing::process_rendered_preview(
                    &source_preview,
                    resolved.controls,
                    &resolved.palette,
                    resolved.compare,
                )
                .map_err(|error| BackendError::internal(format!("process source preview: {error}")))
            },
        ) {
            Some(Ok(output)) => output,
            Some(Err(error)) => {
                cleanup_file(&preview_path);
                self.finish_async_job(
                    &job_id,
                    &fingerprint,
                    failed_job_snapshot(
                        job_id.clone(),
                        JobKind::ProcessStudy,
                        Some(study.study_id.clone()),
                        error,
                    ),
                );
                return;
            }
            None => return,
        };

        let written_preview = self.run_job_stage(
            &job_id,
            &fingerprint,
            84,
            "writingPreview",
            "Writing processed preview",
            &[preview_path.as_path()],
            || {
                render::save_preview_bmp(&preview_path, &output.preview)
                    .map_err(BackendError::internal)?;
                self.track_artifact_bytes(&preview_path);
                Ok(())
            },
        );
        match written_preview {
            Some(Ok(())) => {}
            Some(Err(error)) => {
                cleanup_file(&preview_path);
                self.finish_async_job(
                    &job_id,
                    &fingerprint,
                    failed_job_snapshot(
                        job_id.clone(),
                        JobKind::ProcessStudy,
                        Some(study.study_id.clone()),
                        error,
                    ),
                );
                return;
            }
            None => return,
        }

        let result = ProcessStudyCommandResult {
            study_id: study.study_id.clone(),
            preview_path: preview_path.display().to_string(),
            loaded_width: source.width,
            loaded_height: source.height,
            mode: output.mode,
            measurement_scale: source
                .measurement_scale
                .clone()
                .or_else(|| study.measurement_scale.clone()),
        };
        let snapshot = match serde_json::to_value(result)
            .map_err(|error| BackendError::internal(format!("serialize process result: {error}")))
        {
            Ok(payload) => completed_job_snapshot(
                job_id.clone(),
                JobKind::ProcessStudy,
                Some(study.study_id.clone()),
                JobResult {
                    kind: JobKind::ProcessStudy,
                    payload,
                },
            ),
            Err(error) => failed_job_snapshot(
                job_id.clone(),
                JobKind::ProcessStudy,
                Some(study.study_id.clone()),
                error,
            ),
        };
        self.finish_async_job(&job_id, &fingerprint, snapshot);
    }

    fn run_analyze_job_async(
        self: Arc<Self>,
        job_id: String,
        fingerprint: String,
        study: StudyRecord,
    ) {
        let Some(validated) = self.run_job_stage(
            &job_id,
            &fingerprint,
            10,
            "validating",
            "Validating source study",
            &[],
            || validate_input_file(&study.input_path),
        ) else {
            return;
        };
        if let Err(error) = validated {
            self.finish_async_job(
                &job_id,
                &fingerprint,
                failed_job_snapshot(
                    job_id.clone(),
                    JobKind::AnalyzeStudy,
                    Some(study.study_id.clone()),
                    error,
                ),
            );
            return;
        }

        let source = match self.run_job_stage(
            &job_id,
            &fingerprint,
            30,
            "loadingStudy",
            "Loading source pixels",
            &[],
            || self.load_analysis_preview(&study),
        ) {
            Some(Ok(source)) => source,
            Some(Err(error)) => {
                self.finish_async_job(
                    &job_id,
                    &fingerprint,
                    failed_job_snapshot(
                        job_id.clone(),
                        JobKind::AnalyzeStudy,
                        Some(study.study_id.clone()),
                        error,
                    ),
                );
                return;
            }
            None => return,
        };

        let output = match self.run_job_stage(
            &job_id,
            &fingerprint,
            68,
            "analyzingDentalStructures",
            "Detecting tooth regions and bone level",
            &[],
            || {
                let source_preview =
                    render::PreviewImage::gray(source.width, source.height, source.pixels.clone());
                analysis::generate_tooth_overlay(&source_preview).map_err(|error| {
                    BackendError::internal(format!("analyze source preview: {error}"))
                })
            },
        ) {
            Some(Ok(output)) => output,
            Some(Err(error)) => {
                self.finish_async_job(
                    &job_id,
                    &fingerprint,
                    failed_job_snapshot(
                        job_id.clone(),
                        JobKind::AnalyzeStudy,
                        Some(study.study_id.clone()),
                        error,
                    ),
                );
                return;
            }
            None => return,
        };

        let mut preview_path = None;
        let mut filled_preview_path = None;
        let written = self.run_job_stage(
            &job_id,
            &fingerprint,
            90,
            "writingPreview",
            "Writing tooth and bone overlay preview",
            &[],
            || {
                let preview = self.cache.artifact_path("analyze", &fingerprint, "bmp")?;
                let filled = self
                    .cache
                    .artifact_path("analyze-filled", &fingerprint, "bmp")?;
                preview_path = Some(preview.clone());
                filled_preview_path = Some(filled.clone());
                render::save_preview_bmp(&preview, &output.preview)
                    .map_err(BackendError::internal)?;
                self.track_artifact_bytes(&preview);
                render::save_preview_bmp(&filled, &output.filled_preview)
                    .map_err(BackendError::internal)?;
                self.track_artifact_bytes(&filled);
                Ok((preview, filled))
            },
        );
        let (preview_path, filled_preview_path) = match written {
            Some(Ok(paths)) => paths,
            Some(Err(error)) => {
                if let Some(path) = preview_path.as_deref() {
                    cleanup_file(path);
                }
                if let Some(path) = filled_preview_path.as_deref() {
                    cleanup_file(path);
                }
                self.finish_async_job(
                    &job_id,
                    &fingerprint,
                    failed_job_snapshot(
                        job_id.clone(),
                        JobKind::AnalyzeStudy,
                        Some(study.study_id.clone()),
                        error,
                    ),
                );
                return;
            }
            None => {
                if let Some(path) = preview_path.as_deref() {
                    cleanup_file(path);
                }
                if let Some(path) = filled_preview_path.as_deref() {
                    cleanup_file(path);
                }
                return;
            }
        };

        let result = AnalyzeStudyCommandResult {
            study_id: study.study_id.clone(),
            preview_path: preview_path.display().to_string(),
            filled_preview_path: filled_preview_path.display().to_string(),
            loaded_width: source.width,
            loaded_height: source.height,
            mode: output.mode,
            measurement_scale: source
                .measurement_scale
                .clone()
                .or_else(|| study.measurement_scale.clone()),
        };
        let snapshot = match serde_json::to_value(result)
            .map_err(|error| BackendError::internal(format!("serialize analyze result: {error}")))
        {
            Ok(payload) => completed_job_snapshot(
                job_id.clone(),
                JobKind::AnalyzeStudy,
                Some(study.study_id.clone()),
                JobResult {
                    kind: JobKind::AnalyzeStudy,
                    payload,
                },
            ),
            Err(error) => failed_job_snapshot(
                job_id.clone(),
                JobKind::AnalyzeStudy,
                Some(study.study_id.clone()),
                error,
            ),
        };
        self.finish_async_job(&job_id, &fingerprint, snapshot);
    }

    fn update_job_progress(
        &self,
        job_id: &str,
        state: JobState,
        percent: i32,
        stage: &str,
        message: &str,
    ) {
        let snapshot = {
            let mut jobs = self.jobs.lock().expect("job registry mutex poisoned");
            let Some(snapshot) = jobs.get_mut(job_id) else {
                return;
            };
            if is_terminal_state(&snapshot.state) || snapshot.state == JobState::Cancelling {
                return;
            }
            snapshot.state = state;
            snapshot.progress = JobProgress {
                percent,
                stage: stage.to_string(),
                message: message.to_string(),
            };
            snapshot.error = None;
            snapshot.clone()
        };
        self.publish_job_update(&snapshot);
    }

    #[allow(clippy::too_many_arguments)]
    fn run_job_stage<T>(
        &self,
        job_id: &str,
        fingerprint: &str,
        percent: i32,
        stage: &str,
        message: &str,
        cleanup_paths: &[&Path],
        work: impl FnOnce() -> Result<T, BackendError>,
    ) -> Option<Result<T, BackendError>> {
        if self.finish_cancelled_if_requested(job_id, fingerprint, stage, cleanup_paths) {
            return None;
        }
        self.update_job_progress(job_id, JobState::Running, percent, stage, message);
        if self.finish_cancelled_if_requested(job_id, fingerprint, stage, cleanup_paths) {
            return None;
        }

        let result = work();
        if self.finish_cancelled_if_requested(job_id, fingerprint, stage, cleanup_paths) {
            return None;
        }
        Some(result)
    }

    fn finish_cancelled_if_requested(
        &self,
        job_id: &str,
        fingerprint: &str,
        stage: &str,
        cleanup_paths: &[&Path],
    ) -> bool {
        let should_cancel = {
            let jobs = self.jobs.lock().expect("job registry mutex poisoned");
            let Some(snapshot) = jobs.get(job_id) else {
                return true;
            };
            if snapshot.state == JobState::Cancelled {
                return true;
            }
            snapshot.state == JobState::Cancelling
        };
        if !should_cancel {
            return false;
        }

        cleanup_files(cleanup_paths);
        let snapshot = {
            let mut jobs = self.jobs.lock().expect("job registry mutex poisoned");
            let mut active = self
                .active_fingerprints
                .lock()
                .expect("active job fingerprint mutex poisoned");
            if active
                .get(fingerprint)
                .is_some_and(|active_job| active_job == job_id)
            {
                active.remove(fingerprint);
            }
            let Some(current) = jobs.get(job_id).cloned() else {
                return true;
            };
            let mut snapshot = cancelled_job_snapshot(current, "Cancelled by user");
            if !stage.is_empty() {
                snapshot.progress.stage = stage.to_string();
            }
            jobs.insert(job_id.to_string(), snapshot.clone());
            evict_old_terminal_jobs(&mut jobs, job_id);
            snapshot
        };
        self.publish_job_update(&snapshot);
        true
    }

    fn finish_async_job(&self, job_id: &str, fingerprint: &str, terminal_snapshot: JobSnapshot) {
        let snapshot = {
            let mut jobs = self.jobs.lock().expect("job registry mutex poisoned");
            let mut active = self
                .active_fingerprints
                .lock()
                .expect("active job fingerprint mutex poisoned");
            if active
                .get(fingerprint)
                .is_some_and(|active_job| active_job == job_id)
            {
                active.remove(fingerprint);
            }

            let snapshot = match jobs.get(job_id).cloned() {
                Some(current)
                    if current.state == JobState::Cancelling
                        || current.state == JobState::Cancelled =>
                {
                    if let Some(result) = terminal_snapshot.result.as_ref() {
                        cleanup_result_artifacts(result);
                    }
                    cancelled_job_snapshot(current, "Cancelled by user")
                }
                Some(current) if is_terminal_state(&current.state) => current,
                _ => terminal_snapshot,
            };
            jobs.insert(job_id.to_string(), snapshot.clone());
            evict_old_terminal_jobs(&mut jobs, job_id);
            snapshot
        };

        if self.store_completed_result(fingerprint, &snapshot) {
            self.evict_artifacts_if_needed();
        }
        self.publish_job_update(&snapshot);
    }

    fn store_completed_result(&self, fingerprint: &str, snapshot: &JobSnapshot) -> bool {
        if snapshot.state != JobState::Completed || snapshot.from_cache {
            return false;
        }
        let Some(result) = snapshot.result.clone() else {
            return false;
        };
        self.result_cache
            .lock()
            .expect("result cache mutex poisoned")
            .insert(fingerprint.to_string(), result);
        true
    }

    fn track_artifact_bytes(&self, path: &Path) {
        if let Ok(metadata) = fs::metadata(path) {
            self.cache.add_artifact_bytes(metadata.len());
        }
    }

    fn evict_artifacts_if_needed(&self) {
        let _ = self.cache.evict_artifacts_over_limit(MAX_ARTIFACT_BYTES);
    }
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct InputFileIdentity {
    state: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    size: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    mode: Option<u32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    mod_time_unix_nano: Option<u128>,
}

fn current_input_file_identity(input_path: &str) -> InputFileIdentity {
    let Ok(metadata) = fs::metadata(input_path) else {
        return InputFileIdentity {
            state: "unavailable",
            size: None,
            mode: None,
            mod_time_unix_nano: None,
        };
    };

    InputFileIdentity {
        state: if metadata.is_dir() {
            "directory"
        } else {
            "file"
        },
        size: Some(metadata.len()),
        mode: Some(file_mode(&metadata)),
        mod_time_unix_nano: metadata
            .modified()
            .ok()
            .and_then(|time| time.duration_since(UNIX_EPOCH).ok())
            .map(|duration| duration.as_nanos()),
    }
}

fn input_cache_key(input_path: &str) -> String {
    let identity = current_input_file_identity(input_path);
    format!(
        "{}\0{}\0{}\0{}\0{}",
        input_path,
        identity.state,
        identity.size.unwrap_or_default(),
        identity.mode.unwrap_or_default(),
        identity.mod_time_unix_nano.unwrap_or_default(),
    )
}

#[cfg(unix)]
fn file_mode(metadata: &fs::Metadata) -> u32 {
    metadata.permissions().mode()
}

#[cfg(not(unix))]
fn file_mode(_metadata: &fs::Metadata) -> u32 {
    0
}

fn fingerprint_json<T: Serialize>(value: &T) -> Result<String, BackendError> {
    let payload = serde_json::to_vec(value)
        .map_err(|error| BackendError::internal(format!("serialize job fingerprint: {error}")))?;
    Ok(format!("{:016x}", fnv1a64(&payload)))
}

fn fnv1a64(bytes: &[u8]) -> u64 {
    let mut hash = 0xcbf29ce484222325_u64;
    for byte in bytes {
        hash ^= u64::from(*byte);
        hash = hash.wrapping_mul(0x100000001b3);
    }
    hash
}

fn result_artifacts_exist(result: &JobResult) -> bool {
    match result.kind {
        JobKind::RenderStudy => {
            serde_json::from_value::<RenderStudyCommandResult>(result.payload.clone())
                .is_ok_and(|payload| artifact_exists(&payload.preview_path))
        }
        JobKind::AnalyzeStudy => serde_json::from_value::<AnalyzeStudyCommandResult>(
            result.payload.clone(),
        )
        .is_ok_and(|payload| {
            artifact_exists(&payload.preview_path) && artifact_exists(&payload.filled_preview_path)
        }),
        JobKind::ProcessStudy => serde_json::from_value::<ProcessStudyCommandResult>(
            result.payload.clone(),
        )
        .is_ok_and(|payload| artifact_exists(&payload.preview_path)),
    }
}

fn artifact_exists(path: &str) -> bool {
    fs::metadata(path).is_ok_and(|metadata| metadata.is_file())
}

fn validate_input_file(input_path: &str) -> Result<(), BackendError> {
    let metadata = fs::metadata(input_path).map_err(|error| {
        if error.kind() == std::io::ErrorKind::NotFound {
            BackendError::not_found(format!("input file does not exist: {input_path}"))
        } else {
            BackendError::internal(format!(
                "failed to inspect input file {input_path}: {error}"
            ))
        }
    })?;
    if metadata.is_dir() {
        return Err(BackendError::invalid_input(format!(
            "input path must be a file: {input_path}"
        )));
    }
    Ok(())
}

fn queued_job_snapshot(job_id: String, job_kind: JobKind, study_id: Option<String>) -> JobSnapshot {
    JobSnapshot {
        job_id,
        job_kind,
        study_id,
        state: JobState::Queued,
        progress: JobProgress {
            percent: 0,
            stage: "queued".to_string(),
            message: "Queued".to_string(),
        },
        from_cache: false,
        result: None,
        error: None,
    }
}

fn completed_job_snapshot(
    job_id: String,
    job_kind: JobKind,
    study_id: Option<String>,
    result: JobResult,
) -> JobSnapshot {
    JobSnapshot {
        job_id,
        job_kind,
        study_id,
        state: JobState::Completed,
        progress: JobProgress {
            percent: 100,
            stage: "completed".to_string(),
            message: "Completed".to_string(),
        },
        from_cache: false,
        result: Some(result),
        error: None,
    }
}

fn cached_job_snapshot(
    job_id: String,
    job_kind: JobKind,
    study_id: Option<String>,
    result: JobResult,
) -> JobSnapshot {
    JobSnapshot {
        job_id,
        job_kind,
        study_id,
        state: JobState::Completed,
        progress: JobProgress {
            percent: 100,
            stage: "cacheHit".to_string(),
            message: "Loaded from cache".to_string(),
        },
        from_cache: true,
        result: Some(result),
        error: None,
    }
}

fn failed_job_snapshot(
    job_id: String,
    job_kind: JobKind,
    study_id: Option<String>,
    error: BackendError,
) -> JobSnapshot {
    JobSnapshot {
        job_id,
        job_kind,
        study_id,
        state: JobState::Failed,
        progress: JobProgress {
            percent: 100,
            stage: "failed".to_string(),
            message: error.message.clone(),
        },
        from_cache: false,
        result: None,
        error: Some(error),
    }
}

fn cancelled_job_snapshot(mut snapshot: JobSnapshot, message: &str) -> JobSnapshot {
    snapshot.state = JobState::Cancelled;
    snapshot.progress.message = message.to_string();
    snapshot.result = None;
    snapshot.error = None;
    snapshot
}

fn cleanup_result_artifacts(result: &JobResult) {
    match result.kind {
        JobKind::RenderStudy => {
            if let Ok(payload) =
                serde_json::from_value::<RenderStudyCommandResult>(result.payload.clone())
            {
                cleanup_path(&payload.preview_path);
            }
        }
        JobKind::AnalyzeStudy => {
            if let Ok(payload) =
                serde_json::from_value::<AnalyzeStudyCommandResult>(result.payload.clone())
            {
                cleanup_path(&payload.preview_path);
                cleanup_path(&payload.filled_preview_path);
            }
        }
        JobKind::ProcessStudy => {
            if let Ok(payload) =
                serde_json::from_value::<ProcessStudyCommandResult>(result.payload.clone())
            {
                cleanup_path(&payload.preview_path);
            }
        }
    }
}

fn cleanup_file(path: impl AsRef<Path>) {
    let _ = fs::remove_file(path);
}

fn cleanup_files(paths: &[&Path]) {
    for path in paths {
        cleanup_file(path);
    }
}

fn cleanup_path(path: &str) {
    cleanup_file(path);
}

fn is_terminal_state(state: &JobState) -> bool {
    matches!(
        state,
        JobState::Completed | JobState::Failed | JobState::Cancelled
    )
}

fn evict_old_terminal_jobs(jobs: &mut HashMap<String, JobSnapshot>, keep_job_id: &str) {
    while jobs.len() > MAX_TERMINAL_JOBS {
        let Some(evict_job_id) = jobs
            .iter()
            .find(|(job_id, snapshot)| {
                job_id.as_str() != keep_job_id && is_terminal_state(&snapshot.state)
            })
            .map(|(job_id, _)| job_id.clone())
        else {
            return;
        };
        jobs.remove(&evict_job_id);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::contracts::{AnnotationPoint, AnnotationSource, LineAnnotation};

    #[test]
    fn open_study_registers_existing_file() {
        let temp_dir =
            std::env::temp_dir().join(format!("xrayview-rs-open-study-{}", std::process::id()));
        fs::create_dir_all(&temp_dir).unwrap();
        let input_path = temp_dir.join("sample.bmp");
        fs::write(&input_path, build_renderable_test_bmp()).unwrap();

        let app = App::new(Config::default()).unwrap();
        let result = app
            .open_study(OpenStudyCommand {
                input_path: input_path.display().to_string(),
            })
            .unwrap();

        assert!(!result.study.study_id.is_empty());
        assert_eq!(result.study.input_name, "sample.bmp");
        assert_eq!(result.study.measurement_scale, None);
        assert_eq!(app.study_count(), 1);

        let _ = fs::remove_file(input_path);
        let _ = fs::remove_dir(temp_dir);
    }

    #[test]
    fn open_study_records_recent_study_catalog() {
        let temp_dir = std::env::temp_dir().join(format!(
            "xrayview-rs-open-study-catalog-{}",
            std::process::id()
        ));
        let cache_dir = temp_dir.join("cache");
        let state_dir = temp_dir.join("state");
        fs::create_dir_all(&temp_dir).unwrap();
        let input_path = temp_dir.join("sample.bmp");
        fs::write(&input_path, build_renderable_test_bmp()).unwrap();

        let mut config = Config::default();
        config.paths.cache_dir = cache_dir;
        config.paths.persistence_dir = state_dir.clone();
        let app = App::new(config).unwrap();
        let opened = app
            .open_study(OpenStudyCommand {
                input_path: input_path.display().to_string(),
            })
            .unwrap()
            .study;

        let catalog = persistence::Catalog::new(&state_dir).load().unwrap();

        assert_eq!(catalog.recent_studies.len(), 1);
        assert_eq!(catalog.recent_studies[0].input_path, opened.input_path);
        assert_eq!(catalog.recent_studies[0].input_name, "sample.bmp");
        assert_eq!(
            catalog.recent_studies[0].measurement_scale,
            opened.measurement_scale
        );

        let _ = fs::remove_file(input_path);
        let _ = fs::remove_dir_all(temp_dir);
    }

    #[test]
    fn measure_line_annotation_uses_registered_measurement_scale() {
        let app = App::new(Config::default()).unwrap();
        let study = app
            .register_study(
                "/tmp/calibrated-measurement.bmp",
                Some(MeasurementScale {
                    row_spacing_mm: 0.2,
                    column_spacing_mm: 0.3,
                    source: "PixelSpacing".to_string(),
                }),
            )
            .unwrap();

        let result = app
            .measure_line_annotation(MeasureLineAnnotationCommand {
                study_id: study.study_id,
                annotation: LineAnnotation {
                    id: "line-1".to_string(),
                    label: "Measurement 1".to_string(),
                    source: AnnotationSource::Manual,
                    start: AnnotationPoint { x: 10.0, y: 8.0 },
                    end: AnnotationPoint { x: 14.0, y: 11.0 },
                    editable: true,
                    confidence: None,
                    measurement: None,
                },
            })
            .unwrap();

        let measurement = result.annotation.measurement.unwrap();
        assert_eq!(measurement.pixel_length, 5.0);
        assert_eq!(measurement.calibrated_length_mm, Some(1.3));
    }

    #[test]
    fn start_render_job_writes_preview_and_stores_completed_snapshot() {
        let temp_dir =
            std::env::temp_dir().join(format!("xrayview-rs-render-job-{}", std::process::id()));
        let cache_dir = temp_dir.join("cache");
        let state_dir = temp_dir.join("state");
        fs::create_dir_all(&temp_dir).unwrap();
        let input_path = temp_dir.join("sample.bmp");
        fs::write(&input_path, build_renderable_test_bmp()).unwrap();

        let mut config = Config::default();
        config.paths.cache_dir = cache_dir;
        config.paths.persistence_dir = state_dir;
        let app = App::new(config).unwrap();
        let study = app
            .open_study(OpenStudyCommand {
                input_path: input_path.display().to_string(),
            })
            .unwrap()
            .study;

        let started = app
            .start_render_job(RenderStudyCommand {
                study_id: study.study_id.clone(),
            })
            .unwrap();
        let snapshot = app
            .get_job(JobCommand {
                job_id: started.job_id,
            })
            .unwrap();

        assert_eq!(snapshot.state, JobState::Completed);
        assert_eq!(snapshot.job_kind, JobKind::RenderStudy);
        let result = snapshot.result.unwrap();
        assert_eq!(result.kind, JobKind::RenderStudy);
        let payload: RenderStudyCommandResult = serde_json::from_value(result.payload).unwrap();
        assert_eq!(payload.study_id, study.study_id);
        assert_eq!(payload.loaded_width, 4);
        assert_eq!(payload.loaded_height, 2);
        assert!(fs::metadata(&payload.preview_path).unwrap().is_file());
        assert_eq!(payload.measurement_scale, None);

        let _ = fs::remove_file(input_path);
        let _ = fs::remove_dir_all(temp_dir);
    }

    #[test]
    fn start_render_job_broadcasts_completed_job_update() {
        let temp_dir =
            std::env::temp_dir().join(format!("xrayview-rs-job-event-{}", std::process::id()));
        let cache_dir = temp_dir.join("cache");
        let state_dir = temp_dir.join("state");
        fs::create_dir_all(&temp_dir).unwrap();
        let input_path = temp_dir.join("sample.bmp");
        fs::write(&input_path, build_renderable_test_bmp()).unwrap();

        let mut config = Config::default();
        config.paths.cache_dir = cache_dir;
        config.paths.persistence_dir = state_dir;
        let app = App::new(config).unwrap();
        let study = app
            .open_study(OpenStudyCommand {
                input_path: input_path.display().to_string(),
            })
            .unwrap()
            .study;
        let subscription = app.subscribe_job_updates();

        let started = app
            .start_render_job(RenderStudyCommand {
                study_id: study.study_id.clone(),
            })
            .unwrap();
        let snapshot = subscription
            .receiver
            .recv_timeout(std::time::Duration::from_millis(500))
            .unwrap();
        app.unsubscribe_job_updates(subscription.id);

        assert_eq!(snapshot.job_id, started.job_id);
        assert_eq!(snapshot.job_kind, JobKind::RenderStudy);
        assert_eq!(snapshot.state, JobState::Completed);
        assert_eq!(snapshot.study_id, Some(study.study_id));

        let _ = fs::remove_file(input_path);
        let _ = fs::remove_dir_all(temp_dir);
    }

    #[test]
    fn start_render_job_async_publishes_contract_compatible_stage_updates() {
        let (temp_dir, app, study) = app_with_renderable_study(
            "render-async-stage-updates",
            build_renderable_test_bmp(),
        );
        let app = std::sync::Arc::new(app);
        let subscription = app.subscribe_job_updates();

        let started = app
            .start_render_job_async(RenderStudyCommand {
                study_id: study.study_id.clone(),
            })
            .unwrap();

        let mut stages = Vec::new();
        let terminal = loop {
            let snapshot = subscription
                .receiver
                .recv_timeout(std::time::Duration::from_secs(1))
                .unwrap();
            if snapshot.job_id != started.job_id {
                continue;
            }
            stages.push(snapshot.progress.stage.clone());
            if is_terminal_state(&snapshot.state) {
                break snapshot;
            }
        };
        app.unsubscribe_job_updates(subscription.id);

        assert_eq!(terminal.state, JobState::Completed);
        assert_ordered_stages(
            &stages,
            &[
                "queued",
                "validating",
                "loadingStudy",
                "renderingPreview",
                "writingPreview",
                "completed",
            ],
        );

        let _ = fs::remove_dir_all(temp_dir);
    }

    #[test]
    fn start_render_job_reuses_in_session_cached_result_for_same_input() {
        let (temp_dir, app, first_study) = app_with_renderable_study(
            "render-cache-hit",
            build_renderable_test_bmp(),
        );
        let first_started = app
            .start_render_job(RenderStudyCommand {
                study_id: first_study.study_id.clone(),
            })
            .unwrap();
        let first_snapshot = app
            .get_job(JobCommand {
                job_id: first_started.job_id,
            })
            .unwrap();
        let first_payload: RenderStudyCommandResult =
            serde_json::from_value(first_snapshot.result.clone().unwrap().payload).unwrap();
        let second_study = app
            .open_study(OpenStudyCommand {
                input_path: first_study.input_path.clone(),
            })
            .unwrap()
            .study;

        let second_started = app
            .start_render_job(RenderStudyCommand {
                study_id: second_study.study_id.clone(),
            })
            .unwrap();
        let second_snapshot = app
            .get_job(JobCommand {
                job_id: second_started.job_id,
            })
            .unwrap();
        let second_payload: RenderStudyCommandResult =
            serde_json::from_value(second_snapshot.result.clone().unwrap().payload).unwrap();

        assert!(!first_snapshot.from_cache);
        assert!(second_snapshot.from_cache);
        assert_eq!(second_snapshot.progress.stage, "cacheHit");
        assert_eq!(second_snapshot.study_id, Some(second_study.study_id));
        assert_eq!(second_payload.preview_path, first_payload.preview_path);
        assert_eq!(second_payload.study_id, first_study.study_id);

        let _ = fs::remove_dir_all(temp_dir);
    }

    #[test]
    fn render_cache_misses_when_cached_artifact_was_deleted() {
        let (temp_dir, app, study) = app_with_renderable_study(
            "render-cache-deleted-artifact",
            build_renderable_test_bmp(),
        );
        let first_started = app
            .start_render_job(RenderStudyCommand {
                study_id: study.study_id.clone(),
            })
            .unwrap();
        let first_snapshot = app
            .get_job(JobCommand {
                job_id: first_started.job_id,
            })
            .unwrap();
        let first_payload: RenderStudyCommandResult =
            serde_json::from_value(first_snapshot.result.unwrap().payload).unwrap();
        fs::remove_file(&first_payload.preview_path).unwrap();

        let second_started = app
            .start_render_job(RenderStudyCommand {
                study_id: study.study_id.clone(),
            })
            .unwrap();
        let second_snapshot = app
            .get_job(JobCommand {
                job_id: second_started.job_id,
            })
            .unwrap();

        assert!(!second_snapshot.from_cache);
        assert_eq!(second_snapshot.state, JobState::Completed);
        assert!(fs::metadata(&first_payload.preview_path).unwrap().is_file());

        let _ = fs::remove_dir_all(temp_dir);
    }

    #[test]
    fn get_jobs_skips_blank_and_unknown_ids() {
        let app = App::new(Config::default()).unwrap();
        app.jobs.lock().unwrap().insert(
            "job-1".to_string(),
            completed_job_snapshot(
                "job-1".to_string(),
                JobKind::RenderStudy,
                Some("study-1".to_string()),
                JobResult {
                    kind: JobKind::RenderStudy,
                    payload: serde_json::json!({ "studyId": "study-1" }),
                },
            ),
        );

        let snapshots = app
            .get_jobs(GetJobsCommand {
                job_ids: vec![" ".to_string(), "missing".to_string(), "job-1".to_string()],
            })
            .unwrap();

        assert_eq!(snapshots.len(), 1);
        assert_eq!(snapshots[0].job_id, "job-1");
    }

    #[test]
    fn cancel_job_matches_registry_state_transitions() {
        let app = App::new(Config::default()).unwrap();
        let subscription = app.subscribe_job_updates();
        app.store_job_snapshot(test_job_snapshot(
            "job-queued",
            JobKind::RenderStudy,
            JobState::Queued,
            "queued",
            "Queued",
        ));

        let queued = app
            .cancel_job(JobCommand {
                job_id: "job-queued".to_string(),
            })
            .unwrap();

        assert_eq!(queued.state, JobState::Cancelled);
        assert_eq!(queued.progress.message, "Cancelled before start");
        assert!(queued.result.is_none());
        assert!(queued.error.is_none());
        let queued_update = subscription
            .receiver
            .recv_timeout(std::time::Duration::from_millis(500))
            .unwrap();
        assert_eq!(queued_update.job_id, "job-queued");
        assert_eq!(queued_update.state, JobState::Cancelled);

        app.store_job_snapshot(test_job_snapshot(
            "job-running",
            JobKind::ProcessStudy,
            JobState::Running,
            "loadingStudy",
            "Loading source pixels",
        ));

        let running = app
            .cancel_job(JobCommand {
                job_id: "job-running".to_string(),
            })
            .unwrap();

        assert_eq!(running.state, JobState::Cancelling);
        assert_eq!(running.progress.message, "Cancellation requested");
        assert!(running.result.is_none());
        assert!(running.error.is_none());
        let running_update = subscription
            .receiver
            .recv_timeout(std::time::Duration::from_millis(500))
            .unwrap();
        assert_eq!(running_update.job_id, "job-running");
        assert_eq!(running_update.state, JobState::Cancelling);
        app.unsubscribe_job_updates(subscription.id);
    }

    #[test]
    fn store_job_snapshot_caps_terminal_job_retention_and_keeps_latest() {
        let app = App::new(Config::default()).unwrap();

        for index in 0..(MAX_TERMINAL_JOBS + 5) {
            app.store_job_snapshot(test_job_snapshot(
                &format!("job-{index}"),
                JobKind::RenderStudy,
                JobState::Completed,
                "completed",
                "Completed",
            ));
        }

        let jobs = app.jobs.lock().unwrap();
        assert_eq!(jobs.len(), MAX_TERMINAL_JOBS);
        assert!(jobs.contains_key(&format!("job-{}", MAX_TERMINAL_JOBS + 4)));
        assert!(
            jobs.values()
                .all(|snapshot| is_terminal_state(&snapshot.state))
        );
    }

    #[test]
    fn reserve_async_job_deduplicates_active_fingerprint_until_cancelled() {
        let app = App::new(Config::default()).unwrap();

        let first = app
            .reserve_async_job("fingerprint-1", JobKind::RenderStudy, "study-1".to_string())
            .unwrap();
        let first_job_id = match first {
            AsyncJobReservation::Created(job_id) => job_id,
            AsyncJobReservation::Existing(job_id) => {
                panic!("first reservation unexpectedly reused {job_id}")
            }
        };
        let second = app
            .reserve_async_job("fingerprint-1", JobKind::RenderStudy, "study-1".to_string())
            .unwrap();

        match second {
            AsyncJobReservation::Existing(job_id) => assert_eq!(job_id, first_job_id),
            AsyncJobReservation::Created(job_id) => {
                panic!("second reservation unexpectedly created {job_id}")
            }
        }

        let cancelled = app
            .cancel_job(JobCommand {
                job_id: first_job_id,
            })
            .unwrap();
        assert_eq!(cancelled.state, JobState::Cancelled);

        let replacement = app
            .reserve_async_job("fingerprint-1", JobKind::RenderStudy, "study-1".to_string())
            .unwrap();
        match replacement {
            AsyncJobReservation::Created(job_id) => assert_eq!(job_id, "job-2"),
            AsyncJobReservation::Existing(job_id) => {
                panic!("replacement reservation unexpectedly reused {job_id}")
            }
        }
    }

    #[test]
    fn finish_async_job_preserves_cancelled_terminal_state_and_cleans_artifacts() {
        let temp_dir = std::env::temp_dir().join(format!(
            "xrayview-rs-finish-cancelled-{}",
            std::process::id()
        ));
        fs::create_dir_all(&temp_dir).unwrap();
        let preview_path = temp_dir.join("preview.bmp");
        fs::write(&preview_path, b"preview").unwrap();

        let app = App::new(Config::default()).unwrap();
        let reservation = app
            .reserve_async_job("fingerprint-1", JobKind::RenderStudy, "study-1".to_string())
            .unwrap();
        let job_id = match reservation {
            AsyncJobReservation::Created(job_id) => job_id,
            AsyncJobReservation::Existing(job_id) => {
                panic!("reservation unexpectedly reused {job_id}")
            }
        };
        app.update_job_progress(
            &job_id,
            JobState::Running,
            90,
            "writingPreview",
            "Writing preview",
        );
        let cancelling = app
            .cancel_job(JobCommand {
                job_id: job_id.clone(),
            })
            .unwrap();
        assert_eq!(cancelling.state, JobState::Cancelling);

        let completed = completed_job_snapshot(
            job_id.clone(),
            JobKind::RenderStudy,
            Some("study-1".to_string()),
            JobResult {
                kind: JobKind::RenderStudy,
                payload: serde_json::to_value(RenderStudyCommandResult {
                    study_id: "study-1".to_string(),
                    preview_path: preview_path.display().to_string(),
                    loaded_width: 1,
                    loaded_height: 1,
                    measurement_scale: None,
                })
                .unwrap(),
            },
        );

        app.finish_async_job(&job_id, "fingerprint-1", completed);

        let snapshot = app.get_job(JobCommand { job_id }).unwrap();
        assert_eq!(snapshot.state, JobState::Cancelled);
        assert_eq!(snapshot.progress.stage, "writingPreview");
        assert_eq!(snapshot.progress.message, "Cancelled by user");
        assert!(snapshot.result.is_none());
        assert!(snapshot.error.is_none());
        assert!(!preview_path.exists());
        assert!(
            app.active_fingerprints
                .lock()
                .unwrap()
                .get("fingerprint-1")
                .is_none()
        );

        let _ = fs::remove_dir_all(temp_dir);
    }

    #[test]
    fn start_process_job_writes_preview_snapshot() {
        let temp_dir =
            std::env::temp_dir().join(format!("xrayview-rs-process-job-{}", std::process::id()));
        let cache_dir = temp_dir.join("cache");
        let state_dir = temp_dir.join("state");
        fs::create_dir_all(&temp_dir).unwrap();
        let input_path = temp_dir.join("sample.bmp");
        fs::write(&input_path, build_renderable_test_bmp()).unwrap();

        let mut config = Config::default();
        config.paths.cache_dir = cache_dir;
        config.paths.persistence_dir = state_dir;
        let app = App::new(config).unwrap();
        let study = app
            .open_study(OpenStudyCommand {
                input_path: input_path.display().to_string(),
            })
            .unwrap()
            .study;

        let started = app
            .start_process_job(ProcessStudyCommand {
                study_id: study.study_id.clone(),
                preset_id: "default".to_string(),
                invert: true,
                brightness: Some(10),
                contrast: Some(1.0),
                equalize: false,
                compare: false,
                palette: None,
            })
            .unwrap();
        let snapshot = app
            .get_job(JobCommand {
                job_id: started.job_id,
            })
            .unwrap();

        assert_eq!(snapshot.state, JobState::Completed);
        assert_eq!(snapshot.job_kind, JobKind::ProcessStudy);
        let result = snapshot.result.unwrap();
        assert_eq!(result.kind, JobKind::ProcessStudy);
        let payload: ProcessStudyCommandResult = serde_json::from_value(result.payload).unwrap();
        assert_eq!(payload.study_id, study.study_id);
        assert_eq!(payload.loaded_width, 4);
        assert_eq!(payload.loaded_height, 2);
        assert_eq!(payload.mode, "inverted grayscale with brightness +10");
        assert!(fs::metadata(&payload.preview_path).unwrap().is_file());
        assert_eq!(payload.measurement_scale, None);

        let _ = fs::remove_file(input_path);
        let _ = fs::remove_dir_all(temp_dir);
    }

    #[test]
    fn start_process_job_reuses_in_session_cached_result_for_same_controls() {
        let (temp_dir, app, study) = app_with_renderable_study(
            "process-cache-hit",
            build_renderable_test_bmp(),
        );
        let command = ProcessStudyCommand {
            study_id: study.study_id.clone(),
            preset_id: "xray".to_string(),
            invert: false,
            brightness: None,
            contrast: None,
            equalize: true,
            compare: false,
            palette: None,
        };

        let first_started = app.start_process_job(command.clone()).unwrap();
        let first_snapshot = app
            .get_job(JobCommand {
                job_id: first_started.job_id,
            })
            .unwrap();
        let first_payload: ProcessStudyCommandResult =
            serde_json::from_value(first_snapshot.result.clone().unwrap().payload).unwrap();
        let second_started = app.start_process_job(command).unwrap();
        let second_snapshot = app
            .get_job(JobCommand {
                job_id: second_started.job_id,
            })
            .unwrap();
        let second_payload: ProcessStudyCommandResult =
            serde_json::from_value(second_snapshot.result.clone().unwrap().payload).unwrap();

        assert!(!first_snapshot.from_cache);
        assert!(second_snapshot.from_cache);
        assert_eq!(second_payload.preview_path, first_payload.preview_path);

        let _ = fs::remove_dir_all(temp_dir);
    }

    #[test]
    fn source_preview_cache_reuses_decode_for_different_process_jobs() {
        let (temp_dir, app, study) = app_with_renderable_study(
            "source-preview-cache-hit",
            build_renderable_test_bmp(),
        );
        let first = ProcessStudyCommand {
            study_id: study.study_id.clone(),
            preset_id: "default".to_string(),
            invert: false,
            brightness: Some(5),
            contrast: None,
            equalize: false,
            compare: false,
            palette: None,
        };
        let second = ProcessStudyCommand {
            contrast: Some(1.2),
            ..first.clone()
        };

        let first_started = app.start_process_job(first).unwrap();
        let first_snapshot = app
            .get_job(JobCommand {
                job_id: first_started.job_id,
            })
            .unwrap();
        let after_first = app.source_preview_cache_stats();
        let second_started = app.start_process_job(second).unwrap();
        let second_snapshot = app
            .get_job(JobCommand {
                job_id: second_started.job_id,
            })
            .unwrap();
        let after_second = app.source_preview_cache_stats();

        assert!(!first_snapshot.from_cache);
        assert!(!second_snapshot.from_cache);
        assert_eq!(after_first.len, 1);
        assert_eq!(after_first.misses, 1);
        assert_eq!(after_first.hits, 0);
        assert_eq!(after_second.len, 1);
        assert_eq!(after_second.misses, 1);
        assert_eq!(after_second.hits, 1);

        let _ = fs::remove_dir_all(temp_dir);
    }

    #[test]
    fn start_analyze_job_writes_overlay_previews() {
        let temp_dir =
            std::env::temp_dir().join(format!("xrayview-rs-analyze-job-{}", std::process::id()));
        let cache_dir = temp_dir.join("cache");
        let state_dir = temp_dir.join("state");
        fs::create_dir_all(&temp_dir).unwrap();
        let input_path = temp_dir.join("sample.bmp");
        fs::write(&input_path, build_analysis_test_bmp()).unwrap();

        let mut config = Config::default();
        config.paths.cache_dir = cache_dir;
        config.paths.persistence_dir = state_dir;
        let app = App::new(config).unwrap();
        let study = app
            .open_study(OpenStudyCommand {
                input_path: input_path.display().to_string(),
            })
            .unwrap()
            .study;

        let started = app
            .start_analyze_job(AnalyzeStudyCommand {
                study_id: study.study_id.clone(),
            })
            .unwrap();
        let snapshot = app
            .get_job(JobCommand {
                job_id: started.job_id,
            })
            .unwrap();

        assert_eq!(snapshot.state, JobState::Completed);
        assert_eq!(snapshot.job_kind, JobKind::AnalyzeStudy);
        let result = snapshot.result.unwrap();
        assert_eq!(result.kind, JobKind::AnalyzeStudy);
        let payload: AnalyzeStudyCommandResult = serde_json::from_value(result.payload).unwrap();
        assert_eq!(payload.study_id, study.study_id);
        assert_eq!(payload.loaded_width, 20);
        assert_eq!(payload.loaded_height, 20);
        assert!(
            payload
                .mode
                .starts_with("dynamic tooth and bone level overlay")
        );
        assert!(fs::metadata(&payload.preview_path).unwrap().is_file());
        assert!(
            fs::metadata(&payload.filled_preview_path)
                .unwrap()
                .is_file()
        );
        assert_eq!(payload.measurement_scale, None);

        let _ = fs::remove_file(input_path);
        let _ = fs::remove_dir_all(temp_dir);
    }

    #[test]
    fn start_analyze_job_reuses_in_session_cached_result() {
        let (temp_dir, app, study) = app_with_renderable_study(
            "analyze-cache-hit",
            build_analysis_test_bmp(),
        );

        let first_started = app
            .start_analyze_job(AnalyzeStudyCommand {
                study_id: study.study_id.clone(),
            })
            .unwrap();
        let first_snapshot = app
            .get_job(JobCommand {
                job_id: first_started.job_id,
            })
            .unwrap();
        let first_payload: AnalyzeStudyCommandResult =
            serde_json::from_value(first_snapshot.result.clone().unwrap().payload).unwrap();
        let second_started = app
            .start_analyze_job(AnalyzeStudyCommand {
                study_id: study.study_id.clone(),
            })
            .unwrap();
        let second_snapshot = app
            .get_job(JobCommand {
                job_id: second_started.job_id,
            })
            .unwrap();
        let second_payload: AnalyzeStudyCommandResult =
            serde_json::from_value(second_snapshot.result.clone().unwrap().payload).unwrap();

        assert!(!first_snapshot.from_cache);
        assert!(second_snapshot.from_cache);
        assert_eq!(second_payload.preview_path, first_payload.preview_path);
        assert_eq!(
            second_payload.filled_preview_path,
            first_payload.filled_preview_path
        );

        let _ = fs::remove_dir_all(temp_dir);
    }

    fn app_with_renderable_study(
        test_name: &str,
        bytes: Vec<u8>,
    ) -> (std::path::PathBuf, App, StudyRecord) {
        let unique = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        let temp_dir = std::env::temp_dir().join(format!(
            "xrayview-rs-{test_name}-{}-{unique}",
            std::process::id()
        ));
        let cache_dir = temp_dir.join("cache");
        let state_dir = temp_dir.join("state");
        fs::create_dir_all(&temp_dir).unwrap();
        let input_path = temp_dir.join("sample.bmp");
        fs::write(&input_path, bytes).unwrap();

        let mut config = Config::default();
        config.paths.cache_dir = cache_dir;
        config.paths.persistence_dir = state_dir;
        let app = App::new(config).unwrap();
        let study = app
            .open_study(OpenStudyCommand {
                input_path: input_path.display().to_string(),
            })
            .unwrap()
            .study;

        (temp_dir, app, study)
    }

    fn build_renderable_test_bmp() -> Vec<u8> {
        bmp::tests::build_bmp_32(
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

    fn build_analysis_test_bmp() -> Vec<u8> {
        let pixels = analyze_fixture_pixels()
            .iter()
            .map(|value| (*value, *value, *value))
            .collect::<Vec<_>>();
        bmp::tests::build_bmp_32(20, 20, &pixels)
    }

    fn analyze_fixture_pixels() -> &'static [u8] {
        static PIXELS: [u8; 400] = {
            let mut pixels = [24_u8; 400];
            let mut y = 6;
            while y < 14 {
                let mut x = 7;
                while x < 13 {
                    pixels[y * 20 + x] = 210;
                    x += 1;
                }
                y += 1;
            }
            let mut x = 0;
            while x < 20 {
                pixels[10 * 20 + x] = if x % 2 == 0 { 40 } else { 180 };
                x += 1;
            }
            pixels
        };
        &PIXELS
    }

    fn test_job_snapshot(
        job_id: &str,
        job_kind: JobKind,
        state: JobState,
        stage: &str,
        message: &str,
    ) -> JobSnapshot {
        JobSnapshot {
            job_id: job_id.to_string(),
            job_kind,
            study_id: Some("study-1".to_string()),
            state,
            progress: JobProgress {
                percent: 0,
                stage: stage.to_string(),
                message: message.to_string(),
            },
            from_cache: false,
            result: None,
            error: None,
        }
    }

    fn assert_ordered_stages(actual: &[String], expected: &[&str]) {
        let mut cursor = 0;
        for stage in actual {
            if expected
                .get(cursor)
                .is_some_and(|expected| *expected == stage)
            {
                cursor += 1;
                if cursor == expected.len() {
                    return;
                }
            }
        }
        panic!("stage sequence {actual:?} did not include {expected:?} in order");
    }
}
