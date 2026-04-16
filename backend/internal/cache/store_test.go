package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xrayview/backend/internal/contracts"
)

// forceEvictReset clears the debounce state so the next call to
// EvictArtifactsOverLimit performs a full directory walk.
func forceEvictReset(store *Store) {
	store.evictMu.Lock()
	store.lastEviction = time.Time{}
	store.trackedBytes = -1
	store.evictMu.Unlock()
}

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

func TestEvictArtifactsDebounceSkipsWalkWithinInterval(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "xrayview")
	store := NewWithRoot(rootDir)

	// Create 3 × 600-byte files so total (1800) > limit (1000).
	baseTime := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	paths := make(map[string]string, 3)
	for i, name := range []string{"a", "b", "c"} {
		path, err := store.ArtifactPath("render", name, "png")
		if err != nil {
			t.Fatalf("ArtifactPath: %v", err)
		}
		if err := os.WriteFile(path, make([]byte, 600), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		mt := baseTime.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(path, mt, mt); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}
		paths[name] = path
	}

	// First call: no debounce state yet — must do full walk and evict.
	removed, err := store.EvictArtifactsOverLimit(1000)
	if err != nil {
		t.Fatalf("first EvictArtifactsOverLimit: %v", err)
	}
	if removed == 0 {
		t.Fatalf("first call: expected eviction, got 0 removed")
	}

	// Recreate files to exceed the limit again.
	for i, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(paths[name], make([]byte, 600), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		mt := baseTime.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(paths[name], mt, mt); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}
	}

	// Second call within the debounce window: must be skipped even though
	// on-disk total now exceeds the limit again.
	// (trackedBytes was set to the post-eviction total which is under 1000.)
	removed, err = store.EvictArtifactsOverLimit(1000)
	if err != nil {
		t.Fatalf("second EvictArtifactsOverLimit: %v", err)
	}
	if removed != 0 {
		t.Fatalf("second call (within debounce): expected 0 removed, got %d", removed)
	}

	// Simulate debounce expiry by backdating lastEviction.
	store.evictMu.Lock()
	store.lastEviction = time.Now().Add(-(evictDebounceInterval + time.Second))
	store.trackedBytes = -1 // force fresh scan
	store.evictMu.Unlock()

	// Third call after debounce expires: must walk and evict.
	removed, err = store.EvictArtifactsOverLimit(1000)
	if err != nil {
		t.Fatalf("third EvictArtifactsOverLimit: %v", err)
	}
	if removed == 0 {
		t.Fatalf("third call (post-debounce): expected eviction, got 0 removed")
	}
}

func TestEvictArtifactsSkipsWalkWhenTrackedBytesUnderLimit(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "xrayview")
	store := NewWithRoot(rootDir)

	// Pre-set state: trackedBytes clearly under limit, lastEviction recent.
	store.evictMu.Lock()
	store.trackedBytes = 500
	store.lastEviction = time.Now()
	store.evictMu.Unlock()

	// Even if there were files over the limit, we should skip the walk.
	path, err := store.ArtifactPath("render", "big", "png")
	if err != nil {
		t.Fatalf("ArtifactPath: %v", err)
	}
	if err := os.WriteFile(path, make([]byte, 2000), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	removed, err := store.EvictArtifactsOverLimit(1000)
	if err != nil {
		t.Fatalf("EvictArtifactsOverLimit: %v", err)
	}
	if removed != 0 {
		t.Fatalf("expected 0 (debounced by size), got %d removed", removed)
	}
}

func TestAddArtifactBytesAccumulatesWhenKnown(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "xrayview")
	store := NewWithRoot(rootDir)

	// Initially unknown (-1): AddArtifactBytes should be a no-op.
	store.AddArtifactBytes(1000)
	store.evictMu.Lock()
	got := store.trackedBytes
	store.evictMu.Unlock()
	if got != -1 {
		t.Fatalf("AddArtifactBytes with unknown state: trackedBytes = %d, want -1", got)
	}

	// Once a scan has run, AddArtifactBytes should increment.
	store.evictMu.Lock()
	store.trackedBytes = 5000
	store.lastEviction = time.Now()
	store.evictMu.Unlock()

	store.AddArtifactBytes(1500)
	store.evictMu.Lock()
	got = store.trackedBytes
	store.evictMu.Unlock()
	if got != 6500 {
		t.Fatalf("trackedBytes = %d, want 6500", got)
	}
}

func TestAddArtifactBytesIgnoresNonPositive(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "xrayview")
	store := NewWithRoot(rootDir)

	store.evictMu.Lock()
	store.trackedBytes = 1000
	store.lastEviction = time.Now()
	store.evictMu.Unlock()

	store.AddArtifactBytes(0)
	store.AddArtifactBytes(-500)
	store.evictMu.Lock()
	got := store.trackedBytes
	store.evictMu.Unlock()
	if got != 1000 {
		t.Fatalf("trackedBytes = %d after zero/negative AddArtifactBytes, want 1000", got)
	}
}

// BenchmarkEvictArtifactsOverLimit measures the cost of EvictArtifactsOverLimit
// on a populated artifact directory. The "Walk" sub-benchmark simulates the
// pre-optimisation path (every call does a full directory walk). The
// "Sequential" sub-benchmark simulates rapid successive calls from job
// completions; after the optimisation the debounce+size-tracking makes all but
// the first call a cheap memory check.
func BenchmarkEvictArtifactsOverLimit(b *testing.B) {
	const (
		fileCount  = 50
		fileSize   = 1024      // 1 KB each → 50 KB total
		limitBytes = 30 * 1024 // keep 30 KB → ~20 files survive per walk
	)

	b.Run("Walk", func(b *testing.B) {
		// Every iteration repopulates the store and forces a fresh full walk.
		rootDir := b.TempDir()
		store := NewWithRoot(rootDir)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			for j := 0; j < fileCount; j++ {
				path, _ := store.ArtifactPath("bench", fmt.Sprintf("file-%03d", j), "bin")
				_ = os.WriteFile(path, make([]byte, fileSize), 0o644)
			}
			forceEvictReset(store) // resets debounce state between iterations
			b.StartTimer()

			if _, err := store.EvictArtifactsOverLimit(limitBytes); err != nil {
				b.Fatalf("EvictArtifactsOverLimit: %v", err)
			}
		}
	})

	b.Run("Sequential", func(b *testing.B) {
		// Populate once. After the first call the debounce/size-tracking should
		// make subsequent calls a near-zero-cost fast path.
		rootDir := b.TempDir()
		store := NewWithRoot(rootDir)
		for j := 0; j < fileCount; j++ {
			path, err := store.ArtifactPath("bench", fmt.Sprintf("file-%03d", j), "bin")
			if err != nil {
				b.Fatalf("ArtifactPath: %v", err)
			}
			if err := os.WriteFile(path, make([]byte, fileSize), 0o644); err != nil {
				b.Fatalf("WriteFile: %v", err)
			}
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := store.EvictArtifactsOverLimit(limitBytes); err != nil {
				b.Fatalf("EvictArtifactsOverLimit: %v", err)
			}
		}
	})
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
