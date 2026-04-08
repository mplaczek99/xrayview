package cache

import (
	"os"
	"path/filepath"
	"testing"

	"xrayview/go-backend/internal/contracts"
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
