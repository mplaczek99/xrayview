package cache

import (
	"fmt"
	"os"
	"path/filepath"

	"xrayview/go-backend/internal/contracts"
)

type Store struct {
	rootDir string
}

func New(rootDir string) *Store {
	return &Store{rootDir: filepath.Clean(rootDir)}
}

func (store *Store) RootDir() string {
	return store.rootDir
}

func (store *Store) Ensure() error {
	return os.MkdirAll(store.rootDir, 0o755)
}

func (store *Store) ArtifactPath(namespace string, key string, extension string) (string, error) {
	directory := filepath.Join(store.rootDir, "artifacts", namespace)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", contracts.Internal(
			fmt.Sprintf("failed to create cache directory %s: %v", directory, err),
		)
	}

	return filepath.Join(directory, fmt.Sprintf("%s.%s", key, extension)), nil
}
