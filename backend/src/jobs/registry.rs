use std::collections::HashMap;
use std::sync::{Arc, Mutex};

use uuid::Uuid;

use crate::api::{JobKind, JobProgress, JobResult, JobSnapshot, JobState};
use crate::error::{BackendError, BackendResult};

#[derive(Debug, Clone)]
pub enum StartJobOutcome {
    Created(JobSnapshot),
    Existing(JobSnapshot),
}

#[derive(Debug, Clone, Default)]
pub struct JobRegistry {
    inner: Arc<Mutex<JobRegistryInner>>,
}

#[derive(Debug, Default)]
struct JobRegistryInner {
    jobs: HashMap<String, JobEntry>,
    active_fingerprints: HashMap<String, String>,
}

#[derive(Debug, Clone)]
struct JobEntry {
    fingerprint: Option<String>,
    cancellation_requested: bool,
    snapshot: JobSnapshot,
}

impl JobRegistry {
    pub fn start_job(
        &self,
        job_kind: JobKind,
        study_id: Option<String>,
        fingerprint: String,
    ) -> BackendResult<StartJobOutcome> {
        let mut inner = self.lock()?;

        if let Some(existing_job_id) = inner.active_fingerprints.get(&fingerprint)
            && let Some(existing) = inner.jobs.get(existing_job_id)
        {
            return Ok(StartJobOutcome::Existing(existing.snapshot.clone()));
        }

        let job_id = Uuid::new_v4().to_string();
        let snapshot = JobSnapshot {
            job_id: job_id.clone(),
            job_kind,
            study_id,
            state: JobState::Queued,
            progress: JobProgress {
                percent: 0,
                stage: String::from("queued"),
                message: String::from("Queued"),
            },
            from_cache: false,
            result: None,
            error: None,
        };
        inner.active_fingerprints.insert(fingerprint.clone(), job_id.clone());
        inner.jobs.insert(
            job_id,
            JobEntry {
                fingerprint: Some(fingerprint),
                cancellation_requested: false,
                snapshot: snapshot.clone(),
            },
        );

        Ok(StartJobOutcome::Created(snapshot))
    }

    pub fn create_cached_job(
        &self,
        job_kind: JobKind,
        study_id: Option<String>,
        result: JobResult,
    ) -> BackendResult<JobSnapshot> {
        let mut inner = self.lock()?;
        let job_id = Uuid::new_v4().to_string();
        let snapshot = JobSnapshot {
            job_id: job_id.clone(),
            job_kind,
            study_id,
            state: JobState::Completed,
            progress: JobProgress {
                percent: 100,
                stage: String::from("cacheHit"),
                message: String::from("Loaded from cache"),
            },
            from_cache: true,
            result: Some(result),
            error: None,
        };
        inner.jobs.insert(
            job_id,
            JobEntry {
                fingerprint: None,
                cancellation_requested: false,
                snapshot: snapshot.clone(),
            },
        );
        Ok(snapshot)
    }

    pub fn get(&self, job_id: &str) -> BackendResult<JobSnapshot> {
        let inner = self.lock()?;
        inner
            .jobs
            .get(job_id)
            .map(|entry| entry.snapshot.clone())
            .ok_or_else(|| BackendError::not_found(format!("job not found: {job_id}")))
    }

    pub fn update_progress(
        &self,
        job_id: &str,
        state: JobState,
        percent: u8,
        stage: impl Into<String>,
        message: impl Into<String>,
    ) -> BackendResult<JobSnapshot> {
        let mut inner = self.lock()?;
        let entry = inner
            .jobs
            .get_mut(job_id)
            .ok_or_else(|| BackendError::not_found(format!("job not found: {job_id}")))?;
        if matches!(
            entry.snapshot.state,
            JobState::Completed | JobState::Failed | JobState::Cancelled
        ) {
            return Ok(entry.snapshot.clone());
        }
        entry.snapshot.state = state;
        entry.snapshot.progress = JobProgress {
            percent,
            stage: stage.into(),
            message: message.into(),
        };
        entry.snapshot.error = None;
        Ok(entry.snapshot.clone())
    }

    pub fn complete(&self, job_id: &str, result: JobResult) -> BackendResult<JobSnapshot> {
        let mut inner = self.lock()?;
        let fingerprint = {
            let entry = inner
                .jobs
                .get_mut(job_id)
                .ok_or_else(|| BackendError::not_found(format!("job not found: {job_id}")))?;
            entry.snapshot.state = JobState::Completed;
            entry.snapshot.progress = JobProgress {
                percent: 100,
                stage: String::from("completed"),
                message: String::from("Completed"),
            };
            entry.snapshot.result = Some(result);
            entry.snapshot.error = None;
            entry.fingerprint.take()
        };

        if let Some(fingerprint) = fingerprint {
            inner.active_fingerprints.remove(&fingerprint);
        }

        inner
            .jobs
            .get(job_id)
            .map(|entry| entry.snapshot.clone())
            .ok_or_else(|| BackendError::not_found(format!("job not found: {job_id}")))
    }

    pub fn fail(&self, job_id: &str, error: BackendError) -> BackendResult<JobSnapshot> {
        let mut inner = self.lock()?;
        let fingerprint = {
            let entry = inner
                .jobs
                .get_mut(job_id)
                .ok_or_else(|| BackendError::not_found(format!("job not found: {job_id}")))?;
            entry.snapshot.state = JobState::Failed;
            entry.snapshot.progress.message = String::from("Failed");
            entry.snapshot.error = Some(error);
            entry.fingerprint.take()
        };

        if let Some(fingerprint) = fingerprint {
            inner.active_fingerprints.remove(&fingerprint);
        }

        inner
            .jobs
            .get(job_id)
            .map(|entry| entry.snapshot.clone())
            .ok_or_else(|| BackendError::not_found(format!("job not found: {job_id}")))
    }

    pub fn cancel(&self, job_id: &str) -> BackendResult<JobSnapshot> {
        let mut inner = self.lock()?;
        let mut fingerprint_to_remove = None;
        let snapshot = {
            let entry = inner
                .jobs
                .get_mut(job_id)
                .ok_or_else(|| BackendError::not_found(format!("job not found: {job_id}")))?;
            match entry.snapshot.state {
                JobState::Queued => {
                    entry.cancellation_requested = true;
                    entry.snapshot.state = JobState::Cancelled;
                    entry.snapshot.progress.message = String::from("Cancelled before start");
                    fingerprint_to_remove = entry.fingerprint.take();
                }
                JobState::Running | JobState::Cancelling => {
                    entry.cancellation_requested = true;
                    entry.snapshot.state = JobState::Cancelling;
                    entry.snapshot.progress.message = String::from("Cancellation requested");
                }
                JobState::Completed | JobState::Failed | JobState::Cancelled => {}
            }
            entry.snapshot.clone()
        };

        if let Some(fingerprint) = fingerprint_to_remove {
            inner.active_fingerprints.remove(&fingerprint);
        }

        Ok(snapshot)
    }

    pub fn mark_cancelled(
        &self,
        job_id: &str,
        stage: impl Into<String>,
        message: impl Into<String>,
    ) -> BackendResult<JobSnapshot> {
        let mut inner = self.lock()?;
        let fingerprint = {
            let entry = inner
                .jobs
                .get_mut(job_id)
                .ok_or_else(|| BackendError::not_found(format!("job not found: {job_id}")))?;
            entry.snapshot.state = JobState::Cancelled;
            entry.snapshot.progress.stage = stage.into();
            entry.snapshot.progress.message = message.into();
            entry.snapshot.error = None;
            entry.fingerprint.take()
        };

        if let Some(fingerprint) = fingerprint {
            inner.active_fingerprints.remove(&fingerprint);
        }

        inner
            .jobs
            .get(job_id)
            .map(|entry| entry.snapshot.clone())
            .ok_or_else(|| BackendError::not_found(format!("job not found: {job_id}")))
    }

    pub fn is_cancellation_requested(&self, job_id: &str) -> BackendResult<bool> {
        let inner = self.lock()?;
        inner
            .jobs
            .get(job_id)
            .map(|entry| entry.cancellation_requested)
            .ok_or_else(|| BackendError::not_found(format!("job not found: {job_id}")))
    }

    fn lock(&self) -> BackendResult<std::sync::MutexGuard<'_, JobRegistryInner>> {
        self.inner
            .lock()
            .map_err(|_| BackendError::internal("job registry is unavailable"))
    }
}

#[cfg(test)]
mod tests {
    use crate::api::{JobKind, ProcessStudyCommandResult};

    use super::{JobRegistry, StartJobOutcome};

    #[test]
    fn start_job_reuses_active_duplicate_requests() {
        let registry = JobRegistry::default();

        let first = registry
            .start_job(
                JobKind::ProcessStudy,
                Some(String::from("study-1")),
                String::from("fingerprint-1"),
            )
            .expect("start first job");
        let second = registry
            .start_job(
                JobKind::ProcessStudy,
                Some(String::from("study-1")),
                String::from("fingerprint-1"),
            )
            .expect("start duplicate job");

        let StartJobOutcome::Created(first_snapshot) = first else {
            panic!("expected first job to be created");
        };
        let StartJobOutcome::Existing(second_snapshot) = second else {
            panic!("expected duplicate job to be reused");
        };

        assert_eq!(first_snapshot.job_id, second_snapshot.job_id);
    }

    #[test]
    fn complete_job_releases_fingerprint_for_later_runs() {
        let registry = JobRegistry::default();
        let created = registry
            .start_job(
                JobKind::ProcessStudy,
                Some(String::from("study-1")),
                String::from("fingerprint-1"),
            )
            .expect("start job");

        let StartJobOutcome::Created(snapshot) = created else {
            panic!("expected a created job");
        };

        registry
            .complete(
                &snapshot.job_id,
                crate::api::JobResult::ProcessStudy(ProcessStudyCommandResult {
                    study_id: String::from("study-1"),
                    preview_path: "/tmp/preview.png".into(),
                    dicom_path: "/tmp/output.dcm".into(),
                    loaded_width: 1,
                    loaded_height: 1,
                    mode: String::from("grayscale"),
                    measurement_scale: None,
                }),
            )
            .expect("complete job");

        let next = registry
            .start_job(
                JobKind::ProcessStudy,
                Some(String::from("study-1")),
                String::from("fingerprint-1"),
            )
            .expect("start replacement job");

        assert!(matches!(next, StartJobOutcome::Created(_)));
    }
}
