package cache

import (
	"fmt"
	"os"
	"path/filepath"

	"xrayview/go-backend/internal/contracts"
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

func (store *Store) PersistencePath(name string) (string, error) {
	if err := os.MkdirAll(store.persistenceDir, 0o755); err != nil {
		return "", contracts.Internal(
			fmt.Sprintf("failed to create state directory %s: %v", store.persistenceDir, err),
		)
	}

	return filepath.Join(store.persistenceDir, name), nil
}
