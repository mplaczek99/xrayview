use std::{
    collections::{HashMap, VecDeque},
    fs, io,
    path::{Path, PathBuf},
    sync::Arc,
    time::{Duration, Instant, UNIX_EPOCH},
};

use crate::bmp::RenderedPreview;
use crate::contracts::BackendError;
use parking_lot::{Condvar, Mutex};

pub const DEFAULT_ROOT_DIR_NAME: &str = "xrayview";
pub const CACHE_DIR_NAME: &str = "cache";
pub const ARTIFACT_DIR_NAME: &str = "artifacts";
pub const STATE_DIR_NAME: &str = "state";
pub const EVICT_DEBOUNCE_INTERVAL: Duration = Duration::from_secs(30);
pub const DEFAULT_SOURCE_PREVIEW_CACHE_CAPACITY: usize = 4;
pub const DEFAULT_SOURCE_PREVIEW_CACHE_MAX_BYTES: usize = 512 * 1024 * 1024;

pub struct Store {
    root_dir: PathBuf,
    persistence_dir: PathBuf,
    evict_state: Mutex<EvictState>,
}

#[derive(Debug, Clone)]
struct EvictState {
    evicting: bool,
    last_eviction: Option<Instant>,
    tracked_bytes: Option<u64>,
}

#[derive(Debug, Clone)]
struct ArtifactFileInfo {
    path: PathBuf,
    size: u64,
    mod_time_nanos: u128,
}

impl Store {
    pub fn new(root_dir: impl Into<PathBuf>) -> Self {
        let root_dir = root_dir.into();
        let persistence_dir = root_dir
            .parent()
            .unwrap_or_else(|| Path::new("."))
            .join(STATE_DIR_NAME);
        Self::new_with_paths(root_dir, persistence_dir)
    }

    pub fn new_with_root(root_dir: impl Into<PathBuf>) -> Self {
        let root_dir = root_dir.into();
        Self::new_with_paths(root_dir.join(CACHE_DIR_NAME), root_dir.join(STATE_DIR_NAME))
    }

    pub fn new_with_paths(
        cache_dir: impl Into<PathBuf>,
        persistence_dir: impl Into<PathBuf>,
    ) -> Self {
        Self {
            root_dir: cache_dir.into(),
            persistence_dir: persistence_dir.into(),
            evict_state: Mutex::new(EvictState {
                evicting: false,
                last_eviction: None,
                tracked_bytes: None,
            }),
        }
    }

    #[must_use]
    pub fn root_dir(&self) -> &Path {
        &self.root_dir
    }

    #[must_use]
    pub fn persistence_dir(&self) -> &Path {
        &self.persistence_dir
    }

    pub fn ensure(&self) -> Result<(), BackendError> {
        fs::create_dir_all(&self.root_dir).map_err(|error| {
            BackendError::internal(format!(
                "failed to create cache directory {}: {error}",
                self.root_dir.display()
            ))
        })?;
        fs::create_dir_all(&self.persistence_dir).map_err(|error| {
            BackendError::internal(format!(
                "failed to create state directory {}: {error}",
                self.persistence_dir.display()
            ))
        })
    }

    pub fn artifact_path(
        &self,
        namespace: &str,
        key: &str,
        extension: &str,
    ) -> Result<PathBuf, BackendError> {
        let directory = self.root_dir.join(ARTIFACT_DIR_NAME).join(namespace);
        fs::create_dir_all(&directory).map_err(|error| {
            BackendError::internal(format!(
                "failed to create cache directory {}: {error}",
                directory.display()
            ))
        })?;
        Ok(directory.join(format!("{key}.{extension}")))
    }

    pub fn add_artifact_bytes(&self, delta: u64) {
        if delta == 0 {
            return;
        }
        let mut state = self.evict_state.lock();
        if let Some(tracked_bytes) = state.tracked_bytes.as_mut() {
            *tracked_bytes = tracked_bytes.saturating_add(delta);
        }
    }

    pub fn evict_artifacts_over_limit(&self, max_total_bytes: u64) -> Result<usize, BackendError> {
        {
            let mut state = self.evict_state.lock();
            if state
                .tracked_bytes
                .is_some_and(|tracked_bytes| tracked_bytes <= max_total_bytes)
            {
                return Ok(0);
            }
            if state
                .last_eviction
                .is_some_and(|last| last.elapsed() < EVICT_DEBOUNCE_INTERVAL)
            {
                return Ok(0);
            }
            if state.evicting {
                return Ok(0);
            }
            state.evicting = true;
        }

        let result = self.walk_and_evict(max_total_bytes);
        let mut state = self.evict_state.lock();
        state.evicting = false;
        state.last_eviction = Some(Instant::now());
        state.tracked_bytes = result.as_ref().ok().map(|(_, bytes)| *bytes);
        result.map(|(removed, _)| removed)
    }

    fn walk_and_evict(&self, max_total_bytes: u64) -> Result<(usize, u64), BackendError> {
        let artifact_dir = self.root_dir.join(ARTIFACT_DIR_NAME);
        if fs::metadata(&artifact_dir)
            .map(|metadata| !metadata.is_dir())
            .unwrap_or(true)
        {
            return Ok((0, 0));
        }

        let mut files = Vec::new();
        collect_artifact_files(&artifact_dir, &mut files).map_err(|error| {
            BackendError::internal(format!(
                "walk artifacts directory {}: {error}",
                artifact_dir.display()
            ))
        })?;
        let mut total_size = files.iter().map(|file| file.size).sum::<u64>();
        if total_size <= max_total_bytes {
            return Ok((0, total_size));
        }

        files.sort_by_key(|file| file.mod_time_nanos);
        let mut removed = 0;
        for file in files {
            if total_size <= max_total_bytes {
                break;
            }
            if fs::remove_file(&file.path).is_ok() {
                total_size = total_size.saturating_sub(file.size);
                removed += 1;
            }
        }

        Ok((removed, total_size))
    }

    #[cfg(test)]
    fn force_evict_state(&self, tracked_bytes: Option<u64>, last_eviction: Option<Instant>) {
        let mut state = self.evict_state.lock();
        state.tracked_bytes = tracked_bytes;
        state.last_eviction = last_eviction;
        state.evicting = false;
    }

    #[cfg(test)]
    fn tracked_bytes(&self) -> Option<u64> {
        self.evict_state.lock().tracked_bytes
    }
}

fn collect_artifact_files(root: &Path, files: &mut Vec<ArtifactFileInfo>) -> io::Result<()> {
    for entry in fs::read_dir(root)? {
        let entry = match entry {
            Ok(entry) => entry,
            Err(_) => continue,
        };
        let metadata = match entry.metadata() {
            Ok(metadata) => metadata,
            Err(_) => continue,
        };
        if metadata.is_dir() {
            let _ = collect_artifact_files(&entry.path(), files);
            continue;
        }

        files.push(ArtifactFileInfo {
            path: entry.path(),
            size: metadata.len(),
            mod_time_nanos: metadata
                .modified()
                .ok()
                .and_then(|time| time.duration_since(UNIX_EPOCH).ok())
                .map(|duration| duration.as_nanos())
                .unwrap_or_default(),
        });
    }
    Ok(())
}

pub struct SourcePreviewCache {
    state: Mutex<SourcePreviewCacheState>,
}

struct SourcePreviewCacheState {
    capacity: usize,
    max_bytes: usize,
    total_bytes: usize,
    entries: HashMap<String, SourcePreviewEntry>,
    inflight: HashMap<String, Arc<SourcePreviewInflight>>,
    lru: VecDeque<String>,
    #[cfg(test)]
    hits: usize,
    #[cfg(test)]
    misses: usize,
    #[cfg(test)]
    inflight_waits: usize,
}

#[derive(Debug, Clone)]
struct SourcePreviewEntry {
    preview: RenderedPreview,
    byte_size: usize,
}

struct SourcePreviewInflight {
    result: Mutex<Option<Result<RenderedPreview, BackendError>>>,
    ready: Condvar,
}

#[cfg(test)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct SourcePreviewCacheStats {
    pub len: usize,
    pub total_bytes: usize,
    pub hits: usize,
    pub misses: usize,
    pub inflight_waits: usize,
}

impl SourcePreviewCache {
    pub fn new(capacity: usize, max_bytes: usize) -> Self {
        Self {
            state: Mutex::new(SourcePreviewCacheState {
                capacity: capacity.max(1),
                max_bytes: max_bytes.max(1),
                total_bytes: 0,
                entries: HashMap::new(),
                inflight: HashMap::new(),
                lru: VecDeque::new(),
                #[cfg(test)]
                hits: 0,
                #[cfg(test)]
                misses: 0,
                #[cfg(test)]
                inflight_waits: 0,
            }),
        }
    }

    pub fn default_session_cache() -> Self {
        Self::new(
            DEFAULT_SOURCE_PREVIEW_CACHE_CAPACITY,
            DEFAULT_SOURCE_PREVIEW_CACHE_MAX_BYTES,
        )
    }

    #[must_use]
    pub fn get(&self, key: &str) -> Option<RenderedPreview> {
        self.state.lock().get(key)
    }

    pub fn insert(&self, key: String, preview: RenderedPreview) {
        self.state.lock().insert(key, preview);
    }

    pub fn get_or_try_insert_with(
        &self,
        key: String,
        load: impl FnOnce() -> Result<RenderedPreview, BackendError>,
    ) -> Result<RenderedPreview, BackendError> {
        let inflight = {
            let mut state = self.state.lock();
            if let Some(preview) = state.get(&key) {
                return Ok(preview);
            }
            if let Some(inflight) = state.inflight.get(&key).cloned() {
                #[cfg(test)]
                {
                    state.inflight_waits += 1;
                }
                SourcePreviewLoad::Wait(inflight)
            } else {
                let inflight = Arc::new(SourcePreviewInflight::new());
                state.inflight.insert(key.clone(), Arc::clone(&inflight));
                SourcePreviewLoad::Decode(inflight)
            }
        };

        match inflight {
            SourcePreviewLoad::Wait(inflight) => inflight.wait(),
            SourcePreviewLoad::Decode(inflight) => {
                let result = load();
                {
                    let mut state = self.state.lock();
                    if let Ok(preview) = &result {
                        state.insert(key.clone(), preview.clone());
                    }
                    state.inflight.remove(&key);
                }
                inflight.complete(result.clone());
                result
            }
        }
    }

    #[cfg(test)]
    pub fn stats(&self) -> SourcePreviewCacheStats {
        let state = self.state.lock();
        SourcePreviewCacheStats {
            len: state.entries.len(),
            total_bytes: state.total_bytes,
            hits: state.hits,
            misses: state.misses,
            inflight_waits: state.inflight_waits,
        }
    }
}

enum SourcePreviewLoad {
    Wait(Arc<SourcePreviewInflight>),
    Decode(Arc<SourcePreviewInflight>),
}

impl SourcePreviewInflight {
    fn new() -> Self {
        Self {
            result: Mutex::new(None),
            ready: Condvar::new(),
        }
    }

    fn wait(&self) -> Result<RenderedPreview, BackendError> {
        let mut result = self.result.lock();
        loop {
            if let Some(result) = result.clone() {
                return result;
            }
            self.ready.wait(&mut result);
        }
    }

    fn complete(&self, result: Result<RenderedPreview, BackendError>) {
        *self.result.lock() = Some(result);
        self.ready.notify_all();
    }
}

impl SourcePreviewCacheState {
    fn get(&mut self, key: &str) -> Option<RenderedPreview> {
        let Some(preview) = self.entries.get(key).map(|entry| entry.preview.clone()) else {
            #[cfg(test)]
            {
                self.misses += 1;
            }
            return None;
        };
        self.touch(key);
        #[cfg(test)]
        {
            self.hits += 1;
        }
        Some(preview)
    }

    fn insert(&mut self, key: String, preview: RenderedPreview) {
        let byte_size = preview.pixels.len();
        if let Some(existing) = self.entries.remove(&key) {
            self.total_bytes = self.total_bytes.saturating_sub(existing.byte_size);
            self.lru.retain(|candidate| candidate != &key);
        }

        self.total_bytes = self.total_bytes.saturating_add(byte_size);
        self.entries
            .insert(key.clone(), SourcePreviewEntry { preview, byte_size });
        self.lru.push_front(key);
        self.evict_over_limits();
    }

    fn touch(&mut self, key: &str) {
        self.lru.retain(|candidate| candidate != key);
        self.lru.push_front(key.to_string());
    }

    fn evict_over_limits(&mut self) {
        while self.entries.len() > self.capacity || self.total_bytes > self.max_bytes {
            let Some(victim) = self.lru.pop_back() else {
                break;
            };
            if let Some(entry) = self.entries.remove(&victim) {
                self.total_bytes = self.total_bytes.saturating_sub(entry.byte_size);
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{
        sync::{
            Arc,
            atomic::{AtomicUsize, Ordering},
            mpsc,
        },
        thread,
    };

    #[test]
    fn new_with_root_builds_stable_artifact_and_state_paths() {
        let root =
            std::env::temp_dir().join(format!("xrayview-rs-cache-root-{}", std::process::id()));
        let _ = fs::remove_dir_all(&root);
        let store = Store::new_with_root(&root);

        assert_eq!(store.root_dir(), root.join("cache"));
        assert_eq!(store.persistence_dir(), root.join("state"));
        let render_path = store
            .artifact_path("render", "fingerprint-1", "bmp")
            .unwrap();

        assert_eq!(
            render_path,
            root.join("cache")
                .join("artifacts")
                .join("render")
                .join("fingerprint-1.bmp")
        );
        assert!(
            fs::metadata(root.join("cache").join("artifacts").join("render"))
                .unwrap()
                .is_dir()
        );
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn new_uses_sibling_state_directory_for_explicit_cache_root() {
        let root =
            std::env::temp_dir().join(format!("xrayview-rs-cache-explicit-{}", std::process::id()));
        let cache_root = root.join("cache");
        let store = Store::new(&cache_root);

        assert_eq!(store.root_dir(), cache_root);
        assert_eq!(store.persistence_dir(), root.join("state"));
    }

    #[test]
    fn ensure_creates_cache_and_state_directories() {
        let root =
            std::env::temp_dir().join(format!("xrayview-rs-cache-ensure-{}", std::process::id()));
        let _ = fs::remove_dir_all(&root);
        let store = Store::new_with_root(&root);

        store.ensure().unwrap();

        assert!(fs::metadata(store.root_dir()).unwrap().is_dir());
        assert!(fs::metadata(store.persistence_dir()).unwrap().is_dir());
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn evict_artifacts_over_limit_removes_oldest_files() {
        let root =
            std::env::temp_dir().join(format!("xrayview-rs-cache-evict-{}", std::process::id()));
        let _ = fs::remove_dir_all(&root);
        let store = Store::new_with_root(&root);
        let mut paths = Vec::new();
        for name in ["a", "b", "c"] {
            let path = store.artifact_path("render", name, "bmp").unwrap();
            fs::write(&path, vec![0_u8; 600]).unwrap();
            paths.push(path);
            thread::sleep(Duration::from_millis(2));
        }

        let removed = store.evict_artifacts_over_limit(1000).unwrap();

        assert_eq!(removed, 2);
        assert!(!paths[0].exists());
        assert!(!paths[1].exists());
        assert!(paths[2].is_file());
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn evict_artifacts_over_limit_noops_when_under_budget_or_missing() {
        let root = std::env::temp_dir().join(format!(
            "xrayview-rs-cache-under-budget-{}",
            std::process::id()
        ));
        let _ = fs::remove_dir_all(&root);
        let store = Store::new_with_root(&root);
        assert_eq!(store.evict_artifacts_over_limit(100).unwrap(), 0);

        let path = store.artifact_path("render", "small", "bmp").unwrap();
        fs::write(&path, vec![0_u8; 100]).unwrap();

        assert_eq!(store.evict_artifacts_over_limit(1000).unwrap(), 0);
        assert!(path.is_file());
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn evict_artifacts_skips_walk_when_tracked_bytes_are_under_limit() {
        let root = std::env::temp_dir().join(format!(
            "xrayview-rs-cache-tracked-under-{}",
            std::process::id()
        ));
        let _ = fs::remove_dir_all(&root);
        let store = Store::new_with_root(&root);
        store.force_evict_state(Some(500), Some(Instant::now()));
        let path = store.artifact_path("render", "big", "bmp").unwrap();
        fs::write(&path, vec![0_u8; 2_000]).unwrap();

        assert_eq!(store.evict_artifacts_over_limit(1000).unwrap(), 0);
        assert!(path.is_file());
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn add_artifact_bytes_accumulates_only_when_total_is_known() {
        let root = std::env::temp_dir().join(format!(
            "xrayview-rs-cache-add-bytes-{}",
            std::process::id()
        ));
        let store = Store::new_with_root(root);

        store.add_artifact_bytes(1000);
        assert_eq!(store.tracked_bytes(), None);

        store.force_evict_state(Some(5000), Some(Instant::now()));
        store.add_artifact_bytes(1500);
        store.add_artifact_bytes(0);

        assert_eq!(store.tracked_bytes(), Some(6500));
    }

    #[test]
    fn artifact_path_wraps_directory_creation_errors() {
        let root =
            std::env::temp_dir().join(format!("xrayview-rs-cache-blocked-{}", std::process::id()));
        let _ = fs::remove_dir_all(&root);
        fs::write(&root, b"not-a-directory").unwrap();
        let store = Store::new_with_paths(root.join("cache"), root.join("state"));

        let error = store
            .artifact_path("render", "fingerprint-1", "bmp")
            .unwrap_err();

        assert_eq!(error.code, crate::contracts::BackendErrorCode::Internal);
        assert!(error.message.contains("failed to create cache directory"));
        let _ = fs::remove_file(root);
    }

    #[test]
    fn source_preview_cache_returns_clones_and_tracks_hits() {
        let cache = SourcePreviewCache::new(2, 1024);
        let preview = RenderedPreview {
            width: 2,
            height: 2,
            pixels: vec![1, 2, 3, 4].into(),
            measurement_scale: None,
        };

        assert!(cache.get("study-1").is_none());
        cache.insert("study-1".to_string(), preview.clone());
        let mut cached = cache.get("study-1").unwrap();
        Arc::make_mut(&mut cached.pixels)[0] = 99;
        let cached_again = cache.get("study-1").unwrap();

        assert_eq!(cached_again, preview);
        assert_eq!(
            cache.stats(),
            SourcePreviewCacheStats {
                len: 1,
                total_bytes: 4,
                hits: 2,
                misses: 1,
                inflight_waits: 0,
            }
        );
    }

    #[test]
    fn source_preview_cache_evicts_least_recently_used_by_capacity() {
        let cache = SourcePreviewCache::new(2, 1024);
        let preview = |value| RenderedPreview {
            width: 1,
            height: 1,
            pixels: vec![value].into(),
            measurement_scale: None,
        };
        cache.insert("a".to_string(), preview(1));
        cache.insert("b".to_string(), preview(2));
        assert!(cache.get("a").is_some());
        cache.insert("c".to_string(), preview(3));

        assert!(cache.get("a").is_some());
        assert!(cache.get("b").is_none());
        assert!(cache.get("c").is_some());
    }

    #[test]
    fn source_preview_cache_evicts_by_byte_budget() {
        let cache = SourcePreviewCache::new(10, 5);
        let preview = |bytes: usize| RenderedPreview {
            width: bytes as u32,
            height: 1,
            pixels: vec![7; bytes].into(),
            measurement_scale: None,
        };
        cache.insert("a".to_string(), preview(3));
        cache.insert("b".to_string(), preview(3));

        assert!(cache.get("a").is_none());
        assert!(cache.get("b").is_some());
        assert_eq!(cache.stats().total_bytes, 3);
    }

    #[test]
    fn source_preview_cache_coalesces_concurrent_loads_for_same_key() {
        let cache = Arc::new(SourcePreviewCache::new(4, 1024));
        let load_count = Arc::new(AtomicUsize::new(0));
        let (loader_started_tx, loader_started_rx) = mpsc::channel();
        let (release_loader_tx, release_loader_rx) = mpsc::channel();

        let first_cache = Arc::clone(&cache);
        let first_load_count = Arc::clone(&load_count);
        let first = thread::spawn(move || {
            first_cache
                .get_or_try_insert_with("study-1".to_string(), || {
                    first_load_count.fetch_add(1, Ordering::SeqCst);
                    loader_started_tx.send(()).unwrap();
                    release_loader_rx.recv().unwrap();
                    Ok(RenderedPreview {
                        width: 1,
                        height: 3,
                        pixels: vec![7, 8, 9].into(),
                        measurement_scale: None,
                    })
                })
                .unwrap()
        });

        loader_started_rx.recv().unwrap();

        let second_cache = Arc::clone(&cache);
        let second_load_count = Arc::clone(&load_count);
        let second = thread::spawn(move || {
            second_cache
                .get_or_try_insert_with("study-1".to_string(), || {
                    second_load_count.fetch_add(1, Ordering::SeqCst);
                    Ok(RenderedPreview {
                        width: 1,
                        height: 1,
                        pixels: vec![99].into(),
                        measurement_scale: None,
                    })
                })
                .unwrap()
        });

        let deadline = Instant::now() + Duration::from_secs(1);
        while cache.stats().inflight_waits == 0 {
            assert!(
                Instant::now() < deadline,
                "timed out waiting for second caller to observe inflight decode"
            );
            thread::yield_now();
        }
        assert_eq!(load_count.load(Ordering::SeqCst), 1);
        release_loader_tx.send(()).unwrap();

        let first_preview = first.join().unwrap();
        let second_preview = second.join().unwrap();

        assert_eq!(first_preview, second_preview);
        assert_eq!(load_count.load(Ordering::SeqCst), 1);
        assert_eq!(cache.stats().inflight_waits, 1);
    }
}
