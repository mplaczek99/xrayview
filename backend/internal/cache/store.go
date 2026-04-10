package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"xrayview/backend/internal/contracts"
)

const (
	defaultRootDirName = "xrayview"
	cacheDirName       = "cache"
	artifactDirName    = "artifacts"
	stateDirName       = "state"
)

type Store struct {
	rootDir        string
	persistenceDir string
}

func New(rootDir string) *Store {
	cleanRoot := filepath.Clean(rootDir)

	return &Store{
		rootDir:        cleanRoot,
		persistenceDir: filepath.Join(filepath.Dir(cleanRoot), stateDirName),
	}
}

func NewWithRoot(rootDir string) *Store {
	cleanRoot := filepath.Clean(rootDir)

	return &Store{
		rootDir:        filepath.Join(cleanRoot, cacheDirName),
		persistenceDir: filepath.Join(cleanRoot, stateDirName),
	}
}

func NewWithPaths(cacheDir string, persistenceDir string) *Store {
	return &Store{
		rootDir:        filepath.Clean(cacheDir),
		persistenceDir: filepath.Clean(persistenceDir),
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
func (store *Store) EvictArtifactsOverLimit(maxTotalBytes int64) (int, error) {
	artifactDir := filepath.Join(store.rootDir, artifactDirName)
	if _, err := os.Stat(artifactDir); os.IsNotExist(err) {
		return 0, nil
	}

	var files []artifactFileInfo
	var totalSize int64

	err := filepath.Walk(artifactDir, func(path string, info os.FileInfo, err error) error {
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
	if err != nil {
		return 0, fmt.Errorf("walk artifacts directory: %w", err)
	}

	if totalSize <= maxTotalBytes {
		return 0, nil
	}

	// Sort oldest first.
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime < files[j].modTime
	})

	removed := 0
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

	return removed, nil
}

func (store *Store) PersistencePath(name string) (string, error) {
	if err := os.MkdirAll(store.persistenceDir, 0o755); err != nil {
		return "", contracts.Internal(
			fmt.Sprintf("failed to create state directory %s: %v", store.persistenceDir, err),
		)
	}

	return filepath.Join(store.persistenceDir, name), nil
}
