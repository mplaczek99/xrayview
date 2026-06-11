package cache

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xrayview/backend/internal/bmp"
	"xrayview/backend/internal/contracts"
)

func TestNewWithRootBuildsStableArtifactAndStatePaths(t *testing.T) {
	root := t.TempDir()
	store := NewStoreWithRoot(root)

	if store.RootDir() != filepath.Join(root, "cache") {
		t.Fatalf("root dir = %q", store.RootDir())
	}
	if store.PersistenceDir() != filepath.Join(root, "state") {
		t.Fatalf("state dir = %q", store.PersistenceDir())
	}
	renderPath, err := store.ArtifactPath("render", "fingerprint-1", "bmp")
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(root, "cache", "artifacts", "render", "fingerprint-1.bmp")
	if renderPath != want {
		t.Fatalf("artifact path = %q, want %q", renderPath, want)
	}
	if metadata, err := os.Stat(filepath.Join(root, "cache", "artifacts", "render")); err != nil || !metadata.IsDir() {
		t.Fatalf("artifact directory not created: %v", err)
	}
}

func TestNewUsesSiblingStateDirectoryForExplicitCacheRoot(t *testing.T) {
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "cache")
	store := NewStore(cacheRoot)

	if store.RootDir() != cacheRoot {
		t.Fatalf("root dir = %q", store.RootDir())
	}
	if store.PersistenceDir() != filepath.Join(root, "state") {
		t.Fatalf("state dir = %q", store.PersistenceDir())
	}
}

func TestEnsureCreatesCacheAndStateDirectories(t *testing.T) {
	store := NewStoreWithRoot(t.TempDir())

	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	if metadata, err := os.Stat(store.RootDir()); err != nil || !metadata.IsDir() {
		t.Fatalf("cache dir missing: %v", err)
	}
	if metadata, err := os.Stat(store.PersistenceDir()); err != nil || !metadata.IsDir() {
		t.Fatalf("state dir missing: %v", err)
	}
}

func TestEvictArtifactsOverLimitRemovesOldestFiles(t *testing.T) {
	store := NewStoreWithRoot(t.TempDir())
	var paths []string
	for _, name := range []string{"a", "b", "c"} {
		path, err := store.ArtifactPath("render", name, "bmp")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, bytesOfLength(600), 0o666); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
		time.Sleep(2 * time.Millisecond)
	}

	removed, err := store.EvictArtifactsOverLimit(1000)
	if err != nil {
		t.Fatal(err)
	}

	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	if fileExists(paths[0]) || fileExists(paths[1]) || !fileExists(paths[2]) {
		t.Fatalf("unexpected file survival: %v", paths)
	}
}

func TestEvictArtifactsOverLimitNoopsWhenUnderBudgetOrMissing(t *testing.T) {
	store := NewStoreWithRoot(t.TempDir())
	removed, err := store.EvictArtifactsOverLimit(100)
	if err != nil || removed != 0 {
		t.Fatalf("missing artifacts removed=%d err=%v", removed, err)
	}

	path, err := store.ArtifactPath("render", "small", "bmp")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytesOfLength(100), 0o666); err != nil {
		t.Fatal(err)
	}

	removed, err = store.EvictArtifactsOverLimit(1000)
	if err != nil || removed != 0 {
		t.Fatalf("under budget removed=%d err=%v", removed, err)
	}
	if !fileExists(path) {
		t.Fatal("under-budget artifact was removed")
	}
}

func TestEvictArtifactsSkipsWalkWhenTrackedBytesAreUnderLimit(t *testing.T) {
	store := NewStoreWithRoot(t.TempDir())
	tracked := uint64(500)
	store.ForceEvictState(&tracked, time.Now())
	path, err := store.ArtifactPath("render", "big", "bmp")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytesOfLength(2_000), 0o666); err != nil {
		t.Fatal(err)
	}

	removed, err := store.EvictArtifactsOverLimit(1000)
	if err != nil || removed != 0 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if !fileExists(path) {
		t.Fatal("artifact was removed despite tracked fast path")
	}
}

func TestAddArtifactBytesAccumulatesOnlyWhenTotalIsKnown(t *testing.T) {
	store := NewStoreWithRoot(t.TempDir())

	store.AddArtifactBytes(1000)
	if store.TrackedBytes() != nil {
		t.Fatal("tracked bytes changed before baseline")
	}

	tracked := uint64(5000)
	store.ForceEvictState(&tracked, time.Now())
	store.AddArtifactBytes(1500)
	store.AddArtifactBytes(0)

	if got := store.TrackedBytes(); got == nil || *got != 6500 {
		t.Fatalf("tracked bytes = %v, want 6500", got)
	}
}

func TestArtifactPathWrapsDirectoryCreationErrors(t *testing.T) {
	parent := t.TempDir()
	blocker := filepath.Join(parent, "blocker")
	if err := os.WriteFile(blocker, []byte("not-a-directory"), 0o666); err != nil {
		t.Fatal(err)
	}
	store := NewStoreWithPaths(filepath.Join(blocker, "cache"), filepath.Join(blocker, "state"))

	_, err := store.ArtifactPath("render", "fingerprint-1", "bmp")
	if err == nil {
		t.Fatal("expected error")
	}
	var backendErr contracts.BackendError
	if !errors.As(err, &backendErr) {
		t.Fatalf("error = %T, want BackendError", err)
	}
	if backendErr.Code != contracts.Internal {
		t.Fatalf("code = %s, want internal", backendErr.Code)
	}
}

func TestArtifactPathRejectsUnsafePathSegments(t *testing.T) {
	store := NewStoreWithRoot(t.TempDir())

	cases := [][3]string{
		{"../render", "fingerprint-1", "bmp"},
		{"render", "../fingerprint-1", "bmp"},
		{"render", "fingerprint-1", "../bmp"},
		{"render", "fingerprint/1", "bmp"},
		{"render", `fingerprint\1`, "bmp"},
		{"render", ".", "bmp"},
		{"render", "", "bmp"},
	}
	for _, tc := range cases {
		_, err := store.ArtifactPath(tc[0], tc[1], tc[2])
		if err == nil {
			t.Fatalf("expected error for %#v", tc)
		}
		var backendErr contracts.BackendError
		if !errors.As(err, &backendErr) || backendErr.Code != contracts.InvalidInput {
			t.Fatalf("error = %#v, want invalid input", err)
		}
	}
}

func TestSourcePreviewCacheReturnsClonesAndTracksHits(t *testing.T) {
	cache := NewSourcePreviewCache(2, 1024)
	preview := decodedPreview(2, 2, []byte{1, 2, 3, 4})

	if _, ok := cache.Get("study-1"); ok {
		t.Fatal("unexpected cache hit")
	}
	cache.Insert("study-1", preview)
	cached, ok := cache.Get("study-1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	cached.Pixels[0] = 99
	cachedAgain, ok := cache.Get("study-1")
	if !ok {
		t.Fatal("expected second cache hit")
	}

	if string(cachedAgain.Pixels) != string(preview.Pixels) {
		t.Fatalf("cached pixels = %v, want %v", cachedAgain.Pixels, preview.Pixels)
	}
	want := SourcePreviewCacheStats{Len: 1, TotalBytes: 4, Hits: 2, Misses: 1}
	if got := cache.Stats(); got != want {
		t.Fatalf("stats = %+v, want %+v", got, want)
	}
}

func TestSourcePreviewCacheEvictsLeastRecentlyUsedByCapacity(t *testing.T) {
	cache := NewSourcePreviewCache(2, 1024)
	cache.Insert("a", decodedPreview(1, 1, []byte{1}))
	cache.Insert("b", decodedPreview(1, 1, []byte{2}))
	if _, ok := cache.Get("a"); !ok {
		t.Fatal("expected a")
	}
	cache.Insert("c", decodedPreview(1, 1, []byte{3}))

	if _, ok := cache.Get("a"); !ok {
		t.Fatal("expected a to survive")
	}
	if _, ok := cache.Get("b"); ok {
		t.Fatal("expected b to be evicted")
	}
	if _, ok := cache.Get("c"); !ok {
		t.Fatal("expected c to survive")
	}
}

func TestSourcePreviewCacheEvictsByByteBudget(t *testing.T) {
	cache := NewSourcePreviewCache(10, 5)
	cache.Insert("a", decodedPreview(3, 1, bytesOfLength(3)))
	cache.Insert("b", decodedPreview(3, 1, bytesOfLength(3)))

	if _, ok := cache.Get("a"); ok {
		t.Fatal("expected a to be evicted")
	}
	if _, ok := cache.Get("b"); !ok {
		t.Fatal("expected b to survive")
	}
	if got := cache.Stats().TotalBytes; got != 3 {
		t.Fatalf("total bytes = %d, want 3", got)
	}
}

func TestSourcePreviewCacheCoalescesConcurrentLoadsForSameKey(t *testing.T) {
	cache := NewSourcePreviewCache(4, 1024)
	var loadCount atomic.Int64
	loaderStarted := make(chan struct{})
	releaseLoader := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan bmp.DecodedSourcePreview, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		preview, err := cache.GetOrTryInsertWith("study-1", func() (bmp.DecodedSourcePreview, error) {
			loadCount.Add(1)
			close(loaderStarted)
			<-releaseLoader
			return decodedPreview(1, 3, []byte{7, 8, 9}), nil
		})
		if err != nil {
			t.Errorf("first load: %v", err)
			return
		}
		results <- preview
	}()

	<-loaderStarted

	wg.Add(1)
	go func() {
		defer wg.Done()
		preview, err := cache.GetOrTryInsertWith("study-1", func() (bmp.DecodedSourcePreview, error) {
			loadCount.Add(1)
			return decodedPreview(1, 1, []byte{99}), nil
		})
		if err != nil {
			t.Errorf("second load: %v", err)
			return
		}
		results <- preview
	}()

	deadline := time.Now().Add(time.Second)
	for cache.Stats().InflightWaits == 0 {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for inflight waiter")
		}
		time.Sleep(time.Millisecond)
	}
	if loadCount.Load() != 1 {
		t.Fatalf("load count = %d, want 1", loadCount.Load())
	}
	close(releaseLoader)
	wg.Wait()
	close(results)

	var previews []bmp.DecodedSourcePreview
	for preview := range results {
		previews = append(previews, preview)
	}
	if len(previews) != 2 || string(previews[0].Pixels) != string(previews[1].Pixels) {
		t.Fatalf("previews = %+v", previews)
	}
	if loadCount.Load() != 1 {
		t.Fatalf("load count = %d, want 1", loadCount.Load())
	}
	if cache.Stats().InflightWaits != 1 {
		t.Fatalf("inflight waits = %d, want 1", cache.Stats().InflightWaits)
	}
}

func bytesOfLength(length int) []byte {
	return make([]byte, length)
}

func fileExists(path string) bool {
	metadata, err := os.Stat(path)
	return err == nil && metadata.Mode().IsRegular()
}

func decodedPreview(width, height uint32, pixels []byte) bmp.DecodedSourcePreview {
	return bmp.DecodedSourcePreview{Width: width, Height: height, Pixels: append([]byte(nil), pixels...)}
}
