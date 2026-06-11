package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"xrayview/backend/internal/bmp"
	"xrayview/backend/internal/contracts"
)

const (
	DefaultRootDirName = "xrayview"
	CacheDirName       = "cache"
	ArtifactDirName    = "artifacts"
	StateDirName       = "state"

	DefaultSourcePreviewCacheCapacity = 4
	DefaultSourcePreviewCacheMaxBytes = 512 * 1024 * 1024
)

var EvictDebounceInterval = 30 * time.Second

// Store manages the disk-backed artifact cache. Artifacts live under
// cache/artifacts/<namespace>/<key>.<extension>, matching the Rust layout.
type Store struct {
	rootDir        string
	persistenceDir string

	evictMu sync.Mutex
	evict   evictState
}

type evictState struct {
	evicting     bool
	lastEviction time.Time
	trackedBytes *uint64
}

type artifactFileInfo struct {
	path         string
	size         uint64
	modTimeNanos int64
}

// NewStore derives the sibling state directory from an explicit cache path.
func NewStore(rootDir string) *Store {
	parent := filepath.Dir(rootDir)
	if parent == "." && filepath.Base(rootDir) == rootDir {
		parent = "."
	}
	return NewStoreWithPaths(rootDir, filepath.Join(parent, StateDirName))
}

// NewStoreWithRoot builds cache and state directories under a common parent.
func NewStoreWithRoot(rootDir string) *Store {
	return NewStoreWithPaths(filepath.Join(rootDir, CacheDirName), filepath.Join(rootDir, StateDirName))
}

// NewStoreWithPaths accepts explicit cache and state locations.
func NewStoreWithPaths(cacheDir, persistenceDir string) *Store {
	return &Store{rootDir: cacheDir, persistenceDir: persistenceDir}
}

func (store *Store) RootDir() string {
	return store.rootDir
}

func (store *Store) PersistenceDir() string {
	return store.persistenceDir
}

// Ensure creates cache and state directories. It is safe to call repeatedly.
func (store *Store) Ensure() error {
	if err := os.MkdirAll(store.rootDir, 0o777); err != nil {
		backendErr := contracts.InternalError(fmt.Sprintf("failed to create cache directory %s: %v", store.rootDir, err))
		return backendErr
	}
	if err := os.MkdirAll(store.persistenceDir, 0o777); err != nil {
		backendErr := contracts.InternalError(fmt.Sprintf("failed to create state directory %s: %v", store.persistenceDir, err))
		return backendErr
	}
	return nil
}

// ArtifactPath validates path segments, creates the namespace directory, and
// returns the final artifact filename.
func (store *Store) ArtifactPath(namespace, key, extension string) (string, error) {
	if err := validateArtifactSegment("namespace", namespace); err != nil {
		return "", err
	}
	if err := validateArtifactSegment("key", key); err != nil {
		return "", err
	}
	if err := validateArtifactSegment("extension", extension); err != nil {
		return "", err
	}

	directory := filepath.Join(store.rootDir, ArtifactDirName, namespace)
	if err := os.MkdirAll(directory, 0o777); err != nil {
		backendErr := contracts.InternalError(fmt.Sprintf("failed to create cache directory %s: %v", directory, err))
		return "", backendErr
	}
	return filepath.Join(directory, key+"."+extension), nil
}

// AddArtifactBytes updates the incremental size tracker after a write, but
// only after a full eviction pass has established the baseline.
func (store *Store) AddArtifactBytes(delta uint64) {
	if delta == 0 {
		return
	}
	store.evictMu.Lock()
	defer store.evictMu.Unlock()
	if store.evict.trackedBytes != nil {
		next := *store.evict.trackedBytes + delta
		if next < *store.evict.trackedBytes {
			next = ^uint64(0)
		}
		*store.evict.trackedBytes = next
	}
}

// EvictArtifactsOverLimit removes oldest artifact files until the total size
// is within budget. Fast paths skip directory walks when cached state proves
// eviction is unnecessary or recently attempted.
func (store *Store) EvictArtifactsOverLimit(maxTotalBytes uint64) (int, error) {
	store.evictMu.Lock()
	if store.evict.trackedBytes != nil && *store.evict.trackedBytes <= maxTotalBytes {
		store.evictMu.Unlock()
		return 0, nil
	}
	if !store.evict.lastEviction.IsZero() && time.Since(store.evict.lastEviction) < EvictDebounceInterval {
		store.evictMu.Unlock()
		return 0, nil
	}
	if store.evict.evicting {
		store.evictMu.Unlock()
		return 0, nil
	}
	store.evict.evicting = true
	store.evictMu.Unlock()

	removed, remainingBytes, err := store.walkAndEvict(maxTotalBytes)

	store.evictMu.Lock()
	store.evict.evicting = false
	store.evict.lastEviction = time.Now()
	if err == nil {
		value := remainingBytes
		store.evict.trackedBytes = &value
	}
	store.evictMu.Unlock()
	return removed, err
}

func (store *Store) walkAndEvict(maxTotalBytes uint64) (int, uint64, error) {
	artifactDir := filepath.Join(store.rootDir, ArtifactDirName)
	if metadata, err := os.Stat(artifactDir); err != nil || !metadata.IsDir() {
		return 0, 0, nil
	}

	files := collectArtifactFiles(artifactDir)
	var totalSize uint64
	for _, file := range files {
		totalSize += file.size
	}
	if totalSize <= maxTotalBytes {
		return 0, totalSize, nil
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTimeNanos < files[j].modTimeNanos
	})
	removed := 0
	for _, file := range files {
		if totalSize <= maxTotalBytes {
			break
		}
		if err := os.Remove(file.path); err == nil {
			totalSize -= file.size
			removed++
		}
	}
	return removed, totalSize, nil
}

func collectArtifactFiles(root string) []artifactFileInfo {
	var files []artifactFileInfo
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		files = append(files, artifactFileInfo{
			path:         path,
			size:         uint64(info.Size()),
			modTimeNanos: info.ModTime().UnixNano(),
		})
		return nil
	})
	return files
}

func validateArtifactSegment(label, value string) error {
	if value == "" || value == "." || value == ".." ||
		strings.Contains(value, "/") || strings.Contains(value, `\`) {
		backendErr := contracts.InvalidInputError(fmt.Sprintf("artifact %s must be a safe path segment", label))
		return backendErr
	}
	return nil
}

// ForceEvictState is a test hook for the eviction fast paths.
func (store *Store) ForceEvictState(trackedBytes *uint64, lastEviction time.Time) {
	store.evictMu.Lock()
	defer store.evictMu.Unlock()
	store.evict.trackedBytes = trackedBytes
	store.evict.lastEviction = lastEviction
	store.evict.evicting = false
}

// TrackedBytes is a test hook for the incremental byte counter.
func (store *Store) TrackedBytes() *uint64 {
	store.evictMu.Lock()
	defer store.evictMu.Unlock()
	if store.evict.trackedBytes == nil {
		return nil
	}
	value := *store.evict.trackedBytes
	return &value
}

// SourcePreviewCache is an in-memory LRU cache for decoded BMP source
// previews. It is bounded by entry count and byte budget, and it coalesces
// concurrent loads for the same key.
type SourcePreviewCache struct {
	mu    sync.Mutex
	state sourcePreviewCacheState
}

type sourcePreviewCacheState struct {
	capacity   int
	maxBytes   int
	totalBytes int
	entries    map[string]sourcePreviewEntry
	inflight   map[string]*sourcePreviewInflight
	lru        []string

	hits          int
	misses        int
	inflightWaits int
}

type sourcePreviewEntry struct {
	preview  bmp.DecodedSourcePreview
	byteSize int
}

type sourcePreviewInflight struct {
	mu     sync.Mutex
	ready  *sync.Cond
	done   bool
	result sourcePreviewResult
}

type sourcePreviewResult struct {
	preview bmp.DecodedSourcePreview
	err     error
}

type SourcePreviewCacheStats struct {
	Len           int
	TotalBytes    int
	Hits          int
	Misses        int
	InflightWaits int
}

func NewSourcePreviewCache(capacity, maxBytes int) *SourcePreviewCache {
	if capacity < 1 {
		capacity = 1
	}
	if maxBytes < 1 {
		maxBytes = 1
	}
	return &SourcePreviewCache{
		state: sourcePreviewCacheState{
			capacity: capacity,
			maxBytes: maxBytes,
			entries:  make(map[string]sourcePreviewEntry),
			inflight: make(map[string]*sourcePreviewInflight),
		},
	}
}

func DefaultSessionSourcePreviewCache() *SourcePreviewCache {
	return NewSourcePreviewCache(DefaultSourcePreviewCacheCapacity, DefaultSourcePreviewCacheMaxBytes)
}

func (cache *SourcePreviewCache) Get(key string) (bmp.DecodedSourcePreview, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.state.get(key)
}

func (cache *SourcePreviewCache) Insert(key string, preview bmp.DecodedSourcePreview) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.state.insert(key, preview)
}

// GetOrTryInsertWith returns a cached preview, waits for an in-flight loader
// for the same key, or runs load once and publishes its result to waiters.
func (cache *SourcePreviewCache) GetOrTryInsertWith(key string, load func() (bmp.DecodedSourcePreview, error)) (bmp.DecodedSourcePreview, error) {
	cache.mu.Lock()
	if preview, ok := cache.state.get(key); ok {
		cache.mu.Unlock()
		return preview, nil
	}
	if inflight := cache.state.inflight[key]; inflight != nil {
		cache.state.inflightWaits++
		cache.mu.Unlock()
		return inflight.wait()
	}

	inflight := newSourcePreviewInflight()
	cache.state.inflight[key] = inflight
	cache.mu.Unlock()

	preview, err := load()
	result := sourcePreviewResult{preview: preview, err: err}

	cache.mu.Lock()
	if err == nil {
		cache.state.insert(key, preview)
	}
	delete(cache.state.inflight, key)
	cache.mu.Unlock()

	inflight.complete(result)
	if err != nil {
		return bmp.DecodedSourcePreview{}, err
	}
	return clonePreview(preview), nil
}

func (cache *SourcePreviewCache) Stats() SourcePreviewCacheStats {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return SourcePreviewCacheStats{
		Len:           len(cache.state.entries),
		TotalBytes:    cache.state.totalBytes,
		Hits:          cache.state.hits,
		Misses:        cache.state.misses,
		InflightWaits: cache.state.inflightWaits,
	}
}

func newSourcePreviewInflight() *sourcePreviewInflight {
	inflight := &sourcePreviewInflight{}
	inflight.ready = sync.NewCond(&inflight.mu)
	return inflight
}

func (inflight *sourcePreviewInflight) wait() (bmp.DecodedSourcePreview, error) {
	inflight.mu.Lock()
	defer inflight.mu.Unlock()
	for !inflight.done {
		inflight.ready.Wait()
	}
	if inflight.result.err != nil {
		return bmp.DecodedSourcePreview{}, inflight.result.err
	}
	return clonePreview(inflight.result.preview), nil
}

func (inflight *sourcePreviewInflight) complete(result sourcePreviewResult) {
	inflight.mu.Lock()
	inflight.result = result
	inflight.done = true
	inflight.mu.Unlock()
	inflight.ready.Broadcast()
}

func (state *sourcePreviewCacheState) get(key string) (bmp.DecodedSourcePreview, bool) {
	entry, ok := state.entries[key]
	if !ok {
		state.misses++
		return bmp.DecodedSourcePreview{}, false
	}
	state.touch(key)
	state.hits++
	return clonePreview(entry.preview), true
}

func (state *sourcePreviewCacheState) insert(key string, preview bmp.DecodedSourcePreview) {
	if existing, ok := state.entries[key]; ok {
		state.totalBytes -= existing.byteSize
		state.removeFromLRU(key)
	}

	stored := clonePreview(preview)
	byteSize := len(stored.Pixels)
	state.totalBytes += byteSize
	state.entries[key] = sourcePreviewEntry{preview: stored, byteSize: byteSize}
	state.lru = append([]string{key}, state.lru...)
	state.evictOverLimits()
}

func (state *sourcePreviewCacheState) touch(key string) {
	state.removeFromLRU(key)
	state.lru = append([]string{key}, state.lru...)
}

func (state *sourcePreviewCacheState) removeFromLRU(key string) {
	next := state.lru[:0]
	for _, candidate := range state.lru {
		if candidate != key {
			next = append(next, candidate)
		}
	}
	state.lru = next
}

func (state *sourcePreviewCacheState) evictOverLimits() {
	for len(state.entries) > state.capacity || state.totalBytes > state.maxBytes {
		if len(state.lru) == 0 {
			break
		}
		victim := state.lru[len(state.lru)-1]
		state.lru = state.lru[:len(state.lru)-1]
		if entry, ok := state.entries[victim]; ok {
			delete(state.entries, victim)
			state.totalBytes -= entry.byteSize
		}
	}
}

func clonePreview(preview bmp.DecodedSourcePreview) bmp.DecodedSourcePreview {
	pixels := append([]byte(nil), preview.Pixels...)
	return bmp.DecodedSourcePreview{Width: preview.Width, Height: preview.Height, Pixels: pixels}
}
