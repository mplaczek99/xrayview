package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

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
