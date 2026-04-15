package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"xrayview/backend/internal/contracts"
)

const (
	defaultRootDirName = "xrayview"
	cacheDirName       = "cache"
	artifactDirName    = "artifacts"
	stateDirName       = "state"

	// evictDebounceInterval is the minimum time between full artifact-directory
	// walks. Callers that complete jobs faster than this rate share a single walk.
	evictDebounceInterval = 30 * time.Second
)

type Store struct {
	rootDir        string
	persistenceDir string

	// evictMu protects the three fields below.
	evictMu      sync.Mutex
	evicting     bool      // a walk is already in progress; skip concurrent calls
	lastEviction time.Time // zero → no walk has run yet
	trackedBytes int64     // approximate total size of artifact files; -1 = unknown
}

func New(rootDir string) *Store {
	cleanRoot := filepath.Clean(rootDir)

	return &Store{
		rootDir:        cleanRoot,
		persistenceDir: filepath.Join(filepath.Dir(cleanRoot), stateDirName),
		trackedBytes:   -1,
	}
}

func NewWithRoot(rootDir string) *Store {
	cleanRoot := filepath.Clean(rootDir)

	return &Store{
		rootDir:        filepath.Join(cleanRoot, cacheDirName),
		persistenceDir: filepath.Join(cleanRoot, stateDirName),
		trackedBytes:   -1,
	}
}

func NewWithPaths(cacheDir string, persistenceDir string) *Store {
	return &Store{
		rootDir:        filepath.Clean(cacheDir),
		persistenceDir: filepath.Clean(persistenceDir),
		trackedBytes:   -1,
	}
}

func DefaultRootDir() string {
	return filepath.Join(os.TempDir(), defaultRootDirName)
}

func (store *Store) RootDir() string {
	return store.rootDir
}

func (store *Store) PersistenceDir() string {
	return store.persistenceDir
}

func (store *Store) Ensure() error {
	if err := os.MkdirAll(store.rootDir, 0o755); err != nil {
		return err
	}

	return os.MkdirAll(store.persistenceDir, 0o755)
}

func (store *Store) ArtifactPath(namespace string, key string, extension string) (string, error) {
	directory := filepath.Join(store.rootDir, artifactDirName, namespace)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", contracts.Internal(
			fmt.Sprintf("failed to create cache directory %s: %v", directory, err),
		)
	}

	return filepath.Join(directory, fmt.Sprintf("%s.%s", key, extension)), nil
}

type artifactFileInfo struct {
	path    string
	size    int64
	modTime int64
}

// EvictArtifactsOverLimit removes the oldest artifact files (by modification
// time) until the total size of the artifacts directory is at or below
// maxTotalBytes. It returns the number of files removed.
//
// Fast paths:
//   - If the in-memory size estimate is known and below maxTotalBytes, and the
//     debounce window has not expired, the directory walk is skipped entirely.
//   - If another eviction is already running the call returns immediately.
//
// After each full walk the result is recorded as the new size estimate. Callers
// that write artifacts should call AddArtifactBytes so the estimate stays
// current between walks.
func (store *Store) EvictArtifactsOverLimit(maxTotalBytes int64) (int, error) {
	store.evictMu.Lock()

	// Fast path 1: in-memory size is known and at or below the limit — no
	// eviction is needed, skip the walk unconditionally.
	if store.trackedBytes >= 0 && store.trackedBytes <= maxTotalBytes {
		store.evictMu.Unlock()
		return 0, nil
	}

	// Fast path 2: debounce — at most one full walk every evictDebounceInterval.
	// Protects against N concurrent job completions all triggering walks.
	if !store.lastEviction.IsZero() && time.Since(store.lastEviction) < evictDebounceInterval {
		store.evictMu.Unlock()
		return 0, nil
	}

	// Prevent a concurrent walk from starting while this one runs.
	if store.evicting {
		store.evictMu.Unlock()
		return 0, nil
	}
	store.evicting = true
	store.evictMu.Unlock()

	// After the walk (or on any early return), update the cached state.
	var (
		postTotal int64
		walkErr   error
		removed   int
	)
	defer func() {
		store.evictMu.Lock()
		store.evicting = false
		store.lastEviction = time.Now()
		if walkErr != nil {
			// Walk failed partway — the accumulated total is unreliable.
			store.trackedBytes = -1
		} else {
			store.trackedBytes = postTotal
		}
		store.evictMu.Unlock()
	}()

	artifactDir := filepath.Join(store.rootDir, artifactDirName)
	if _, err := os.Stat(artifactDir); os.IsNotExist(err) {
		postTotal = 0
		return 0, nil
	}

	var files []artifactFileInfo
	var totalSize int64

	walkErr = filepath.Walk(artifactDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if info.IsDir() {
			return nil
		}
		size := info.Size()
		files = append(files, artifactFileInfo{
			path:    path,
			size:    size,
			modTime: info.ModTime().UnixNano(),
		})
		totalSize += size
		return nil
	})
	if walkErr != nil {
		return 0, fmt.Errorf("walk artifacts directory: %w", walkErr)
	}

	if totalSize <= maxTotalBytes {
		postTotal = totalSize
		return 0, nil
	}

	// Sort oldest first.
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime < files[j].modTime
	})

	for _, file := range files {
		if totalSize <= maxTotalBytes {
			break
		}
		if err := os.Remove(file.path); err != nil {
			continue
		}
		totalSize -= file.size
		removed++
	}

	postTotal = totalSize
	return removed, nil
}

// AddArtifactBytes increments the in-memory artifact-size estimate by delta.
// Callers should invoke this after successfully writing an artifact file whose
// size is delta bytes. The estimate is used by EvictArtifactsOverLimit to skip
// the directory walk when the cache is clearly under the size limit.
//
// The estimate is reset to the actual on-disk total after every full walk, so
// minor inaccuracies (e.g. from untracked writes) are self-correcting.
func (store *Store) AddArtifactBytes(delta int64) {
	if delta <= 0 {
		return
	}
	store.evictMu.Lock()
	if store.trackedBytes >= 0 {
		store.trackedBytes += delta
	}
	store.evictMu.Unlock()
}

func (store *Store) PersistencePath(name string) (string, error) {
	if err := os.MkdirAll(store.persistenceDir, 0o755); err != nil {
		return "", contracts.Internal(
			fmt.Sprintf("failed to create state directory %s: %v", store.persistenceDir, err),
		)
	}

	return filepath.Join(store.persistenceDir, name), nil
}
