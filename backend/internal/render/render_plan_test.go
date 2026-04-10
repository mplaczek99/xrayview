package render

import (
	"testing"

	"xrayview/backend/internal/imaging"
)

func TestRenderSourceImageUsesEmbeddedWindowByDefault(t *testing.T) {
	source := imaging.SourceImage{
		Width:    3,
		Height:   1,
		Pixels:   []float32{0, 127.5, 255},
		MinValue: 0,
		MaxValue: 255,
		DefaultWindow: &imaging.WindowLevel{
			Center: 128,
			Width:  256,
		},
	}

	preview := RenderSourceImage(source, DefaultRenderPlan())

	if got, want := preview.Format, imaging.FormatGray8; got != want {
		t.Fatalf("Format = %q, want %q", got, want)
	}
	if got, want := preview.Pixels, []uint8{0, 128, 255}; !equalBytes(got, want) {
		t.Fatalf("Pixels = %v, want %v", got, want)
	}
}

func TestRenderSourceImageFullRangeIgnoresEmbeddedWindow(t *testing.T) {
	source := imaging.SourceImage{
		Width:    3,
		Height:   1,
		Pixels:   []float32{0, 64, 128},
		MinValue: 0,
		MaxValue: 128,
		DefaultWindow: &imaging.WindowLevel{
			Center: 32,
			Width:  64,
		},
	}

	preview := RenderSourceImage(source, RenderPlan{
		Window: FullRangeWindowMode(),
	})

	if got, want := preview.Pixels, []uint8{0, 128, 255}; !equalBytes(got, want) {
		t.Fatalf("Pixels = %v, want %v", got, want)
	}
}

func TestRenderSourceImageAppliesSourceInvertAfterWindowing(t *testing.T) {
	source := imaging.SourceImage{
		Width:    3,
		Height:   1,
		Pixels:   []float32{0, 127.5, 255},
		MinValue: 0,
		MaxValue: 255,
		DefaultWindow: &imaging.WindowLevel{
			Center: 128,
			Width:  256,
		},
		Invert: true,
	}

	preview := RenderSourceImage(source, DefaultRenderPlan())

	if got, want := preview.Pixels, []uint8{255, 127, 0}; !equalBytes(got, want) {
		t.Fatalf("Pixels = %v, want %v", got, want)
	}
}

func BenchmarkRenderGrayscalePixels(b *testing.B) {
	const width, height = 2048, 1536
	pixels := make([]float32, width*height)
	for i := range pixels {
		pixels[i] = float32(i % 4096)
	}

	source := imaging.SourceImage{
		Width:    width,
		Height:   height,
		Pixels:   pixels,
		MinValue: 0,
		MaxValue: 4095,
		DefaultWindow: &imaging.WindowLevel{
			Center: 2048,
			Width:  4096,
		},
	}

	plan := DefaultRenderPlan()

	b.ResetTimer()
	for range b.N {
		RenderGrayscalePixels(source, plan)
	}
}

func BenchmarkRenderGrayscalePixelsFullRange(b *testing.B) {
	const width, height = 2048, 1536
	pixels := make([]float32, width*height)
	for i := range pixels {
		pixels[i] = float32(i % 4096)
	}

	source := imaging.SourceImage{
		Width:    width,
		Height:   height,
		Pixels:   pixels,
		MinValue: 0,
		MaxValue: 4095,
	}

	plan := RenderPlan{Window: FullRangeWindowMode()}

	b.ResetTimer()
	for range b.N {
		RenderGrayscalePixels(source, plan)
	}
}

func BenchmarkRenderGrayscalePixelsInvert(b *testing.B) {
	const width, height = 2048, 1536
	pixels := make([]float32, width*height)
	for i := range pixels {
		pixels[i] = float32(i % 4096)
	}

	source := imaging.SourceImage{
		Width:    width,
		Height:   height,
		Pixels:   pixels,
		MinValue: 0,
		MaxValue: 4095,
		DefaultWindow: &imaging.WindowLevel{
			Center: 2048,
			Width:  4096,
		},
		Invert: true,
	}

	plan := DefaultRenderPlan()

	b.ResetTimer()
	for range b.N {
		RenderGrayscalePixels(source, plan)
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
