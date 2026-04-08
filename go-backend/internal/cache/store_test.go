package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultRootDirMatchesRustCompatibleTempLayout(t *testing.T) {
	expected := filepath.Join(os.TempDir(), "xrayview")
	if got := DefaultRootDir(); got != expected {
		t.Fatalf("DefaultRootDir = %q, want %q", got, expected)
	}
}

func TestNewWithRootBuildsStableArtifactAndStatePaths(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "xrayview")
	store := NewWithRoot(rootDir)

	if got, want := store.RootDir(), filepath.Join(rootDir, "cache"); got != want {
		t.Fatalf("RootDir = %q, want %q", got, want)
	}
	if got, want := store.PersistenceDir(), filepath.Join(rootDir, "state"); got != want {
		t.Fatalf("PersistenceDir = %q, want %q", got, want)
	}

	renderPath, err := store.ArtifactPath("render", "fingerprint-1", "png")
	if err != nil {
		t.Fatalf("ArtifactPath returned error: %v", err)
	}
	if got, want := renderPath, filepath.Join(rootDir, "cache", "artifacts", "render", "fingerprint-1.png"); got != want {
		t.Fatalf("render artifact path = %q, want %q", got, want)
	}

	catalogPath, err := store.PersistencePath("catalog.json")
	if err != nil {
		t.Fatalf("PersistencePath returned error: %v", err)
	}
	if got, want := catalogPath, filepath.Join(rootDir, "state", "catalog.json"); got != want {
		t.Fatalf("catalog path = %q, want %q", got, want)
	}

	if info, err := os.Stat(filepath.Join(rootDir, "cache", "artifacts", "render")); err != nil || !info.IsDir() {
		t.Fatalf("render artifact directory missing: %v", err)
	}
	if info, err := os.Stat(filepath.Join(rootDir, "state")); err != nil || !info.IsDir() {
		t.Fatalf("state directory missing: %v", err)
	}
}

func TestNewUsesSiblingStateDirectoryForExplicitCacheRoot(t *testing.T) {
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	store := New(cacheRoot)

	if got, want := store.RootDir(), cacheRoot; got != want {
		t.Fatalf("RootDir = %q, want %q", got, want)
	}
	if got, want := store.PersistenceDir(), filepath.Join(filepath.Dir(cacheRoot), "state"); got != want {
		t.Fatalf("PersistenceDir = %q, want %q", got, want)
	}
}

func TestNewWithPathsPreservesExplicitOverrides(t *testing.T) {
	cacheRoot := filepath.Join(t.TempDir(), "custom-cache")
	persistenceRoot := filepath.Join(t.TempDir(), "custom-state")
	store := NewWithPaths(cacheRoot, persistenceRoot)

	if got, want := store.RootDir(), cacheRoot; got != want {
		t.Fatalf("RootDir = %q, want %q", got, want)
	}
	if got, want := store.PersistenceDir(), persistenceRoot; got != want {
		t.Fatalf("PersistenceDir = %q, want %q", got, want)
	}
}
