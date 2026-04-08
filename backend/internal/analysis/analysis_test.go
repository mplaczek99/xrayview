package analysis

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

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

func TestHistogramPercentileMatchesRustTargetRounding(t *testing.T) {
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
	mask := []bool{
		false, false, false,
		false, true, false,
		false, false, false,
	}

	got := openBinaryMask(mask, 3, 3)
	want := make([]bool, len(mask))

	if !equalBools(got, want) {
		t.Fatalf("openBinaryMask = %v, want %v", got, want)
	}
}

func TestCloseBinaryMaskFillsSinglePixelHole(t *testing.T) {
	mask := []bool{
		true, true, true,
		true, false, true,
		true, true, true,
	}

	got := closeBinaryMask(mask, 3, 3)
	want := []bool{
		true, true, true,
		true, true, true,
		true, true, true,
	}

	if !equalBools(got, want) {
		t.Fatalf("closeBinaryMask = %v, want %v", got, want)
	}
}

func TestCollectCandidatesAndPrimarySelection(t *testing.T) {
	const width = 100
	const height = 100

	mask := make([]bool, width*height)
	normalized := make([]uint8, width*height)
	toothness := make([]uint8, width*height)

	fillBoolRect(mask, width, 10, 20, 10, 30, true)
	fillBoolRect(mask, width, 45, 25, 10, 30, true)
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

func TestAnalyzePreviewSamplePreviewMatchesRustFixtureSemantics(t *testing.T) {
	fixture := loadAnalyzeFixture(t)
	analysis, err := AnalyzePreview(loadAnalyzePreviewFixture(t), nil)
	if err != nil {
		t.Fatalf("AnalyzePreview returned error: %v", err)
	}

	if diff := absInt(len(analysis.Teeth) - len(fixture.Analysis.Teeth)); diff > 2 {
		t.Fatalf(
			"len(analysis.Teeth) = %d, fixture = %d, diff = %d, want <= 2",
			len(analysis.Teeth),
			len(fixture.Analysis.Teeth),
			diff,
		)
	}
	if got, want := len(analysis.Warnings), len(fixture.Analysis.Warnings); got != want {
		t.Fatalf("len(analysis.Warnings) = %d, want %d", got, want)
	}
	if analysis.Tooth == nil || fixture.Analysis.Tooth == nil {
		t.Fatalf("analysis.Tooth = %#v, fixture.Analysis.Tooth = %#v, want both non-nil", analysis.Tooth, fixture.Analysis.Tooth)
	}

	assertBoundingBoxClose(t, analysis.Tooth.Geometry.BoundingBox, fixture.Analysis.Tooth.Geometry.BoundingBox, 6)
	if diff := math.Abs(analysis.Tooth.Confidence - fixture.Analysis.Tooth.Confidence); diff > 0.05 {
		t.Fatalf("analysis.Tooth.Confidence = %v, fixture = %v, diff = %v, want <= 0.05", analysis.Tooth.Confidence, fixture.Analysis.Tooth.Confidence, diff)
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
	mask []bool,
	width uint32,
	x, y uint32,
	rectWidth, rectHeight uint32,
	value bool,
) {
	for yy := y; yy < y+rectHeight; yy++ {
		for xx := x; xx < x+rectWidth; xx++ {
			mask[yy*width+xx] = value
		}
	}
}

type analyzeFixture struct {
	Analysis contracts.ToothAnalysis `json:"analysis"`
}

func loadAnalyzeFixture(t *testing.T) analyzeFixture {
	t.Helper()

	raw, err := os.ReadFile(repoPathFromHere(t, "backend", "tests", "fixtures", "parity", "sample-dental-radiograph", "analyze-study.json"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var fixture analyzeFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	return fixture
}

func loadAnalyzePreviewFixture(t *testing.T) imaging.PreviewImage {
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

func repoPathFromHere(t *testing.T, pathParts ...string) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller returned no file path")
	}

	parts := []string{filepath.Dir(currentFile), "..", "..", ".."}
	parts = append(parts, pathParts...)
	return filepath.Clean(filepath.Join(parts...))
}

func assertBoundingBoxClose(t *testing.T, got, want contracts.BoundingBox, tolerance uint32) {
	t.Helper()

	assertUint32Close(t, "BoundingBox.X", got.X, want.X, tolerance)
	assertUint32Close(t, "BoundingBox.Y", got.Y, want.Y, tolerance)
	assertUint32Close(t, "BoundingBox.Width", got.Width, want.Width, tolerance)
	assertUint32Close(t, "BoundingBox.Height", got.Height, want.Height, tolerance)
}

func assertUint32Close(t *testing.T, label string, got, want, tolerance uint32) {
	t.Helper()

	diff := uint32(0)
	if got > want {
		diff = got - want
	} else {
		diff = want - got
	}
	if diff > tolerance {
		t.Fatalf("%s = %d, want %d +/- %d", label, got, want, tolerance)
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

func equalBools(left, right []bool) bool {
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
