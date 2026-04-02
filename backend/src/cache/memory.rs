use std::collections::HashMap;
use std::path::Path;
use std::sync::{Arc, Mutex};

use crate::api::JobResult;
use crate::error::{BackendError, BackendResult};

#[derive(Debug, Clone, Default)]
pub struct MemoryCache {
    entries: Arc<Mutex<HashMap<String, JobResult>>>,
}

impl MemoryCache {
    pub fn get(&self, key: &str) -> BackendResult<Option<JobResult>> {
        let mut entries = self.lock()?;
        let Some(result) = entries.get(key).cloned() else {
            return Ok(None);
        };

        if result_artifacts_exist(&result) {
            Ok(Some(result))
        } else {
            entries.remove(key);
            Ok(None)
        }
    }

    pub fn insert(&self, key: String, result: JobResult) -> BackendResult<()> {
        self.lock()?.insert(key, result);
        Ok(())
    }

    fn lock(&self) -> BackendResult<std::sync::MutexGuard<'_, HashMap<String, JobResult>>> {
        self.entries
            .lock()
            .map_err(|_| BackendError::internal("memory cache is unavailable"))
    }
}

fn result_artifacts_exist(result: &JobResult) -> bool {
    match result {
        JobResult::RenderStudy(result) => Path::new(&result.preview_path).exists(),
        JobResult::ProcessStudy(result) => {
            Path::new(&result.preview_path).exists() && Path::new(&result.dicom_path).exists()
        }
        JobResult::AnalyzeStudy(result) => Path::new(&result.preview_path).exists(),
    }
}

#[cfg(test)]
mod tests {
    use std::fs;
    use tempfile::tempdir;

    use crate::api::{JobResult, RenderStudyCommandResult};

    use super::MemoryCache;

    #[test]
    fn cache_hit_requires_artifacts_to_still_exist() {
        let cache = MemoryCache::default();
        let temp = tempdir().expect("temp dir");
        let preview_path = temp.path().join("preview.png");
        fs::write(&preview_path, b"png").expect("write preview");

        cache
            .insert(
                String::from("render:1"),
                JobResult::RenderStudy(RenderStudyCommandResult {
                    study_id: String::from("study-1"),
                    preview_path: preview_path.clone(),
                    measurement_scale: None,
                }),
            )
            .expect("insert cache entry");

        assert!(cache.get("render:1").expect("load cache entry").is_some());

        fs::remove_file(&preview_path).expect("remove preview");

        assert!(cache.get("render:1").expect("invalidate cache entry").is_none());
    }
}
