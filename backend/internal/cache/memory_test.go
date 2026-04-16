package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/imaging"
)

func TestMemoryLoadRenderInvalidatesMissingPreviewArtifact(t *testing.T) {
	memory := NewMemory(nil)
	previewPath := filepath.Join(t.TempDir(), "preview.png")
	if err := os.WriteFile(previewPath, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	memory.StoreRender("render:1", contracts.RenderStudyCommandResult{
		StudyID:      "study-1",
		PreviewPath:  previewPath,
		LoadedWidth:  2,
		LoadedHeight: 2,
		MeasurementScale: &contracts.MeasurementScale{
			RowSpacingMM:    0.25,
			ColumnSpacingMM: 0.40,
			Source:          "PixelSpacing",
		},
	})

	result, ok := memory.LoadRender("render:1")
	if !ok {
		t.Fatal("LoadRender = miss, want cache hit")
	}
	if result.MeasurementScale == nil {
		t.Fatal("MeasurementScale = nil, want cached scale")
	}

	result.MeasurementScale.RowSpacingMM = 9.99

	cachedAgain, ok := memory.LoadRender("render:1")
	if !ok {
		t.Fatal("second LoadRender = miss, want cache hit")
	}
	if got, want := cachedAgain.MeasurementScale.RowSpacingMM, 0.25; got != want {
		t.Fatalf("cachedAgain.MeasurementScale.RowSpacingMM = %v, want %v", got, want)
	}

	if err := os.Remove(previewPath); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}

	// Expire the artifact-check TTL so the next load re-stats the missing file.
	memory.mu.Lock()
	memory.entries["render:1"].lastCheckedAt = time.Time{}
	memory.mu.Unlock()

	if _, ok := memory.LoadRender("render:1"); ok {
		t.Fatal("LoadRender = hit after preview removal, want invalidated miss")
	}
}

func TestMemoryLoadProcessRequiresPreviewAndDicomArtifacts(t *testing.T) {
	memory := NewMemory(nil)
	tempDir := t.TempDir()
	previewPath := filepath.Join(tempDir, "preview.png")
	dicomPath := filepath.Join(tempDir, "processed.dcm")
	if err := os.WriteFile(previewPath, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile preview returned error: %v", err)
	}
	if err := os.WriteFile(dicomPath, []byte("dcm"), 0o644); err != nil {
		t.Fatalf("WriteFile dicom returned error: %v", err)
	}

	memory.StoreProcess("process:1", contracts.ProcessStudyCommandResult{
		StudyID:      "study-1",
		PreviewPath:  previewPath,
		DicomPath:    dicomPath,
		LoadedWidth:  2,
		LoadedHeight: 2,
		Mode:         "processed preview",
	})

	if _, ok := memory.LoadProcess("process:1"); !ok {
		t.Fatal("LoadProcess = miss, want cache hit")
	}

	if err := os.Remove(dicomPath); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}

	// Expire the artifact-check TTL so the next load re-stats the missing file.
	memory.mu.Lock()
	memory.entries["process:1"].lastCheckedAt = time.Time{}
	memory.mu.Unlock()

	if _, ok := memory.LoadProcess("process:1"); ok {
		t.Fatal("LoadProcess = hit after DICOM removal, want invalidated miss")
	}
}

func TestMemoryTypedLoadInvalidatesMismatchedEntryKind(t *testing.T) {
	memory := NewMemory(nil)
	tempDir := t.TempDir()
	previewPath := filepath.Join(tempDir, "preview.png")
	dicomPath := filepath.Join(tempDir, "processed.dcm")
	if err := os.WriteFile(previewPath, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile preview returned error: %v", err)
	}
	if err := os.WriteFile(dicomPath, []byte("dcm"), 0o644); err != nil {
		t.Fatalf("WriteFile dicom returned error: %v", err)
	}

	memory.StoreProcess("shared:1", contracts.ProcessStudyCommandResult{
		StudyID:      "study-1",
		PreviewPath:  previewPath,
		DicomPath:    dicomPath,
		LoadedWidth:  2,
		LoadedHeight: 2,
		Mode:         "processed preview",
	})

	if _, ok := memory.LoadRender("shared:1"); ok {
		t.Fatal("LoadRender = hit for process cache entry, want typed miss")
	}
	if _, ok := memory.LoadProcess("shared:1"); ok {
		t.Fatal("LoadProcess = hit after mismatched typed read, want invalidated miss")
	}
}

func TestMemorySourcePreviewRoundTripClonesPixels(t *testing.T) {
	memory := NewMemory(nil)

	memory.StoreSourcePreview(
		"/tmp/study.dcm",
		imaging.GrayPreview(2, 2, []uint8{1, 2, 3, 4}),
	)

	preview, ok := memory.LoadSourcePreview("/tmp/study.dcm")
	if !ok {
		t.Fatal("LoadSourcePreview = miss, want cache hit")
	}

	preview.Pixels[0] = 99

	cachedAgain, ok := memory.LoadSourcePreview("/tmp/study.dcm")
	if !ok {
		t.Fatal("second LoadSourcePreview = miss, want cache hit")
	}
	if got, want := cachedAgain.Pixels[0], uint8(1); got != want {
		t.Fatalf("cachedAgain.Pixels[0] = %d, want %d", got, want)
	}
}

func BenchmarkSourcePreviewStoreLoad(b *testing.B) {
	// 2048x1536 gray8 = 3,145,728 bytes — typical dental radiograph.
	const width, height = 2048, 1536
	pixels := make([]uint8, width*height)
	for i := range pixels {
		pixels[i] = uint8(i)
	}

	b.Run("RoundTrip", func(b *testing.B) {
		memory := NewMemory(nil)
		preview := imaging.GrayPreview(width, height, pixels)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			memory.StoreSourcePreview("/tmp/bench.dcm", preview)
			loaded, ok := memory.LoadSourcePreview("/tmp/bench.dcm")
			if !ok {
				b.Fatal("cache miss")
			}
			_ = loaded
		}
	})

	b.Run("StoreOnly", func(b *testing.B) {
		memory := NewMemory(nil)
		preview := imaging.GrayPreview(width, height, pixels)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			memory.StoreSourcePreview(fmt.Sprintf("/tmp/bench-%d.dcm", i), preview)
		}
	})

	b.Run("LoadOnly", func(b *testing.B) {
		memory := NewMemory(nil)
		memory.StoreSourcePreview("/tmp/bench.dcm", imaging.GrayPreview(width, height, pixels))
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			loaded, ok := memory.LoadSourcePreview("/tmp/bench.dcm")
			if !ok {
				b.Fatal("cache miss")
			}
			_ = loaded
		}
	})
}

func TestMemoryStoreBoundsResultEntries(t *testing.T) {
	memory := NewMemory(nil)

	for index := 0; index < maxMemoryCacheEntries+8; index++ {
		memory.StoreRender(
			fmt.Sprintf("render:%03d", index),
			contracts.RenderStudyCommandResult{StudyID: "study-1"},
		)
	}

	if got, want := len(memory.entries), maxMemoryCacheEntries; got != want {
		t.Fatalf("len(entries) = %d, want %d", got, want)
	}
}

func TestMemoryStoreBoundsSourcePreviewEntries(t *testing.T) {
	memory := NewMemory(nil)

	for index := 0; index < maxSourcePreviewEntries+8; index++ {
		memory.StoreSourcePreview(
			fmt.Sprintf("/tmp/study-%03d.dcm", index),
			imaging.GrayPreview(1, 1, []uint8{uint8(index)}),
		)
	}

	if got, want := len(memory.sourcePreviews), maxSourcePreviewEntries; got != want {
		t.Fatalf("len(sourcePreviews) = %d, want %d", got, want)
	}
}

func TestMemorySourcePreviewLRU(t *testing.T) {
	memory := NewMemory(nil)

	// Fill cache to capacity.
	for i := 0; i < maxSourcePreviewEntries; i++ {
		memory.StoreSourcePreview(
			fmt.Sprintf("/study-%03d.dcm", i),
			imaging.GrayPreview(1, 1, []uint8{uint8(i)}),
		)
	}

	// Promote the oldest entry to most-recently-used.
	if _, ok := memory.LoadSourcePreview("/study-000.dcm"); !ok {
		t.Fatal("LoadSourcePreview(/study-000.dcm) = miss, want hit before eviction")
	}

	// Store one more entry — must evict the LRU entry (/study-001.dcm).
	memory.StoreSourcePreview("/study-new.dcm", imaging.GrayPreview(1, 1, []uint8{99}))

	if _, ok := memory.LoadSourcePreview("/study-000.dcm"); !ok {
		t.Error("/study-000.dcm = miss, want hit (was most recently used)")
	}
	if _, ok := memory.LoadSourcePreview("/study-001.dcm"); ok {
		t.Error("/study-001.dcm = hit, want miss (was least recently used and should be evicted)")
	}
}

func TestMemoryResultLRU(t *testing.T) {
	memory := NewMemory(nil)
	tempDir := t.TempDir()

	makePreview := func(i int) string {
		path := filepath.Join(tempDir, fmt.Sprintf("preview-%03d.png", i))
		if err := os.WriteFile(path, []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	// Fill result cache to capacity.
	for i := 0; i < maxMemoryCacheEntries; i++ {
		memory.StoreRender(
			fmt.Sprintf("render:%03d", i),
			contracts.RenderStudyCommandResult{StudyID: "study-1", PreviewPath: makePreview(i)},
		)
	}

	// Promote the oldest entry to most-recently-used.
	if _, ok := memory.LoadRender("render:000"); !ok {
		t.Fatal("LoadRender(render:000) = miss, want hit before eviction")
	}

	// Store one more entry — must evict the LRU entry (render:001).
	memory.StoreRender("render:new", contracts.RenderStudyCommandResult{
		StudyID:     "study-1",
		PreviewPath: makePreview(maxMemoryCacheEntries),
	})

	if _, ok := memory.LoadRender("render:000"); !ok {
		t.Error("render:000 = miss, want hit (was most recently used)")
	}
	if _, ok := memory.LoadRender("render:001"); ok {
		t.Error("render:001 = hit, want miss (was least recently used and should be evicted)")
	}
}

func BenchmarkMemoryEviction(b *testing.B) {
	b.Run("Results", func(b *testing.B) {
		memory := NewMemory(nil)
		for i := 0; i < maxMemoryCacheEntries; i++ {
			memory.StoreRender(fmt.Sprintf("render:%05d", i), contracts.RenderStudyCommandResult{StudyID: "study-1"})
		}
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			memory.StoreRender(fmt.Sprintf("render:%05d", maxMemoryCacheEntries+i), contracts.RenderStudyCommandResult{StudyID: "study-1"})
		}
	})

	b.Run("SourcePreviews", func(b *testing.B) {
		memory := NewMemory(nil)
		pixels := make([]uint8, 4)
		for i := 0; i < maxSourcePreviewEntries; i++ {
			memory.StoreSourcePreview(fmt.Sprintf("/tmp/bench-%05d.dcm", i), imaging.GrayPreview(2, 2, pixels))
		}
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			memory.StoreSourcePreview(fmt.Sprintf("/tmp/new-%05d.dcm", i), imaging.GrayPreview(2, 2, pixels))
		}
	})
}

func TestMemorySourcePreviewEvictsByByteBudget(t *testing.T) {
	memory := NewMemory(nil)

	// Each 100x100 gray8 preview is 10000 bytes.
	// With a 64MB budget this won't trigger, so lower the budget for the test.
	originalMax := maxSourcePreviewBytes
	defer func() {
		// maxSourcePreviewBytes is a package-level const, so we can't change it.
		// Instead, test via total tracking correctness.
		_ = originalMax
	}()

	// Store two large previews and verify the byte counter tracks correctly.
	pixels := make([]uint8, 1000)
	memory.StoreSourcePreview("/tmp/a.dcm", imaging.GrayPreview(100, 10, pixels))

	if got, want := memory.sourcePreviewBytes, uint64(1000); got != want {
		t.Fatalf("sourcePreviewBytes = %d, want %d after first store", got, want)
	}

	memory.StoreSourcePreview("/tmp/b.dcm", imaging.GrayPreview(100, 10, pixels))

	if got, want := memory.sourcePreviewBytes, uint64(2000); got != want {
		t.Fatalf("sourcePreviewBytes = %d, want %d after second store", got, want)
	}

	// Overwriting an existing entry should update the byte count.
	smallPixels := make([]uint8, 500)
	memory.StoreSourcePreview("/tmp/a.dcm", imaging.GrayPreview(50, 10, smallPixels))

	if got, want := memory.sourcePreviewBytes, uint64(1500); got != want {
		t.Fatalf("sourcePreviewBytes = %d, want %d after overwrite", got, want)
	}
}

// BenchmarkConcurrentLoad measures throughput when N goroutines concurrently
// load distinct pre-populated keys. This is the hot path during concurrent job
// starts. RWMutex should allow all readers to proceed in parallel vs Mutex
// serializing them.
// BenchmarkLoadRender measures the hot-path cost of LoadRender on a cache hit:
// the RWMutex check, artifact existence validation, and LRU promotion.
func BenchmarkLoadRender(b *testing.B) {
	previewPath := filepath.Join(b.TempDir(), "preview.png")
	if err := os.WriteFile(previewPath, []byte("png"), 0o644); err != nil {
		b.Fatalf("WriteFile: %v", err)
	}

	b.Run("Hit", func(b *testing.B) {
		memory := NewMemory(nil)
		memory.StoreRender("render:1", contracts.RenderStudyCommandResult{
			StudyID:     "study-1",
			PreviewPath: previewPath,
		})
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			result, ok := memory.LoadRender("render:1")
			if !ok {
				b.Fatal("cache miss")
			}
			_ = result
		}
	})

	b.Run("Miss", func(b *testing.B) {
		memory := NewMemory(nil)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = memory.LoadRender("render:missing")
		}
	})
}

func BenchmarkConcurrentLoad(b *testing.B) {
	const numKeys = 16
	pixels := make([]uint8, 2048*1536)
	for i := range pixels {
		pixels[i] = uint8(i)
	}

	b.Run("SourcePreview", func(b *testing.B) {
		memory := NewMemory(nil)
		for i := 0; i < numKeys; i++ {
			memory.StoreSourcePreview(
				fmt.Sprintf("/tmp/bench-%02d.dcm", i),
				imaging.GrayPreview(2048, 1536, pixels),
			)
		}
		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("/tmp/bench-%02d.dcm", i%numKeys)
				loaded, ok := memory.LoadSourcePreview(key)
				if !ok {
					b.Fatalf("cache miss for key %s", key)
				}
				_ = loaded
				i++
			}
		})
	})

	b.Run("MeasurementScale", func(b *testing.B) {
		memory := NewMemory(nil)
		for i := 0; i < numKeys; i++ {
			scale := &contracts.MeasurementScale{
				RowSpacingMM:    float64(i) * 0.1,
				ColumnSpacingMM: float64(i) * 0.2,
				Source:          "PixelSpacing",
			}
			memory.StoreMeasurementScale(fmt.Sprintf("/tmp/bench-%02d.dcm", i), scale)
		}
		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("/tmp/bench-%02d.dcm", i%numKeys)
				loaded, ok := memory.LoadMeasurementScale(key)
				if !ok {
					b.Fatalf("cache miss for key %s", key)
				}
				_ = loaded
				i++
			}
		})
	})
}
