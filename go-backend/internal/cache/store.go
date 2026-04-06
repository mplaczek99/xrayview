package cache

import (
	"os"
	"path/filepath"
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
