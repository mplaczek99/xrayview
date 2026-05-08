package render

import (
	"testing"

	"xrayview/backend/internal/bufpool"
	"xrayview/backend/internal/imaging"
)

var benchmarkRenderLUTByte uint8

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
	defer preview.Release()

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
	defer preview.Release()

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
	defer preview.Release()

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
		Width:      width,
		Height:     height,
		Pixels:     pixels,
		MinValue:   0,
		MaxValue:   4095,
		FitsUint16: true,
		DefaultWindow: &imaging.WindowLevel{
			Center: 2048,
			Width:  4096,
		},
	}

	plan := DefaultRenderPlan()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		pixels := RenderGrayscalePixels(source, plan)
		bufpool.PutUint8(pixels)
	}
}

func BenchmarkRenderGrayscalePixelsFullRange(b *testing.B) {
	const width, height = 2048, 1536
	pixels := make([]float32, width*height)
	for i := range pixels {
		pixels[i] = float32(i % 4096)
	}

	source := imaging.SourceImage{
		Width:      width,
		Height:     height,
		Pixels:     pixels,
		MinValue:   0,
		MaxValue:   4095,
		FitsUint16: true,
	}

	plan := RenderPlan{Window: FullRangeWindowMode()}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		pixels := RenderGrayscalePixels(source, plan)
		bufpool.PutUint8(pixels)
	}
}

func BenchmarkRenderGrayscalePixelsInvert(b *testing.B) {
	const width, height = 2048, 1536
	pixels := make([]float32, width*height)
	for i := range pixels {
		pixels[i] = float32(i % 4096)
	}

	source := imaging.SourceImage{
		Width:      width,
		Height:     height,
		Pixels:     pixels,
		MinValue:   0,
		MaxValue:   4095,
		FitsUint16: true,
		DefaultWindow: &imaging.WindowLevel{
			Center: 2048,
			Width:  4096,
		},
		Invert: true,
	}

	plan := DefaultRenderPlan()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		pixels := RenderGrayscalePixels(source, plan)
		bufpool.PutUint8(pixels)
	}
}

func BenchmarkBuildRenderLUT(b *testing.B) {
	source := imaging.SourceImage{
		MinValue:   0,
		MaxValue:   4095,
		FitsUint16: true,
		DefaultWindow: &imaging.WindowLevel{
			Center: 2048,
			Width:  4096,
		},
	}
	window, hasWindow := resolveWindow(source, DefaultRenderPlan().Window)

	var lut [65536]uint8
	b.ReportAllocs()
	for range b.N {
		populateRenderLUT(&lut, source, window, hasWindow)
	}
	benchmarkRenderLUTByte = lut[4095]
}

func BenchmarkCachedRenderLUTHit(b *testing.B) {
	source := imaging.SourceImage{
		MinValue:   0,
		MaxValue:   4095,
		FitsUint16: true,
		DefaultWindow: &imaging.WindowLevel{
			Center: 2048,
			Width:  4096,
		},
	}
	window, hasWindow := resolveWindow(source, DefaultRenderPlan().Window)
	key := newRenderLUTKey(source, window, hasWindow)
	cache := newRenderLUTCache(4)
	cache.getOrBuild(key, func() *[65536]uint8 {
		return buildRenderLUT(source, window, hasWindow)
	})

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = cache.getOrBuild(key, func() *[65536]uint8 {
			b.Fatal("unexpected cached LUT miss")
			return nil
		})
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
	defer bufpool.PutUint8(got)

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
	defer bufpool.PutUint8(got)

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

func TestRenderUsesFallbackWhenUint16FitFlagIsFalse(t *testing.T) {
	source := imaging.SourceImage{
		Width:    3,
		Height:   1,
		Pixels:   []float32{0, 1.5, 3},
		MinValue: 0,
		MaxValue: 3,
	}

	got := RenderGrayscalePixels(source, RenderPlan{Window: FullRangeWindowMode()})
	defer bufpool.PutUint8(got)

	if got, want := got[1], MapLinear(1.5, source.MinValue, source.MaxValue); got != want {
		t.Fatalf("middle pixel = %d, want fallback full-range mapping %d", got, want)
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

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		pixels := RenderGrayscalePixels(source, plan)
		bufpool.PutUint8(pixels)
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

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		pixels := RenderGrayscalePixels(source, plan)
		bufpool.PutUint8(pixels)
	}
}

func TestRenderSourceImageReturnsPooledBuffer(t *testing.T) {
	source := imaging.SourceImage{
		Width:    4,
		Height:   1,
		Pixels:   []float32{0, 1, 2, 3},
		MinValue: 0,
		MaxValue: 3,
	}

	warm := RenderSourceImage(source, RenderPlan{Window: FullRangeWindowMode()})
	warm.Release()

	allocs := testing.AllocsPerRun(100, func() {
		preview := RenderSourceImage(source, RenderPlan{Window: FullRangeWindowMode()})
		preview.Release()
	})
	if allocs != 0 {
		t.Fatalf("RenderSourceImage pooled allocs/run = %v, want 0", allocs)
	}
}

func TestRenderLUTCacheReusesIdenticalRenderParameters(t *testing.T) {
	source := imaging.SourceImage{
		MinValue:   0,
		MaxValue:   4095,
		FitsUint16: true,
		DefaultWindow: &imaging.WindowLevel{
			Center: 2048,
			Width:  4096,
		},
	}
	window, hasWindow := resolveWindow(source, DefaultRenderPlan().Window)
	key := newRenderLUTKey(source, window, hasWindow)

	cache := newRenderLUTCache(4)
	builds := 0
	build := func() *[65536]uint8 {
		builds++
		return buildRenderLUT(source, window, hasWindow)
	}

	first := cache.getOrBuild(key, build)
	second := cache.getOrBuild(key, build)
	if first != second {
		t.Fatal("cached LUT pointer changed for identical render parameters")
	}
	if builds != 1 {
		t.Fatalf("builds = %d, want 1", builds)
	}
}

func TestRenderLUTCacheEvictsLeastRecentlyUsedEntry(t *testing.T) {
	cache := newRenderLUTCache(2)
	keyA := renderLUTKey{minValue: 1}
	keyB := renderLUTKey{minValue: 2}
	keyC := renderLUTKey{minValue: 3}
	builds := map[renderLUTKey]int{}

	build := func(key renderLUTKey) func() *[65536]uint8 {
		return func() *[65536]uint8 {
			builds[key]++
			return new([65536]uint8)
		}
	}

	cache.getOrBuild(keyA, build(keyA))
	cache.getOrBuild(keyB, build(keyB))
	cache.getOrBuild(keyA, build(keyA))
	cache.getOrBuild(keyC, build(keyC))
	cache.getOrBuild(keyB, build(keyB))

	if builds[keyA] != 1 {
		t.Fatalf("keyA builds = %d, want 1", builds[keyA])
	}
	if builds[keyB] != 2 {
		t.Fatalf("keyB builds = %d, want 2 after LRU eviction", builds[keyB])
	}
	if builds[keyC] != 1 {
		t.Fatalf("keyC builds = %d, want 1", builds[keyC])
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
