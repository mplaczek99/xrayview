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
	"xrayview/backend/internal/dicommeta"
	"xrayview/backend/internal/imaging"
	"xrayview/backend/internal/render"
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

func TestAnalyzePreviewXrays2PrefersCentralIncisorBoundaryTrace(t *testing.T) {
	analysis, err := AnalyzePreview(loadXrays2PreviewFixture(t), nil)
	if err != nil {
		t.Fatalf("AnalyzePreview returned error: %v", err)
	}
	if analysis.Tooth == nil {
		t.Fatal("analysis.Tooth = nil, want close-up incisor candidate")
	}
	if got := len(analysis.Teeth); got != 4 {
		t.Fatalf("len(analysis.Teeth) = %d, want 4 separator-bounded close-up bands", got)
	}

	bbox := analysis.Tooth.Geometry.BoundingBox
	centerX := bbox.X + bbox.Width/2
	if centerX < 300 || centerX > 520 {
		t.Fatalf("primary tooth centerX = %d, want a central incisor near the middle gap", centerX)
	}
	if bbox.Width < 90 || bbox.Width > 170 {
		t.Fatalf("primary tooth width = %d, want a bounded central-incisor silhouette", bbox.Width)
	}
	if bbox.Height < 700 {
		t.Fatalf("primary tooth height = %d, want a crown-connected root extension", bbox.Height)
	}
	if bbox.Y < 170 || bbox.Y > 240 {
		t.Fatalf("primary tooth top = %d, want the contour to extend into the upper root region without leaving the tooth band", bbox.Y)
	}
	if bbox.Y+bbox.Height >= analysis.Image.Height-150 {
		t.Fatalf("primary tooth bottom = %d, want the contour to stop above the bottom border/background", bbox.Y+bbox.Height)
	}
	if len(analysis.Tooth.Geometry.Outline) < 12 {
		t.Fatalf("outline vertices = %d, want a traced contour", len(analysis.Tooth.Geometry.Outline))
	}
	if analysis.Tooth.Confidence < 0.70 {
		t.Fatalf("primary tooth confidence = %.2f, want >= 0.70", analysis.Tooth.Confidence)
	}

	centralCandidates := 0
	edgeCandidates := 0
	wantLabels := []string{"T1", "T2", "T3", "T4"}
	wantRoles := []string{"partial-left", "full-left", "full-right", "partial-right"}
	var prevCenterX uint32
	for index, tooth := range analysis.Teeth {
		candidateBox := tooth.Geometry.BoundingBox
		candidateCenterX := candidateBox.X + candidateBox.Width/2
		if index > 0 && candidateCenterX < prevCenterX {
			t.Fatalf("candidate centerX[%d] = %d, want detections sorted left-to-right after %d", index, candidateCenterX, prevCenterX)
		}
		prevCenterX = candidateCenterX
		if tooth.Label != wantLabels[index] {
			t.Fatalf("tooth label[%d] = %q, want %q", index, tooth.Label, wantLabels[index])
		}
		if tooth.Ordinal != index+1 {
			t.Fatalf("tooth ordinal[%d] = %d, want %d", index, tooth.Ordinal, index+1)
		}
		if tooth.Role != wantRoles[index] {
			t.Fatalf("tooth role[%d] = %q, want %q", index, tooth.Role, wantRoles[index])
		}
		if candidateCenterX >= 250 && candidateCenterX <= 600 {
			centralCandidates++
		} else {
			edgeCandidates++
		}
		minHeight := uint32(680)
		if index == 0 {
			minHeight = 380
		}
		if index == 3 {
			minHeight = 430
		}
		if candidateBox.Height < minHeight {
			t.Fatalf("candidate height[%d] = %d, want >= %d for this close-up band", index, candidateBox.Height, minHeight)
		}
		if candidateBox.Y+candidateBox.Height >= analysis.Image.Height-150 {
			t.Fatalf("candidate bottom = %d, want each contour to stay off the image floor", candidateBox.Y+candidateBox.Height)
		}
		widthRatio, capVariation := closeupContourShapeMetrics(tooth)
		minWidthRatio := 1.05
		minCapVariation := uint32(4)
		if index == 1 || index == 2 {
			minWidthRatio = 1.15
			minCapVariation = 6
		}
		if widthRatio < minWidthRatio {
			t.Fatalf("candidate width ratio[%d] = %.2f, want >= %.2f for a tapered contour", index, widthRatio, minWidthRatio)
		}
		if capVariation < minCapVariation {
			t.Fatalf("candidate cap variation[%d] = %d, want >= %d for a rounded crown/root cap", index, capVariation, minCapVariation)
		}
		maxShelf := uint32(52)
		if index == 1 || index == 2 {
			maxShelf = 42
		}
		shelfRun := outlineLongestLowerShelf(tooth.Geometry.Outline, tooth.Geometry.BoundingBox)
		if shelfRun > maxShelf {
			t.Fatalf("candidate lower shelf[%d] = %d, want <= %d for a curved lower crown contour", index, shelfRun, maxShelf)
		}
		maxJaggedness := 0.19
		if index == 0 || index == 3 {
			maxJaggedness = 0.18
		}
		jaggedness := outlineJaggedness(tooth.Geometry.Outline)
		if jaggedness > maxJaggedness {
			t.Fatalf("candidate jaggedness[%d] = %.3f, want <= %.3f after contour refinement", index, jaggedness, maxJaggedness)
		}
		if index == 1 || index == 2 {
			upperWidth, midWidth, ok := closeupBodyWidthMetrics(tooth)
			if !ok {
				t.Fatalf("candidate body width[%d] unavailable, want upper/mid width statistics", index)
			}
			if midWidth < upperWidth*1.75 {
				t.Fatalf("candidate mid-body width[%d] = %.1f, upper-root width = %.1f, want mid-body >= 1.75x upper-root", index, midWidth, upperWidth)
			}
			innerShift, ok := closeupInnerWallShift(tooth)
			if !ok {
				t.Fatalf("candidate inner wall shift[%d] unavailable, want separator-adjacent wall statistics", index)
			}
			if innerShift < 8.0 {
				t.Fatalf("candidate inner wall shift[%d] = %.1f, want >= 8.0px toward the separator in the mid-body", index, innerShift)
			}
		} else if index == 3 {
			upperWidth, midWidth, ok := closeupBodyWidthMetrics(tooth)
			if !ok {
				t.Fatalf("candidate body width[%d] unavailable, want upper/mid width statistics", index)
			}
			if midWidth < upperWidth*2.20 {
				t.Fatalf("candidate mid-body width[%d] = %.1f, upper width = %.1f, want T4 mid-body >= 2.20x upper width", index, midWidth, upperWidth)
			}
			if float64(candidateBox.Height) < float64(candidateBox.Width)*2.5 {
				t.Fatalf("candidate aspect[%d] = %d/%d, want T4 taller than a short rectangular partial blob", index, candidateBox.Height, candidateBox.Width)
			}
			if spread := closeupWidthSpreadMetric(tooth, 0.45, 0.85); spread < 50 {
				t.Fatalf("candidate width spread[%d] = %.1f, want T4 lower/mid contour variation >= 50px", index, spread)
			}
		}
	}
	if centralCandidates < 2 {
		t.Fatalf("central candidates = %d, want both middle incisor bands", centralCandidates)
	}
	if edgeCandidates < 2 {
		t.Fatalf("edge candidates = %d, want both outer partial bands", edgeCandidates)
	}
	if analysis.Tooth.Label != "T2" {
		t.Fatalf("primary tooth label = %q, want %q", analysis.Tooth.Label, "T2")
	}
	if analysis.Tooth.Ordinal != 2 {
		t.Fatalf("primary tooth ordinal = %d, want %d", analysis.Tooth.Ordinal, 2)
	}
	if analysis.Tooth.Role != "full-left" {
		t.Fatalf("primary tooth role = %q, want %q", analysis.Tooth.Role, "full-left")
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

func loadXrays2PreviewFixture(t testing.TB) imaging.PreviewImage {
	t.Helper()

	study, err := dicommeta.DecodeFile(repoPathFromHere(t, "images", "TIF", "xrays2.tif"))
	if err != nil {
		t.Fatalf("DecodeFile returned error: %v", err)
	}
	return render.RenderSourceImage(study.Image, render.DefaultRenderPlan())
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

func closeupContourShapeMetrics(tooth contracts.ToothCandidate) (float64, uint32) {
	spans := outlineRowSpans(tooth.Geometry.Outline)
	if len(spans) == 0 {
		return 0, 0
	}

	bbox := tooth.Geometry.BoundingBox
	rowWidths := make([]uint32, 0, len(spans))
	upperWidths := make([]uint32, 0, len(spans))
	lowerWidths := make([]uint32, 0, len(spans))
	upperLimit := bbox.Y + bbox.Height/3
	lowerStart := bbox.Y + (bbox.Height*2)/3
	for y, span := range spans {
		width := span[1] - span[0] + 1
		rowWidths = append(rowWidths, width)
		if y <= upperLimit {
			upperWidths = append(upperWidths, width)
		}
		if y >= lowerStart {
			lowerWidths = append(lowerWidths, width)
		}
	}
	if len(upperWidths) == 0 || len(lowerWidths) == 0 {
		return 0, 0
	}
	upperMedian := float64(medianUint32(upperWidths))
	lowerMax := float64(maxUint32Slice(lowerWidths))
	widthRatio := lowerMax / math.Max(upperMedian, 1.0)

	topVariation := outlineCapVariation(tooth.Geometry.Outline, true)
	bottomVariation := outlineCapVariation(tooth.Geometry.Outline, false)
	capVariation := topVariation
	if bottomVariation > capVariation {
		capVariation = bottomVariation
	}
	return widthRatio, capVariation
}

func closeupBodyWidthMetrics(tooth contracts.ToothCandidate) (float64, float64, bool) {
	spans := outlineRowSpans(tooth.Geometry.Outline)
	if len(spans) == 0 {
		return 0, 0, false
	}
	bbox := tooth.Geometry.BoundingBox
	upperStart := bbox.Y
	upperEnd := bbox.Y + bbox.Height/4
	midStart := bbox.Y + (bbox.Height*2)/5
	midEnd := bbox.Y + (bbox.Height*3)/5
	upperWidths := collectOutlineWidthsInRange(spans, upperStart, upperEnd)
	midWidths := collectOutlineWidthsInRange(spans, midStart, midEnd)
	if len(upperWidths) == 0 || len(midWidths) == 0 {
		return 0, 0, false
	}
	return float64(medianUint32(upperWidths)), float64(medianUint32(midWidths)), true
}

func closeupInnerWallShift(tooth contracts.ToothCandidate) (float64, bool) {
	spans := outlineRowSpans(tooth.Geometry.Outline)
	if len(spans) == 0 {
		return 0, false
	}
	bbox := tooth.Geometry.BoundingBox
	upperStart := bbox.Y
	upperEnd := bbox.Y + bbox.Height/4
	midStart := bbox.Y + (bbox.Height*2)/5
	midEnd := bbox.Y + (bbox.Height*3)/5

	upperEdges := collectOutlineEdgesInRange(spans, upperStart, upperEnd, tooth.Role == "full-left")
	midEdges := collectOutlineEdgesInRange(spans, midStart, midEnd, tooth.Role == "full-left")
	if len(upperEdges) == 0 || len(midEdges) == 0 {
		return 0, false
	}
	upper := float64(medianUint32(upperEdges))
	mid := float64(medianUint32(midEdges))
	if tooth.Role == "full-left" {
		return mid - upper, true
	}
	if tooth.Role == "full-right" {
		return upper - mid, true
	}
	return 0, false
}

func closeupWidthSpreadMetric(tooth contracts.ToothCandidate, startFrac float64, endFrac float64) float64 {
	spans := outlineRowSpans(tooth.Geometry.Outline)
	if len(spans) == 0 {
		return 0
	}
	bbox := tooth.Geometry.BoundingBox
	startY := bbox.Y + uint32(math.Round(float64(bbox.Height)*startFrac))
	endY := bbox.Y + uint32(math.Round(float64(bbox.Height)*endFrac))
	widths := collectOutlineWidthsInRange(spans, startY, endY)
	if len(widths) < 4 {
		return 0
	}
	sort.Slice(widths, func(i, j int) bool { return widths[i] < widths[j] })
	p10 := widths[len(widths)/10]
	p90 := widths[(len(widths)*9)/10]
	return float64(p90 - p10)
}

func outlineJaggedness(outline []contracts.Point) float64 {
	if len(outline) < 3 {
		return 0
	}
	excess := 0.0
	perimeter := 0.0
	for index, point := range outline {
		next := outline[(index+1)%len(outline)]
		dx := math.Abs(float64(next.X) - float64(point.X))
		dy := math.Abs(float64(next.Y) - float64(point.Y))
		manhattan := dx + dy
		euclidean := math.Hypot(dx, dy)
		excess += manhattan - euclidean
		perimeter += euclidean
	}
	if perimeter == 0 {
		return 0
	}
	return excess / perimeter
}

func collectOutlineWidthsInRange(spans map[uint32][2]uint32, startY uint32, endY uint32) []uint32 {
	widths := make([]uint32, 0, len(spans))
	for y, span := range spans {
		if y < startY || y > endY {
			continue
		}
		widths = append(widths, span[1]-span[0]+1)
	}
	return widths
}

func collectOutlineEdgesInRange(spans map[uint32][2]uint32, startY uint32, endY uint32, rightEdge bool) []uint32 {
	edges := make([]uint32, 0, len(spans))
	for y, span := range spans {
		if y < startY || y > endY {
			continue
		}
		if rightEdge {
			edges = append(edges, span[1])
			continue
		}
		edges = append(edges, span[0])
	}
	return edges
}

func outlineRowSpans(outline []contracts.Point) map[uint32][2]uint32 {
	spans := make(map[uint32][2]uint32)
	if len(outline) == 0 {
		return spans
	}
	update := func(point contracts.Point) {
		span, ok := spans[point.Y]
		if !ok {
			spans[point.Y] = [2]uint32{point.X, point.X}
			return
		}
		if point.X < span[0] {
			span[0] = point.X
		}
		if point.X > span[1] {
			span[1] = point.X
		}
		spans[point.Y] = span
	}
	for index, point := range outline {
		next := outline[(index+1)%len(outline)]
		dx := absInt(int(next.X) - int(point.X))
		dy := absInt(int(next.Y) - int(point.Y))
		steps := maxInt(dx, dy)
		if steps == 0 {
			update(point)
			continue
		}
		for step := 0; step <= steps; step++ {
			t := float64(step) / float64(steps)
			interp := contracts.Point{
				X: uint32(math.Round((1.0-t)*float64(point.X) + t*float64(next.X))),
				Y: uint32(math.Round((1.0-t)*float64(point.Y) + t*float64(next.Y))),
			}
			update(interp)
		}
	}
	return spans
}

func outlineCapVariation(outline []contracts.Point, top bool) uint32 {
	if len(outline) == 0 {
		return 0
	}

	xExtrema := make(map[uint32]uint32)
	for _, point := range outline {
		value, ok := xExtrema[point.X]
		if !ok {
			xExtrema[point.X] = point.Y
			continue
		}
		if top {
			if point.Y < value {
				xExtrema[point.X] = point.Y
			}
			continue
		}
		if point.Y > value {
			xExtrema[point.X] = point.Y
		}
	}
	if len(xExtrema) < 4 {
		return 0
	}

	values := make([]uint32, 0, len(xExtrema))
	for _, value := range xExtrema {
		values = append(values, value)
	}
	return maxUint32Slice(values) - minUint32Slice(values)
}

func outlineLongestLowerShelf(outline []contracts.Point, bbox contracts.BoundingBox) uint32 {
	if len(outline) == 0 {
		return 0
	}
	xBottom := make(map[uint32]uint32)
	lowerStart := bbox.Y + (bbox.Height*2)/3
	for _, point := range outline {
		if point.Y < lowerStart {
			continue
		}
		value, ok := xBottom[point.X]
		if !ok || point.Y > value {
			xBottom[point.X] = point.Y
		}
	}
	if len(xBottom) < 4 {
		return 0
	}

	xs := make([]uint32, 0, len(xBottom))
	for x := range xBottom {
		xs = append(xs, x)
	}
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })

	var longest uint32
	var current uint32 = 1
	for index := 1; index < len(xs); index++ {
		prevX := xs[index-1]
		x := xs[index]
		if x != prevX+1 {
			if current > longest {
				longest = current
			}
			current = 1
			continue
		}
		if absInt(int(xBottom[x])-int(xBottom[prevX])) <= 1 {
			current++
			continue
		}
		if current > longest {
			longest = current
		}
		current = 1
	}
	if current > longest {
		longest = current
	}
	return longest
}

func maxUint32Slice(values []uint32) uint32 {
	if len(values) == 0 {
		return 0
	}
	maxValue := values[0]
	for _, value := range values[1:] {
		if value > maxValue {
			maxValue = value
		}
	}
	return maxValue
}

func minUint32Slice(values []uint32) uint32 {
	if len(values) == 0 {
		return 0
	}
	minValue := values[0]
	for _, value := range values[1:] {
		if value < minValue {
			minValue = value
		}
	}
	return minValue
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
