package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"xrayview/backend/internal/contracts"
)

func TestMemoryStoreAndLoadAnalyzeRoundTrip(t *testing.T) {
	memory := NewMemory(nil)
	tempDir := t.TempDir()
	previewPath := filepath.Join(tempDir, "preview.png")
	if err := os.WriteFile(previewPath, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	original := contracts.AnalyzeStudyCommandResult{
		StudyID:     "study-1",
		PreviewPath: previewPath,
		Analysis: contracts.ToothAnalysis{
			Image: contracts.ToothImageMetadata{
				Width:  640,
				Height: 480,
			},
			Calibration: contracts.ToothCalibration{
				PixelUnits: "px",
			},
			Teeth: []contracts.ToothCandidate{
				{
					Confidence:     0.85,
					MaskAreaPixels: 1200,
					Measurements: contracts.ToothMeasurementBundle{
						Pixel: contracts.ToothMeasurementValues{
							ToothWidth:  40.0,
							ToothHeight: 80.0,
							Units:       "px",
						},
					},
					Geometry: contracts.ToothGeometry{
						BoundingBox: contracts.BoundingBox{X: 100, Y: 50, Width: 44, Height: 90},
					},
				},
			},
			Tooth: &contracts.ToothCandidate{
				Confidence:     0.85,
				MaskAreaPixels: 1200,
			},
		},
		SuggestedAnnotations: contracts.AnnotationBundle{
			Lines: []contracts.LineAnnotation{
				{
					ID:     "auto-tooth-1-width",
					Label:  "Tooth 1 width",
					Source: contracts.AnnotationSourceAutoTooth,
				},
			},
			Rectangles: []contracts.RectangleAnnotation{
				{
					ID:     "auto-tooth-1-bounding-box",
					Label:  "Tooth 1 bounding box",
					Source: contracts.AnnotationSourceAutoTooth,
				},
			},
		},
	}

	memory.StoreAnalyze("analyze:1", original)

	result, ok := memory.LoadAnalyze("analyze:1")
	if !ok {
		t.Fatal("LoadAnalyze = miss, want cache hit")
	}
	if got, want := result.StudyID, "study-1"; got != want {
		t.Fatalf("StudyID = %q, want %q", got, want)
	}
	if got, want := len(result.Analysis.Teeth), 1; got != want {
		t.Fatalf("len(Teeth) = %d, want %d", got, want)
	}
	if got, want := result.Analysis.Tooth.Confidence, 0.85; got != want {
		t.Fatalf("Tooth.Confidence = %v, want %v", got, want)
	}
	if got, want := len(result.SuggestedAnnotations.Lines), 1; got != want {
		t.Fatalf("len(Lines) = %d, want %d", got, want)
	}
	if got, want := len(result.SuggestedAnnotations.Rectangles), 1; got != want {
		t.Fatalf("len(Rectangles) = %d, want %d", got, want)
	}
}

func TestMemoryLoadAnalyzeClonesData(t *testing.T) {
	memory := NewMemory(nil)
	tempDir := t.TempDir()
	previewPath := filepath.Join(tempDir, "preview.png")
	if err := os.WriteFile(previewPath, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	confidence := 0.9
	memory.StoreAnalyze("analyze:clone", contracts.AnalyzeStudyCommandResult{
		StudyID:     "study-1",
		PreviewPath: previewPath,
		Analysis: contracts.ToothAnalysis{
			Teeth: []contracts.ToothCandidate{
				{Confidence: 0.9},
			},
		},
		SuggestedAnnotations: contracts.AnnotationBundle{
			Lines: []contracts.LineAnnotation{
				{
					ID:         "line-1",
					Confidence: &confidence,
					Measurement: &contracts.LineMeasurement{
						PixelLength: 42.0,
					},
				},
			},
			Rectangles: []contracts.RectangleAnnotation{
				{
					ID:         "rect-1",
					Confidence: &confidence,
				},
			},
		},
	})

	result, ok := memory.LoadAnalyze("analyze:clone")
	if !ok {
		t.Fatal("LoadAnalyze = miss, want cache hit")
	}

	// Mutate loaded result
	result.Analysis.Teeth[0].Confidence = 0.0
	if result.SuggestedAnnotations.Lines[0].Confidence != nil {
		*result.SuggestedAnnotations.Lines[0].Confidence = 0.0
	}

	// Reload and verify original is intact
	reloaded, ok := memory.LoadAnalyze("analyze:clone")
	if !ok {
		t.Fatal("second LoadAnalyze = miss, want cache hit")
	}
	if got, want := reloaded.Analysis.Teeth[0].Confidence, 0.9; got != want {
		t.Fatalf("reloaded Teeth[0].Confidence = %v, want %v", got, want)
	}
	if reloaded.SuggestedAnnotations.Lines[0].Confidence == nil {
		t.Fatal("reloaded Lines[0].Confidence = nil, want non-nil")
	}
	if got, want := *reloaded.SuggestedAnnotations.Lines[0].Confidence, 0.9; got != want {
		t.Fatalf("reloaded Lines[0].Confidence = %v, want %v", got, want)
	}
}

func TestMemoryLoadAnalyzeInvalidatesMissingPreview(t *testing.T) {
	memory := NewMemory(nil)
	tempDir := t.TempDir()
	previewPath := filepath.Join(tempDir, "preview.png")
	if err := os.WriteFile(previewPath, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	memory.StoreAnalyze("analyze:missing", contracts.AnalyzeStudyCommandResult{
		StudyID:     "study-1",
		PreviewPath: previewPath,
	})

	if err := os.Remove(previewPath); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}

	// Expire the artifact-check TTL so the next load re-stats the missing file.
	memory.mu.Lock()
	memory.entries["analyze:missing"].lastCheckedAt = time.Time{}
	memory.mu.Unlock()

	if _, ok := memory.LoadAnalyze("analyze:missing"); ok {
		t.Fatal("LoadAnalyze = hit after preview removal, want invalidated miss")
	}
}

func TestMemoryLoadAnalyzeReturnsFalseForMissingKey(t *testing.T) {
	memory := NewMemory(nil)

	if _, ok := memory.LoadAnalyze("nonexistent"); ok {
		t.Fatal("LoadAnalyze = hit for missing key, want miss")
	}
}

func TestMemoryLoadAnalyzeInvalidatesKindMismatch(t *testing.T) {
	memory := NewMemory(nil)
	tempDir := t.TempDir()
	previewPath := filepath.Join(tempDir, "preview.png")
	if err := os.WriteFile(previewPath, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	memory.StoreRender("shared:analyze", contracts.RenderStudyCommandResult{
		StudyID:     "study-1",
		PreviewPath: previewPath,
	})

	if _, ok := memory.LoadAnalyze("shared:analyze"); ok {
		t.Fatal("LoadAnalyze = hit for render entry, want typed miss")
	}
}
