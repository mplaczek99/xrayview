package processing

import (
	"testing"

	"xrayview/backend/internal/imaging"
)

func TestCombineComparisonPlacesImagesSideBySide(t *testing.T) {
	left := imaging.GrayPreview(2, 1, []uint8{10, 20})
	right := imaging.RGBAPreview(2, 1, []uint8{
		100, 110, 120, 255,
		200, 210, 220, 255,
	})

	got, err := CombineComparison(left, right)
	if err != nil {
		t.Fatalf("CombineComparison returned error: %v", err)
	}

	if got.Format != imaging.FormatRGBA8 {
		t.Fatalf("Format = %q, want %q", got.Format, imaging.FormatRGBA8)
	}
	if got.Width != 4 || got.Height != 1 {
		t.Fatalf("size = %dx%d, want 4x1", got.Width, got.Height)
	}
	if want := []uint8{
		10, 10, 10, 255,
		20, 20, 20, 255,
		100, 110, 120, 255,
		200, 210, 220, 255,
	}; !equalBytes(got.Pixels, want) {
		t.Fatalf("Pixels = %v, want %v", got.Pixels, want)
	}
}

func TestCombineComparisonExpandsGrayProcessedOutput(t *testing.T) {
	left := imaging.GrayPreview(2, 1, []uint8{10, 20})
	right := imaging.GrayPreview(2, 1, []uint8{30, 40})

	got, err := CombineComparison(left, right)
	if err != nil {
		t.Fatalf("CombineComparison returned error: %v", err)
	}

	if want := []uint8{
		10, 10, 10, 255,
		20, 20, 20, 255,
		30, 30, 30, 255,
		40, 40, 40, 255,
	}; !equalBytes(got.Pixels, want) {
		t.Fatalf("Pixels = %v, want %v", got.Pixels, want)
	}
}

func TestCombineComparisonRequiresGrayLeftSource(t *testing.T) {
	_, err := CombineComparison(
		imaging.RGBAPreview(1, 1, []uint8{0, 0, 0, 255}),
		imaging.GrayPreview(1, 1, []uint8{0}),
	)
	if err == nil {
		t.Fatal("CombineComparison returned nil error, want gray left-source failure")
	}
}

func TestCombineComparisonRequiresMatchingDimensions(t *testing.T) {
	_, err := CombineComparison(
		imaging.GrayPreview(1, 1, []uint8{0}),
		imaging.GrayPreview(2, 1, []uint8{0, 0}),
	)
	if err == nil {
		t.Fatal("CombineComparison returned nil error, want dimension mismatch failure")
	}
}
