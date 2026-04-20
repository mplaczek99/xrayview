package analysis

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"xrayview/backend/internal/bufpool"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/imaging"
)

func TestLocalGradientClampsAtImageEdges(t *testing.T) {
	pixels := []uint8{
		10, 20, 30,
		40, 50, 60,
		70, 80, 90,
	}

	if got, want := localGradient(pixels, 3, 3, 0, 0), uint8(40); got != want {
		t.Fatalf("localGradient(corner) = %d, want %d", got, want)
	}
	if got, want := localGradient(pixels, 3, 3, 1, 1), uint8(80); got != want {
		t.Fatalf("localGradient(center) = %d, want %d", got, want)
	}
}

func TestHistogramPercentileMatchesTargetRounding(t *testing.T) {
	var histogram [256]uint32
	histogram[10] = 2
	histogram[20] = 2
	histogram[30] = 1

	if got, want := histogramPercentile(histogram, 5, 0.00), uint8(10); got != want {
		t.Fatalf("histogramPercentile(0.00) = %d, want %d", got, want)
	}
	if got, want := histogramPercentile(histogram, 5, 0.50), uint8(20); got != want {
		t.Fatalf("histogramPercentile(0.50) = %d, want %d", got, want)
	}
	if got, want := histogramPercentile(histogram, 5, 0.99), uint8(30); got != want {
		t.Fatalf("histogramPercentile(0.99) = %d, want %d", got, want)
	}
}

func TestNormalizePixelsStretchesPercentileWindow(t *testing.T) {
	pixels := []uint8{10, 20, 30, 40, 50}

	got := normalizePixels(pixels)
	want := []uint8{0, 63, 127, 191, 255}

	if !equalBytes(got, want) {
		t.Fatalf("normalizePixels = %v, want %v", got, want)
	}
}

func TestOpenBinaryMaskRemovesSinglePixelNoise(t *testing.T) {
	mask := []uint8{
		0, 0, 0,
		0, 1, 0,
		0, 0, 0,
	}

	got := openBinaryMask(mask, 3, 3)
	want := make([]uint8, len(mask))

	if !equalBytes(got, want) {
		t.Fatalf("openBinaryMask = %v, want %v", got, want)
	}
}

func TestCloseBinaryMaskFillsSinglePixelHole(t *testing.T) {
	mask := []uint8{
		1, 1, 1,
		1, 0, 1,
		1, 1, 1,
	}

	got := closeBinaryMask(mask, 3, 3)
	want := []uint8{
		1, 1, 1,
		1, 1, 1,
		1, 1, 1,
	}

	if !equalBytes(got, want) {
		t.Fatalf("closeBinaryMask = %v, want %v", got, want)
	}
}

func TestCollectCandidatesAndPrimarySelection(t *testing.T) {
	const width = 100
	const height = 100

	mask := make([]uint8, width*height)
	normalized := make([]uint8, width*height)
	toothness := make([]uint8, width*height)

	fillBoolRect(mask, width, 10, 20, 10, 30, 1)
	fillBoolRect(mask, width, 45, 25, 10, 30, 1)
	fillByteRect(normalized, width, 10, 20, 10, 30, 210)
	fillByteRect(normalized, width, 45, 25, 10, 30, 210)
	fillByteRect(toothness, width, 10, 20, 10, 30, 210)
	fillByteRect(toothness, width, 45, 25, 10, 30, 210)

	search := searchRegion{x: 0, y: 0, width: width, height: height}
	candidates := collectCandidates(mask, normalized, toothness, width, height, search)
	if got, want := len(candidates), 2; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}

	detected := selectDetectedCandidates(candidates)
	if got, want := len(detected), 2; got != want {
		t.Fatalf("len(detected) = %d, want %d", got, want)
	}
	if got, want := detected[0].bbox.X, uint32(10); got != want {
		t.Fatalf("detected[0].bbox.X = %d, want %d", got, want)
	}
	if got, want := detected[1].bbox.X, uint32(45); got != want {
		t.Fatalf("detected[1].bbox.X = %d, want %d", got, want)
	}

	primary := selectPrimaryCandidate(detected)
	if primary == nil {
		t.Fatal("selectPrimaryCandidate returned nil")
	}
	if got, want := primary.bbox.X, uint32(45); got != want {
		t.Fatalf("primary.bbox.X = %d, want %d", got, want)
	}
	if !primary.strict {
		t.Fatal("primary.strict = false, want true")
	}
}

func TestGeometryFromPixelsExtractsLongestWidthAndHeightLines(t *testing.T) {
	bbox := contracts.BoundingBox{
		X:      10,
		Y:      20,
		Width:  6,
		Height: 6,
	}

	coordinates := [][2]uint32{
		{13, 20},
		{11, 21}, {12, 21}, {13, 21}, {14, 21},
		{13, 22},
		{13, 23},
		{13, 24},
	}
	pixels := make([]int, 0, len(coordinates))
	for _, coordinate := range coordinates {
		pixels = append(pixels, int(coordinate[1]*32+coordinate[0]))
	}

	geometry := geometryFromPixels(pixels, bbox, 32)

	if got, want := geometry.WidthLine.Start, (contracts.Point{X: 11, Y: 21}); got != want {
		t.Fatalf("WidthLine.Start = %#v, want %#v", got, want)
	}
	if got, want := geometry.WidthLine.End, (contracts.Point{X: 14, Y: 21}); got != want {
		t.Fatalf("WidthLine.End = %#v, want %#v", got, want)
	}
	if got, want := geometry.HeightLine.Start, (contracts.Point{X: 13, Y: 20}); got != want {
		t.Fatalf("HeightLine.Start = %#v, want %#v", got, want)
	}
	if got, want := geometry.HeightLine.End, (contracts.Point{X: 13, Y: 24}); got != want {
		t.Fatalf("HeightLine.End = %#v, want %#v", got, want)
	}
}

func TestAnalyzePreviewRejectsNonGrayPreview(t *testing.T) {
	_, err := AnalyzePreview(imaging.RGBAPreview(1, 1, []uint8{0, 0, 0, 255}), nil)
	if err == nil {
		t.Fatal("AnalyzePreview returned nil error, want unsupported format error")
	}
}

func TestAnalyzePreviewSyntheticToothPrefersTallCentralCandidate(t *testing.T) {
	analysis, err := AnalyzePreview(syntheticToothPreview(), nil)
	if err != nil {
		t.Fatalf("AnalyzePreview returned error: %v", err)
	}

	if analysis.Tooth == nil {
		t.Fatal("analysis.Tooth = nil, want candidate")
	}
	if analysis.Tooth.Confidence < 0.5 {
		t.Fatalf("analysis.Tooth.Confidence = %v, want >= 0.5", analysis.Tooth.Confidence)
	}
	if analysis.Tooth.Geometry.BoundingBox.X <= 80 || analysis.Tooth.Geometry.BoundingBox.X >= 130 {
		t.Fatalf("analysis.Tooth.Geometry.BoundingBox.X = %d, want between 80 and 130", analysis.Tooth.Geometry.BoundingBox.X)
	}
	if analysis.Tooth.Measurements.Pixel.ToothHeight <= analysis.Tooth.Measurements.Pixel.ToothWidth {
		t.Fatalf(
			"tooth height = %v, width = %v, want height > width",
			analysis.Tooth.Measurements.Pixel.ToothHeight,
			analysis.Tooth.Measurements.Pixel.ToothWidth,
		)
	}
}

func TestAnalyzePreviewCalibrationGeneratesMillimeterMeasurements(t *testing.T) {
	analysis, err := AnalyzePreview(
		syntheticToothPreview(),
		&contracts.MeasurementScale{
			RowSpacingMM:    0.2,
			ColumnSpacingMM: 0.3,
			Source:          "PixelSpacing",
		},
	)
	if err != nil {
		t.Fatalf("AnalyzePreview returned error: %v", err)
	}

	if analysis.Tooth == nil {
		t.Fatal("analysis.Tooth = nil, want candidate")
	}
	calibrated := analysis.Tooth.Measurements.Calibrated
	if calibrated == nil {
		t.Fatal("analysis.Tooth.Measurements.Calibrated = nil, want measurements")
	}
	if got, want := calibrated.Units, millimeterUnits; got != want {
		t.Fatalf("calibrated.Units = %q, want %q", got, want)
	}
	if calibrated.ToothWidth <= 0 || calibrated.ToothHeight <= 0 {
		t.Fatalf("calibrated = %#v, want positive millimeter measurements", calibrated)
	}
}

func TestAnalyzePreviewSamplePreviewReturnsMultipleDetectedTeeth(t *testing.T) {
	analysis, err := AnalyzePreview(loadAnalyzePreviewFixture(t), nil)
	if err != nil {
		t.Fatalf("AnalyzePreview returned error: %v", err)
	}

	if len(analysis.Teeth) <= 1 {
		t.Fatalf("len(analysis.Teeth) = %d, want > 1", len(analysis.Teeth))
	}
}

func TestAnalyzePreviewSamplePreviewReturnsCandidateOrStructuredWarning(t *testing.T) {
	analysis, err := AnalyzePreview(loadAnalyzePreviewFixture(t), nil)
	if err != nil {
		t.Fatalf("AnalyzePreview returned error: %v", err)
	}

	if got, want := analysis.Image.Width, uint32(2048); got != want {
		t.Fatalf("analysis.Image.Width = %d, want %d", got, want)
	}
	if got, want := analysis.Image.Height, uint32(1088); got != want {
		t.Fatalf("analysis.Image.Height = %d, want %d", got, want)
	}
	if len(analysis.Teeth) == 0 && len(analysis.Warnings) == 0 {
		t.Fatal("analysis returned neither teeth nor warnings")
	}
	if got, want := analysis.Tooth != nil, len(analysis.Teeth) > 0; got != want {
		t.Fatalf("analysis.Tooth != nil = %t, want %t", got, want)
	}
}

func TestAnalyzePreviewSamplePreviewPassesLooseDetectionSanity(t *testing.T) {
	preview := loadAnalyzePreviewFixture(t)
	analysis, err := AnalyzePreview(preview, nil)
	if err != nil {
		t.Fatalf("AnalyzePreview returned error: %v", err)
	}
	if analysis.Tooth == nil {
		t.Fatal("analysis.Tooth = nil, want detected primary candidate")
	}

	search := defaultSearchRegion(preview.Width, preview.Height)
	searchArea := float64(maxUint32(search.area(), 1))
	candidateCount := len(analysis.Teeth)
	if candidateCount < 24 || candidateCount > 32 {
		t.Fatalf("len(analysis.Teeth) = %d, want within panoramic coverage band [24, 32]", candidateCount)
	}

	areas := make([]uint32, 0, len(analysis.Teeth))
	var maxArea uint32
	upperCenters := make([]uint32, 0, len(analysis.Teeth))
	lowerCenters := make([]uint32, 0, len(analysis.Teeth))
	searchMidY := search.y + search.height/2
	for _, candidate := range analysis.Teeth {
		areas = append(areas, candidate.MaskAreaPixels)
		maxArea = maxUint32(maxArea, candidate.MaskAreaPixels)
		centerY := candidate.Geometry.BoundingBox.Y + candidate.Geometry.BoundingBox.Height/2
		centerX := candidate.Geometry.BoundingBox.X + candidate.Geometry.BoundingBox.Width/2
		if centerY < searchMidY {
			upperCenters = append(upperCenters, centerX)
		} else {
			lowerCenters = append(lowerCenters, centerX)
		}
	}
	medianArea := medianUint32(areas)
	if medianArea < 250 || medianArea > 5000 {
		t.Fatalf("median candidate area = %d, want within loose sanity band [250, 5000]", medianArea)
	}
	if got := float64(maxArea) / searchArea; got > 0.04 {
		t.Fatalf("largest candidate area ratio = %.4f, want <= 0.04 of search region", got)
	}
	if got := float64(maxArea) / float64(maxUint32(medianArea, 1)); got > 12.0 {
		t.Fatalf("largest candidate area / median area = %.2f, want <= 12.0", got)
	}
	if got := float64(analysis.Tooth.MaskAreaPixels) / float64(maxUint32(medianArea, 1)); got > 8.0 {
		t.Fatalf("primary candidate area / median area = %.2f, want <= 8.0", got)
	}
	if len(upperCenters) < 12 || len(lowerCenters) < 12 {
		t.Fatalf("upper/lower candidate split = %d/%d, want at least 12 candidates per arch", len(upperCenters), len(lowerCenters))
	}

	largeStraddlingCandidates := 0
	for _, candidate := range analysis.Teeth {
		bbox := candidate.Geometry.BoundingBox
		bboxBottom := bbox.Y + bbox.Height - 1
		if bbox.Y <= searchMidY && bboxBottom >= searchMidY &&
			candidate.MaskAreaPixels > maxUint32(medianArea*3, 900) {
			largeStraddlingCandidates++
		}
	}
	if largeStraddlingCandidates > 1 {
		t.Fatalf(
			"large candidates spanning the arch midline = %d, want <= 1 to avoid upper/lower arch collapse",
			largeStraddlingCandidates,
		)
	}
	if gap := maxCenterGap(upperCenters); gap > 220 {
		t.Fatalf("upper-arch max center gap = %d px, want <= 220 to avoid missing anterior teeth", gap)
	}
	if gap := maxCenterGap(lowerCenters); gap > 220 {
		t.Fatalf("lower-arch max center gap = %d px, want <= 220 to avoid missing anterior teeth", gap)
	}
}

func TestAnalyzePreviewSamplePreviewRoughlyMatchesReferenceToothLayout(t *testing.T) {
	preview := loadAnalyzePreviewFixture(t)
	analysis, err := AnalyzePreview(preview, nil)
	if err != nil {
		t.Fatalf("AnalyzePreview returned error: %v", err)
	}

	type refBox struct {
		x int
		y int
		w int
		h int
	}
	reference := []refBox{
		{1162, 375, 35, 245}, {813, 402, 44, 229}, {1390, 406, 72, 223}, {1030, 408, 40, 200},
		{1102, 408, 26, 194}, {953, 410, 28, 203}, {1249, 414, 67, 215}, {1491, 415, 82, 214},
		{1315, 420, 30, 209}, {573, 425, 23, 209}, {671, 435, 41, 205}, {744, 435, 41, 201},
		{481, 437, 93, 192}, {1021, 634, 49, 168}, {1069, 637, 34, 201}, {921, 639, 31, 163},
		{980, 643, 42, 157}, {1128, 644, 35, 225}, {1461, 644, 31, 194}, {1344, 647, 45, 201},
		{1198, 648, 50, 195}, {493, 650, 81, 212}, {1264, 650, 52, 192}, {595, 652, 77, 222},
		{711, 653, 34, 225}, {856, 654, 33, 211}, {784, 657, 30, 212},
	}
	type candidateBox struct {
		x int
		y int
		w int
		h int
	}
	generated := make([]candidateBox, 0, len(analysis.Teeth))
	for _, tooth := range analysis.Teeth {
		bbox := tooth.Geometry.BoundingBox
		generated = append(generated, candidateBox{
			x: int(bbox.X),
			y: int(bbox.Y),
			w: int(bbox.Width),
			h: int(bbox.Height),
		})
	}

	used := make([]bool, len(generated))
	totalCenterError := 0.0
	totalSizeError := 0.0
	worstScore := 0.0
	matchCount := 0
	for _, ref := range reference {
		bestIndex := -1
		bestScore := math.MaxFloat64
		refCenterX := float64(ref.x + ref.w/2)
		refCenterY := float64(ref.y + ref.h/2)
		for index, candidate := range generated {
			if used[index] {
				continue
			}
			candidateCenterX := float64(candidate.x + candidate.w/2)
			candidateCenterY := float64(candidate.y + candidate.h/2)
			centerScore := math.Abs(candidateCenterX-refCenterX) + math.Abs(candidateCenterY-refCenterY)*1.25
			sizeScore := math.Abs(float64(candidate.w-ref.w))*0.5 + math.Abs(float64(candidate.h-ref.h))*0.35
			score := centerScore + sizeScore
			if score < bestScore {
				bestScore = score
				bestIndex = index
			}
		}
		if bestIndex < 0 {
			continue
		}
		used[bestIndex] = true
		candidate := generated[bestIndex]
		candidateCenterX := float64(candidate.x + candidate.w/2)
		candidateCenterY := float64(candidate.y + candidate.h/2)
		totalCenterError += math.Abs(candidateCenterX-refCenterX) + math.Abs(candidateCenterY-refCenterY)
		totalSizeError += math.Abs(float64(candidate.w-ref.w)) + math.Abs(float64(candidate.h-ref.h))
		worstScore = math.Max(worstScore, bestScore)
		matchCount++
	}

	if matchCount != len(reference) {
		t.Fatalf("matched reference boxes = %d, want %d", matchCount, len(reference))
	}
	averageCenterError := totalCenterError / float64(matchCount)
	averageSizeError := totalSizeError / float64(matchCount)
	if averageCenterError > 126.0 {
		t.Fatalf("average center error = %.1f px, want <= 126.0", averageCenterError)
	}
	if averageSizeError > 90.0 {
		t.Fatalf("average size error = %.1f px, want <= 90.0", averageSizeError)
	}
	if worstScore > 610.0 {
		t.Fatalf("worst matched reference score = %.1f, want <= 610.0", worstScore)
	}
}

func syntheticToothPreview() imaging.PreviewImage {
	const width = 240
	const height = 160

	pixels := make([]uint8, width*height)
	for index := range pixels {
		pixels[index] = 24
	}

	fillByteRect(pixels, width, 14, 24, 212, 106, 54)
	fillByteRect(pixels, width, 38, 54, 34, 34, 174)
	fillTriangleRoot(pixels, width, 38, 88, 62, 32, 174)

	fillByteRect(pixels, width, 100, 42, 42, 38, 236)
	fillTriangleRoot(pixels, width, 100, 80, 92, 54, 236)

	fillByteRect(pixels, width, 172, 56, 28, 32, 160)
	fillTriangleRoot(pixels, width, 172, 88, 50, 30, 160)

	return imaging.GrayPreview(width, height, pixels)
}

func fillTriangleRoot(
	pixels []uint8,
	width uint32,
	x, y uint32,
	rootWidth, rootHeight uint32,
	value uint8,
) {
	centerX := x + rootWidth/2
	for offset := uint32(0); offset < rootHeight; offset++ {
		rowY := y + offset
		span := saturatingSubUint32(rootWidth, (offset*rootWidth)/rootHeight)
		halfSpan := span / 2
		startX := saturatingSubUint32(centerX, halfSpan)
		endX := minUint32(centerX+halfSpan, width-1)
		for xx := startX; xx <= endX; xx++ {
			pixels[rowY*width+xx] = value
		}
	}
}

func fillByteRect(
	pixels []uint8,
	width uint32,
	x, y uint32,
	rectWidth, rectHeight uint32,
	value uint8,
) {
	for yy := y; yy < y+rectHeight; yy++ {
		for xx := x; xx < x+rectWidth; xx++ {
			pixels[yy*width+xx] = value
		}
	}
}

func fillBoolRect(
	mask []uint8,
	width uint32,
	x, y uint32,
	rectWidth, rectHeight uint32,
	value uint8,
) {
	for yy := y; yy < y+rectHeight; yy++ {
		for xx := x; xx < x+rectWidth; xx++ {
			mask[yy*width+xx] = value
		}
	}
}

func loadAnalyzePreviewFixture(t testing.TB) imaging.PreviewImage {
	t.Helper()

	file, err := os.Open(repoPathFromHere(t, "backend", "tests", "fixtures", "parity", "sample-dental-radiograph", "analyze-preview.png"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer file.Close()

	decoded, err := png.Decode(file)
	if err != nil {
		t.Fatalf("png.Decode returned error: %v", err)
	}

	bounds := decoded.Bounds()
	pixels := make([]uint8, 0, bounds.Dx()*bounds.Dy())
	if gray, ok := decoded.(*image.Gray); ok {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			rowStart := gray.PixOffset(bounds.Min.X, y)
			rowEnd := rowStart + bounds.Dx()
			pixels = append(pixels, gray.Pix[rowStart:rowEnd]...)
		}
	} else {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				pixels = append(pixels, color.GrayModel.Convert(decoded.At(x, y)).(color.Gray).Y)
			}
		}
	}

	return imaging.GrayPreview(uint32(bounds.Dx()), uint32(bounds.Dy()), pixels)
}

func repoPathFromHere(t testing.TB, pathParts ...string) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller returned no file path")
	}

	parts := []string{filepath.Dir(currentFile), "..", "..", ".."}
	parts = append(parts, pathParts...)
	return filepath.Clean(filepath.Join(parts...))
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

func medianUint32(values []uint32) uint32 {
	if len(values) == 0 {
		return 0
	}

	sorted := append([]uint32(nil), values...)
	sort.Slice(sorted, func(left, right int) bool {
		return sorted[left] < sorted[right]
	})

	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

func maxCenterGap(values []uint32) uint32 {
	if len(values) < 2 {
		return 0
	}
	sorted := append([]uint32(nil), values...)
	sort.Slice(sorted, func(left, right int) bool {
		return sorted[left] < sorted[right]
	})
	var maxGap uint32
	for index := 1; index < len(sorted); index++ {
		gap := sorted[index] - sorted[index-1]
		if gap > maxGap {
			maxGap = gap
		}
	}
	return maxGap
}

func TestGaussianBlurGrayFastMatchesOriginal(t *testing.T) {
	const width = 2048
	const height = 1536
	pixels := make([]uint8, width*height)
	for i := range pixels {
		pixels[i] = uint8((i*7 + 13) % 256)
	}

	for _, sigma := range []float64{1.4, 9.0} {
		original := gaussianBlurGray(pixels, width, height, sigma)
		fast := gaussianBlurGrayFast(pixels, width, height, sigma)

		if len(original) != len(fast) {
			t.Fatalf("sigma=%.1f: length mismatch %d vs %d", sigma, len(original), len(fast))
		}

		maxDiff := 0
		diffs := 0
		for i := range original {
			d := int(original[i]) - int(fast[i])
			if d < 0 {
				d = -d
			}
			if d > maxDiff {
				maxDiff = d
			}
			if d > 0 {
				diffs++
			}
		}

		// Allow ±1 from rounding difference (math.Round vs +0.5 cast).
		if maxDiff > 1 {
			t.Fatalf("sigma=%.1f: max pixel diff = %d, want <= 1", sigma, maxDiff)
		}
		t.Logf("sigma=%.1f: %d/%d pixels differ by 1 (%.2f%%)", sigma, diffs, len(pixels), float64(diffs)/float64(len(pixels))*100)

		bufpool.PutUint8(original)
		bufpool.PutUint8(fast)
	}
}

func BenchmarkGaussianBlurDual(b *testing.B) {
	const width = 2048
	const height = 1536
	pixels := make([]uint8, width*height)
	for i := range pixels {
		pixels[i] = uint8(i % 256)
	}

	b.Run("Sequential", func(b *testing.B) {
		b.SetBytes(int64(len(pixels)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			small := gaussianBlurGray(pixels, width, height, 1.4)
			large := gaussianBlurGray(pixels, width, height, 9.0)
			bufpool.PutUint8(small)
			bufpool.PutUint8(large)
		}
	})

	b.Run("Fused", func(b *testing.B) {
		b.SetBytes(int64(len(pixels)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			small, large := dualGaussianBlurGray(pixels, width, height, 1.4, 9.0)
			bufpool.PutUint8(small)
			bufpool.PutUint8(large)
		}
	})
}

func BenchmarkAnalyzeGrayscalePixels(b *testing.B) {
	preview := syntheticToothPreview()
	b.SetBytes(int64(len(preview.Pixels)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := AnalyzePreview(preview, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestGaussianBlurIntegerMatchesFloat(t *testing.T) {
	const width = 2048
	const height = 1536
	pixels := make([]uint8, width*height)
	for i := range pixels {
		pixels[i] = uint8((i*7 + 13) % 256)
	}

	reference := gaussianBlurGrayFast(pixels, width, height, 1.4)
	integer := gaussianBlurGrayInteger(pixels, width, height)

	if len(reference) != len(integer) {
		t.Fatalf("length mismatch: %d vs %d", len(reference), len(integer))
	}

	maxDiff := 0
	diffs := 0
	for i := range reference {
		d := int(reference[i]) - int(integer[i])
		if d < 0 {
			d = -d
		}
		if d > maxDiff {
			maxDiff = d
		}
		if d > 0 {
			diffs++
		}
	}

	if maxDiff > 1 {
		t.Fatalf("max pixel diff = %d, want <= 1", maxDiff)
	}
	t.Logf("integer vs float: %d/%d pixels differ (%.2f%%), max diff = %d",
		diffs, len(pixels), float64(diffs)/float64(len(pixels))*100, maxDiff)

	bufpool.PutUint8(reference)
	bufpool.PutUint8(integer)
}

func BenchmarkGaussianBlurSmall(b *testing.B) {
	const width = 2048
	const height = 1536
	pixels := make([]uint8, width*height)
	for i := range pixels {
		pixels[i] = uint8(i % 256)
	}

	b.Run("Float", func(b *testing.B) {
		b.SetBytes(int64(len(pixels)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			out := gaussianBlurGrayFast(pixels, width, height, 1.4)
			bufpool.PutUint8(out)
		}
	})

	b.Run("Integer", func(b *testing.B) {
		b.SetBytes(int64(len(pixels)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			out := gaussianBlurGrayInteger(pixels, width, height)
			bufpool.PutUint8(out)
		}
	})
}

func BenchmarkCollectCandidates(b *testing.B) {
	// 2048x1536 mask with many small blobs + a few large blobs — mimics realistic
	// tooth segmentation output where most connected components are noise (area <= 150).
	const width = 2048
	const height = 1536
	mask := make([]uint8, width*height)
	normalized := make([]uint8, width*height)
	toothness := make([]uint8, width*height)

	// Fill with pseudo-random data for normalized / toothness.
	for i := range normalized {
		normalized[i] = uint8((i*7 + 13) % 256)
		toothness[i] = uint8((i*11 + 37) % 256)
	}

	// Scatter many small blobs (3x3, area=9) — noise components, rejected by area filter.
	for y := 50; y < height-50; y += 20 {
		for x := 50; x < width-50; x += 20 {
			for dy := 0; dy < 3; dy++ {
				for dx := 0; dx < 3; dx++ {
					mask[(y+dy)*width+(x+dx)] = 1
				}
			}
		}
	}
	// Add a few large blobs (120x12, area=1440) — surviving tooth candidates.
	for i := 0; i < 5; i++ {
		startX := 200 + i*300
		startY := 400
		for dy := 0; dy < 12; dy++ {
			for dx := 0; dx < 120; dx++ {
				mask[(startY+dy)*width+(startX+dx)] = 1
			}
		}
	}

	search := searchRegion{x: 0, y: 0, width: width, height: height}

	b.SetBytes(int64(width * height))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		candidates := collectCandidates(mask, normalized, toothness, uint32(width), uint32(height), search)
		_ = candidates
	}
}

func BenchmarkAnalyzePreviewSample(b *testing.B) {
	preview := loadAnalyzePreviewFixture(b)
	b.SetBytes(int64(len(preview.Pixels)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := AnalyzePreview(preview, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMorphologicalOps(b *testing.B) {
	const width = 2048
	const height = 1536
	// ~30% density mask — realistic for tooth segmentation results.
	mask := make([]uint8, width*height)
	for i := range mask {
		if i%3 == 0 {
			mask[i] = 1
		}
	}

	b.Run("Dilate", func(b *testing.B) {
		b.SetBytes(int64(width * height))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = dilateBinaryMask(mask, width, height)
		}
	})

	b.Run("Erode", func(b *testing.B) {
		b.SetBytes(int64(width * height))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = erodeBinaryMask(mask, width, height)
		}
	})

	b.Run("OpenClose", func(b *testing.B) {
		b.SetBytes(int64(width * height))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = openBinaryMask(closeBinaryMask(mask, width, height), width, height)
		}
	})
}
