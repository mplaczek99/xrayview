use std::{
    fs, io,
    path::{Path, PathBuf},
};

use chrono::{DateTime, SecondsFormat, Utc};
use parking_lot::Mutex;
use serde::{Deserialize, Serialize};

use crate::contracts::{BackendError, BackendErrorCode, MeasurementScale, StudyRecord};

pub const RECENT_STUDY_LIMIT: usize = 10;

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RecentStudyEntry {
    #[serde(default)]
    pub input_path: String,
    #[serde(default)]
    pub input_name: String,
    #[serde(default)]
    pub measurement_scale: Option<MeasurementScale>,
    #[serde(default)]
    pub last_opened_at: String,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct StudyCatalog {
    #[serde(default)]
    pub recent_studies: Vec<RecentStudyEntry>,
}

pub struct Catalog {
    root_dir: PathBuf,
    path: PathBuf,
    operation_lock: Mutex<()>,
    state: Mutex<CatalogState>,
    now: Mutex<Box<dyn Fn() -> DateTime<Utc> + Send + Sync>>,
}

#[derive(Debug, Clone)]
struct CatalogState {
    loaded: bool,
    cache: StudyCatalog,
}

impl Catalog {
    pub fn new(root_dir: impl Into<PathBuf>) -> Self {
        Self::new_at_path(root_dir.into().join("catalog.json"))
    }

    pub fn new_at_path(path: impl Into<PathBuf>) -> Self {
        let path = path.into();
        let root_dir = path
            .parent()
            .unwrap_or_else(|| Path::new("."))
            .to_path_buf();
        Self {
            root_dir,
            path,
            operation_lock: Mutex::new(()),
            state: Mutex::new(CatalogState {
                loaded: false,
                cache: empty_study_catalog(),
            }),
            now: Mutex::new(Box::new(Utc::now)),
        }
    }

    #[must_use]
    pub fn root_dir(&self) -> &Path {
        &self.root_dir
    }

    #[must_use]
    pub fn path(&self) -> &Path {
        &self.path
    }

    #[cfg(test)]
    pub fn set_now<F>(&self, now: F)
    where
        F: Fn() -> DateTime<Utc> + Send + Sync + 'static,
    {
        *self.now.lock() = Box::new(now);
    }

    pub fn ensure(&self) -> Result<(), BackendError> {
        fs::create_dir_all(&self.root_dir).map_err(|error| {
            BackendError::internal(format!(
                "failed to create catalog directory {}: {error}",
                self.root_dir.display()
            ))
        })
    }

    pub fn load(&self) -> Result<StudyCatalog, BackendError> {
        let _operation_guard = self.operation_lock.lock();
        let value = match self.load_from_disk() {
            Ok(value) => value,
            Err(error) => {
                let mut state = self.state.lock();
                state.loaded = false;
                state.cache = empty_study_catalog();
                return Err(error);
            }
        };

        let mut state = self.state.lock();
        state.loaded = true;
        state.cache = value.clone();
        Ok(value)
    }

    pub fn record_opened_study(&self, study: &StudyRecord) -> Result<(), BackendError> {
        let _operation_guard = self.operation_lock.lock();
        let mut value = self.load_or_default()?;
        value
            .recent_studies
            .retain(|entry| entry.input_path != study.input_path);

        value.recent_studies.insert(
            0,
            RecentStudyEntry {
                input_path: study.input_path.clone(),
                input_name: study.input_name.clone(),
                measurement_scale: study.measurement_scale.clone(),
                last_opened_at: self.now.lock()().to_rfc3339_opts(SecondsFormat::Secs, true),
            },
        );
        value.recent_studies.truncate(RECENT_STUDY_LIMIT);

        self.save(value)
    }

    fn load_from_disk(&self) -> Result<StudyCatalog, BackendError> {
        let contents = match fs::read(&self.path) {
            Ok(contents) => contents,
            Err(error) if error.kind() == io::ErrorKind::NotFound => {
                return Ok(empty_study_catalog());
            }
            Err(error) => {
                return Err(BackendError::internal(format!(
                    "failed to read study catalog {}: {error}",
                    self.path.display()
                )));
            }
        };

        serde_json::from_slice::<StudyCatalog>(&contents).map_err(|error| {
            let _ = fs::rename(&self.path, self.corrupt_path());
            BackendError::new(
                BackendErrorCode::CacheCorrupted,
                format!(
                    "study catalog at {} is invalid JSON: {error}",
                    self.path.display()
                ),
            )
        })
    }

    fn load_or_default(&self) -> Result<StudyCatalog, BackendError> {
        {
            let state = self.state.lock();
            if state.loaded {
                return Ok(state.cache.clone());
            }
        }

        match self.load_from_disk() {
            Ok(value) => {
                let mut state = self.state.lock();
                state.loaded = true;
                state.cache = value.clone();
                Ok(value)
            }
            Err(error) if error.code == BackendErrorCode::CacheCorrupted => {
                Ok(empty_study_catalog())
            }
            Err(error) => Err(error),
        }
    }

    fn save(&self, mut value: StudyCatalog) -> Result<(), BackendError> {
        self.ensure()?;
        value.recent_studies.shrink_to_fit();

        let payload = serde_json::to_vec_pretty(&value)
            .map_err(|error| BackendError::internal(format!("serialize study catalog: {error}")))?;
        fs::write(&self.path, payload).map_err(|error| {
            BackendError::internal(format!(
                "failed to write study catalog {}: {error}",
                self.path.display()
            ))
        })?;

        let mut state = self.state.lock();
        state.loaded = true;
        state.cache = value;
        Ok(())
    }

    fn corrupt_path(&self) -> PathBuf {
        match (self.path.file_stem(), self.path.extension()) {
            (Some(stem), Some(extension)) => {
                let mut file_name = stem.to_os_string();
                file_name.push(".corrupt.");
                file_name.push(extension);
                self.path.with_file_name(file_name)
            }
            _ => {
                let mut path = self.path.as_os_str().to_os_string();
                path.push(".corrupt");
                PathBuf::from(path)
            }
        }
    }
}

fn empty_study_catalog() -> StudyCatalog {
    StudyCatalog {
        recent_studies: Vec::new(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn study(input_path: impl Into<String>, input_name: impl Into<String>) -> StudyRecord {
        StudyRecord {
            study_id: "study-1".to_string(),
            input_path: input_path.into(),
            input_name: input_name.into(),
            measurement_scale: None,
        }
    }

    #[test]
    fn record_opened_study_keeps_most_recent_entry_first() {
        let root =
            std::env::temp_dir().join(format!("xrayview-rs-catalog-recent-{}", std::process::id()));
        let _ = fs::remove_dir_all(&root);
        let catalog = Catalog::new(&root);
        catalog.set_now(|| {
            DateTime::parse_from_rfc3339("2026-01-02T03:04:05Z")
                .unwrap()
                .into()
        });

        catalog
            .record_opened_study(&study("/tmp/one.bmp", "one.bmp"))
            .unwrap();
        catalog
            .record_opened_study(&study("/tmp/two.bmp", "two.bmp"))
            .unwrap();

        let value = catalog.load().unwrap();
        assert_eq!(value.recent_studies.len(), 2);
        assert_eq!(value.recent_studies[0].input_name, "two.bmp");
        assert_eq!(value.recent_studies[1].input_name, "one.bmp");

        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn load_missing_catalog_returns_empty_recent_studies_array() {
        let root = std::env::temp_dir().join(format!(
            "xrayview-rs-catalog-missing-{}",
            std::process::id()
        ));
        let _ = fs::remove_dir_all(&root);
        let catalog = Catalog::new(&root);

        let value = catalog.load().unwrap();

        assert!(value.recent_studies.is_empty());
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn load_treats_invalid_catalog_as_corrupted_cache() {
        let root = std::env::temp_dir().join(format!(
            "xrayview-rs-catalog-corrupt-{}",
            std::process::id()
        ));
        let _ = fs::remove_dir_all(&root);
        fs::create_dir_all(&root).unwrap();
        fs::write(root.join("catalog.json"), b"{ not json").unwrap();
        let catalog = Catalog::new(&root);

        let error = catalog.load().unwrap_err();

        assert_eq!(error.code, BackendErrorCode::CacheCorrupted);
        assert!(
            fs::metadata(root.join("catalog.corrupt.json"))
                .unwrap()
                .is_file()
        );
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn load_tolerates_unknown_and_missing_entry_fields_like_lenient_json_unmarshal() {
        let root = std::env::temp_dir().join(format!(
            "xrayview-rs-catalog-forgiving-{}",
            std::process::id()
        ));
        let _ = fs::remove_dir_all(&root);
        fs::create_dir_all(&root).unwrap();
        fs::write(
            root.join("catalog.json"),
            br#"{
              "recentStudies": [
                {
                  "inputPath": "/tmp/one.bmp",
                  "inputName": "one.bmp",
                  "extra": true
                }
              ],
              "futureField": "ignored"
            }"#,
        )
        .unwrap();
        let catalog = Catalog::new(&root);

        let value = catalog.load().unwrap();

        assert_eq!(value.recent_studies.len(), 1);
        assert_eq!(value.recent_studies[0].input_path, "/tmp/one.bmp");
        assert_eq!(value.recent_studies[0].input_name, "one.bmp");
        assert_eq!(value.recent_studies[0].measurement_scale, None);
        assert_eq!(value.recent_studies[0].last_opened_at, "");
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn record_opened_study_recovers_from_corrupt_catalog() {
        let root = std::env::temp_dir().join(format!(
            "xrayview-rs-catalog-recover-{}",
            std::process::id()
        ));
        let _ = fs::remove_dir_all(&root);
        fs::create_dir_all(&root).unwrap();
        fs::write(root.join("catalog.json"), b"{ not json").unwrap();
        let catalog = Catalog::new(&root);

        catalog
            .record_opened_study(&study("/tmp/recovered.bmp", "recovered.bmp"))
            .unwrap();
        let value = catalog.load().unwrap();

        assert_eq!(value.recent_studies.len(), 1);
        assert_eq!(value.recent_studies[0].input_name, "recovered.bmp");
        assert!(
            fs::metadata(root.join("catalog.corrupt.json"))
                .unwrap()
                .is_file()
        );
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn record_opened_study_reorders_existing_study_without_duplicate() {
        let root =
            std::env::temp_dir().join(format!("xrayview-rs-catalog-dedupe-{}", std::process::id()));
        let _ = fs::remove_dir_all(&root);
        let catalog = Catalog::new(&root);

        catalog
            .record_opened_study(&study("/tmp/one.bmp", "one.bmp"))
            .unwrap();
        catalog
            .record_opened_study(&study("/tmp/two.bmp", "two.bmp"))
            .unwrap();
        catalog
            .record_opened_study(&study("/tmp/one.bmp", "one.bmp"))
            .unwrap();
        let value = catalog.load().unwrap();

        assert_eq!(value.recent_studies.len(), 2);
        assert_eq!(value.recent_studies[0].input_path, "/tmp/one.bmp");
        assert_eq!(value.recent_studies[1].input_path, "/tmp/two.bmp");
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn record_opened_study_truncates_to_ten_entries() {
        let root =
            std::env::temp_dir().join(format!("xrayview-rs-catalog-limit-{}", std::process::id()));
        let _ = fs::remove_dir_all(&root);
        let catalog = Catalog::new(&root);

        for index in 0..12 {
            let input_name = format!("study-{index:02}.bmp");
            catalog
                .record_opened_study(&study(format!("/tmp/{input_name}"), input_name))
                .unwrap();
        }
        let value = catalog.load().unwrap();

        assert_eq!(value.recent_studies.len(), RECENT_STUDY_LIMIT);
        assert_eq!(value.recent_studies[0].input_name, "study-11.bmp");
        assert_eq!(
            value.recent_studies[RECENT_STUDY_LIMIT - 1].input_name,
            "study-02.bmp"
        );
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn record_opened_study_persists_measurement_scale_and_rfc3339_timestamp() {
        let root =
            std::env::temp_dir().join(format!("xrayview-rs-catalog-scale-{}", std::process::id()));
        let _ = fs::remove_dir_all(&root);
        let catalog = Catalog::new(&root);
        catalog.set_now(|| {
            DateTime::parse_from_rfc3339("2026-01-02T03:04:05Z")
                .unwrap()
                .into()
        });
        let mut opened = study("/tmp/scaled.bmp", "scaled.bmp");
        opened.measurement_scale = Some(MeasurementScale {
            row_spacing_mm: 0.2,
            column_spacing_mm: 0.3,
            source: "PixelSpacing".to_string(),
        });

        catalog.record_opened_study(&opened).unwrap();
        let payload: serde_json::Value =
            serde_json::from_slice(&fs::read(catalog.path()).unwrap()).unwrap();

        assert_eq!(
            payload["recentStudies"][0]["measurementScale"],
            serde_json::json!({
                "rowSpacingMm": 0.2,
                "columnSpacingMm": 0.3,
                "source": "PixelSpacing"
            })
        );
        assert_eq!(
            payload["recentStudies"][0]["lastOpenedAt"],
            "2026-01-02T03:04:05Z"
        );
        let _ = fs::remove_dir_all(root);
    }
}
