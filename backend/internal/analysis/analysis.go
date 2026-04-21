package analysis

// Tooth-candidate detector. Not ML — a pipeline of cheap image ops tuned
// against the fixtures in images/.
//
// AnalyzeGrayscalePixels is the whole show. It stretches contrast (2nd/98th
// percentile, so metal restorations don't flatten the histogram), runs two
// Gaussian blurs — σ=1.4 for sensor noise, σ=9.0 for the bone/gum background
// — and folds their difference, a local gradient, and the normalized
// intensity into a per-pixel "toothness" score. Then: threshold toothness
// and intensity at per-region percentiles with hard floors (so dim panoramics
// don't drop into noise), a 3×3 close+open to fuse and despeckle, BFS
// connected components, and a score pass that picks one primary tooth.
//
// selectPrimaryCandidate prefers anything that clears the strict
// area/aspect gates; if nothing does, it falls back to the best relaxed
// candidate and the caller tags the output with a warning. The Warnings
// slice is UI contract — the frontend renders those strings verbatim, so
// don't rewrite them casually.
//
// measurementScale is optional. Pixel measurements always come back; mm
// values ride along only when the caller has spacing.

import (
	"fmt"
	"math"
	"sort"

	"xrayview/backend/internal/bufpool"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/imaging"
)

const (
	pixelUnits      = "px"
	millimeterUnits = "mm"
	toothnessFloor  = 96
	intensityFloor  = 82

	// minDetectedArea is the minimum pixel area for a connected component to be
	// included in analysis output. Used by both collectCandidates (to decide
	// whether to allocate a pixel slice) and selectDetectedCandidates (to filter
	// the final candidate list). Both must stay in sync.
	minDetectedArea = 150
)

type searchRegion struct {
	x      uint32
	y      uint32
	width  uint32
	height uint32
}

func (region searchRegion) area() uint32 {
	return region.width * region.height
}

type componentCandidate struct {
	pixels []int
	bbox   contracts.BoundingBox
	area   uint32
	score  float64
	strict bool
}

type analysisDebugSnapshot struct {
	SearchRegion            searchRegion
	UpperSearchRegion       searchRegion
	LowerSearchRegion       searchRegion
	OcclusalSplitY          uint32
	ToothnessThreshold      uint8
	IntensityThreshold      uint8
	UpperToothnessThreshold uint8
	UpperIntensityThreshold uint8
	LowerToothnessThreshold uint8
	LowerIntensityThreshold uint8
	Normalized              []uint8
	SmallBlur               []uint8
	LargeBlur               []uint8
	SmallMinusLarge         []int16
	LocalContrastLegacy     []uint8
	LocalContrast           []uint8
	Gradient                []uint8
	Toothness               []uint8
	PostThresholdMask       []uint8
	PostCloseMask           []uint8
	PostOpenMask            []uint8
	MaskCoverageRatio       float64
	UpperMaskCoverageRatio  float64
	LowerMaskCoverageRatio  float64
	DetectedCandidates      []componentCandidate
	AllCandidates           []componentCandidate
}

type regionThresholdResult struct {
	mask               []uint8
	toothnessThreshold uint8
	intensityThreshold uint8
	coverageRatio      float64
}

func AnalyzePreview(
	preview imaging.PreviewImage,
	measurementScale *contracts.MeasurementScale,
) (contracts.ToothAnalysis, error) {
	if preview.Format != imaging.FormatGray8 {
		return contracts.ToothAnalysis{}, fmt.Errorf("tooth analysis currently requires an 8-bit grayscale preview")
	}

	return AnalyzeGrayscalePixels(preview.Width, preview.Height, preview.Pixels, measurementScale)
}

func AnalyzeGrayscalePixels(
	width, height uint32,
	pixels []uint8,
	measurementScale *contracts.MeasurementScale,
) (contracts.ToothAnalysis, error) {
	return analyzeGrayscalePixelsWithDebug(width, height, pixels, measurementScale, nil)
}

func analyzeGrayscalePixelsWithDebug(
	width, height uint32,
	pixels []uint8,
	measurementScale *contracts.MeasurementScale,
	debug *analysisDebugSnapshot,
) (contracts.ToothAnalysis, error) {
	expectedLen := int(width) * int(height)
	if len(pixels) != expectedLen {
		return contracts.ToothAnalysis{}, fmt.Errorf(
			"grayscale analysis expects %d pixels for dimensions %dx%d, got %d",
			expectedLen,
			width,
			height,
			len(pixels),
		)
	}

	search := defaultSearchRegion(width, height)
	if debug != nil {
		debug.SearchRegion = search
	}
	normalized := normalizePixels(pixels)
	if debug != nil {
		debug.Normalized = cloneUint8Slice(normalized)
	}
	// Band-pass pair. σ=1.4 suppresses sensor noise; σ=9.0 approximates the
	// slowly-varying bone/gum background. Their difference is what survives
	// into the toothness map.
	smallBlur, largeBlur := dualGaussianBlurGray(normalized, width, height, 1.4, 9.0)
	if debug != nil {
		debug.SmallBlur = cloneUint8Slice(smallBlur)
		debug.LargeBlur = cloneUint8Slice(largeBlur)
	}
	toothness := buildToothnessMap(normalized, smallBlur, largeBlur, width, height, debug)

	// Return blur buffers — no longer needed after toothness map is built.
	bufpool.PutUint8(smallBlur)
	bufpool.PutUint8(largeBlur)

	// Percentiles + hard floors, both tuned against the fixtures in images/.
	// The percentiles keep recall on thin-crown molars; the floors stop dim
	// panoramics (where the 79th-percentile toothness is still nearly zero)
	// from collapsing into an all-noise mask. Bump these carefully — raising
	// either percentile loses recall, lowering either floor pulls in gum and
	// bone shadow.
	upperSearch := search
	lowerSearch := searchRegion{}
	splitY := search.y + search.height/2
	var upperThreshold regionThresholdResult
	var lowerThreshold regionThresholdResult
	var mask []uint8
	var closedMask []uint8
	if width < 512 {
		toothnessThreshold := maxUint8(percentileInRegion(toothness, width, search, 0.79), toothnessFloor)
		intensityThreshold := maxUint8(percentileInRegion(normalized, width, search, 0.60), intensityFloor)
		mask = make([]uint8, len(normalized))
		for y := search.y; y < search.y+search.height; y++ {
			rowStart := int(y * width)
			for x := search.x; x < search.x+search.width; x++ {
				index := rowStart + int(x)
				if toothness[index] >= toothnessThreshold && normalized[index] >= intensityThreshold {
					mask[index] = 1
				}
			}
		}
		upperThreshold = regionThresholdResult{
			mask:               cloneUint8Slice(mask),
			toothnessThreshold: toothnessThreshold,
			intensityThreshold: intensityThreshold,
			coverageRatio:      float64(countMaskPixels(mask)) / float64(maxUint32(search.area(), 1)),
		}
		closedMask = closeBinaryMask(mask, int(width), int(height))
		mask = openBinaryMask(closedMask, int(width), int(height))
	} else {
		upperSearch, lowerSearch, splitY = splitSearchRegionByOcclusalGap(normalized, toothness, width, search)
		upperThreshold = thresholdMaskForRegion(normalized, toothness, width, upperSearch, 0.76, 0.52)
		if upperThreshold.coverageRatio < 0.012 {
			upperThreshold = thresholdMaskForRegion(normalized, toothness, width, upperSearch, 0.68, 0.45)
		}
		lowerThreshold = thresholdMaskForRegion(normalized, toothness, width, lowerSearch, 0.76, 0.52)
		if lowerThreshold.coverageRatio < 0.012 {
			lowerThreshold = thresholdMaskForRegion(normalized, toothness, width, lowerSearch, 0.68, 0.45)
		}
		mask = orMasks(upperThreshold.mask, lowerThreshold.mask)
		closedUpperMask := closeBinaryMask(upperThreshold.mask, int(width), int(height))
		closedLowerMask := closeBinaryMask(lowerThreshold.mask, int(width), int(height))
		closedMask = orMasks(closedUpperMask, closedLowerMask)
		mask = orMasks(
			openBinaryMask(closedUpperMask, int(width), int(height)),
			openBinaryMask(closedLowerMask, int(width), int(height)),
		)
	}
	maskPixelCount := countMaskPixels(mask)
	if debug != nil {
		debug.OcclusalSplitY = splitY
		debug.UpperSearchRegion = upperSearch
		debug.LowerSearchRegion = lowerSearch
		debug.ToothnessThreshold = minNonZeroUint8(upperThreshold.toothnessThreshold, lowerThreshold.toothnessThreshold)
		debug.IntensityThreshold = minNonZeroUint8(upperThreshold.intensityThreshold, lowerThreshold.intensityThreshold)
		debug.UpperToothnessThreshold = upperThreshold.toothnessThreshold
		debug.UpperIntensityThreshold = upperThreshold.intensityThreshold
		debug.LowerToothnessThreshold = lowerThreshold.toothnessThreshold
		debug.LowerIntensityThreshold = lowerThreshold.intensityThreshold
		debug.PostThresholdMask = cloneUint8Slice(upperThreshold.mask)
		if width >= 512 {
			debug.PostThresholdMask = cloneUint8Slice(orMasks(upperThreshold.mask, lowerThreshold.mask))
		}
		debug.MaskCoverageRatio = float64(maskPixelCount) / float64(maxUint32(search.area(), 1))
		debug.UpperMaskCoverageRatio = upperThreshold.coverageRatio
		debug.LowerMaskCoverageRatio = lowerThreshold.coverageRatio
		debug.PostCloseMask = cloneUint8Slice(closedMask)
		debug.PostOpenMask = cloneUint8Slice(mask)
	}

	candidates := collectCandidates(mask, normalized, toothness, width, height, search)
	seedCandidates := selectDetectedCandidates(candidates)
	seedGeometries := make([]contracts.ToothGeometry, len(seedCandidates))
	for index, candidate := range seedCandidates {
		seedGeometries[index] = geometryFromPixels(candidate.pixels, candidate.bbox, width)
	}
	finalCandidates := seedCandidates
	finalGeometries := append([]contracts.ToothGeometry(nil), seedGeometries...)
	if width >= 512 {
		panoramicCandidates, panoramicGeometries := buildPanoramicProfileCandidates(normalized, toothness, mask, width, search, splitY)
		if len(panoramicCandidates) > 0 {
			finalCandidates = panoramicCandidates
			finalGeometries = panoramicGeometries
		}
	}
	finalGeometries = attachClosestOutlines(finalGeometries, seedGeometries)
	primaryCandidate := selectPrimaryCandidate(finalCandidates)
	primaryIndex := -1
	if primaryCandidate != nil {
		for index := range finalCandidates {
			if &finalCandidates[index] == primaryCandidate {
				primaryIndex = index
				break
			}
		}
	}

	// Return analysis buffers — candidates hold pixel indices, not buffer refs.
	bufpool.PutUint8(normalized)
	bufpool.PutUint8(toothness)
	if debug != nil {
		debug.AllCandidates = cloneComponentCandidates(candidates)
		debug.DetectedCandidates = cloneComponentCandidates(finalCandidates)
	}

	warnings := make([]string, 0, 2)
	if measurementScale == nil {
		warnings = append(warnings, "Calibration metadata unavailable; returning pixel measurements only.")
	}

	if len(finalCandidates) > 0 && primaryCandidate != nil && !primaryCandidate.strict {
		warnings = append(warnings, "No component met the primary tooth filters; using relaxed tooth candidates.")
	}

	teeth := make([]contracts.ToothCandidate, 0, len(finalCandidates))
	for index, candidate := range finalCandidates {
		teeth = append(teeth, buildToothCandidate(candidate, finalGeometries[index], measurementScale))
	}

	var tooth *contracts.ToothCandidate
	if primaryCandidate != nil && primaryIndex >= 0 {
		candidate := buildToothCandidate(*primaryCandidate, finalGeometries[primaryIndex], measurementScale)
		tooth = &candidate
	} else {
		warnings = append(warnings, "The backend could not isolate a tooth candidate from this study.")
	}

	return contracts.ToothAnalysis{
		Image: contracts.ToothImageMetadata{
			Width:  width,
			Height: height,
		},
		Calibration: contracts.ToothCalibration{
			PixelUnits:                     pixelUnits,
			MeasurementScale:               measurementScale,
			RealWorldMeasurementsAvailable: measurementScale != nil,
		},
		Tooth:    tooth,
		Teeth:    teeth,
		Warnings: warnings,
	}, nil
}

// buildToothCandidate packages a single component as a
// contracts.ToothCandidate. Pixel measurements are always present;
// Calibrated is non-nil only when the caller supplied a measurementScale,
// in which case the frontend shows mm labels. Without it, the UI falls
// back to "px" off the Pixel bundle.
func buildToothCandidate(
	candidate componentCandidate,
	geometry contracts.ToothGeometry,
	measurementScale *contracts.MeasurementScale,
) contracts.ToothCandidate {
	pixel := contracts.ToothMeasurementValues{
		ToothWidth:        float64(lineSegmentLength(geometry.WidthLine)),
		ToothHeight:       float64(lineSegmentLength(geometry.HeightLine)),
		BoundingBoxWidth:  float64(geometry.BoundingBox.Width),
		BoundingBoxHeight: float64(geometry.BoundingBox.Height),
		Units:             pixelUnits,
	}

	var calibrated *contracts.ToothMeasurementValues
	if measurementScale != nil {
		values := contracts.ToothMeasurementValues{
			ToothWidth:        roundMeasurement(pixel.ToothWidth * measurementScale.ColumnSpacingMM),
			ToothHeight:       roundMeasurement(pixel.ToothHeight * measurementScale.RowSpacingMM),
			BoundingBoxWidth:  roundMeasurement(pixel.BoundingBoxWidth * measurementScale.ColumnSpacingMM),
			BoundingBoxHeight: roundMeasurement(pixel.BoundingBoxHeight * measurementScale.RowSpacingMM),
			Units:             millimeterUnits,
		}
		calibrated = &values
	}

	return contracts.ToothCandidate{
		Confidence:     roundConfidence(candidate.score),
		MaskAreaPixels: candidate.area,
		Measurements: contracts.ToothMeasurementBundle{
			Pixel:      pixel,
			Calibrated: calibrated,
		},
		Geometry: geometry,
	}
}

// defaultSearchRegion trims the image down to where teeth actually live.
// Panoramic dental X-rays park the crowns in the vertical middle and pad
// the frame with patient-ID text, radiographer initials, and heavy jaw
// shadow. Clipping the top ~20% and bottom ~22%, plus a one-eighth
// horizontal margin on each side, keeps those regions out of the
// connected-component pass.
func defaultSearchRegion(width, height uint32) searchRegion {
	xMargin := maxUint32(width/8, 8)
	topMargin := maxUint32(uint32(math.Round(float64(height)*0.16)), 8)
	bottom := maxUint32(uint32(math.Round(float64(height)*0.84)), topMargin+1)

	return searchRegion{
		x:      xMargin,
		y:      topMargin,
		width:  maxUint32(saturatingSubUint32(width, xMargin*2), 1),
		height: maxUint32(saturatingSubUint32(bottom, topMargin), 1),
	}
}

// normalizePixels is a 2nd/98th-percentile contrast stretch into 0..255.
// Percentiles rather than min/max so a handful of saturated pixels (metal
// restorations, film-edge glare) don't flatten the rest of the histogram.
// If the percentile window collapses, returns a straight copy — the
// thresholding stage still has something to work against.
func normalizePixels(pixels []uint8) []uint8 {
	var histogram [256]uint32
	for _, value := range pixels {
		histogram[value]++
	}

	total := uint32(len(pixels))
	lower := histogramPercentile(histogram, total, 0.02)
	upper := histogramPercentile(histogram, total, 0.98)
	if upper <= lower {
		buf := bufpool.GetUint8(len(pixels))
		copy(buf, pixels)
		return buf
	}

	normalized := bufpool.GetUint8(len(pixels))
	scaleRange := uint32(upper - lower)
	for index, value := range pixels {
		switch {
		case value <= lower:
			normalized[index] = 0
		case value >= upper:
			normalized[index] = 255
		default:
			normalized[index] = uint8((uint32(value-lower) * 255) / scaleRange)
		}
	}

	return normalized
}

// buildToothnessMap produces the per-pixel "toothness" score that drives
// one half of the mask threshold. Weighted mix of normalized intensity
// (×5), the band-pass local contrast between the two blurs (×4), and a
// cheap local gradient (×2), averaged back down into a uint8 so the
// thresholding below can share a histogram with the raw intensity path.
func buildToothnessMap(
	normalized []uint8,
	smallBlur []uint8,
	largeBlur []uint8,
	width, height uint32,
	debug *analysisDebugSnapshot,
) []uint8 {
	toothness := bufpool.GetUint8(len(normalized))
	var smallMinusLarge []int16
	var localContrastLegacy []uint8
	var localContrast []uint8
	var gradientMap []uint8
	if debug != nil {
		smallMinusLarge = make([]int16, len(normalized))
		localContrastLegacy = make([]uint8, len(normalized))
		localContrast = make([]uint8, len(normalized))
		gradientMap = make([]uint8, len(normalized))
	}
	for y := uint32(0); y < height; y++ {
		for x := uint32(0); x < width; x++ {
			index := int(y*width + x)
			small := int16(smallBlur[index])
			large := int16(largeBlur[index])
			smallLargeDelta := int(small - large)
			legacyContrast := clampUint8FromInt(128 + smallLargeDelta)
			// Use only structure magnitude in the live score so flat bright
			// regions do not inherit a high baseline toothness.
			structuralContrast := clampUint8FromInt(absInt(smallLargeDelta))
			gradient := localGradient(normalized, width, height, x, y)
			combined := (uint16(normalized[index])*4 +
				uint16(structuralContrast)*5 +
				uint16(gradient)*2) / 11
			toothness[index] = uint8(combined)
			if debug != nil {
				smallMinusLarge[index] = int16(smallLargeDelta)
				localContrastLegacy[index] = legacyContrast
				localContrast[index] = structuralContrast
				gradientMap[index] = gradient
			}
		}
	}
	if debug != nil {
		debug.SmallMinusLarge = smallMinusLarge
		debug.LocalContrastLegacy = localContrastLegacy
		debug.LocalContrast = localContrast
		debug.Gradient = gradientMap
		debug.Toothness = cloneUint8Slice(toothness)
	}

	return toothness
}

func localGradient(pixels []uint8, width, height, x, y uint32) uint8 {
	left := pixels[int(y*width+saturatingSubUint32(x, 1))]
	right := pixels[int(y*width+minUint32(x+1, width-1))]
	top := pixels[int(saturatingSubUint32(y, 1)*width+x)]
	bottom := pixels[int(minUint32(y+1, height-1)*width+x)]

	horizontal := absInt(int(right) - int(left))
	vertical := absInt(int(bottom) - int(top))
	return clampUint8FromInt(horizontal + vertical)
}

func percentileInRegion(values []uint8, width uint32, region searchRegion, percentile float64) uint8 {
	var histogram [256]uint32
	var total uint32

	for y := region.y; y < region.y+region.height; y++ {
		rowStart := int(y * width)
		for x := region.x; x < region.x+region.width; x++ {
			histogram[values[rowStart+int(x)]]++
			total++
		}
	}

	return histogramPercentile(histogram, total, percentile)
}

func thresholdMaskForRegion(
	normalized []uint8,
	toothness []uint8,
	width uint32,
	region searchRegion,
	toothnessPercentile float64,
	intensityPercentile float64,
) regionThresholdResult {
	mask := make([]uint8, len(normalized))
	if region.width == 0 || region.height == 0 {
		return regionThresholdResult{mask: mask}
	}

	toothnessThreshold := maxUint8(percentileInRegion(toothness, width, region, toothnessPercentile), toothnessFloor)
	intensityThreshold := maxUint8(percentileInRegion(normalized, width, region, intensityPercentile), intensityFloor)
	maskPixels := 0
	for y := region.y; y < region.y+region.height; y++ {
		rowStart := int(y * width)
		for x := region.x; x < region.x+region.width; x++ {
			index := rowStart + int(x)
			if toothness[index] >= toothnessThreshold && normalized[index] >= intensityThreshold {
				mask[index] = 1
				maskPixels++
			}
		}
	}

	return regionThresholdResult{
		mask:               mask,
		toothnessThreshold: toothnessThreshold,
		intensityThreshold: intensityThreshold,
		coverageRatio:      float64(maskPixels) / float64(maxUint32(region.area(), 1)),
	}
}

func splitSearchRegionByOcclusalGap(normalized []uint8, toothness []uint8, width uint32, search searchRegion) (searchRegion, searchRegion, uint32) {
	if search.height <= 2 {
		return search, searchRegion{}, search.y
	}

	xInset := maxUint32(search.width/5, 24)
	if xInset*2 >= search.width {
		xInset = search.width / 8
	}
	profileX := search.x + xInset
	profileWidth := maxUint32(saturatingSubUint32(search.width, xInset*2), 1)
	toothnessThreshold := maxUint8(percentileInRegion(toothness, width, search, 0.70), toothnessFloor)
	intensityThreshold := maxUint8(percentileInRegion(normalized, width, search, 0.48), intensityFloor)
	rowProfile := make([]float64, search.height)
	for y := search.y; y < search.y+search.height; y++ {
		rowStart := int(y * width)
		var hits float64
		for x := profileX; x < profileX+profileWidth; x++ {
			index := rowStart + int(x)
			if toothness[index] >= toothnessThreshold && normalized[index] >= intensityThreshold {
				hits++
			}
		}
		rowProfile[y-search.y] = hits
	}
	smoothed := smoothFloatProfile(rowProfile, 11)
	if len(smoothed) == 0 {
		return search, searchRegion{}, search.y + search.height/2
	}
	height := len(smoothed)
	windowStart := clampInt(int(math.Round(float64(height)*0.58)), 0, height-1)
	windowEnd := clampInt(int(math.Round(float64(height)*0.86)), windowStart+1, height)
	bestY := uint32(argminFloatSlice(smoothed[windowStart:windowEnd])+windowStart) + search.y

	upperHeight := maxUint32(saturatingSubUint32(bestY, search.y), 1)
	lowerY := minUint32(bestY+1, search.y+search.height-1)
	lowerHeight := maxUint32(saturatingSubUint32(search.y+search.height, lowerY), 1)

	return searchRegion{
			x:      search.x,
			y:      search.y,
			width:  search.width,
			height: upperHeight,
		}, searchRegion{
			x:      search.x,
			y:      lowerY,
			width:  search.width,
			height: lowerHeight,
		}, bestY
}

func histogramPercentile(histogram [256]uint32, total uint32, percentile float64) uint8 {
	if total == 0 {
		return 0
	}

	target := uint32(math.Round(float64(total-1)*percentile)) + 1
	var cumulative uint32
	for value, count := range histogram {
		cumulative += count
		if cumulative >= target {
			return uint8(value)
		}
	}

	return 255
}

func closeBinaryMask(mask []uint8, width, height int) []uint8 {
	return erodeBinaryMask(dilateBinaryMask(mask, width, height), width, height)
}

func openBinaryMask(mask []uint8, width, height int) []uint8 {
	return dilateBinaryMask(erodeBinaryMask(mask, width, height), width, height)
}

// dilateBinaryMask performs binary morphological dilation with a 3x3 structuring element.
// The 3x3 operation is decomposed into separable 1×3 horizontal and 3×1 vertical passes.
// Interior rows are processed without bounds checks, unrolled 4 columns at a time.
func dilateBinaryMask(mask []uint8, width, height int) []uint8 {
	n := width * height
	tmp := bufpool.GetUint8(n)
	tmp = tmp[:n]
	out := make([]uint8, n)

	// Horizontal pass: OR left/center/right neighbors within each row.
	for y := 0; y < height; y++ {
		row := y * width
		if width == 1 {
			tmp[row] = mask[row]
			continue
		}
		tmp[row] = mask[row] | mask[row+1]
		for x := 1; x < width-1; x++ {
			tmp[row+x] = mask[row+x-1] | mask[row+x] | mask[row+x+1]
		}
		tmp[row+width-1] = mask[row+width-2] | mask[row+width-1]
	}

	// Vertical pass: OR top/center/bottom neighbors within each column.
	if height == 1 {
		copy(out, tmp)
		bufpool.PutUint8(tmp)
		return out
	}
	// First row: no top neighbor.
	for x := 0; x < width; x++ {
		out[x] = tmp[x] | tmp[width+x]
	}
	// Interior rows: no bounds checks, 4-column unroll for ILP.
	for y := 1; y < height-1; y++ {
		row := y * width
		prevRow := row - width
		nextRow := row + width
		x := 0
		for ; x <= width-4; x += 4 {
			out[row+x] = tmp[prevRow+x] | tmp[row+x] | tmp[nextRow+x]
			out[row+x+1] = tmp[prevRow+x+1] | tmp[row+x+1] | tmp[nextRow+x+1]
			out[row+x+2] = tmp[prevRow+x+2] | tmp[row+x+2] | tmp[nextRow+x+2]
			out[row+x+3] = tmp[prevRow+x+3] | tmp[row+x+3] | tmp[nextRow+x+3]
		}
		for ; x < width; x++ {
			out[row+x] = tmp[prevRow+x] | tmp[row+x] | tmp[nextRow+x]
		}
	}
	// Last row: no bottom neighbor.
	lastRow := (height - 1) * width
	prevLastRow := lastRow - width
	for x := 0; x < width; x++ {
		out[lastRow+x] = tmp[prevLastRow+x] | tmp[lastRow+x]
	}

	bufpool.PutUint8(tmp)
	return out
}

// erodeBinaryMask performs binary morphological erosion with a 3x3 structuring element.
// Uses the same separable-pass structure as dilateBinaryMask with AND instead of OR.
func erodeBinaryMask(mask []uint8, width, height int) []uint8 {
	n := width * height
	tmp := bufpool.GetUint8(n)
	tmp = tmp[:n]
	out := make([]uint8, n)

	// Horizontal pass: AND left/center/right neighbors within each row.
	for y := 0; y < height; y++ {
		row := y * width
		if width == 1 {
			tmp[row] = mask[row]
			continue
		}
		tmp[row] = mask[row] & mask[row+1]
		for x := 1; x < width-1; x++ {
			tmp[row+x] = mask[row+x-1] & mask[row+x] & mask[row+x+1]
		}
		tmp[row+width-1] = mask[row+width-2] & mask[row+width-1]
	}

	// Vertical pass: AND top/center/bottom neighbors within each column.
	if height == 1 {
		copy(out, tmp)
		bufpool.PutUint8(tmp)
		return out
	}
	// First row: no top neighbor.
	for x := 0; x < width; x++ {
		out[x] = tmp[x] & tmp[width+x]
	}
	// Interior rows: no bounds checks, 4-column unroll for ILP.
	for y := 1; y < height-1; y++ {
		row := y * width
		prevRow := row - width
		nextRow := row + width
		x := 0
		for ; x <= width-4; x += 4 {
			out[row+x] = tmp[prevRow+x] & tmp[row+x] & tmp[nextRow+x]
			out[row+x+1] = tmp[prevRow+x+1] & tmp[row+x+1] & tmp[nextRow+x+1]
			out[row+x+2] = tmp[prevRow+x+2] & tmp[row+x+2] & tmp[nextRow+x+2]
			out[row+x+3] = tmp[prevRow+x+3] & tmp[row+x+3] & tmp[nextRow+x+3]
		}
		for ; x < width; x++ {
			out[row+x] = tmp[prevRow+x] & tmp[row+x] & tmp[nextRow+x]
		}
	}
	// Last row: no bottom neighbor.
	lastRow := (height - 1) * width
	prevLastRow := lastRow - width
	for x := 0; x < width; x++ {
		out[lastRow+x] = tmp[prevLastRow+x] & tmp[lastRow+x]
	}

	bufpool.PutUint8(tmp)
	return out
}

func collectCandidates(
	mask []uint8,
	normalized []uint8,
	toothness []uint8,
	width, height uint32,
	search searchRegion,
) []componentCandidate {
	widthInt := int(width)
	heightInt := int(height)
	visited := make([]bool, len(mask))
	// queue doubles as pixel storage: after BFS completes, queue[0:len(queue)]
	// holds every pixel index in the component in BFS order.
	queue := make([]int, 0, 1024)
	candidates := make([]componentCandidate, 0)

	for y := int(search.y); y < int(search.y+search.height); y++ {
		for x := int(search.x); x < int(search.x+search.width); x++ {
			startIndex := y*widthInt + x
			if visited[startIndex] || mask[startIndex] == 0 {
				continue
			}

			visited[startIndex] = true
			queue = append(queue[:0], startIndex)
			head := 0

			minX := uint32(x)
			maxX := uint32(x)
			minY := uint32(y)
			maxY := uint32(y)
			var intensitySum uint64
			var toothnessSum uint64

			for head < len(queue) {
				index := queue[head]
				head++

				px := uint32(index % widthInt)
				py := uint32(index / widthInt)
				minX = minUint32(minX, px)
				maxX = maxUint32(maxX, px)
				minY = minUint32(minY, py)
				maxY = maxUint32(maxY, py)
				intensitySum += uint64(normalized[index])
				toothnessSum += uint64(toothness[index])

				for ny := maxInt(int(py)-1, 0); ny <= minInt(int(py)+1, heightInt-1); ny++ {
					for nx := maxInt(int(px)-1, 0); nx <= minInt(int(px)+1, widthInt-1); nx++ {
						neighbor := ny*widthInt + nx
						if !visited[neighbor] && mask[neighbor] != 0 {
							visited[neighbor] = true
							queue = append(queue, neighbor)
						}
					}
				}
			}

			area := uint32(len(queue))
			if area == 0 {
				continue
			}

			bbox := contracts.BoundingBox{
				X:      minX,
				Y:      minY,
				Width:  maxX - minX + 1,
				Height: maxY - minY + 1,
			}
			meanIntensity := float64(intensitySum) / float64(area)
			meanToothness := float64(toothnessSum) / float64(area)
			strict := isStrictToothCandidate(area, bbox, search)
			score := scoreCandidate(area, bbox, search, meanIntensity, meanToothness, strict)

			// Only allocate a pixel slice for components that will pass the
			// area filter in selectDetectedCandidates. Small components are
			// counted and scored but their pixel indices are not kept.
			var pixels []int
			if area > minDetectedArea {
				pixels = make([]int, len(queue))
				copy(pixels, queue)
			}

			candidates = append(candidates, componentCandidate{
				pixels: pixels,
				bbox:   bbox,
				area:   area,
				score:  score,
				strict: strict,
			})
		}
	}

	return candidates
}

func isStrictToothCandidate(area uint32, bbox contracts.BoundingBox, search searchRegion) bool {
	areaRatio := float64(area) / float64(maxUint32(search.area(), 1))
	widthRatio := float64(bbox.Width) / float64(maxUint32(search.width, 1))
	heightRatio := float64(bbox.Height) / float64(maxUint32(search.height, 1))
	aspectRatio := float64(bbox.Height) / float64(maxUint32(bbox.Width, 1))

	return areaRatio >= 0.001 &&
		areaRatio <= 0.035 &&
		widthRatio >= 0.02 &&
		widthRatio <= 0.16 &&
		heightRatio >= 0.12 &&
		heightRatio <= 0.68 &&
		aspectRatio >= 0.8 &&
		aspectRatio <= 4.5
}

func scoreCandidate(
	area uint32,
	bbox contracts.BoundingBox,
	search searchRegion,
	meanIntensity float64,
	meanToothness float64,
	strict bool,
) float64 {
	searchArea := float64(maxUint32(search.area(), 1))
	areaScore := clamp01(float64(area) / (searchArea * 0.02))
	heightScore := clamp01(float64(bbox.Height) / (float64(search.height) * 0.46))
	widthRatio := float64(bbox.Width) / float64(maxUint32(search.width, 1))
	widthScore := 1.0 - math.Min(math.Abs(widthRatio-0.08)/0.08, 1.0)
	aspectRatio := float64(bbox.Height) / float64(maxUint32(bbox.Width, 1))
	aspectScore := 1.0 - math.Min(math.Abs(aspectRatio-1.9)/1.9, 1.0)
	fillRatio := float64(area) / float64(maxUint32(bbox.Width*bbox.Height, 1))
	fillScore := 1.0 - math.Min(math.Abs(fillRatio-0.42)/0.42, 1.0)
	meanScore := clamp01((meanIntensity - 110.0) / 120.0)
	toothnessScore := clamp01((meanToothness - 120.0) / 100.0)
	centerX := float64(bbox.X) + float64(bbox.Width)/2.0
	searchCenterX := float64(search.x) + float64(search.width)/2.0
	centerScore := 1.0 - math.Min(math.Abs(centerX-searchCenterX)/(float64(search.width)/2.0), 1.0)

	score := 0.17*areaScore +
		0.17*heightScore +
		0.15*aspectScore +
		0.10*widthScore +
		0.09*fillScore +
		0.06*meanScore +
		0.06*toothnessScore +
		0.20*centerScore

	if !strict {
		score *= 0.86
	}

	return score
}

func selectDetectedCandidates(candidates []componentCandidate) []componentCandidate {
	detectedCandidates := make([]componentCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.area > minDetectedArea {
			detectedCandidates = append(detectedCandidates, candidate)
		}
	}
	sortDetectedCandidates(detectedCandidates)
	return detectedCandidates
}

// selectPrimaryCandidate picks the tooth we surface as "the" primary.
// Strict first — any component that passed isStrictToothCandidate's
// area/aspect/size gates — and if nothing qualifies we fall back to the
// best-scoring relaxed candidate. The fallback is deliberate: detection
// never returns "no tooth found" while any plausible component exists,
// but the caller (AnalyzeGrayscalePixels) appends a relaxed-candidate
// warning to the output so the UI can say so. That warning is contract.
func selectPrimaryCandidate(candidates []componentCandidate) *componentCandidate {
	var bestStrict *componentCandidate
	for index := range candidates {
		candidate := &candidates[index]
		if candidate.strict && (bestStrict == nil || candidate.score > bestStrict.score) {
			bestStrict = candidate
		}
	}
	if bestStrict != nil {
		return bestStrict
	}

	var best *componentCandidate
	for index := range candidates {
		candidate := &candidates[index]
		if best == nil || candidate.score > best.score {
			best = candidate
		}
	}

	return best
}

func sortDetectedCandidates(candidates []componentCandidate) {
	sort.Slice(candidates, func(leftIndex, rightIndex int) bool {
		left := candidates[leftIndex]
		right := candidates[rightIndex]

		leftCenterX := candidateCenterX(left)
		rightCenterX := candidateCenterX(right)
		if leftCenterX != rightCenterX {
			return leftCenterX < rightCenterX
		}

		leftCenterY := candidateCenterY(left)
		rightCenterY := candidateCenterY(right)
		if leftCenterY != rightCenterY {
			return leftCenterY < rightCenterY
		}

		return left.score > right.score
	})
}

func candidateCenterX(candidate componentCandidate) uint64 {
	return uint64(candidate.bbox.X)*2 + uint64(candidate.bbox.Width)
}

func candidateCenterY(candidate componentCandidate) uint64 {
	return uint64(candidate.bbox.Y)*2 + uint64(candidate.bbox.Height)
}

type indexedCandidate struct {
	index     int
	candidate componentCandidate
}

func buildPanoramicProfileCandidates(
	normalized []uint8,
	toothness []uint8,
	mask []uint8,
	imageWidth uint32,
	search searchRegion,
	splitY uint32,
) ([]componentCandidate, []contracts.ToothGeometry) {
	xInset := maxUint32(search.width/16, 48)
	profileX := search.x + xInset
	profileWidth := maxUint32(saturatingSubUint32(search.width, xInset*2), 1)
	upperProfileBand := searchRegion{
		x:      profileX,
		y:      search.y + uint32(math.Round(float64(search.height)*0.18)),
		width:  profileWidth,
		height: uint32(math.Round(float64(search.height) * 0.16)),
	}
	lowerProfileBand := searchRegion{
		x:      profileX,
		y:      search.y + uint32(math.Round(float64(search.height)*0.52)),
		width:  profileWidth,
		height: uint32(math.Round(float64(search.height) * 0.14)),
	}
	upperBoxBand := searchRegion{
		x:      profileX,
		y:      search.y + uint32(math.Round(float64(search.height)*0.17)),
		width:  profileWidth,
		height: uint32(math.Round(float64(search.height) * 0.31)),
	}
	lowerBoxBand := searchRegion{
		x:      profileX,
		y:      search.y + uint32(math.Round(float64(search.height)*0.43)),
		width:  profileWidth,
		height: uint32(math.Round(float64(search.height) * 0.34)),
	}
	spans := buildPanoramicSharedSpans(normalized, toothness, imageWidth, upperProfileBand, lowerProfileBand)
	if len(spans) == 0 {
		return nil, nil
	}

	upperCandidates, upperGeometries := buildArchProfileCandidates(
		normalized,
		toothness,
		mask,
		imageWidth,
		search,
		upperProfileBand,
		upperBoxBand,
		splitY,
		true,
		spans,
	)
	lowerCandidates, lowerGeometries := buildArchProfileCandidates(
		normalized,
		toothness,
		mask,
		imageWidth,
		search,
		lowerProfileBand,
		lowerBoxBand,
		splitY,
		false,
		spans,
	)

	type pairedCandidate struct {
		candidate componentCandidate
		geometry  contracts.ToothGeometry
	}
	paired := make([]pairedCandidate, 0, len(upperCandidates)+len(lowerCandidates))
	for index := range upperCandidates {
		paired = append(paired, pairedCandidate{candidate: upperCandidates[index], geometry: upperGeometries[index]})
	}
	for index := range lowerCandidates {
		paired = append(paired, pairedCandidate{candidate: lowerCandidates[index], geometry: lowerGeometries[index]})
	}
	sort.SliceStable(paired, func(left, right int) bool {
		if candidateCenterX(paired[left].candidate) != candidateCenterX(paired[right].candidate) {
			return candidateCenterX(paired[left].candidate) < candidateCenterX(paired[right].candidate)
		}
		return candidateCenterY(paired[left].candidate) < candidateCenterY(paired[right].candidate)
	})

	candidates := make([]componentCandidate, len(paired))
	geometries := make([]contracts.ToothGeometry, len(paired))
	for index, entry := range paired {
		candidates[index] = entry.candidate
		geometries[index] = entry.geometry
	}
	return candidates, geometries
}

func buildArchProfileCandidates(
	normalized []uint8,
	toothness []uint8,
	mask []uint8,
	imageWidth uint32,
	search searchRegion,
	profileBand searchRegion,
	boxBand searchRegion,
	splitY uint32,
	isUpper bool,
	spans []profileSpan,
) ([]componentCandidate, []contracts.ToothGeometry) {
	if len(spans) == 0 {
		return nil, nil
	}
	archSpans := buildArchAdjustedSpans(normalized, toothness, imageWidth, profileBand, spans, isUpper)
	if len(archSpans) == 0 {
		archSpans = spans
	}
	candidates := make([]componentCandidate, 0, len(spans))
	geometries := make([]contracts.ToothGeometry, 0, len(spans))
	for _, span := range archSpans {
		left := profileBand.x + uint32(span.start)
		right := profileBand.x + uint32(span.end)
		bbox := refinePanoramicToothBox(
			normalized,
			toothness,
			mask,
			imageWidth,
			search,
			profileBand,
			boxBand,
			splitY,
			left,
			right,
			isUpper,
		)
		geometry := panoramicGeometryFromBox(bbox)

		profileValue := meanPanoramicProfileValue(normalized, toothness, imageWidth, profileBand, span)
		area := bbox.Width * maxUint32(minUint32(bbox.Height/5, 60), 36)
		strict := isStrictToothCandidate(area, bbox, search)
		score := scoreCandidate(area, bbox, search, profileValue, profileValue, strict)
		candidates = append(candidates, componentCandidate{
			bbox:   bbox,
			area:   area,
			score:  score,
			strict: strict,
		})
		geometries = append(geometries, geometry)
	}

	return candidates, geometries
}

func panoramicGeometryFromBox(
	bbox contracts.BoundingBox,
) contracts.ToothGeometry {
	centerX := bbox.X + bbox.Width/2
	lineY := bbox.Y + bbox.Height/2

	return contracts.ToothGeometry{
		BoundingBox: bbox,
		WidthLine: contracts.LineSegment{
			Start: contracts.Point{X: bbox.X, Y: lineY},
			End:   contracts.Point{X: bbox.X + bbox.Width - 1, Y: lineY},
		},
		HeightLine: contracts.LineSegment{
			Start: contracts.Point{X: centerX, Y: bbox.Y},
			End:   contracts.Point{X: centerX, Y: bbox.Y + bbox.Height - 1},
		},
		Outline: []contracts.Point{},
	}
}

func refinePanoramicToothBox(
	normalized []uint8,
	toothness []uint8,
	mask []uint8,
	imageWidth uint32,
	search searchRegion,
	profileBand searchRegion,
	boxBand searchRegion,
	splitY uint32,
	left uint32,
	right uint32,
	isUpper bool,
) contracts.BoundingBox {
	if right < left {
		right = left
	}
	centerX := estimatePanoramicCenterX(normalized, toothness, imageWidth, profileBand, left, right)
	centerY := estimatePanoramicSeedY(normalized, toothness, mask, imageWidth, profileBand, left, right)
	refinedLeft, refinedRight := refinePanoramicHorizontalBounds(
		normalized,
		toothness,
		mask,
		imageWidth,
		profileBand,
		left,
		right,
		centerX,
		centerY,
	)
	refinedTop, refinedBottom := refinePanoramicVerticalBounds(
		normalized,
		toothness,
		mask,
		imageWidth,
		search,
		boxBand,
		splitY,
		refinedLeft,
		refinedRight,
		centerY,
		isUpper,
	)

	return contracts.BoundingBox{
		X:      refinedLeft,
		Y:      refinedTop,
		Width:  maxUint32(saturatingSubUint32(refinedRight, refinedLeft)+1, 1),
		Height: maxUint32(saturatingSubUint32(refinedBottom, refinedTop)+1, 1),
	}
}

func estimatePanoramicCenterX(
	normalized []uint8,
	toothness []uint8,
	imageWidth uint32,
	band searchRegion,
	left uint32,
	right uint32,
) uint32 {
	if right <= left {
		return left
	}
	targetX := left + (right-left)/2
	bestX := targetX
	bestScore := -1.0
	for x := left; x <= right; x++ {
		var sum float64
		for y := band.y; y < band.y+band.height; y++ {
			index := int(y*imageWidth + x)
			sum += float64(normalized[index])*0.35 + float64(toothness[index])*0.65
		}
		score := sum - float64(absInt(int(x)-int(targetX)))*8.0
		if score > bestScore {
			bestScore = score
			bestX = x
		}
	}
	return bestX
}

func estimatePanoramicSeedY(
	normalized []uint8,
	toothness []uint8,
	mask []uint8,
	imageWidth uint32,
	band searchRegion,
	left uint32,
	right uint32,
) uint32 {
	bestY := band.y + band.height/2
	bestScore := -1.0
	for y := band.y; y < band.y+band.height; y++ {
		var sum float64
		var count float64
		for x := left; x <= right; x++ {
			index := int(y*imageWidth + x)
			sum += float64(normalized[index])*0.30 + float64(toothness[index])*0.50 + float64(mask[index])*90.0
			count++
		}
		if count == 0 {
			continue
		}
		score := sum / count
		if score > bestScore {
			bestScore = score
			bestY = y
		}
	}
	return bestY
}

func refinePanoramicHorizontalBounds(
	normalized []uint8,
	toothness []uint8,
	mask []uint8,
	imageWidth uint32,
	profileBand searchRegion,
	initialLeft uint32,
	initialRight uint32,
	centerX uint32,
	centerY uint32,
) (uint32, uint32) {
	windowTop := maxUint32(centerY-18, profileBand.y)
	windowBottom := minUint32(centerY+18, profileBand.y+profileBand.height-1)
	searchLeft := maxUint32(saturatingSubUint32(initialLeft, 4), profileBand.x)
	searchRight := minUint32(initialRight+4, profileBand.x+profileBand.width-1)
	initialWidth := int(maxUint32(saturatingSubUint32(initialRight, initialLeft)+1, 1))
	profile := make([]float64, profileBand.width)
	for x := searchLeft; x <= searchRight; x++ {
		var sum float64
		var count float64
		for y := windowTop; y <= windowBottom; y++ {
			index := int(y*imageWidth + x)
			sum += float64(normalized[index])*0.30 + float64(toothness[index])*0.50 + float64(mask[index])*90.0
			count++
		}
		if count > 0 {
			profile[x-profileBand.x] = sum / count
		}
	}
	centerIndex := int(saturatingSubUint32(centerX, profileBand.x))
	centerIndex = clampInt(centerIndex, int(searchLeft-profileBand.x), int(searchRight-profileBand.x))
	searchSlice := append([]float64(nil), profile[searchLeft-profileBand.x:searchRight-profileBand.x+1]...)
	baseline := percentileFloat64(searchSlice, 0.22)
	peak := profile[centerIndex]
	threshold := baseline + (peak-baseline)*0.32
	leftIndex := centerIndex
	minIndex := int(searchLeft - profileBand.x)
	maxIndex := int(searchRight - profileBand.x)
	for leftIndex > minIndex && profile[leftIndex-1] >= threshold {
		leftIndex--
	}
	rightIndex := centerIndex
	for rightIndex < maxIndex && profile[rightIndex+1] >= threshold {
		rightIndex++
	}
	minWidth := maxInt(int(math.Round(float64(initialWidth)*0.82)), 36)
	maxWidth := minInt(int(math.Round(float64(initialWidth)*0.98)), maxInt(initialWidth+4, 48))
	width := rightIndex - leftIndex + 1
	if width < minWidth {
		leftIndex = maxInt(centerIndex-minWidth/2, minIndex)
		rightIndex = minInt(leftIndex+minWidth-1, maxIndex)
		leftIndex = maxInt(rightIndex-minWidth+1, minIndex)
	}
	if width > maxWidth {
		leftIndex = maxInt(centerIndex-maxWidth/2, minIndex)
		rightIndex = minInt(leftIndex+maxWidth-1, maxIndex)
		leftIndex = maxInt(rightIndex-maxWidth+1, minIndex)
	}
	return profileBand.x + uint32(leftIndex), profileBand.x + uint32(rightIndex)
}

func refinePanoramicVerticalBounds(
	normalized []uint8,
	toothness []uint8,
	mask []uint8,
	imageWidth uint32,
	search searchRegion,
	boxBand searchRegion,
	splitY uint32,
	left uint32,
	right uint32,
	seedY uint32,
	isUpper bool,
) (uint32, uint32) {
	laneWidth := maxUint32(saturatingSubUint32(right, left)+1, 1)
	innerInset := minUint32(maxUint32(laneWidth/8, 2), 10)
	innerLeft := minUint32(left+innerInset, right)
	innerRight := maxUint32(right-innerInset, innerLeft)
	flankWidth := minUint32(maxUint32(laneWidth/2, 14), 64)
	leftOuterStart := maxUint32(saturatingSubUint32(left, flankWidth), search.x)
	leftOuterEnd := left
	if leftOuterEnd > search.x {
		leftOuterEnd--
	}
	rightOuterStart := minUint32(right+1, search.x+search.width-1)
	rightOuterEnd := minUint32(right+flankWidth, search.x+search.width-1)
	var fitBand searchRegion
	if isUpper {
		fitStart := search.y + uint32(math.Round(float64(search.height)*0.24))
		fitBand = searchRegion{
			x:      left,
			y:      fitStart,
			width:  maxUint32(saturatingSubUint32(right, left)+1, 1),
			height: maxUint32(saturatingSubUint32(splitY, fitStart), 1),
		}
	} else {
		fitBand = searchRegion{
			x:      left,
			y:      maxUint32(splitY, search.y+uint32(math.Round(float64(search.height)*0.36))),
			width:  maxUint32(saturatingSubUint32(right, left)+1, 1),
			height: maxUint32(saturatingSubUint32(search.y+search.height, maxUint32(splitY, search.y+uint32(math.Round(float64(search.height)*0.36)))), 1),
		}
	}
	fitBand = clampSearchRegionToSearch(fitBand, search)
	rowProfile := make([]float64, fitBand.height)
	for y := fitBand.y; y < fitBand.y+fitBand.height; y++ {
		var innerNorm float64
		var innerToothness float64
		var innerMask float64
		var innerCount float64
		for x := innerLeft; x <= innerRight; x++ {
			index := int(y*imageWidth + x)
			innerNorm += float64(normalized[index])
			innerToothness += float64(toothness[index])
			innerMask += float64(mask[index])
			innerCount++
		}
		if innerCount == 0 {
			continue
		}
		outerNorm := 0.0
		outerCount := 0.0
		for x := leftOuterStart; x <= leftOuterEnd && leftOuterStart <= leftOuterEnd; x++ {
			index := int(y*imageWidth + x)
			outerNorm += float64(normalized[index])
			outerCount++
		}
		for x := rightOuterStart; x <= rightOuterEnd && rightOuterStart <= rightOuterEnd; x++ {
			index := int(y*imageWidth + x)
			outerNorm += float64(normalized[index])
			outerCount++
		}
		innerNorm /= innerCount
		innerToothness /= innerCount
		innerMask /= innerCount
		if outerCount > 0 {
			outerNorm /= outerCount
		}
		rowProfile[y-fitBand.y] = innerNorm*0.58 + innerToothness*0.28 + innerMask*35.0 - outerNorm*0.46
	}
	seedIndex := clampInt(int(saturatingSubUint32(seedY, fitBand.y)), 0, len(rowProfile)-1)
	peak := rowProfile[seedIndex]
	baseline := percentileFloat64(rowProfile, 0.20)
	threshold := baseline + (peak-baseline)*0.12
	minHeight := 150
	maxHeight := 300
	if isUpper {
		minHeight = 180
		maxHeight = 320
	}

	topIndex := seedIndex
	misses := 0
	for topIndex > 0 {
		next := topIndex - 1
		if rowProfile[next] < threshold {
			misses++
			if misses >= 6 {
				break
			}
		} else {
			misses = 0
		}
		topIndex = next
	}
	bottomIndex := seedIndex
	misses = 0
	for bottomIndex+1 < len(rowProfile) {
		next := bottomIndex + 1
		if rowProfile[next] < threshold {
			misses++
			if misses >= 8 {
				break
			}
		} else {
			misses = 0
		}
		bottomIndex = next
	}
	height := bottomIndex - topIndex + 1
	if height < minHeight {
		needed := minHeight - height
		topIndex = maxInt(topIndex-needed/3, 0)
		bottomIndex = minInt(bottomIndex+(needed-needed/3), len(rowProfile)-1)
	}
	if bottomIndex-topIndex+1 > maxHeight {
		if isUpper {
			bottomIndex = minInt(topIndex+maxHeight-1, len(rowProfile)-1)
		} else {
			topIndex = maxInt(bottomIndex-maxHeight+1, 0)
		}
	}
	top := fitBand.y + uint32(topIndex)
	bottom := fitBand.y + uint32(bottomIndex)
	if isUpper {
		top = maxUint32(saturatingSubUint32(top, 6), boxBand.y)
		minBottom := maxUint32(splitY+8, top+1)
		bottom = maxUint32(bottom+18, minBottom)
		bottom = minUint32(bottom, search.y+search.height-1)
	} else {
		top = minUint32(top, splitY+18)
		top = maxUint32(saturatingSubUint32(top, 12), search.y)
		bottom = minUint32(bottom+18, search.y+search.height-1)
	}
	return top, bottom
}

func clampSearchRegionToSearch(region searchRegion, search searchRegion) searchRegion {
	if region.x < search.x {
		region.x = search.x
	}
	if region.y < search.y {
		region.y = search.y
	}
	searchRight := search.x + search.width
	searchBottom := search.y + search.height
	regionRight := region.x + region.width
	regionBottom := region.y + region.height
	if regionRight > searchRight {
		region.width = maxUint32(saturatingSubUint32(searchRight, region.x), 1)
	}
	if regionBottom > searchBottom {
		region.height = maxUint32(saturatingSubUint32(searchBottom, region.y), 1)
	}
	return region
}

type profileSpan struct {
	start int
	end   int
}

func buildPanoramicSharedSpans(
	normalized []uint8,
	toothness []uint8,
	imageWidth uint32,
	upperProfileBand searchRegion,
	lowerProfileBand searchRegion,
) []profileSpan {
	upperProfile := buildArchColumnProfile(normalized, toothness, imageWidth, upperProfileBand)
	lowerProfile := buildArchColumnProfile(normalized, toothness, imageWidth, lowerProfileBand)
	if len(upperProfile) == 0 || len(lowerProfile) == 0 || len(upperProfile) != len(lowerProfile) {
		return nil
	}

	combined := make([]float64, len(upperProfile))
	for index := range combined {
		combined[index] = upperProfile[index]*0.52 + lowerProfile[index]*0.48
	}
	smoothed := smoothFloatProfile(combined, 17)
	archStart, archEnd := estimateArchSpan(smoothed)
	if archEnd-archStart+1 < 320 {
		return nil
	}
	centerGap := estimateArchCenterGap(smoothed, archStart, archEnd)
	return buildTemplateToothSpans(smoothed, archStart, archEnd, centerGap)
}

func meanPanoramicProfileValue(
	normalized []uint8,
	toothness []uint8,
	imageWidth uint32,
	profileBand searchRegion,
	span profileSpan,
) float64 {
	if span.end < span.start || profileBand.height == 0 {
		return 0
	}
	left := profileBand.x + uint32(span.start)
	right := profileBand.x + uint32(span.end)
	var sum float64
	var count float64
	for y := profileBand.y; y < profileBand.y+profileBand.height; y++ {
		rowStart := int(y * imageWidth)
		for x := left; x <= right; x++ {
			index := rowStart + int(x)
			sum += float64(normalized[index])*0.65 + float64(toothness[index])*0.35
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / count
}

func buildArchAdjustedSpans(
	normalized []uint8,
	toothness []uint8,
	imageWidth uint32,
	profileBand searchRegion,
	templateSpans []profileSpan,
	isUpper bool,
) []profileSpan {
	if len(templateSpans) == 0 {
		return nil
	}
	profile := buildArchColumnProfile(normalized, toothness, imageWidth, profileBand)
	if len(profile) == 0 {
		return nil
	}
	smoothed := smoothFloatProfile(profile, 9)
	centers := make([]int, len(templateSpans))
	halfWidths := make([]int, len(templateSpans))
	for index, span := range templateSpans {
		targetCenter := (span.start + span.end) / 2
		targetWidth := span.end - span.start + 1
		halfWidths[index] = maxInt(targetWidth/2, 14)
		searchRadius := minInt(maxInt(targetWidth/5, 8), 22)
		distancePenalty := 1.8
		if !isUpper {
			searchRadius = minInt(maxInt(targetWidth/3, 12), 34)
			distancePenalty = 1.1
		}
		bestIndex := clampInt(targetCenter, 0, len(smoothed)-1)
		bestScore := smoothed[bestIndex]
		for candidate := maxInt(targetCenter-searchRadius, 0); candidate <= minInt(targetCenter+searchRadius, len(smoothed)-1); candidate++ {
			score := smoothed[candidate] - float64(absInt(candidate-targetCenter))*distancePenalty
			if score > bestScore {
				bestIndex = candidate
				bestScore = score
			}
		}
		centers[index] = bestIndex
	}
	minDistance := 28
	if !isUpper {
		minDistance = 24
	}
	for index := 1; index < len(centers); index++ {
		if centers[index]-centers[index-1] < minDistance {
			centers[index] = minInt(centers[index-1]+minDistance, len(smoothed)-1)
		}
	}
	for index := len(centers) - 2; index >= 0; index-- {
		if centers[index+1]-centers[index] < minDistance {
			centers[index] = maxInt(centers[index+1]-minDistance, 0)
		}
	}

	boundaries := make([]int, len(centers)+1)
	boundaries[0] = templateSpans[0].start
	boundaries[len(boundaries)-1] = templateSpans[len(templateSpans)-1].end
	for index := 1; index < len(centers); index++ {
		leftCenter := centers[index-1]
		rightCenter := centers[index]
		searchStart := minInt(maxInt(leftCenter+4, 0), len(smoothed)-1)
		searchEnd := maxInt(minInt(rightCenter-4, len(smoothed)-1), searchStart)
		boundary := (leftCenter + rightCenter) / 2
		bestValue := smoothed[clampInt(boundary, 0, len(smoothed)-1)]
		for candidate := searchStart; candidate <= searchEnd; candidate++ {
			if smoothed[candidate] < bestValue {
				bestValue = smoothed[candidate]
				boundary = candidate
			}
		}
		boundaries[index] = boundary
	}

	spans := make([]profileSpan, 0, len(centers))
	for index, center := range centers {
		leftBound := boundaries[index]
		if index > 0 {
			leftBound++
		}
		rightBound := boundaries[index+1]
		width := rightBound - leftBound + 1
		targetWidth := minInt(maxInt(halfWidths[index]*2, 28), maxInt(width, 28))
		left := maxInt(center-targetWidth/2, leftBound)
		right := minInt(left+targetWidth-1, rightBound)
		left = maxInt(right-targetWidth+1, leftBound)
		if right-left+1 < 28 {
			left = maxInt(center-14, leftBound)
			right = minInt(left+27, rightBound)
			left = maxInt(right-27, leftBound)
		}
		spans = append(spans, profileSpan{start: left, end: right})
	}
	return spans
}

func estimateArchSpan(values []float64) (int, int) {
	if len(values) == 0 {
		return 0, -1
	}

	baseline := percentileFloat64(values, 0.18)
	peak := percentileFloat64(values, 0.88)
	threshold := baseline + (peak-baseline)*0.22
	start := 0
	for start < len(values)-1 && values[start] < threshold {
		start++
	}
	end := len(values) - 1
	for end > start && values[end] < threshold {
		end--
	}
	start = maxInt(start+10, 0)
	end = minInt(end-10, len(values)-1)
	return start, end
}

func estimateArchCenterGap(values []float64, archStart int, archEnd int) int {
	if len(values) == 0 {
		return 0
	}
	center := (archStart + archEnd) / 2
	windowRadius := minInt(maxInt((archEnd-archStart)/10, 28), 96)
	windowStart := maxInt(center-windowRadius, archStart)
	windowEnd := minInt(center+windowRadius, archEnd)
	bestIndex := center
	bestValue := values[center]
	for index := windowStart; index <= windowEnd; index++ {
		if values[index] < bestValue {
			bestIndex = index
			bestValue = values[index]
		}
	}
	return bestIndex
}

func buildTemplateToothSpans(values []float64, archStart int, archEnd int, centerGap int) []profileSpan {
	if archEnd <= archStart {
		return nil
	}

	edgeInset := maxInt((archEnd-archStart)/16, 52)
	archStart = minInt(archStart+edgeInset, archEnd)
	archEnd = maxInt(archEnd-edgeInset, archStart)
	gapHalfWidth := maxInt((archEnd-archStart)/96, 2)
	leftEnd := maxInt(centerGap-gapHalfWidth, archStart+80)
	rightStart := minInt(centerGap+gapHalfWidth, archEnd-80)
	if leftEnd-archStart < 180 || archEnd-rightStart < 180 {
		leftEnd = (archStart + archEnd) / 2
		rightStart = leftEnd + 1
	}

	posteriorToAnteriorWeights := []float64{1.08, 1.04, 1.00, 0.98, 0.96, 0.94, 0.92}
	spans := make([]profileSpan, 0, 10)
	spans = append(spans, buildHalfArchTemplateSpans(values, archStart, leftEnd, posteriorToAnteriorWeights, false)...)
	spans = append(spans, buildHalfArchTemplateSpans(values, rightStart, archEnd, posteriorToAnteriorWeights, true)...)
	return spans
}

func buildHalfArchTemplateSpans(
	values []float64,
	start int,
	end int,
	weights []float64,
	towardRight bool,
) []profileSpan {
	if end <= start || len(weights) == 0 {
		return nil
	}

	orderedWeights := append([]float64(nil), weights...)
	if towardRight {
		reverseFloat64s(orderedWeights)
	}
	centers := buildWeightedTemplateCenters(values, start, end, orderedWeights)
	if len(centers) == 0 {
		return nil
	}

	boundaries := make([]int, len(centers)+1)
	boundaries[0] = start
	boundaries[len(boundaries)-1] = end
	for index := 1; index < len(centers); index++ {
		leftCenter := centers[index-1]
		rightCenter := centers[index]
		searchStart := minInt(maxInt(leftCenter+4, start), end)
		searchEnd := maxInt(minInt(rightCenter-4, end), searchStart)
		boundary := (leftCenter + rightCenter) / 2
		bestValue := values[clampInt(boundary, start, end)]
		for candidate := searchStart; candidate <= searchEnd; candidate++ {
			if values[candidate] < bestValue {
				bestValue = values[candidate]
				boundary = candidate
			}
		}
		boundaries[index] = boundary
	}

	spans := make([]profileSpan, 0, len(centers))
	for index, center := range centers {
		slotStart := boundaries[index]
		if index > 0 {
			slotStart++
		}
		slotEnd := boundaries[index+1]
		if slotEnd <= slotStart {
			slotEnd = minInt(slotStart+31, end)
		}
		slotWidth := slotEnd - slotStart + 1
		left := slotStart
		right := slotEnd
		if slotWidth > 96 {
			targetWidth := minInt(maxInt(int(math.Round(float64(slotWidth)*0.86)), 42), 96)
			left = maxInt(center-targetWidth/2, slotStart)
			right = minInt(left+targetWidth-1, slotEnd)
			left = maxInt(right-targetWidth+1, slotStart)
		} else {
			edgeInset := minInt(maxInt(slotWidth/24, 1), 3)
			left = minInt(left+edgeInset, right)
			right = maxInt(right-edgeInset, left)
		}
		if right-left+1 < 28 {
			left = maxInt(center-14, start)
			right = minInt(left+27, end)
			left = maxInt(right-27, start)
		}
		spans = append(spans, profileSpan{start: left, end: right})
	}
	return spans
}

func buildWeightedTemplateCenters(values []float64, start int, end int, weights []float64) []int {
	if end <= start || len(weights) == 0 {
		return nil
	}
	totalWidth := end - start + 1
	weightSum := 0.0
	for _, weight := range weights {
		weightSum += weight
	}
	if weightSum <= 0 {
		return nil
	}

	centers := make([]int, len(weights))
	offset := 0.0
	for index, weight := range weights {
		slotStart := float64(start) + (offset/weightSum)*float64(totalWidth)
		offset += weight
		slotEnd := float64(start) + (offset/weightSum)*float64(totalWidth)
		targetCenter := int(math.Round((slotStart + slotEnd - 1) / 2.0))
		searchRadius := minInt(maxInt(int(math.Round((slotEnd-slotStart)/10.0)), 4), 8)
		bestIndex := clampInt(targetCenter, start, end)
		bestScore := values[bestIndex]
		for candidate := maxInt(bestIndex-searchRadius, start); candidate <= minInt(bestIndex+searchRadius, end); candidate++ {
			score := values[candidate] - float64(absInt(candidate-targetCenter))*2.5
			if score > bestScore {
				bestIndex = candidate
				bestScore = score
			}
		}
		centers[index] = bestIndex
	}

	minDistance := maxInt(totalWidth/(len(weights)*3), 32)
	for index := 1; index < len(centers); index++ {
		if centers[index]-centers[index-1] < minDistance {
			centers[index] = minInt(centers[index-1]+minDistance, end)
		}
	}
	for index := len(centers) - 2; index >= 0; index-- {
		if centers[index+1]-centers[index] < minDistance {
			centers[index] = maxInt(centers[index+1]-minDistance, start)
		}
	}
	return centers
}

func refineArchVerticalBounds(
	normalized []uint8,
	toothness []uint8,
	mask []uint8,
	imageWidth uint32,
	boxBand searchRegion,
	left uint32,
	right uint32,
	isUpper bool,
) (uint32, uint32) {
	if boxBand.height == 0 {
		return boxBand.y, boxBand.y
	}

	xInset := minUint32(maxUint32((right-left+1)/6, 2), 12)
	sampleLeft := minUint32(left+xInset, right)
	sampleRight := maxUint32(right-xInset, sampleLeft)
	profile := make([]float64, boxBand.height)
	for offsetY := uint32(0); offsetY < boxBand.height; offsetY++ {
		y := boxBand.y + offsetY
		rowStart := int(y * imageWidth)
		var sum float64
		var count float64
		for x := sampleLeft; x <= sampleRight; x++ {
			index := rowStart + int(x)
			sum += float64(normalized[index])*0.45 + float64(toothness[index])*0.40 + float64(mask[index])*90.0
			count++
		}
		if count > 0 {
			profile[offsetY] = sum / count
		}
	}
	smoothed := smoothFloatProfile(profile, 7)
	start, end := longestHighRun(smoothed)
	if end <= start {
		return boxBand.y, boxBand.y + boxBand.height - 1
	}

	topPad := uint32(18)
	bottomPad := uint32(22)
	if isUpper {
		top := maxUint32(boxBand.y+uint32(start), boxBand.y)
		top = saturatingSubUint32(top, topPad)
		bottom := minUint32(boxBand.y+uint32(end)+bottomPad, boxBand.y+boxBand.height-1)
		return top, bottom
	}

	top := maxUint32(boxBand.y+uint32(start), boxBand.y)
	top = saturatingSubUint32(top, 8)
	bottom := minUint32(boxBand.y+uint32(end)+28, boxBand.y+boxBand.height-1)
	return top, bottom
}

func longestHighRun(values []float64) (int, int) {
	if len(values) == 0 {
		return 0, -1
	}
	baseline := percentileFloat64(values, 0.18)
	peak := percentileFloat64(values, 0.86)
	threshold := baseline + (peak-baseline)*0.34
	bestStart := 0
	bestEnd := -1
	start := -1
	for index, value := range values {
		if value >= threshold {
			if start < 0 {
				start = index
			}
			continue
		}
		if start >= 0 && index-start > bestEnd-bestStart+1 {
			bestStart = start
			bestEnd = index - 1
		}
		start = -1
	}
	if start >= 0 && len(values)-start > bestEnd-bestStart+1 {
		bestStart = start
		bestEnd = len(values) - 1
	}
	return bestStart, bestEnd
}

func percentileFloat64(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	index := int(math.Round(float64(len(sorted)-1) * percentile))
	index = clampInt(index, 0, len(sorted)-1)
	return sorted[index]
}

func meanFloatSlice(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func reverseFloat64s(values []float64) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func clampInt(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func buildArchColumnProfile(
	normalized []uint8,
	toothness []uint8,
	imageWidth uint32,
	band searchRegion,
) []float64 {
	if band.width == 0 || band.height == 0 {
		return nil
	}

	profile := make([]float64, band.width)
	denominator := float64(maxUint32(band.height, 1))
	for xOffset := uint32(0); xOffset < band.width; xOffset++ {
		x := band.x + xOffset
		var sum float64
		for y := band.y; y < band.y+band.height; y++ {
			index := int(y*imageWidth + x)
			sum += float64(normalized[index])*0.65 + float64(toothness[index])*0.35
		}
		profile[xOffset] = sum / denominator
	}
	return profile
}

func smoothFloatProfile(values []float64, radius int) []float64 {
	if len(values) == 0 {
		return nil
	}
	if radius <= 0 {
		return append([]float64(nil), values...)
	}

	prefix := make([]float64, len(values)+1)
	for index, value := range values {
		prefix[index+1] = prefix[index] + value
	}
	smoothed := make([]float64, len(values))
	for index := range values {
		start := maxInt(index-radius, 0)
		end := minInt(index+radius+1, len(values))
		smoothed[index] = (prefix[end] - prefix[start]) / float64(end-start)
	}
	return smoothed
}

func buildToothBandRowProfile(
	normalized []uint8,
	toothness []uint8,
	imageWidth uint32,
	band searchRegion,
) []float64 {
	if band.width == 0 || band.height == 0 {
		return nil
	}

	profile := make([]float64, band.height)
	histogram := make([]uint32, 256)
	for yOffset := uint32(0); yOffset < band.height; yOffset++ {
		for index := range histogram {
			histogram[index] = 0
		}
		y := band.y + yOffset
		rowStart := int(y * imageWidth)
		for x := band.x; x < band.x+band.width; x++ {
			index := rowStart + int(x)
			value := uint8(math.Round(float64(normalized[index])*0.30 + float64(toothness[index])*0.70))
			histogram[value]++
		}
		threshold := histogramPercentileFromUint32(histogram, band.width, 0.72)
		var sum float64
		var count float64
		for x := band.x; x < band.x+band.width; x++ {
			index := rowStart + int(x)
			value := uint8(math.Round(float64(normalized[index])*0.30 + float64(toothness[index])*0.70))
			if value < threshold {
				continue
			}
			sum += float64(value)
			count++
		}
		if count == 0 {
			profile[yOffset] = float64(threshold)
			continue
		}
		profile[yOffset] = sum / count
	}
	return profile
}

func histogramPercentileFromUint32(histogram []uint32, total uint32, percentile float64) uint8 {
	if total == 0 {
		return 0
	}
	targetRank := uint32(math.Round(percentile * float64(total-1)))
	var cumulative uint32
	for value, count := range histogram {
		cumulative += count
		if cumulative > targetRank {
			return uint8(value)
		}
	}
	return uint8(len(histogram) - 1)
}

func argmaxFloatSlice(values []float64) int {
	if len(values) == 0 {
		return 0
	}
	bestIndex := 0
	bestValue := values[0]
	for index := 1; index < len(values); index++ {
		if values[index] > bestValue {
			bestIndex = index
			bestValue = values[index]
		}
	}
	return bestIndex
}

func argminFloatSlice(values []float64) int {
	if len(values) == 0 {
		return 0
	}
	bestIndex := 0
	bestValue := values[0]
	for index := 1; index < len(values); index++ {
		if values[index] < bestValue {
			bestIndex = index
			bestValue = values[index]
		}
	}
	return bestIndex
}

func detectProfilePeaks(values []float64, minPeaks, maxPeaks, minDistance int) []int {
	if len(values) == 0 {
		return nil
	}
	type peak struct {
		index int
		value float64
	}
	allPeaks := make([]peak, 0)
	for index := 1; index < len(values)-1; index++ {
		if values[index] >= values[index-1] && values[index] >= values[index+1] {
			allPeaks = append(allPeaks, peak{index: index, value: values[index]})
		}
	}
	if len(allPeaks) == 0 {
		return nil
	}

	sort.Slice(allPeaks, func(left, right int) bool {
		return allPeaks[left].value > allPeaks[right].value
	})
	selected := make([]peak, 0, len(allPeaks))
	for _, candidate := range allPeaks {
		keep := true
		for _, existing := range selected {
			if absInt(candidate.index-existing.index) < minDistance {
				keep = false
				break
			}
		}
		if keep {
			selected = append(selected, candidate)
		}
	}

	if len(selected) > maxPeaks {
		selected = selected[:maxPeaks]
	}
	if len(selected) < minPeaks {
		for _, candidate := range allPeaks {
			found := false
			for _, existing := range selected {
				if existing.index == candidate.index {
					found = true
					break
				}
			}
			if found {
				continue
			}
			selected = append(selected, candidate)
			if len(selected) >= minPeaks {
				break
			}
		}
	}

	sort.Slice(selected, func(left, right int) bool {
		return selected[left].index < selected[right].index
	})
	indices := make([]int, len(selected))
	for index, peak := range selected {
		indices[index] = peak.index
	}
	return indices
}

func mergeProfilePeaks(values []float64, peaks []int, mergeDistance int, shallowValleyRatio float64) []int {
	if len(peaks) <= 1 {
		return peaks
	}

	merged := make([]int, 0, len(peaks))
	cluster := []int{peaks[0]}
	flush := func() {
		if len(cluster) == 0 {
			return
		}
		bestPeak := cluster[0]
		bestValue := values[bestPeak]
		var weightedIndex float64
		var weightSum float64
		for _, peak := range cluster {
			weight := math.Max(values[peak], 1.0)
			weightedIndex += float64(peak) * weight
			weightSum += weight
			if values[peak] > bestValue {
				bestPeak = peak
				bestValue = values[peak]
			}
		}
		if weightSum > 0 {
			candidate := int(math.Round(weightedIndex / weightSum))
			candidate = minInt(maxInt(candidate, 0), len(values)-1)
			if values[candidate] >= bestValue*0.98 {
				bestPeak = candidate
			}
		}
		merged = append(merged, bestPeak)
		cluster = cluster[:0]
	}

	for index := 1; index < len(peaks); index++ {
		prev := cluster[len(cluster)-1]
		current := peaks[index]
		merge := false
		if current-prev <= mergeDistance {
			merge = true
		} else {
			valley := minFloatSlice(values[prev : current+1])
			ceiling := math.Min(values[prev], values[current])
			if ceiling > 0 && valley/ceiling >= shallowValleyRatio {
				merge = true
			}
		}

		if merge {
			cluster = append(cluster, current)
			continue
		}

		flush()
		cluster = append(cluster, current)
	}
	flush()
	return merged
}

func restoreProfilePeaks(values []float64, peaks []int, originalPeaks []int, targetCount int, minDistance int) []int {
	if len(peaks) >= targetCount || len(originalPeaks) == 0 {
		return peaks
	}

	selected := append([]int(nil), peaks...)
	type scoredPeak struct {
		index int
		value float64
	}
	candidates := make([]scoredPeak, 0, len(originalPeaks))
	for _, peak := range originalPeaks {
		found := false
		for _, existing := range selected {
			if existing == peak {
				found = true
				break
			}
		}
		if found {
			continue
		}
		candidates = append(candidates, scoredPeak{index: peak, value: values[peak]})
	}
	sort.Slice(candidates, func(left, right int) bool {
		return candidates[left].value > candidates[right].value
	})
	for _, candidate := range candidates {
		tooClose := false
		for _, existing := range selected {
			if absInt(existing-candidate.index) < minDistance {
				tooClose = true
				break
			}
		}
		if tooClose {
			continue
		}
		selected = append(selected, candidate.index)
		if len(selected) >= targetCount {
			break
		}
	}
	sort.Ints(selected)
	return selected
}

func minFloatSlice(values []float64) float64 {
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

func geometryFromPixels(
	pixels []int,
	bbox contracts.BoundingBox,
	imageWidth uint32,
) contracts.ToothGeometry {
	bboxWidth := int(bbox.Width)
	bboxHeight := int(bbox.Height)
	rowMin := make([]uint32, bboxHeight)
	rowMax := make([]uint32, bboxHeight)
	rowSeen := make([]bool, bboxHeight)
	colMin := make([]uint32, bboxWidth)
	colMax := make([]uint32, bboxWidth)
	colSeen := make([]bool, bboxWidth)

	for index := range rowMin {
		rowMin[index] = ^uint32(0)
	}
	for index := range colMin {
		colMin[index] = ^uint32(0)
	}

	imageWidthInt := int(imageWidth)
	for _, index := range pixels {
		x := uint32(index % imageWidthInt)
		y := uint32(index / imageWidthInt)
		localX := int(x - bbox.X)
		localY := int(y - bbox.Y)

		rowMin[localY] = minUint32(rowMin[localY], x)
		rowMax[localY] = maxUint32(rowMax[localY], x)
		rowSeen[localY] = true

		colMin[localX] = minUint32(colMin[localX], y)
		colMax[localX] = maxUint32(colMax[localX], y)
		colSeen[localX] = true
	}

	widthLine := contracts.LineSegment{
		Start: contracts.Point{
			X: bbox.X,
			Y: bbox.Y,
		},
		End: contracts.Point{
			X: bbox.X + bbox.Width - 1,
			Y: bbox.Y,
		},
	}
	var bestWidth uint32
	for offset, seen := range rowSeen {
		if !seen {
			continue
		}
		span := rowMax[offset] - rowMin[offset] + 1
		if span > bestWidth {
			bestWidth = span
			widthLine = contracts.LineSegment{
				Start: contracts.Point{
					X: rowMin[offset],
					Y: bbox.Y + uint32(offset),
				},
				End: contracts.Point{
					X: rowMax[offset],
					Y: bbox.Y + uint32(offset),
				},
			}
		}
	}

	heightLine := contracts.LineSegment{
		Start: contracts.Point{
			X: bbox.X,
			Y: bbox.Y,
		},
		End: contracts.Point{
			X: bbox.X,
			Y: bbox.Y + bbox.Height - 1,
		},
	}
	var bestHeight uint32
	for offset, seen := range colSeen {
		if !seen {
			continue
		}
		span := colMax[offset] - colMin[offset] + 1
		if span > bestHeight {
			bestHeight = span
			heightLine = contracts.LineSegment{
				Start: contracts.Point{
					X: bbox.X + uint32(offset),
					Y: colMin[offset],
				},
				End: contracts.Point{
					X: bbox.X + uint32(offset),
					Y: colMax[offset],
				},
			}
		}
	}

	return contracts.ToothGeometry{
		BoundingBox: bbox,
		WidthLine:   widthLine,
		HeightLine:  heightLine,
		Outline:     traceOutlineFromPixels(pixels, bbox, imageWidth),
	}
}

type outlineSegment struct {
	start contracts.Point
	end   contracts.Point
}

func attachClosestOutlines(
	geometries []contracts.ToothGeometry,
	outlined []contracts.ToothGeometry,
) []contracts.ToothGeometry {
	if len(geometries) == 0 || len(outlined) == 0 {
		return geometries
	}

	result := make([]contracts.ToothGeometry, len(geometries))
	copy(result, geometries)
	for index := range result {
		if len(result[index].Outline) > 0 {
			continue
		}

		bestOutline := bestMatchingOutline(result[index].BoundingBox, outlined)
		if len(bestOutline) == 0 {
			continue
		}

		result[index].Outline = bestOutline
	}

	return result
}

func bestMatchingOutline(
	target contracts.BoundingBox,
	outlined []contracts.ToothGeometry,
) []contracts.Point {
	bestOutline := []contracts.Point{}
	bestScore := -1.0
	for _, candidate := range outlined {
		if len(candidate.Outline) == 0 {
			continue
		}
		score := outlineMatchScore(target, candidate.BoundingBox)
		if score > bestScore {
			bestScore = score
			bestOutline = candidate.Outline
		}
	}
	if len(bestOutline) == 0 {
		return []contracts.Point{}
	}

	return clonePointSlice(bestOutline)
}

func outlineMatchScore(target contracts.BoundingBox, source contracts.BoundingBox) float64 {
	intersectionWidth := intersectionLength(target.X, target.Width, source.X, source.Width)
	intersectionHeight := intersectionLength(target.Y, target.Height, source.Y, source.Height)
	intersectionArea := float64(intersectionWidth * intersectionHeight)
	sourceArea := float64(maxUint32(source.Width*source.Height, 1))
	overlapScore := 0.0
	if sourceArea > 0 {
		overlapScore = intersectionArea / sourceArea
	}

	targetCenterX := float64(target.X) + float64(target.Width)/2.0
	targetCenterY := float64(target.Y) + float64(target.Height)/2.0
	sourceCenterX := float64(source.X) + float64(source.Width)/2.0
	sourceCenterY := float64(source.Y) + float64(source.Height)/2.0
	distance := math.Hypot(targetCenterX-sourceCenterX, targetCenterY-sourceCenterY)
	scale := math.Hypot(float64(maxUint32(target.Width, source.Width)), float64(maxUint32(target.Height, source.Height)))
	if scale <= 0 {
		scale = 1
	}
	centerScore := 1.0 / (1.0 + distance/scale)

	return overlapScore*3.0 + centerScore
}

func intersectionLength(startA, lengthA, startB, lengthB uint32) uint32 {
	endA := startA + lengthA
	endB := startB + lengthB
	start := maxUint32(startA, startB)
	end := minUint32(endA, endB)
	if end <= start {
		return 0
	}
	return end - start
}

func traceOutlineFromPixels(
	pixels []int,
	bbox contracts.BoundingBox,
	imageWidth uint32,
) []contracts.Point {
	if len(pixels) == 0 || bbox.Width == 0 || bbox.Height == 0 {
		return []contracts.Point{}
	}

	localWidth := int(bbox.Width)
	localHeight := int(bbox.Height)
	localMask := make([]bool, localWidth*localHeight)
	imageWidthInt := int(imageWidth)
	for _, index := range pixels {
		x := uint32(index % imageWidthInt)
		y := uint32(index / imageWidthInt)
		localX := int(x - bbox.X)
		localY := int(y - bbox.Y)
		if localX < 0 || localX >= localWidth || localY < 0 || localY >= localHeight {
			continue
		}
		localMask[localY*localWidth+localX] = true
	}

	segments := make([]outlineSegment, 0, len(pixels)*2)
	addSegment := func(startX, startY, endX, endY uint32) {
		segments = append(segments, outlineSegment{
			start: contracts.Point{X: bbox.X + startX, Y: bbox.Y + startY},
			end:   contracts.Point{X: bbox.X + endX, Y: bbox.Y + endY},
		})
	}
	isFilled := func(x, y int) bool {
		return x >= 0 && x < localWidth && y >= 0 && y < localHeight && localMask[y*localWidth+x]
	}

	for y := 0; y < localHeight; y++ {
		for x := 0; x < localWidth; x++ {
			if !localMask[y*localWidth+x] {
				continue
			}

			if !isFilled(x, y-1) {
				addSegment(uint32(x), uint32(y), uint32(x+1), uint32(y))
			}
			if !isFilled(x+1, y) {
				addSegment(uint32(x+1), uint32(y), uint32(x+1), uint32(y+1))
			}
			if !isFilled(x, y+1) {
				addSegment(uint32(x+1), uint32(y+1), uint32(x), uint32(y+1))
			}
			if !isFilled(x-1, y) {
				addSegment(uint32(x), uint32(y+1), uint32(x), uint32(y))
			}
		}
	}

	loops := traceOutlineLoops(segments)
	if len(loops) == 0 {
		return []contracts.Point{}
	}

	bestLoop := loops[0]
	bestArea := polygonAreaTwice(bestLoop)
	for _, loop := range loops[1:] {
		area := polygonAreaTwice(loop)
		if area > bestArea || (area == bestArea && len(loop) > len(bestLoop)) {
			bestLoop = loop
			bestArea = area
		}
	}

	return simplifyClosedPointLoop(bestLoop)
}

func traceOutlineLoops(segments []outlineSegment) [][]contracts.Point {
	if len(segments) == 0 {
		return nil
	}

	adjacency := make(map[uint64][]int, len(segments))
	for index, segment := range segments {
		adjacency[pointKey(segment.start)] = append(adjacency[pointKey(segment.start)], index)
	}

	used := make([]bool, len(segments))
	loops := make([][]contracts.Point, 0, 1)
	for startIndex, segment := range segments {
		if used[startIndex] {
			continue
		}

		used[startIndex] = true
		loop := []contracts.Point{segment.start, segment.end}
		prevDirection := segmentDirection(segment)
		current := segment.end
		startKey := pointKey(segment.start)

		for {
			if pointKey(current) == startKey {
				break
			}

			nextIndex := chooseNextOutlineSegment(current, prevDirection, adjacency, segments, used)
			if nextIndex < 0 {
				loop = nil
				break
			}

			used[nextIndex] = true
			nextSegment := segments[nextIndex]
			loop = append(loop, nextSegment.end)
			prevDirection = segmentDirection(nextSegment)
			current = nextSegment.end
		}

		if len(loop) >= 4 {
			loops = append(loops, loop)
		}
	}

	return loops
}

func chooseNextOutlineSegment(
	current contracts.Point,
	prevDirection int,
	adjacency map[uint64][]int,
	segments []outlineSegment,
	used []bool,
) int {
	options := adjacency[pointKey(current)]
	bestIndex := -1
	bestRank := len(options) + 5
	for _, candidateIndex := range options {
		if used[candidateIndex] {
			continue
		}
		candidateDirection := segmentDirection(segments[candidateIndex])
		rank := directionTurnRank(prevDirection, candidateDirection)
		if rank < bestRank {
			bestRank = rank
			bestIndex = candidateIndex
		}
	}
	return bestIndex
}

func segmentDirection(segment outlineSegment) int {
	dx := int(segment.end.X) - int(segment.start.X)
	dy := int(segment.end.Y) - int(segment.start.Y)
	switch {
	case dx > 0:
		return 0
	case dy > 0:
		return 1
	case dx < 0:
		return 2
	default:
		return 3
	}
}

func directionTurnRank(previous, candidate int) int {
	turn := (candidate - previous + 4) % 4
	switch turn {
	case 1:
		return 0
	case 0:
		return 1
	case 3:
		return 2
	default:
		return 3
	}
}

func polygonAreaTwice(points []contracts.Point) uint64 {
	if len(points) < 3 {
		return 0
	}

	var area int64
	for index := range points {
		next := points[(index+1)%len(points)]
		current := points[index]
		area += int64(current.X)*int64(next.Y) - int64(next.X)*int64(current.Y)
	}
	if area < 0 {
		return uint64(-area)
	}
	return uint64(area)
}

func simplifyClosedPointLoop(points []contracts.Point) []contracts.Point {
	if len(points) == 0 {
		return []contracts.Point{}
	}

	simplified := make([]contracts.Point, 0, len(points))
	for _, point := range points {
		if len(simplified) == 0 || simplified[len(simplified)-1] != point {
			simplified = append(simplified, point)
		}
	}
	if len(simplified) > 1 && simplified[0] == simplified[len(simplified)-1] {
		simplified = simplified[:len(simplified)-1]
	}
	if len(simplified) < 3 {
		return simplified
	}

	for {
		changed := false
		next := make([]contracts.Point, 0, len(simplified))
		for index := range simplified {
			prev := simplified[(index-1+len(simplified))%len(simplified)]
			current := simplified[index]
			following := simplified[(index+1)%len(simplified)]
			if isCollinear(prev, current, following) {
				changed = true
				continue
			}
			next = append(next, current)
		}
		simplified = next
		if !changed || len(simplified) < 3 {
			break
		}
	}

	return simplified
}

func isCollinear(left, middle, right contracts.Point) bool {
	ax := int64(middle.X) - int64(left.X)
	ay := int64(middle.Y) - int64(left.Y)
	bx := int64(right.X) - int64(middle.X)
	by := int64(right.Y) - int64(middle.Y)
	return ax*by-ay*bx == 0
}

func pointKey(point contracts.Point) uint64 {
	return uint64(point.X)<<32 | uint64(point.Y)
}

func clonePointSlice(points []contracts.Point) []contracts.Point {
	if len(points) == 0 {
		return []contracts.Point{}
	}

	cloned := make([]contracts.Point, len(points))
	copy(cloned, points)
	return cloned
}

func lineSegmentLength(line contracts.LineSegment) uint32 {
	dx := math.Abs(float64(line.End.X) - float64(line.Start.X))
	dy := math.Abs(float64(line.End.Y) - float64(line.Start.Y))
	return uint32(math.Round(math.Hypot(dx, dy))) + 1
}

func roundMeasurement(value float64) float64 {
	return math.Round(value*10.0) / 10.0
}

func roundConfidence(value float64) float64 {
	if value < 0 {
		value = 0
	}
	if value > 0.99 {
		value = 0.99
	}
	return math.Round(value*100.0) / 100.0
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func gaussianBlurGray(pixels []uint8, width, height uint32, sigma float64) []uint8 {
	if sigma == 0 {
		sigma = 0.8
	}

	kernel := gaussianKernel1D(kernelSizeFromSigma(sigma), sigma)
	radius := len(kernel) / 2
	widthInt := int(width)
	heightInt := int(height)

	transient := bufpool.GetFloat32(len(pixels))
	for y := 0; y < heightInt; y++ {
		rowStart := y * widthInt
		for x := 0; x < widthInt; x++ {
			var sum float32
			for kernelIndex, weight := range kernel {
				sourceX := x + kernelIndex - radius
				if sourceX < 0 {
					sourceX = 0
				} else if sourceX >= widthInt {
					sourceX = widthInt - 1
				}
				sum += float32(pixels[rowStart+sourceX]) * weight
			}
			transient[rowStart+x] = sum
		}
	}

	blurred := bufpool.GetUint8(len(pixels))
	for y := 0; y < heightInt; y++ {
		for x := 0; x < widthInt; x++ {
			var sum float32
			for kernelIndex, weight := range kernel {
				sourceY := y + kernelIndex - radius
				if sourceY < 0 {
					sourceY = 0
				} else if sourceY >= heightInt {
					sourceY = heightInt - 1
				}
				sum += transient[sourceY*widthInt+x] * weight
			}
			blurred[y*widthInt+x] = clampUint8FromFloat32(sum)
		}
	}

	bufpool.PutFloat32(transient)
	return blurred
}

// dualGaussianBlurGray performs two separable Gaussian blurs. Uses the
// integer-arithmetic path for sigma=1.4 (fixed kernel) and the optimized
// float path for larger kernels.
func dualGaussianBlurGray(pixels []uint8, width, height uint32, sigma1, sigma2 float64) ([]uint8, []uint8) {
	var blurred1 []uint8
	if sigma1 == 1.4 {
		blurred1 = gaussianBlurGrayInteger(pixels, width, height)
	} else {
		blurred1 = gaussianBlurGrayFast(pixels, width, height, sigma1)
	}
	blurred2 := gaussianBlurGrayFast(pixels, width, height, sigma2)
	return blurred1, blurred2
}

// gaussianBlurGrayInteger performs a separable Gaussian blur using fixed-point
// integer arithmetic. The kernel [7, 27, 57, 74, 57, 27, 7] (sum=256)
// approximates a Gaussian with sigma≈1.4 (kernel size 7, radius 3).
// Horizontal pass stores weighted sums as uint16 (max 65280); vertical pass
// accumulates uint32 and divides by 65536 (>>16) for the final uint8 output.
func gaussianBlurGrayInteger(pixels []uint8, width, height uint32) []uint8 {
	widthInt := int(width)
	heightInt := int(height)
	pixelCount := len(pixels)

	// Fixed-point kernel: [7, 27, 57, 74, 57, 27, 7], sum = 256.
	const (
		w0     = 7
		w1     = 27
		w2     = 57
		w3     = 74
		radius = 3
	)

	transient := bufpool.GetUint16(pixelCount)

	// --- Horizontal pass ---
	for y := 0; y < heightInt; y++ {
		rs := y * widthInt

		// Left boundary (x < radius): clamp source indices to 0.
		for x := 0; x < radius && x < widthInt; x++ {
			var sum uint32
			for k := 0; k < 7; k++ {
				sx := x + k - radius
				if sx < 0 {
					sx = 0
				} else if sx >= widthInt {
					sx = widthInt - 1
				}
				kw := [7]uint32{w0, w1, w2, w3, w2, w1, w0}
				sum += uint32(pixels[rs+sx]) * kw[k]
			}
			transient[rs+x] = uint16(sum)
		}

		// Interior: fully unrolled, no bounds checks.
		for x := radius; x < widthInt-radius; x++ {
			b := rs + x - radius
			transient[rs+x] = uint16(
				uint32(pixels[b])*w0 + uint32(pixels[b+1])*w1 +
					uint32(pixels[b+2])*w2 + uint32(pixels[b+3])*w3 +
					uint32(pixels[b+4])*w2 + uint32(pixels[b+5])*w1 +
					uint32(pixels[b+6])*w0)
		}

		// Right boundary (x >= width-radius): clamp to width-1.
		for x := widthInt - radius; x < widthInt; x++ {
			if x < radius {
				continue
			}
			var sum uint32
			for k := 0; k < 7; k++ {
				sx := x + k - radius
				if sx >= widthInt {
					sx = widthInt - 1
				}
				kw := [7]uint32{w0, w1, w2, w3, w2, w1, w0}
				sum += uint32(pixels[rs+sx]) * kw[k]
			}
			transient[rs+x] = uint16(sum)
		}
	}

	blurred := bufpool.GetUint8(pixelCount)

	// --- Vertical pass ---
	// Top boundary rows (y < radius): clamp source row to 0.
	for y := 0; y < radius && y < heightInt; y++ {
		ro := y * widthInt
		for x := 0; x < widthInt; x++ {
			var sum uint32
			for k := 0; k < 7; k++ {
				sy := y + k - radius
				if sy < 0 {
					sy = 0
				} else if sy >= heightInt {
					sy = heightInt - 1
				}
				kw := [7]uint32{w0, w1, w2, w3, w2, w1, w0}
				sum += uint32(transient[sy*widthInt+x]) * kw[k]
			}
			blurred[ro+x] = uint8((sum + 32768) >> 16)
		}
	}

	// Interior rows: fully unrolled kernel, 4-column unrolling.
	for y := radius; y < heightInt-radius; y++ {
		ro := y * widthInt
		by := y - radius
		r0 := by * widthInt
		r1 := (by + 1) * widthInt
		r2 := (by + 2) * widthInt
		r3 := (by + 3) * widthInt
		r4 := (by + 4) * widthInt
		r5 := (by + 5) * widthInt
		r6 := (by + 6) * widthInt

		x := 0
		for ; x <= widthInt-4; x += 4 {
			s0 := uint32(transient[r0+x])*w0 + uint32(transient[r1+x])*w1 +
				uint32(transient[r2+x])*w2 + uint32(transient[r3+x])*w3 +
				uint32(transient[r4+x])*w2 + uint32(transient[r5+x])*w1 +
				uint32(transient[r6+x])*w0
			s1 := uint32(transient[r0+x+1])*w0 + uint32(transient[r1+x+1])*w1 +
				uint32(transient[r2+x+1])*w2 + uint32(transient[r3+x+1])*w3 +
				uint32(transient[r4+x+1])*w2 + uint32(transient[r5+x+1])*w1 +
				uint32(transient[r6+x+1])*w0
			s2 := uint32(transient[r0+x+2])*w0 + uint32(transient[r1+x+2])*w1 +
				uint32(transient[r2+x+2])*w2 + uint32(transient[r3+x+2])*w3 +
				uint32(transient[r4+x+2])*w2 + uint32(transient[r5+x+2])*w1 +
				uint32(transient[r6+x+2])*w0
			s3 := uint32(transient[r0+x+3])*w0 + uint32(transient[r1+x+3])*w1 +
				uint32(transient[r2+x+3])*w2 + uint32(transient[r3+x+3])*w3 +
				uint32(transient[r4+x+3])*w2 + uint32(transient[r5+x+3])*w1 +
				uint32(transient[r6+x+3])*w0
			blurred[ro+x] = uint8((s0 + 32768) >> 16)
			blurred[ro+x+1] = uint8((s1 + 32768) >> 16)
			blurred[ro+x+2] = uint8((s2 + 32768) >> 16)
			blurred[ro+x+3] = uint8((s3 + 32768) >> 16)
		}
		for ; x < widthInt; x++ {
			sum := uint32(transient[r0+x])*w0 + uint32(transient[r1+x])*w1 +
				uint32(transient[r2+x])*w2 + uint32(transient[r3+x])*w3 +
				uint32(transient[r4+x])*w2 + uint32(transient[r5+x])*w1 +
				uint32(transient[r6+x])*w0
			blurred[ro+x] = uint8((sum + 32768) >> 16)
		}
	}

	// Bottom boundary rows (y >= height-radius): clamp to last row.
	for y := heightInt - radius; y < heightInt; y++ {
		if y < radius {
			continue
		}
		ro := y * widthInt
		for x := 0; x < widthInt; x++ {
			var sum uint32
			for k := 0; k < 7; k++ {
				sy := y + k - radius
				if sy >= heightInt {
					sy = heightInt - 1
				}
				kw := [7]uint32{w0, w1, w2, w3, w2, w1, w0}
				sum += uint32(transient[sy*widthInt+x]) * kw[k]
			}
			blurred[ro+x] = uint8((sum + 32768) >> 16)
		}
	}

	bufpool.PutUint16(transient)
	return blurred
}

// gaussianBlurGrayFast is an optimized separable Gaussian blur. It splits both
// passes into boundary (with clamping) and interior (no bounds checks) regions.
// For sigma=9 (kernel=57, radius=28), 96% of pixels are in the interior where
// the inner loop is branch-free. Also uses a fast float32 clamp that avoids
// math.Round and the float32→float64→float32 conversion.
func gaussianBlurGrayFast(pixels []uint8, width, height uint32, sigma float64) []uint8 {
	if sigma == 0 {
		sigma = 0.8
	}

	kernel := gaussianKernel1D(kernelSizeFromSigma(sigma), sigma)
	radius := len(kernel) / 2
	widthInt := int(width)
	heightInt := int(height)
	pixelCount := len(pixels)

	transient := bufpool.GetFloat32(pixelCount)

	// Horizontal pass: boundary pixels (x < radius || x >= width-radius)
	// need clamping; interior pixels are branch-free.
	for y := 0; y < heightInt; y++ {
		rowStart := y * widthInt

		// Left boundary.
		for x := 0; x < radius && x < widthInt; x++ {
			var sum float32
			for ki, w := range kernel {
				sx := x + ki - radius
				if sx < 0 {
					sx = 0
				}
				sum += float32(pixels[rowStart+sx]) * w
			}
			transient[rowStart+x] = sum
		}

		// Interior (no bounds checks). Slice source to give BCE a hint.
		for x := radius; x < widthInt-radius; x++ {
			base := rowStart + x - radius
			src := pixels[base : base+len(kernel)]
			var sum float32
			for ki, w := range kernel {
				sum += float32(src[ki]) * w
			}
			transient[rowStart+x] = sum
		}

		// Right boundary.
		for x := widthInt - radius; x < widthInt; x++ {
			if x < radius {
				continue // narrow image, already handled by left boundary
			}
			var sum float32
			for ki, w := range kernel {
				sx := x + ki - radius
				if sx >= widthInt {
					sx = widthInt - 1
				}
				sum += float32(pixels[rowStart+sx]) * w
			}
			transient[rowStart+x] = sum
		}
	}

	blurred := bufpool.GetUint8(pixelCount)

	// Vertical pass: boundary rows (y < radius || y >= height-radius) need
	// clamping; interior rows are branch-free.

	// Top boundary rows.
	for y := 0; y < radius && y < heightInt; y++ {
		rowOffset := y * widthInt
		for x := 0; x < widthInt; x++ {
			var sum float32
			for ki, w := range kernel {
				sy := y + ki - radius
				if sy < 0 {
					sy = 0
				}
				sum += transient[sy*widthInt+x] * w
			}
			blurred[rowOffset+x] = fastClampUint8(sum)
		}
	}

	// Interior rows (no bounds checks). Process 4 columns at a time for
	// instruction-level parallelism: each kernel row offset is computed
	// once and shared across 4 independent accumulators.
	for y := radius; y < heightInt-radius; y++ {
		rowOffset := y * widthInt
		baseY := y - radius
		x := 0
		for ; x <= widthInt-4; x += 4 {
			var s0, s1, s2, s3 float32
			for ki, w := range kernel {
				ro := (baseY + ki) * widthInt
				s0 += transient[ro+x] * w
				s1 += transient[ro+x+1] * w
				s2 += transient[ro+x+2] * w
				s3 += transient[ro+x+3] * w
			}
			blurred[rowOffset+x] = fastClampUint8(s0)
			blurred[rowOffset+x+1] = fastClampUint8(s1)
			blurred[rowOffset+x+2] = fastClampUint8(s2)
			blurred[rowOffset+x+3] = fastClampUint8(s3)
		}
		for ; x < widthInt; x++ {
			var sum float32
			for ki, w := range kernel {
				sum += transient[(baseY+ki)*widthInt+x] * w
			}
			blurred[rowOffset+x] = fastClampUint8(sum)
		}
	}

	// Bottom boundary rows.
	for y := heightInt - radius; y < heightInt; y++ {
		if y < radius {
			continue // short image, already handled by top boundary
		}
		rowOffset := y * widthInt
		for x := 0; x < widthInt; x++ {
			var sum float32
			for ki, w := range kernel {
				sy := y + ki - radius
				if sy >= heightInt {
					sy = heightInt - 1
				}
				sum += transient[sy*widthInt+x] * w
			}
			blurred[rowOffset+x] = fastClampUint8(sum)
		}
	}

	bufpool.PutFloat32(transient)
	return blurred
}

// fastClampUint8 converts a float32 to uint8 with rounding. Avoids the
// float32→float64 promotion and math.Round overhead in clampUint8FromFloat32.
func fastClampUint8(v float32) uint8 {
	v += 0.5
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v)
}

func kernelSizeFromSigma(sigma float64) int {
	possibleSize := uint32(math.Max((((sigma-0.8)/0.3)+1.0)*2.0+1.0, 3.0))
	if possibleSize%2 == 0 {
		return int(possibleSize + 1)
	}
	return int(possibleSize)
}

func gaussianKernel1D(width int, sigma float64) []float32 {
	kernel := make([]float32, width)
	scale := 1.0 / (math.Sqrt(2.0*math.Pi) * sigma)
	mean := float64(width / 2)
	var sum float64

	for x := 0; x < width; x++ {
		weight := math.Exp(-0.5*math.Pow((float64(x)-mean)/sigma, 2.0)) * scale
		kernel[x] = float32(weight)
		sum += weight
	}

	if sum != 0 {
		normalizedScale := float32(1.0 / sum)
		for index := range kernel {
			kernel[index] *= normalizedScale
		}
	}

	return kernel
}

func clampUint8FromInt(value int) uint8 {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return uint8(value)
}

func clampUint8FromFloat32(value float32) uint8 {
	rounded := math.Round(float64(value))
	if rounded < 0 {
		return 0
	}
	if rounded > 255 {
		return 255
	}
	return uint8(rounded)
}

func minUint32(left, right uint32) uint32 {
	if left < right {
		return left
	}
	return right
}

func maxUint32(left, right uint32) uint32 {
	if left > right {
		return left
	}
	return right
}

func saturatingSubUint32(left, right uint32) uint32 {
	if left < right {
		return 0
	}
	return left - right
}

func maxUint8(left, right uint8) uint8 {
	if left > right {
		return left
	}
	return right
}

func minNonZeroUint8(left, right uint8) uint8 {
	switch {
	case left == 0:
		return right
	case right == 0:
		return left
	case left < right:
		return left
	default:
		return right
	}
}

func cloneUint8Slice(values []uint8) []uint8 {
	cloned := make([]uint8, len(values))
	copy(cloned, values)
	return cloned
}

func cloneComponentCandidates(candidates []componentCandidate) []componentCandidate {
	cloned := make([]componentCandidate, len(candidates))
	for index, candidate := range candidates {
		cloned[index] = candidate
		if len(candidate.pixels) > 0 {
			cloned[index].pixels = append([]int(nil), candidate.pixels...)
		}
	}
	return cloned
}

func orMasks(left, right []uint8) []uint8 {
	if len(left) != len(right) {
		return nil
	}
	combined := make([]uint8, len(left))
	for index := range left {
		if left[index] != 0 || right[index] != 0 {
			combined[index] = 1
		}
	}
	return combined
}

func countMaskPixels(mask []uint8) int {
	count := 0
	for _, value := range mask {
		if value != 0 {
			count++
		}
	}
	return count
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
