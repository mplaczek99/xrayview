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

func TestCombineComparisonParallelPathMatchesExpectedPixels(t *testing.T) {
	const (
		width  = 257
		height = 256
	)
	leftPixels := benchmarkGrayComparePixels(width * height)
	rightGrayPixels := benchmarkGrayComparePixels(width * height)
	rightRGBAPixels := benchmarkRGBAComparePixels(width * height)
	left := imaging.GrayPreview(width, height, leftPixels)

	for _, testCase := range []struct {
		name  string
		right imaging.PreviewImage
	}{
		{name: "gray_right", right: imaging.GrayPreview(width, height, rightGrayPixels)},
		{name: "rgba_right", right: imaging.RGBAPreview(width, height, rightRGBAPixels)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := CombineComparison(left, testCase.right)
			if err != nil {
				t.Fatalf("CombineComparison returned error: %v", err)
			}
			defer got.Release()

			for _, point := range [][2]int{
				{0, 0},
				{width - 1, 0},
				{width / 2, height / 2},
				{0, height - 1},
				{width - 1, height - 1},
			} {
				x, y := point[0], point[1]
				leftValue := leftPixels[y*width+x]
				leftBase := (y*width*2 + x) * 4
				if want := []uint8{leftValue, leftValue, leftValue, 255}; !equalBytes(got.Pixels[leftBase:leftBase+4], want) {
					t.Fatalf("left pixel (%d,%d) = %v, want %v", x, y, got.Pixels[leftBase:leftBase+4], want)
				}

				rightBase := (y*width*2 + width + x) * 4
				var want []uint8
				switch testCase.right.Format {
				case imaging.FormatGray8:
					rightValue := rightGrayPixels[y*width+x]
					want = []uint8{rightValue, rightValue, rightValue, 255}
				case imaging.FormatRGBA8:
					srcBase := (y*width + x) * 4
					want = rightRGBAPixels[srcBase : srcBase+4]
				default:
					t.Fatalf("unexpected right format %q", testCase.right.Format)
				}
				if !equalBytes(got.Pixels[rightBase:rightBase+4], want) {
					t.Fatalf("right pixel (%d,%d) = %v, want %v", x, y, got.Pixels[rightBase:rightBase+4], want)
				}
			}
		})
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

func BenchmarkCombineComparisonGrayRight(b *testing.B) {
	const (
		width  = 2048
		height = 1536
	)
	leftPixels := benchmarkGrayComparePixels(width * height)
	rightPixels := benchmarkGrayComparePixels(width * height)
	left := imaging.GrayPreview(width, height, leftPixels)
	right := imaging.GrayPreview(width, height, rightPixels)

	warmup, err := CombineComparison(left, right)
	if err != nil {
		b.Fatalf("CombineComparison returned error: %v", err)
	}
	warmup.Release()

	b.ReportAllocs()
	b.SetBytes(int64(width * height * 2 * 4))
	b.ResetTimer()
	for range b.N {
		got, err := CombineComparison(left, right)
		if err != nil {
			b.Fatalf("CombineComparison returned error: %v", err)
		}
		got.Release()
	}
}

func BenchmarkCombineComparisonRGBARight(b *testing.B) {
	const (
		width  = 2048
		height = 1536
	)
	leftPixels := benchmarkGrayComparePixels(width * height)
	rightPixels := benchmarkRGBAComparePixels(width * height)
	left := imaging.GrayPreview(width, height, leftPixels)
	right := imaging.RGBAPreview(width, height, rightPixels)

	warmup, err := CombineComparison(left, right)
	if err != nil {
		b.Fatalf("CombineComparison returned error: %v", err)
	}
	warmup.Release()

	b.ReportAllocs()
	b.SetBytes(int64(width * height * 2 * 4))
	b.ResetTimer()
	for range b.N {
		got, err := CombineComparison(left, right)
		if err != nil {
			b.Fatalf("CombineComparison returned error: %v", err)
		}
		got.Release()
	}
}

func benchmarkGrayComparePixels(count int) []uint8 {
	pixels := make([]uint8, count)
	for index := range pixels {
		pixels[index] = uint8((index*37 + index/257) & 0xff)
	}

	return pixels
}

func benchmarkRGBAComparePixels(count int) []uint8 {
	pixels := make([]uint8, count*4)
	for index := 0; index < count; index++ {
		value := uint8((index*37 + index/257) & 0xff)
		base := index * 4
		pixels[base] = value
		pixels[base+1] = value / 2
		pixels[base+2] = 255 - value
		pixels[base+3] = 255
	}

	return pixels
}
