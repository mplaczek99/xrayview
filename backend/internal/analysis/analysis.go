package analysis

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
	normalized := normalizePixels(pixels)
	smallBlur, largeBlur := dualGaussianBlurGray(normalized, width, height, 1.4, 9.0)
	toothness := buildToothnessMap(normalized, smallBlur, largeBlur, width, height)

	// Return blur buffers — no longer needed after toothness map is built.
	bufpool.PutUint8(smallBlur)
	bufpool.PutUint8(largeBlur)

	toothnessThreshold := maxUint8(percentileInRegion(toothness, width, search, 0.79), 118)
	intensityThreshold := maxUint8(percentileInRegion(normalized, width, search, 0.69), 82)

	mask := make([]bool, len(normalized))
	for y := search.y; y < search.y+search.height; y++ {
		rowStart := int(y * width)
		for x := search.x; x < search.x+search.width; x++ {
			index := rowStart + int(x)
			mask[index] = toothness[index] >= toothnessThreshold && normalized[index] >= intensityThreshold
		}
	}

	mask = openBinaryMask(closeBinaryMask(mask, int(width), int(height)), int(width), int(height))

	candidates := collectCandidates(mask, normalized, toothness, width, height, search)

	// Return analysis buffers — candidates hold pixel indices, not buffer refs.
	bufpool.PutUint8(normalized)
	bufpool.PutUint8(toothness)
	detectedCandidates := selectDetectedCandidates(candidates)
	primaryCandidate := selectPrimaryCandidate(detectedCandidates)

	warnings := make([]string, 0, 2)
	if measurementScale == nil {
		warnings = append(warnings, "Calibration metadata unavailable; returning pixel measurements only.")
	}

	if len(detectedCandidates) > 0 && primaryCandidate != nil && !primaryCandidate.strict {
		warnings = append(warnings, "No component met the primary tooth filters; using relaxed tooth candidates.")
	}

	teeth := make([]contracts.ToothCandidate, 0, len(detectedCandidates))
	for _, candidate := range detectedCandidates {
		teeth = append(teeth, buildToothCandidate(candidate, measurementScale, width))
	}

	var tooth *contracts.ToothCandidate
	if primaryCandidate != nil {
		candidate := buildToothCandidate(*primaryCandidate, measurementScale, width)
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

func buildToothCandidate(
	candidate componentCandidate,
	measurementScale *contracts.MeasurementScale,
	imageWidth uint32,
) contracts.ToothCandidate {
	geometry := geometryFromPixels(candidate.pixels, candidate.bbox, imageWidth)
	pixel := contracts.ToothMeasurementValues{
		ToothWidth:        float64(lineSegmentLength(geometry.WidthLine)),
		ToothHeight:       float64(lineSegmentLength(geometry.HeightLine)),
		BoundingBoxWidth:  float64(candidate.bbox.Width),
		BoundingBoxHeight: float64(candidate.bbox.Height),
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

func defaultSearchRegion(width, height uint32) searchRegion {
	xMargin := maxUint32(width/8, 8)
	topMargin := maxUint32(uint32(math.Round(float64(height)*0.20)), 8)
	bottom := maxUint32(uint32(math.Round(float64(height)*0.78)), topMargin+1)

	return searchRegion{
		x:      xMargin,
		y:      topMargin,
		width:  maxUint32(saturatingSubUint32(width, xMargin*2), 1),
		height: maxUint32(saturatingSubUint32(bottom, topMargin), 1),
	}
}

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

func buildToothnessMap(
	normalized []uint8,
	smallBlur []uint8,
	largeBlur []uint8,
	width, height uint32,
) []uint8 {
	toothness := bufpool.GetUint8(len(normalized))
	for y := uint32(0); y < height; y++ {
		for x := uint32(0); x < width; x++ {
			index := int(y*width + x)
			small := int16(smallBlur[index])
			large := int16(largeBlur[index])
			localContrast := clampUint8FromInt(128 + int(small-large))
			gradient := localGradient(normalized, width, height, x, y)
			combined := (uint16(normalized[index])*5 +
				uint16(localContrast)*4 +
				uint16(gradient)*2) / 11
			toothness[index] = uint8(combined)
		}
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

func closeBinaryMask(mask []bool, width, height int) []bool {
	return erodeBinaryMask(dilateBinaryMask(mask, width, height), width, height)
}

func openBinaryMask(mask []bool, width, height int) []bool {
	return dilateBinaryMask(erodeBinaryMask(mask, width, height), width, height)
}

func dilateBinaryMask(mask []bool, width, height int) []bool {
	dilated := make([]bool, len(mask))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value := false
			for ny := maxInt(y-1, 0); ny <= minInt(y+1, height-1); ny++ {
				for nx := maxInt(x-1, 0); nx <= minInt(x+1, width-1); nx++ {
					if mask[ny*width+nx] {
						value = true
						break
					}
				}
				if value {
					break
				}
			}
			dilated[y*width+x] = value
		}
	}

	return dilated
}

func erodeBinaryMask(mask []bool, width, height int) []bool {
	eroded := make([]bool, len(mask))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value := true
			for ny := maxInt(y-1, 0); ny <= minInt(y+1, height-1); ny++ {
				for nx := maxInt(x-1, 0); nx <= minInt(x+1, width-1); nx++ {
					if !mask[ny*width+nx] {
						value = false
						break
					}
				}
				if !value {
					break
				}
			}
			eroded[y*width+x] = value
		}
	}

	return eroded
}

func collectCandidates(
	mask []bool,
	normalized []uint8,
	toothness []uint8,
	width, height uint32,
	search searchRegion,
) []componentCandidate {
	widthInt := int(width)
	heightInt := int(height)
	visited := make([]bool, len(mask))
	queue := make([]int, 0, 256)
	candidates := make([]componentCandidate, 0)

	for y := int(search.y); y < int(search.y+search.height); y++ {
		for x := int(search.x); x < int(search.x+search.width); x++ {
			startIndex := y*widthInt + x
			if visited[startIndex] || !mask[startIndex] {
				continue
			}

			visited[startIndex] = true
			queue = append(queue[:0], startIndex)
			head := 0

			pixels := make([]int, 0, 256)
			minX := uint32(x)
			maxX := uint32(x)
			minY := uint32(y)
			maxY := uint32(y)
			var intensitySum uint64
			var toothnessSum uint64

			for head < len(queue) {
				index := queue[head]
				head++

				pixels = append(pixels, index)
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
						if !visited[neighbor] && mask[neighbor] {
							visited[neighbor] = true
							queue = append(queue, neighbor)
						}
					}
				}
			}

			area := uint32(len(pixels))
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
		if candidate.area > 150 {
			detectedCandidates = append(detectedCandidates, candidate)
		}
	}
	sortDetectedCandidates(detectedCandidates)
	return detectedCandidates
}

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
	}
}

func lineSegmentLength(line contracts.LineSegment) uint32 {
	if line.Start.Y == line.End.Y {
		return line.End.X - line.Start.X + 1
	}
	return line.End.Y - line.Start.Y + 1
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

// dualGaussianBlurGray performs two separable Gaussian blurs using the
// optimized blur path that splits boundary/interior processing.
func dualGaussianBlurGray(pixels []uint8, width, height uint32, sigma1, sigma2 float64) ([]uint8, []uint8) {
	blurred1 := gaussianBlurGrayFast(pixels, width, height, sigma1)
	blurred2 := gaussianBlurGrayFast(pixels, width, height, sigma2)
	return blurred1, blurred2
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
