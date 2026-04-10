package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xrayview/backend/internal/contracts"
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

func TestEnsureCreatesCacheAndStateDirectories(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "xrayview")
	store := NewWithRoot(rootDir)

	if err := store.Ensure(); err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}

	for _, path := range []string{store.RootDir(), store.PersistenceDir()} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%q) returned error: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("%q is not a directory", path)
		}
	}
}

func TestEvictArtifactsOverLimitRemovesOldestFiles(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "xrayview")
	store := NewWithRoot(rootDir)

	// Create three artifact files, each 1000 bytes.
	for _, name := range []string{"a", "b", "c"} {
		path, err := store.ArtifactPath("render", name, "png")
		if err != nil {
			t.Fatalf("ArtifactPath returned error: %v", err)
		}
		if err := os.WriteFile(path, make([]byte, 1000), 0o644); err != nil {
			t.Fatalf("WriteFile returned error: %v", err)
		}
	}

	// Evict to 1500 bytes — should remove at least one file.
	removed, err := store.EvictArtifactsOverLimit(1500)
	if err != nil {
		t.Fatalf("EvictArtifactsOverLimit returned error: %v", err)
	}

	if removed < 1 {
		t.Fatalf("expected at least 1 file removed, got %d", removed)
	}
}

func TestEvictArtifactsOverLimitNoOpWhenUnderBudget(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "xrayview")
	store := NewWithRoot(rootDir)

	path, err := store.ArtifactPath("render", "small", "png")
	if err != nil {
		t.Fatalf("ArtifactPath returned error: %v", err)
	}
	if err := os.WriteFile(path, make([]byte, 100), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	removed, err := store.EvictArtifactsOverLimit(10000)
	if err != nil {
		t.Fatalf("EvictArtifactsOverLimit returned error: %v", err)
	}
	if removed != 0 {
		t.Fatalf("expected 0 files removed, got %d", removed)
	}
}

func TestEvictArtifactsOverLimitNoOpWhenNoArtifactDir(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "nonexistent")
	store := NewWithRoot(rootDir)

	removed, err := store.EvictArtifactsOverLimit(100)
	if err != nil {
		t.Fatalf("EvictArtifactsOverLimit returned error: %v", err)
	}
	if removed != 0 {
		t.Fatalf("expected 0 files removed, got %d", removed)
	}
}

func TestEvictArtifactsOverLimitRemovesOldestArtifactsFirst(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "xrayview")
	store := NewWithRoot(rootDir)

	baseTime := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	paths := make(map[string]string, 3)
	for index, name := range []string{"a", "b", "c"} {
		path, err := store.ArtifactPath("render", name, "png")
		if err != nil {
			t.Fatalf("ArtifactPath returned error: %v", err)
		}
		if err := os.WriteFile(path, make([]byte, 600), 0o644); err != nil {
			t.Fatalf("WriteFile returned error: %v", err)
		}
		modTime := baseTime.Add(time.Duration(index) * time.Second)
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatalf("Chtimes returned error: %v", err)
		}
		paths[name] = path
	}

	removed, err := store.EvictArtifactsOverLimit(1000)
	if err != nil {
		t.Fatalf("EvictArtifactsOverLimit returned error: %v", err)
	}
	if got, want := removed, 2; got != want {
		t.Fatalf("removed = %d, want %d", got, want)
	}

	for _, name := range []string{"a", "b"} {
		if _, err := os.Stat(paths[name]); !os.IsNotExist(err) {
			t.Fatalf("%s artifact unexpectedly remained, err = %v", name, err)
		}
	}
	if info, err := os.Stat(paths["c"]); err != nil || info.IsDir() {
		t.Fatalf("newest artifact missing or invalid: %v", err)
	}
}

func TestArtifactPathAndPersistencePathWrapDirectoryCreationErrors(t *testing.T) {
	blockedRoot := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blockedRoot, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	store := NewWithPaths(filepath.Join(blockedRoot, "cache"), filepath.Join(blockedRoot, "state"))
	tests := []struct {
		name        string
		call        func() (string, error)
		wantMessage string
	}{
		{
			name: "artifact path",
			call: func() (string, error) {
				return store.ArtifactPath("render", "fingerprint-1", "png")
			},
			wantMessage: "failed to create cache directory",
		},
		{
			name: "persistence path",
			call: func() (string, error) {
				return store.PersistencePath("catalog.json")
			},
			wantMessage: "failed to create state directory",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.call()
			if err == nil {
				t.Fatal("returned nil error, want internal error")
			}

			backendErr, ok := err.(contracts.BackendError)
			if !ok {
				t.Fatalf("error type = %T, want contracts.BackendError", err)
			}
			if got, want := backendErr.Code, contracts.BackendErrorCodeInternal; got != want {
				t.Fatalf("error code = %q, want %q", got, want)
			}
			if !strings.Contains(backendErr.Message, test.wantMessage) {
				t.Fatalf("error message = %q, want substring %q", backendErr.Message, test.wantMessage)
			}
		})
	}
}
