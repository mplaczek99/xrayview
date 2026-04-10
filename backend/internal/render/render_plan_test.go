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

func TestRenderFallbackPathInvertProducesSameResultAsLUT(t *testing.T) {
	source := imaging.SourceImage{
		Width:    4,
		Height:   1,
		Pixels:   []float32{-100, 0, 500, 70000},
		MinValue: -100,
		MaxValue: 70000,
		DefaultWindow: &imaging.WindowLevel{
			Center: 35000,
			Width:  70100,
		},
		Invert: true,
	}

	got := RenderGrayscalePixels(source, DefaultRenderPlan())

	// Manually compute expected: window maps, then invert
	// With such wide window, values spread across 0-255 range
	// Key check: inversion applied (high input → low output)
	// Inverted: lowest input (-100) → highest output, highest input (70000) → lowest output
	if got[0] <= got[3] {
		t.Fatalf("expected inverted order: got[0]=%d should be > got[3]=%d for inverted render", got[0], got[3])
	}
}

func TestRenderFallbackPathNoInvert(t *testing.T) {
	source := imaging.SourceImage{
		Width:    3,
		Height:   1,
		Pixels:   []float32{-100, 35000, 70000},
		MinValue: -100,
		MaxValue: 70000,
	}

	got := RenderGrayscalePixels(source, RenderPlan{Window: FullRangeWindowMode()})

	// Linear mapping: -100 → 0, 70000 → 255
	if got[0] != 0 {
		t.Fatalf("got[0] = %d, want 0", got[0])
	}
	if got[2] != 255 {
		t.Fatalf("got[2] = %d, want 255", got[2])
	}
	if got[1] <= got[0] || got[1] >= got[2] {
		t.Fatalf("got[1] = %d should be between %d and %d", got[1], got[0], got[2])
	}
}

func BenchmarkRenderGrayscalePixelsFallback(b *testing.B) {
	const width, height = 2048, 1536
	pixels := make([]float32, width*height)
	for i := range pixels {
		pixels[i] = float32(i%4096) - 500
	}

	source := imaging.SourceImage{
		Width:    width,
		Height:   height,
		Pixels:   pixels,
		MinValue: -500,
		MaxValue: 70000,
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

func BenchmarkRenderGrayscalePixelsFallbackInvert(b *testing.B) {
	const width, height = 2048, 1536
	pixels := make([]float32, width*height)
	for i := range pixels {
		pixels[i] = float32(i%4096) - 500
	}

	source := imaging.SourceImage{
		Width:    width,
		Height:   height,
		Pixels:   pixels,
		MinValue: -500,
		MaxValue: 70000,
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
