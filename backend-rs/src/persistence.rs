// Recent-studies catalog. The single file (`catalog.json`) is the source of
// truth for the "Recent" panel in the UI. It's small (≤10 entries), pretty-
// printed JSON, hand-editable if you really need to.
//
// Two key behaviors worth knowing about:
//   1. Corrupt-file recovery: if catalog.json is unparseable, we rename it
//      to catalog.corrupt.json and start over with an empty list. The next
//      record_opened_study call succeeds, and the user keeps working.
//   2. Lock ordering: every public method takes operation_lock for the
//      entire op (load + mutate + save). state_lock is short-held only for
//      the cache copy. Don't invert this — it's serialized on purpose so
//      concurrent open_study calls can't interleave bad state.

use std::{
    fs, io,
    path::{Path, PathBuf},
};

use chrono::{DateTime, SecondsFormat, Utc};
use parking_lot::Mutex;
use serde::{Deserialize, Serialize};

use crate::contracts::{BackendError, BackendErrorCode, MeasurementScale, StudyRecord};

// Hard cap on the recent list. UI shows them in a fixed-height panel so
// growing this would require frontend work too.
pub const RECENT_STUDY_LIMIT: usize = 10;

// One row of the recent list. Lenient on the wire — every field has
// #[serde(default)] so older catalog.json files (missing newer fields)
// still parse. This is the "lenient unmarshal" behavior the tests pin down.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RecentStudyEntry {
    #[serde(default)]
    pub input_path: String,
    #[serde(default)]
    pub input_name: String,
    #[serde(default)]
    pub measurement_scale: Option<MeasurementScale>,
    // RFC 3339 string, second-precision. Not parsed back out — it's display-only.
    #[serde(default)]
    pub last_opened_at: String,
}

// Top-level on-disk shape. Note we don't `deny_unknown_fields` here on
// purpose — we want forward-compat with future catalog versions.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct StudyCatalog {
    #[serde(default)]
    pub recent_studies: Vec<RecentStudyEntry>,
}

// The runtime catalog handle. Holds the path it persists to, two mutexes
// (operation + state), and a `now` clock that tests can override.
pub struct Catalog {
    root_dir: PathBuf,
    path: PathBuf,
    // Serializes whole-operation critical sections (load → modify → save).
    // Coarse but fine here — catalog ops are rare and small.
    operation_lock: Mutex<()>,
    // Holds the in-memory snapshot + a flag saying whether we've ever loaded.
    // Locked briefly to read/write the cache between disk ops.
    state: Mutex<CatalogState>,
    // Injectable clock — tests override this with set_now to get
    // deterministic timestamps. Production uses Utc::now.
    now: Mutex<Box<dyn Fn() -> DateTime<Utc> + Send + Sync>>,
}

// In-memory cache so repeated reads after a successful load don't hit disk.
// `loaded=false` triggers a re-read on next access.
#[derive(Debug, Clone)]
struct CatalogState {
    loaded: bool,
    cache: StudyCatalog,
}

impl Catalog {
    // catalog.json is placed inside the persistence dir.
    pub fn new(root_dir: impl Into<PathBuf>) -> Self {
        let root_dir = root_dir.into();
        let path = root_dir.join("catalog.json");
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

    // Clock override only available under #[cfg(test)] — keep production
    // builds from accidentally injecting a frozen clock.
    #[cfg(test)]
    pub fn set_now<F>(&self, now: F)
    where
        F: Fn() -> DateTime<Utc> + Send + Sync + 'static,
    {
        *self.now.lock() = Box::new(now);
    }

    // Create the directory on disk if missing. Idempotent — safe to call
    // every operation. We do call it lazily, only from save(), to avoid
    // creating empty dirs on read-only access.
    pub fn ensure(&self) -> Result<(), BackendError> {
        fs::create_dir_all(&self.root_dir).map_err(|error| {
            BackendError::internal(format!(
                "failed to create catalog directory {}: {error}",
                self.root_dir.display()
            ))
        })
    }

    // Forced reload from disk. Unlike load_or_default this doesn't fall back
    // to empty on corrupt input — the caller (probably the UI) wants to see
    // the error so it can show a "recover?" prompt.
    //
    // On error we also wipe the cache so subsequent operations re-read from
    // disk rather than serving a stale value.
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

        // Update cache only after a successful read so we don't accidentally
        // promote a partial result.
        let mut state = self.state.lock();
        state.loaded = true;
        state.cache = value.clone();
        Ok(value)
    }

    // Record a study open: move-to-front, dedupe by input_path, truncate to
    // RECENT_STUDY_LIMIT, persist. The dedupe + insert-at-0 + truncate idiom
    // gives us LRU semantics without a dedicated data structure.
    pub fn record_opened_study(&self, study: &StudyRecord) -> Result<(), BackendError> {
        let _operation_guard = self.operation_lock.lock();
        // load_or_default not load — if the file is corrupt, we start fresh
        // (the corrupt file has already been moved aside by load_from_disk).
        let mut value = self.load_or_default()?;
        // Dedupe: remove any pre-existing entry for the same path.
        value
            .recent_studies
            .retain(|entry| entry.input_path != study.input_path);

        // Insert at the front (most recent first).
        value.recent_studies.insert(
            0,
            RecentStudyEntry {
                input_path: study.input_path.clone(),
                input_name: study.input_name.clone(),
                measurement_scale: study.measurement_scale.clone(),
                // RFC 3339 with second precision and trailing 'Z'. Stable
                // enough for the tests to byte-compare.
                last_opened_at: self.now.lock()().to_rfc3339_opts(SecondsFormat::Secs, true),
            },
        );
        // truncate is cheap when len() is already ≤ limit.
        value.recent_studies.truncate(RECENT_STUDY_LIMIT);

        self.save(value)
    }

    // Raw disk read + parse. Three outcomes:
    //   * File missing → empty catalog (first run is the obvious case).
    //   * I/O error → internal error, surfaced as-is.
    //   * Parse error → CacheCorrupted, AND the bad file is renamed to
    //     catalog.corrupt.json so the user can still inspect it. The
    //     `let _ =` swallows the rename error on purpose — if we can't
    //     even rename, we still want to report the parse failure.
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
            // Side-step the corrupt file so the next save() doesn't overwrite
            // it. We deliberately don't propagate the rename error — losing
            // the corruption forensics is less bad than losing the user's
            // ability to record new studies.
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

    // Cache-aware load used inside record_opened_study. The double-check
    // pattern: peek at state first (cheap), only hit disk if we've never
    // loaded. Corrupt-file errors map silently to empty here because the
    // caller is about to overwrite the file anyway.
    fn load_or_default(&self) -> Result<StudyCatalog, BackendError> {
        // Scoped block so the state lock drops before the disk read below.
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
            // Corrupt is recoverable from this caller's perspective — the
            // file's been moved aside, and we're about to write a fresh one.
            Err(error) if error.code == BackendErrorCode::CacheCorrupted => {
                Ok(empty_study_catalog())
            }
            Err(error) => Err(error),
        }
    }

    // Atomic-ish write: serialize, then fs::write (which is single-syscall
    // on most platforms). No fsync — catalog.json is a convenience cache,
    // not durable state.
    fn save(&self, mut value: StudyCatalog) -> Result<(), BackendError> {
        self.ensure()?;
        // Drop reserved capacity so the JSON we emit doesn't blow up if the
        // Vec was pre-grown — purely cosmetic, doesn't affect correctness.
        value.recent_studies.shrink_to_fit();

        // Pretty-printed for human readability (the file is small enough
        // that the size cost doesn't matter).
        let payload = serde_json::to_vec_pretty(&value)
            .map_err(|error| BackendError::internal(format!("serialize study catalog: {error}")))?;
        fs::write(&self.path, payload).map_err(|error| {
            BackendError::internal(format!(
                "failed to write study catalog {}: {error}",
                self.path.display()
            ))
        })?;

        // Update cache to match disk so the next load_or_default hits memory.
        let mut state = self.state.lock();
        state.loaded = true;
        state.cache = value;
        Ok(())
    }

    // Compute the sidecar name for a corrupt file:
    //   /foo/catalog.json → /foo/catalog.corrupt.json
    //   /foo/catalog (no extension) → /foo/catalog.corrupt
    // Done with OsString manipulation so we don't lose non-UTF-8 path bytes
    // (Windows paths can be weird).
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

// Shorthand for the "empty list" catalog. Pulled out so we don't end up with
// subtly-different empty defaults scattered across the file.
fn empty_study_catalog() -> StudyCatalog {
    StudyCatalog {
        recent_studies: Vec::new(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // Test helper — every test needs a StudyRecord and only ever varies the
    // path and name. Default study_id="study-1" is fine because the catalog
    // dedupes by input_path, not study_id.
    fn study(input_path: impl Into<String>, input_name: impl Into<String>) -> StudyRecord {
        StudyRecord {
            study_id: "study-1".to_string(),
            input_path: input_path.into(),
            input_name: input_name.into(),
            measurement_scale: None,
        }
    }

    // Insert-at-front semantics: second study should land first in the list.
    // Each test uses a unique tempdir keyed on PID so parallel cargo-test
    // runs don't collide.
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

    // First-run path: directory doesn't even exist yet, load should succeed
    // with an empty list rather than erroring.
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

    // Corrupt JSON → CacheCorrupted error AND the bad file gets renamed to
    // catalog.corrupt.json (visible side-effect we assert on).
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

    // Forward-compat: a future catalog with extra fields and partial entries
    // should still parse cleanly. This is the test that enforces the
    // "no deny_unknown_fields here" policy on StudyCatalog/RecentStudyEntry.
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

    // End-to-end recovery: starting from a corrupt file, calling
    // record_opened_study should succeed (because load_or_default swallows
    // CacheCorrupted) AND leave a .corrupt.json sidecar.
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

    // Re-opening "one.bmp" after "two.bmp" should bump it back to the front,
    // not append it as a duplicate. This is the dedupe-by-input_path behavior.
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

    // RECENT_STUDY_LIMIT enforcement: insert 12, expect 10 remaining with
    // the most recent ones kept. study-11 is newest (last inserted), study-02
    // is oldest survivor (study-00 and study-01 got dropped).
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

    // Byte-level check of the on-disk JSON shape — pins both the camelCase
    // field names and the second-precision RFC 3339 timestamp format. If
    // anyone switches to ISO nanos or `Z` → `+00:00`, this fires.
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
