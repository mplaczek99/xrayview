package persistence

import (
	"os"
	"path/filepath"
)

type Catalog struct {
	rootDir string
}

func New(rootDir string) *Catalog {
	return &Catalog{rootDir: filepath.Clean(rootDir)}
}

func (catalog *Catalog) RootDir() string {
	return catalog.rootDir
}

func (catalog *Catalog) Ensure() error {
	return os.MkdirAll(catalog.rootDir, 0o755)
}
