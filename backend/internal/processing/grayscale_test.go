package processing

import (
	"testing"

	"xrayview/backend/internal/dicommeta"
	"xrayview/backend/internal/imaging"
	"xrayview/backend/internal/render"
	"xrayview/backend/internal/testfixtures"
)

func TestProcessGrayscalePixelsFixedOrderAppliesInvertBeforeBrightnessAndContrast(t *testing.T) {
	pixels := []uint8{100}
	mode := ProcessGrayscalePixels(pixels, GrayscaleControls{
		Invert:     true,
		Brightness: 20,
		Contrast:   2.0,
	})

	if got, want := pixels, []uint8{222}; !equalBytes(got, want) {
		t.Fatalf("pixels = %v, want %v", got, want)
	}
	if got, want := mode, "inverted grayscale with brightness +20 with contrast 2"; got != want {
		t.Fatalf("mode = %q, want %q", got, want)
	}
}

func TestProcessGrayscalePixelsBrightnessLookupClamps(t *testing.T) {
	pixels := []uint8{0, 250}
	mode := ProcessGrayscalePixels(pixels, GrayscaleControls{
		Brightness: 10,
		Contrast:   1.0,
	})

	if got, want := pixels, []uint8{10, 255}; !equalBytes(got, want) {
		t.Fatalf("pixels = %v, want %v", got, want)
	}
	if got, want := mode, "grayscale with brightness +10"; got != want {
		t.Fatalf("mode = %q, want %q", got, want)
	}
}

func TestProcessGrayscalePixelsContrastLookupRoundsAndClamps(t *testing.T) {
	pixels := []uint8{0, 127, 128, 255}
	mode := ProcessGrayscalePixels(pixels, GrayscaleControls{
		Contrast: 2.0,
	})

	if got, want := pixels, []uint8{0, 126, 128, 255}; !equalBytes(got, want) {
		t.Fatalf("pixels = %v, want %v", got, want)
	}
	if got, want := mode, "grayscale with contrast 2"; got != want {
		t.Fatalf("mode = %q, want %q", got, want)
	}
}

func TestProcessGrayscalePixelsEqualizeRedistributesHistogram(t *testing.T) {
	pixels := []uint8{0, 128, 128, 255}
	mode := ProcessGrayscalePixels(pixels, GrayscaleControls{
		Contrast: 1.0,
		Equalize: true,
	})

	if got, want := pixels, []uint8{0, 170, 170, 255}; !equalBytes(got, want) {
		t.Fatalf("pixels = %v, want %v", got, want)
	}
	if got, want := mode, "grayscale with histogram equalization"; got != want {
		t.Fatalf("mode = %q, want %q", got, want)
	}
}

func TestProcessGrayscalePixelsEqualizeRunsAfterPointOperations(t *testing.T) {
	pixels := []uint8{0, 50, 200, 200}
	mode := ProcessGrayscalePixels(pixels, GrayscaleControls{
		Brightness: 20,
		Contrast:   1.0,
		Equalize:   true,
	})

	if got, want := pixels, []uint8{0, 85, 255, 255}; !equalBytes(got, want) {
		t.Fatalf("pixels = %v, want %v", got, want)
	}
	if got, want := mode, "grayscale with brightness +20 with histogram equalization"; got != want {
		t.Fatalf("mode = %q, want %q", got, want)
	}
}

func TestProcessGrayscalePixelsEqualizeLeavesFlatImageUntouched(t *testing.T) {
	pixels := []uint8{42, 42, 42}
	ProcessGrayscalePixels(pixels, GrayscaleControls{
		Contrast: 1.0,
		Equalize: true,
	})

	if got, want := pixels, []uint8{42, 42, 42}; !equalBytes(got, want) {
		t.Fatalf("pixels = %v, want %v", got, want)
	}
}

func TestProcessPreviewImageRequiresGrayPreviewInput(t *testing.T) {
	_, _, err := ProcessPreviewImage(imaging.RGBAPreview(1, 1, []uint8{0, 0, 0, 255}), GrayscaleControls{})
	if err == nil {
		t.Fatal("ProcessPreviewImage returned nil error, want gray8 validation failure")
	}
}

func TestProcessPreviewImageMatchesGrayscaleFixture(t *testing.T) {
	study, err := dicommeta.DecodeFile(sampleDicomPath(t))
	if err != nil {
		t.Fatalf("DecodeFile returned error: %v", err)
	}

	preview := render.RenderSourceImage(study.Image, render.DefaultRenderPlan())
	processed, mode, err := ProcessPreviewImage(preview, GrayscaleControls{
		Brightness: 10,
		Contrast:   1.4,
		Equalize:   true,
	})
	if err != nil {
		t.Fatalf("ProcessPreviewImage returned error: %v", err)
	}

	if got, want := mode, "grayscale with brightness +10 with contrast 1.4 with histogram equalization"; got != want {
		t.Fatalf("mode = %q, want %q", got, want)
	}

	want := testfixtures.SampleProcessedPreview()
	if got, want := processed.Width, want.Width; got != want {
		t.Fatalf("processed width = %d, want %d", got, want)
	}
	if got, want := processed.Height, want.Height; got != want {
		t.Fatalf("processed height = %d, want %d", got, want)
	}
	if got, want := processed.Pixels, want.Pixels; !equalBytes(got, want) {
		t.Fatalf("processed preview does not match the grayscale fixture")
	}
}

func sampleDicomPath(t *testing.T) string {
	t.Helper()

	return testfixtures.WriteSampleDicom(t)
}

func BenchmarkProcessGrayscalePixels(b *testing.B) {
	// Typical dental radiograph: 2048x1536 = ~3M pixels
	const width, height = 2048, 1536
	size := width * height

	makePixels := func() []uint8 {
		pixels := make([]uint8, size)
		for i := range pixels {
			pixels[i] = uint8(i % 256)
		}
		return pixels
	}

	b.Run("identity", func(b *testing.B) {
		pixels := makePixels()
		controls := GrayscaleControls{Contrast: 1.0}
		b.SetBytes(int64(size))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ProcessGrayscalePixels(pixels, controls)
		}
	})

	b.Run("invert+brightness+contrast", func(b *testing.B) {
		pixels := makePixels()
		controls := GrayscaleControls{
			Invert:     true,
			Brightness: 20,
			Contrast:   1.5,
		}
		b.SetBytes(int64(size))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ProcessGrayscalePixels(pixels, controls)
		}
	})

	b.Run("invert+brightness+contrast+equalize", func(b *testing.B) {
		controls := GrayscaleControls{
			Invert:     true,
			Brightness: 20,
			Contrast:   1.5,
			Equalize:   true,
		}
		b.SetBytes(int64(size))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pixels := makePixels()
			ProcessGrayscalePixels(pixels, controls)
		}
	})
}

func BenchmarkApplyLookupInPlace(b *testing.B) {
	const size = 2048 * 1536
	pixels := make([]uint8, size)
	for i := range pixels {
		pixels[i] = uint8(i % 256)
	}
	var lookup [256]uint8
	for i := range lookup {
		lookup[i] = 255 - uint8(i)
	}
	b.SetBytes(int64(size))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		applyLookupInPlace(pixels, &lookup)
	}
}

func equalBytes(left, right []uint8) bool {
	if len(left) != len(right) {
		return false
	}

	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}

	return true
}
