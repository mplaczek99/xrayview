// Two caches live in this file:
//
//   * `Store` — on-disk artifact cache (rendered BMPs, analysis outputs).
//     Lives under cache_dir/artifacts/<namespace>/<key>.<ext>. Eviction is
//     LRU by mtime, debounced to once every 30s so high-throughput job
//     runs don't thrash the directory walk.
//
//   * `SourcePreviewCache` — in-memory decoded source previews keyed by
//     study fingerprint. Bounded by both entry count *and* byte budget;
//     evictions are pure LRU. Has a single-flight `get_or_try_insert_with`
//     so concurrent decodes of the same key only ever run one decoder.

use std::{
    collections::{HashMap, VecDeque},
    fs,
    path::{Path, PathBuf},
    sync::Arc,
    time::{Duration, Instant, UNIX_EPOCH},
};

use crate::bmp::DecodedSourcePreview;
use crate::contracts::BackendError;
use parking_lot::{Condvar, Mutex};
use walkdir::WalkDir;

// Path-name constants. Pub because the desktop shell uses them when computing
// paths to surface in dialogs.
pub const CACHE_DIR_NAME: &str = "cache";
pub const ARTIFACT_DIR_NAME: &str = "artifacts";
pub const STATE_DIR_NAME: &str = "state";

// Eviction debounce — 30s picked to be longer than a typical interactive
// burst (slider drags, repeated process runs) but shorter than a coffee
// break. If the user just clicked through 20 presets, we don't want to walk
// the artifacts dir 20 times.
pub const EVICT_DEBOUNCE_INTERVAL: Duration = Duration::from_secs(30);

// Source preview cache defaults. 4 entries × 512 MB ≈ enough for a few
// large bitewings in flight without ballooning RSS. Tunable per-instance
// via SourcePreviewCache::new.
pub const DEFAULT_SOURCE_PREVIEW_CACHE_CAPACITY: usize = 4;
pub const DEFAULT_SOURCE_PREVIEW_CACHE_MAX_BYTES: usize = 512 * 1024 * 1024;

// Disk-backed artifact cache. State is tiny (just the eviction bookkeeping);
// everything else is derived from the filesystem.
pub struct Store {
    root_dir: PathBuf,
    persistence_dir: PathBuf,
    evict_state: Mutex<EvictState>,
}

// In-memory shadow of "do we need to evict?" so most calls can be answered
// without touching disk.
#[derive(Debug, Clone)]
struct EvictState {
    // True while an eviction is in flight — second concurrent caller bails.
    evicting: bool,
    // When the last eviction finished. Used for the 30s debounce.
    last_eviction: Option<Instant>,
    // Sum of artifact bytes on disk. None means "we haven't measured yet, so
    // we don't know" — add_artifact_bytes only updates if Some(_).
    tracked_bytes: Option<u64>,
}

// Per-file info collected during the artifact walk. mod_time_nanos is used
// as the LRU key (older files get evicted first).
#[derive(Debug, Clone)]
struct ArtifactFileInfo {
    path: PathBuf,
    size: u64,
    mod_time_nanos: u128,
}

impl Store {
    // Given a cache directory, derive its sibling state directory (../state).
    // This is the ctor used when CACHE_DIR env var is set explicitly.
    pub fn new(root_dir: impl Into<PathBuf>) -> Self {
        let root_dir = root_dir.into();
        let persistence_dir = root_dir
            .parent()
            .unwrap_or_else(|| Path::new("."))
            .join(STATE_DIR_NAME);
        Self::new_with_paths(root_dir, persistence_dir)
    }

    // Given the *common parent*, build cache+state subdirs. This is the ctor
    // used when only BASE_DIR is set (the common case).
    pub fn new_with_root(root_dir: impl Into<PathBuf>) -> Self {
        let root_dir = root_dir.into();
        Self::new_with_paths(root_dir.join(CACHE_DIR_NAME), root_dir.join(STATE_DIR_NAME))
    }

    // The explicit-paths ctor — tests reach for this one to put cache and
    // state in unrelated directories.
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
                // tracked_bytes starts as None — first eviction populates it.
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

    // Create both cache and state directories. Idempotent. Called once at
    // App::prepare() time, but also implicitly by artifact_path on the cache
    // side so the directories never go missing mid-session.
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

    // Build the on-disk path for a (namespace, key, extension) triple and
    // make sure the parent dir exists. Doesn't create the file itself —
    // that's the caller's job. Namespace is one of "render", "process",
    // "analyze" etc.; key is usually a fingerprint hash.
    pub fn artifact_path(
        &self,
        namespace: &str,
        key: &str,
        extension: &str,
    ) -> Result<PathBuf, BackendError> {
        validate_artifact_segment("namespace", namespace)?;
        validate_artifact_segment("key", key)?;
        validate_artifact_segment("extension", extension)?;

        let directory = self.root_dir.join(ARTIFACT_DIR_NAME).join(namespace);
        fs::create_dir_all(&directory).map_err(|error| {
            BackendError::internal(format!(
                "failed to create cache directory {}: {error}",
                directory.display()
            ))
        })?;
        Ok(directory.join(format!("{key}.{extension}")))
    }

    // Incremental byte-count update. Called by the job machinery after
    // writing an artifact so we can short-circuit eviction without re-walking
    // the directory.
    //
    // Only updates the count if we've already measured (Some(_)). If
    // tracked_bytes is None we just no-op — first eviction will measure.
    pub fn add_artifact_bytes(&self, delta: u64) {
        if delta == 0 {
            return;
        }
        let mut state = self.evict_state.lock();
        if let Some(tracked_bytes) = state.tracked_bytes.as_mut() {
            *tracked_bytes = tracked_bytes.saturating_add(delta);
        }
    }

    // The main entry point. Returns Ok(0) without doing any work in three
    // cases: we're already under budget, we evicted recently, or another
    // thread is currently evicting. Otherwise we mark `evicting=true`, do
    // the walk + delete, then update bookkeeping under the lock again.
    //
    // The lock is dropped during walk_and_evict because that's the slow
    // part (recursive readdir + remove_file) and we don't want add_artifact_bytes
    // calls to stall behind it.
    pub fn evict_artifacts_over_limit(&self, max_total_bytes: u64) -> Result<usize, BackendError> {
        {
            let mut state = self.evict_state.lock();
            // Fast path: tracked size says we're fine.
            if state
                .tracked_bytes
                .is_some_and(|tracked_bytes| tracked_bytes <= max_total_bytes)
            {
                return Ok(0);
            }
            // Debounce: don't walk again so soon.
            if state
                .last_eviction
                .is_some_and(|last| last.elapsed() < EVICT_DEBOUNCE_INTERVAL)
            {
                return Ok(0);
            }
            // Concurrent eviction — let the existing one win.
            if state.evicting {
                return Ok(0);
            }
            state.evicting = true;
        }

        // Lock is dropped here on purpose — walk_and_evict can take a while.
        let result = self.walk_and_evict(max_total_bytes);
        let mut state = self.evict_state.lock();
        state.evicting = false;
        state.last_eviction = Some(Instant::now());
        // Even on error we update last_eviction so we still honor the debounce.
        // tracked_bytes only gets refreshed on success.
        state.tracked_bytes = result.as_ref().ok().map(|(_, bytes)| *bytes);
        result.map(|(removed, _)| removed)
    }

    // The actual filesystem walk + delete. Returns (removed_count, remaining_bytes).
    // If the artifact dir doesn't exist, that's fine — return (0, 0) without error.
    fn walk_and_evict(&self, max_total_bytes: u64) -> Result<(usize, u64), BackendError> {
        let artifact_dir = self.root_dir.join(ARTIFACT_DIR_NAME);
        // Treat "missing" and "not a directory" the same — nothing to evict.
        if fs::metadata(&artifact_dir)
            .map(|metadata| !metadata.is_dir())
            .unwrap_or(true)
        {
            return Ok((0, 0));
        }

        let mut files = collect_artifact_files(&artifact_dir);
        // Quick check post-walk — we may have measured the dir down under budget.
        let mut total_size = files.iter().map(|file| file.size).sum::<u64>();
        if total_size <= max_total_bytes {
            return Ok((0, total_size));
        }

        // Oldest mtime first → LRU eviction order.
        files.sort_by_key(|file| file.mod_time_nanos);
        let mut removed = 0;
        for file in files {
            if total_size <= max_total_bytes {
                break;
            }
            // Tolerate remove_file failures — file may have been deleted out
            // from under us, or be on a read-only mount.
            if fs::remove_file(&file.path).is_ok() {
                total_size = total_size.saturating_sub(file.size);
                removed += 1;
            }
        }

        Ok((removed, total_size))
    }

    // Test-only override: jam the eviction bookkeeping into a known state.
    // Used to assert that the fast-path checks fire correctly.
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

// Recursive directory walk. Tolerant of per-entry errors — skip the entry
// rather than failing the whole walk, so a single bad file doesn't break
// eviction. UNIX_EPOCH conversion may fail on pre-1970 mtimes (very rare);
// those get sorted to oldest (0), which is the safe default. `follow_links`
// is set false explicitly so a symlinked subtree can't cause a cycle or pull
// in files outside the artifacts dir.
fn collect_artifact_files(root: &Path) -> Vec<ArtifactFileInfo> {
    WalkDir::new(root)
        .follow_links(false)
        .into_iter()
        .filter_map(Result::ok)
        .filter_map(|entry| {
            let metadata = entry.metadata().ok()?;
            if !metadata.is_file() {
                return None;
            }
            Some(ArtifactFileInfo {
                path: entry.into_path(),
                size: metadata.len(),
                mod_time_nanos: metadata
                    .modified()
                    .ok()
                    .and_then(|time| time.duration_since(UNIX_EPOCH).ok())
                    .map(|duration| duration.as_nanos())
                    // unwrap_or_default → 0 → sorts as "oldest possible", evicted first.
                    .unwrap_or_default(),
            })
        })
        .collect()
}

fn validate_artifact_segment(label: &str, value: &str) -> Result<(), BackendError> {
    if value.is_empty()
        || value == "."
        || value == ".."
        || value.contains('/')
        || value.contains('\\')
    {
        return Err(BackendError::invalid_input(format!(
            "artifact {label} must be a safe path segment"
        )));
    }
    Ok(())
}

// In-memory LRU cache for decoded source previews. The whole state lives
// under a single mutex — this is fine because operations are short (HashMap
// + VecDeque manipulation) and contention is low (decoders are rare).
pub struct SourcePreviewCache {
    state: Mutex<SourcePreviewCacheState>,
}

// The protected innards. Note `inflight` runs alongside `entries` — keys can
// only be in one or the other at any moment (either being decoded, or done).
// The single-flight contract in get_or_try_insert_with depends on that.
struct SourcePreviewCacheState {
    capacity: usize,
    max_bytes: usize,
    total_bytes: usize,
    entries: HashMap<String, SourcePreviewEntry>,
    // In-flight decoders so concurrent callers for the same key block on a
    // Condvar instead of racing to decode the same BMP twice.
    inflight: HashMap<String, Arc<SourcePreviewInflight>>,
    // LRU order — front is most recent. push_front on touch, pop_back on evict.
    lru: VecDeque<String>,
    // Telemetry counters — only exist under #[cfg(test)] so production builds
    // don't pay for them.
    #[cfg(test)]
    hits: usize,
    #[cfg(test)]
    misses: usize,
    #[cfg(test)]
    inflight_waits: usize,
}

// We track byte_size separately rather than recomputing pixels.len() on
// every eviction check — keeps total_bytes math O(1).
#[derive(Debug, Clone)]
struct SourcePreviewEntry {
    preview: DecodedSourcePreview,
    byte_size: usize,
}

// Rendezvous primitive for single-flight decode. The first caller fills in
// `result` and signals `ready`; everyone else waits on the condvar.
struct SourcePreviewInflight {
    result: Mutex<Option<Result<DecodedSourcePreview, BackendError>>>,
    ready: Condvar,
}

// Test-only stats snapshot. Used by integration tests to assert hit/miss
// ratios and single-flight coalescing without poking at internals.
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
    // capacity.max(1) and max_bytes.max(1) — zero would be nonsensical and
    // would also break evict_over_limits' termination condition.
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

    // Convenience ctor using the module-level defaults. App::new picks this.
    pub fn default_session_cache() -> Self {
        Self::new(
            DEFAULT_SOURCE_PREVIEW_CACHE_CAPACITY,
            DEFAULT_SOURCE_PREVIEW_CACHE_MAX_BYTES,
        )
    }

    // Simple get/insert. Returned DecodedSourcePreview is a clone (cheap —
    // pixels are Arc<[u8]>), so the caller can hand it around without
    // holding the cache lock.
    #[must_use]
    pub fn get(&self, key: &str) -> Option<DecodedSourcePreview> {
        self.state.lock().get(key)
    }

    pub fn insert(&self, key: String, preview: DecodedSourcePreview) {
        self.state.lock().insert(key, preview);
    }

    // The single-flight entry point. Three cases:
    //   (a) Cache hit → return immediately.
    //   (b) Already decoding → wait on the existing Inflight.
    //   (c) Otherwise → register an Inflight, run the closure, broadcast.
    //
    // We *don't* hold the state mutex while running `load`. Otherwise a slow
    // BMP decode would block every other cache lookup in the system.
    pub fn get_or_try_insert_with(
        &self,
        key: String,
        load: impl FnOnce() -> Result<DecodedSourcePreview, BackendError>,
    ) -> Result<DecodedSourcePreview, BackendError> {
        // Scoped lock — figure out which of the three branches we're in,
        // then release before doing work.
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
                // Claim the key — any concurrent caller now lands in the Wait branch.
                let inflight = Arc::new(SourcePreviewInflight::new());
                state.inflight.insert(key.clone(), Arc::clone(&inflight));
                SourcePreviewLoad::Decode(inflight)
            }
        };

        match inflight {
            SourcePreviewLoad::Wait(inflight) => inflight.wait(),
            SourcePreviewLoad::Decode(inflight) => {
                // Slow path — actually decode.
                let result = load();
                // Re-acquire the lock to publish + clean up inflight.
                {
                    let mut state = self.state.lock();
                    if let Ok(preview) = &result {
                        state.insert(key.clone(), preview.clone());
                    }
                    // Always remove inflight, even on Err — otherwise stuck forever.
                    state.inflight.remove(&key);
                }
                // Broadcast to anyone waiting. Note we share the Err too —
                // identical decoders would fail identically anyway.
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

// Tiny tagged union returned by the locked branch of get_or_try_insert_with.
// Splitting it out keeps the match arms ergonomic and the lock scope tight.
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

    // Block until the in-flight decoder publishes a result. The `while None`
    // loop guards against spurious wakeups (parking_lot's Condvar can wake
    // without notify on some platforms).
    fn wait(&self) -> Result<DecodedSourcePreview, BackendError> {
        let mut result = self.result.lock();
        loop {
            if let Some(result) = result.clone() {
                return result;
            }
            self.ready.wait(&mut result);
        }
    }

    // Publish and broadcast. notify_all (not notify_one) because there can
    // be many waiters.
    fn complete(&self, result: Result<DecodedSourcePreview, BackendError>) {
        *self.result.lock() = Some(result);
        self.ready.notify_all();
    }
}

impl SourcePreviewCacheState {
    // Internal get — also handles LRU touch + hit/miss counters. Returned
    // Option<DecodedSourcePreview> is owned (cloned out) so callers can
    // release the cache lock.
    fn get(&mut self, key: &str) -> Option<DecodedSourcePreview> {
        let Some(preview) = self.entries.get(key).map(|entry| entry.preview.clone()) else {
            #[cfg(test)]
            {
                self.misses += 1;
            }
            return None;
        };
        // touch *after* the clone — touch mutably borrows self, conflicts
        // with the entries borrow above.
        self.touch(key);
        #[cfg(test)]
        {
            self.hits += 1;
        }
        Some(preview)
    }

    // Insert with re-insertion semantics: if the key already exists, drop
    // the old entry first so byte accounting stays accurate. Then push to
    // the front of the LRU and trim if we're over budget.
    fn insert(&mut self, key: String, preview: DecodedSourcePreview) {
        let byte_size = preview.pixels.len();
        if let Some(existing) = self.entries.remove(&key) {
            self.total_bytes = self.total_bytes.saturating_sub(existing.byte_size);
            // O(n) removal from VecDeque — n is small (capacity defaults to 4).
            self.lru.retain(|candidate| candidate != &key);
        }

        self.total_bytes = self.total_bytes.saturating_add(byte_size);
        self.entries
            .insert(key.clone(), SourcePreviewEntry { preview, byte_size });
        self.lru.push_front(key);
        self.evict_over_limits();
    }

    // Move `key` to the front of the LRU queue. O(n) but n ≤ capacity so
    // this is fine for the default size of 4.
    fn touch(&mut self, key: &str) {
        self.lru.retain(|candidate| candidate != key);
        self.lru.push_front(key.to_string());
    }

    // Drop entries from the back of the LRU until *both* limits are
    // satisfied. The loop check is `||` not `&&` — we need to be under
    // BOTH the capacity AND the byte budget.
    fn evict_over_limits(&mut self) {
        while self.entries.len() > self.capacity || self.total_bytes > self.max_bytes {
            let Some(victim) = self.lru.pop_back() else {
                // No more entries to evict — shouldn't happen if the cache
                // is consistent, but guard against an empty LRU loop.
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
    use tempfile::tempdir;

    // new_with_root variant — builds cache+state under a common parent.
    // Pinning the artifact path layout (artifacts/<namespace>/<key>.<ext>)
    // because external tools sometimes inspect the cache directly.
    #[test]
    fn new_with_root_builds_stable_artifact_and_state_paths() {
        let root = tempdir().unwrap();
        let store = Store::new_with_root(root.path());

        assert_eq!(store.root_dir(), root.path().join("cache"));
        assert_eq!(store.persistence_dir(), root.path().join("state"));
        let render_path = store
            .artifact_path("render", "fingerprint-1", "bmp")
            .unwrap();

        assert_eq!(
            render_path,
            root.path()
                .join("cache")
                .join("artifacts")
                .join("render")
                .join("fingerprint-1.bmp")
        );
        assert!(
            fs::metadata(root.path().join("cache").join("artifacts").join("render"))
                .unwrap()
                .is_dir()
        );
    }

    // The `new` ctor (not new_with_root): explicit cache path, state is
    // its sibling. Used when CACHE_DIR env var is set.
    #[test]
    fn new_uses_sibling_state_directory_for_explicit_cache_root() {
        let root = tempdir().unwrap();
        let cache_root = root.path().join("cache");
        let store = Store::new(&cache_root);

        assert_eq!(store.root_dir(), cache_root);
        assert_eq!(store.persistence_dir(), root.path().join("state"));
    }

    // Smoke test for ensure() — both dirs should be created from a fresh
    // root. App::prepare relies on this being idempotent.
    #[test]
    fn ensure_creates_cache_and_state_directories() {
        let root = tempdir().unwrap();
        let store = Store::new_with_root(root.path());

        store.ensure().unwrap();

        assert!(fs::metadata(store.root_dir()).unwrap().is_dir());
        assert!(fs::metadata(store.persistence_dir()).unwrap().is_dir());
    }

    // Pins LRU-by-mtime: write a, b, c with 2ms gaps; budget 1000 bytes
    // forces eviction of a and b (oldest), keeping c. The thread::sleep
    // is the ugly-but-necessary bit — mtime resolution on some filesystems
    // is millisecond-coarse.
    #[test]
    fn evict_artifacts_over_limit_removes_oldest_files() {
        let root = tempdir().unwrap();
        let store = Store::new_with_root(root.path());
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
    }

    // Two no-op cases in one: (a) artifact dir doesn't exist yet, (b) total
    // size is comfortably under budget. Both must return 0 removed.
    #[test]
    fn evict_artifacts_over_limit_noops_when_under_budget_or_missing() {
        let root = tempdir().unwrap();
        let store = Store::new_with_root(root.path());
        assert_eq!(store.evict_artifacts_over_limit(100).unwrap(), 0);

        let path = store.artifact_path("render", "small", "bmp").unwrap();
        fs::write(&path, vec![0_u8; 100]).unwrap();

        assert_eq!(store.evict_artifacts_over_limit(1000).unwrap(), 0);
        assert!(path.is_file());
    }

    // The fast-path test: tracked_bytes says we're at 500 (under the 1000
    // limit), so even though there's a 2000-byte file on disk, eviction
    // skips the walk. Proves the tracked_bytes shortcut works.
    #[test]
    fn evict_artifacts_skips_walk_when_tracked_bytes_are_under_limit() {
        let root = tempdir().unwrap();
        let store = Store::new_with_root(root.path());
        store.force_evict_state(Some(500), Some(Instant::now()));
        let path = store.artifact_path("render", "big", "bmp").unwrap();
        fs::write(&path, vec![0_u8; 2_000]).unwrap();

        assert_eq!(store.evict_artifacts_over_limit(1000).unwrap(), 0);
        assert!(path.is_file());
    }

    // add_artifact_bytes is a no-op when tracked_bytes is None (we don't
    // know the baseline yet). Once force_evict_state primes it to Some(5000),
    // subsequent adds accumulate. Zero-delta calls also no-op.
    #[test]
    fn add_artifact_bytes_accumulates_only_when_total_is_known() {
        let root = tempdir().unwrap();
        let store = Store::new_with_root(root.path());

        store.add_artifact_bytes(1000);
        assert_eq!(store.tracked_bytes(), None);

        store.force_evict_state(Some(5000), Some(Instant::now()));
        store.add_artifact_bytes(1500);
        store.add_artifact_bytes(0);

        assert_eq!(store.tracked_bytes(), Some(6500));
    }

    // Error path: parent path exists but is a *file*, not a directory.
    // create_dir_all fails, and we want the error mapped to Internal with
    // a message that names what we tried to create.
    #[test]
    fn artifact_path_wraps_directory_creation_errors() {
        let parent = tempdir().unwrap();
        let blocker = parent.path().join("blocker");
        fs::write(&blocker, b"not-a-directory").unwrap();
        let store = Store::new_with_paths(blocker.join("cache"), blocker.join("state"));

        let error = store
            .artifact_path("render", "fingerprint-1", "bmp")
            .unwrap_err();

        assert_eq!(error.code, crate::contracts::BackendErrorCode::Internal);
        assert!(error.message.contains("failed to create cache directory"));
    }

    #[test]
    fn artifact_path_rejects_unsafe_path_segments() {
        let root = tempdir().unwrap();
        let store = Store::new_with_root(root.path());

        for (namespace, key, extension) in [
            ("../render", "fingerprint-1", "bmp"),
            ("render", "../fingerprint-1", "bmp"),
            ("render", "fingerprint-1", "../bmp"),
            ("render", "fingerprint/1", "bmp"),
            ("render", "fingerprint\\1", "bmp"),
            ("render", ".", "bmp"),
            ("render", "", "bmp"),
        ] {
            let error = store.artifact_path(namespace, key, extension).unwrap_err();
            assert_eq!(error.code, crate::contracts::BackendErrorCode::InvalidInput);
            assert!(error.message.contains("safe path segment"));
        }
    }

    // Clone semantics + counter accounting. Mutating the returned clone
    // must NOT affect the cached entry — verifies our Arc::make_mut path
    // really does CoW on the way out.
    #[test]
    fn source_preview_cache_returns_clones_and_tracks_hits() {
        let cache = SourcePreviewCache::new(2, 1024);
        let preview = DecodedSourcePreview {
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

    // Capacity-driven eviction: capacity=2, insert a,b, touch a (so b is
    // now LRU), insert c → b evicted, a and c survive. The get("a") in the
    // middle is the touch.
    #[test]
    fn source_preview_cache_evicts_least_recently_used_by_capacity() {
        let cache = SourcePreviewCache::new(2, 1024);
        let preview = |value| DecodedSourcePreview {
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

    // Byte-budget eviction: max_bytes=5, two 3-byte entries → only the
    // second fits, the first gets evicted even though capacity (10) allows
    // both. Pins the `||` (not `&&`) condition in evict_over_limits.
    #[test]
    fn source_preview_cache_evicts_by_byte_budget() {
        let cache = SourcePreviewCache::new(10, 5);
        let preview = |bytes: usize| DecodedSourcePreview {
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

    // Single-flight: two threads call get_or_try_insert_with for the same
    // key. Thread 1 holds the loader closure open via release_loader_rx.
    // Thread 2 should land in the Wait branch (inflight_waits bumps to 1).
    // When thread 1 finally returns, both threads see the same preview AND
    // load_count is still 1 — the second loader closure never ran.
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
                    Ok(DecodedSourcePreview {
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
                    Ok(DecodedSourcePreview {
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
