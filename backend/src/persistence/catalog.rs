use std::fs;
use std::path::PathBuf;

use chrono::Utc;
use serde::{Deserialize, Serialize};

use crate::api::{MeasurementScale, StudyRecord};
use crate::error::{BackendError, BackendResult};

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RecentStudyEntry {
    pub input_path: PathBuf,
    pub input_name: String,
    pub measurement_scale: Option<PersistedMeasurementScale>,
    pub last_opened_at: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct StudyCatalog {
    pub recent_studies: Vec<RecentStudyEntry>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PersistedMeasurementScale {
    pub row_spacing_mm: f64,
    pub column_spacing_mm: f64,
    pub source: String,
}

impl From<MeasurementScale> for PersistedMeasurementScale {
    fn from(scale: MeasurementScale) -> Self {
        Self {
            row_spacing_mm: scale.row_spacing_mm,
            column_spacing_mm: scale.column_spacing_mm,
            source: scale.source.to_string(),
        }
    }
}

#[derive(Debug, Clone)]
pub struct StudyCatalogStore {
    path: PathBuf,
}

impl StudyCatalogStore {
    pub fn new(path: PathBuf) -> Self {
        Self { path }
    }

    pub fn load_or_default(&self) -> StudyCatalog {
        self.load().unwrap_or_default()
    }

    pub fn load(&self) -> BackendResult<StudyCatalog> {
        let Ok(contents) = fs::read_to_string(&self.path) else {
            return Ok(StudyCatalog::default());
        };

        serde_json::from_str(&contents).map_err(|error| {
            let _ = fs::rename(&self.path, self.path.with_extension("corrupt.json"));
            BackendError::cache_corrupted(format!(
                "study catalog at {} is invalid JSON: {error}",
                self.path.display()
            ))
        })
    }

    pub fn record_opened_study(&self, study: &StudyRecord) -> BackendResult<()> {
        let mut catalog = self.load_or_default();
        catalog
            .recent_studies
            .retain(|entry| entry.input_path != study.input_path);
        catalog.recent_studies.insert(
            0,
            RecentStudyEntry {
                input_path: study.input_path.clone(),
                input_name: study.input_name.clone(),
                measurement_scale: study.measurement_scale.map(Into::into),
                last_opened_at: Utc::now().to_rfc3339(),
            },
        );
        catalog.recent_studies.truncate(10);
        self.save(&catalog)
    }

    fn save(&self, catalog: &StudyCatalog) -> BackendResult<()> {
        if let Some(parent) = self.path.parent() {
            fs::create_dir_all(parent).map_err(|error| {
                BackendError::internal(format!(
                    "failed to create catalog directory {}: {error}",
                    parent.display()
                ))
            })?;
        }

        let payload = serde_json::to_string_pretty(catalog)
            .map_err(|error| BackendError::internal(format!("serialize study catalog: {error}")))?;
        fs::write(&self.path, payload).map_err(|error| {
            BackendError::internal(format!(
                "failed to write study catalog {}: {error}",
                self.path.display()
            ))
        })
    }
}

#[cfg(test)]
mod tests {
    use std::fs;
    use tempfile::tempdir;

    use crate::api::StudyRecord;

    use super::StudyCatalogStore;

    #[test]
    fn record_opened_study_keeps_most_recent_entry_first() {
        let temp = tempdir().expect("temp dir");
        let store = StudyCatalogStore::new(temp.path().join("catalog.json"));

        store
            .record_opened_study(&StudyRecord {
                study_id: String::from("study-1"),
                input_path: "/tmp/one.dcm".into(),
                input_name: String::from("one.dcm"),
                measurement_scale: None,
            })
            .expect("record first study");
        store
            .record_opened_study(&StudyRecord {
                study_id: String::from("study-2"),
                input_path: "/tmp/two.dcm".into(),
                input_name: String::from("two.dcm"),
                measurement_scale: None,
            })
            .expect("record second study");

        let catalog = store.load().expect("load catalog");
        assert_eq!(catalog.recent_studies[0].input_name, "two.dcm");
        assert_eq!(catalog.recent_studies[1].input_name, "one.dcm");
    }

    #[test]
    fn invalid_catalog_is_treated_as_corrupted_cache() {
        let temp = tempdir().expect("temp dir");
        let catalog_path = temp.path().join("catalog.json");
        fs::write(&catalog_path, "{ not json").expect("write invalid catalog");
        let store = StudyCatalogStore::new(catalog_path.clone());

        let error = store.load().expect_err("invalid catalog should error");

        assert_eq!(error.code, crate::error::BackendErrorCode::CacheCorrupted);
        assert!(catalog_path.with_extension("corrupt.json").exists());
    }
}
