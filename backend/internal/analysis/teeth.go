package analysis

import (
	"fmt"
	"math"

	"xrayview/backend/internal/bufpool"
	"xrayview/backend/internal/imaging"
)

var toothOverlayGreen = [3]uint8{102, 255, 0}
var boneOverlayRed = [3]uint8{255, 0, 0}

// AnalyzeAlgorithmVersion is part of the Analyze result cache key. Change it
// only when the generated overlay semantics intentionally change.
const AnalyzeAlgorithmVersion = "lowpass-fair-uniform-tooth-and-bone-contour-overlay-v2"

const minimumToothAreaFloorPixels = 51
const toothOutlineThicknessPixels = 2
const boneLineThicknessPixels = 0
const boneOutlineThicknessPixels = 2
const overlayContourLowPassIterations = 18
const overlayContourSmoothingIterations = 4
const overlayContourFairingIterations = 14
const overlayContourFairingLambda = 0.45
const overlayContourFairingMu = -0.47
const overlayContourPointSpacingPixels = 3.0
const overlayContourSimplifyTolerancePixels = 0.0
const overlayStrokeWidthPixels = 2.6

type ToothOverlayResult struct {
	Preview        imaging.PreviewImage
	ToothPixels    int
	BonePixels     int
	Coverage       float64
	CandidateCount int
	Mode           string
}

type componentMeta struct {
	label   int32
	minX    int
	minY    int
	maxX    int
	maxY    int
	area    int
	seedHit bool
}

type overlayPoint struct {
	x float64
	y float64
}

type contourSmoothingScratch struct {
	a []overlayPoint
	b []overlayPoint
}

type gridPoint struct {
	x int
	y int
}

type boundarySegment struct {
	start gridPoint
	end   gridPoint
}

type maskWorkspace struct {
	masks           [][]uint8
	counts          []int
	queue           []int
	componentLabels []int32
	components      []componentMeta
}

func newMaskWorkspace() *maskWorkspace {
	return &maskWorkspace{}
}

func (workspace *maskWorkspace) getMask(length int) []uint8 {
	if length <= 0 {
		return nil
	}
	mask := bufpool.GetUint8(length)
	clear(mask)
	workspace.masks = append(workspace.masks, mask)
	return mask
}

func (workspace *maskWorkspace) countScratch(width int) []int {
	if cap(workspace.counts) < width {
		workspace.counts = make([]int, width)
	}
	return workspace.counts[:width]
}

func (workspace *maskWorkspace) componentLabelScratch(length int) []int32 {
	if cap(workspace.componentLabels) < length {
		workspace.componentLabels = make([]int32, length)
	}
	return workspace.componentLabels[:length]
}

func (workspace *maskWorkspace) release() {
	for _, mask := range workspace.masks {
		bufpool.PutUint8(mask)
	}
	workspace.masks = nil
	workspace.counts = nil
	workspace.queue = nil
	workspace.componentLabels = nil
	workspace.components = nil
}

func GenerateToothOverlay(preview imaging.PreviewImage) (ToothOverlayResult, error) {
	if err := preview.Validate(); err != nil {
		return ToothOverlayResult{}, fmt.Errorf("validate preview image: %w", err)
	}

	gray, err := grayPixels(preview)
	if err != nil {
		return ToothOverlayResult{}, err
	}

	width := int(preview.Width)
	height := int(preview.Height)
	if width < 16 || height < 16 {
		return ToothOverlayResult{}, fmt.Errorf("image is too small for tooth analysis: %dx%d", width, height)
	}

	workspace := newMaskWorkspace()
	defer workspace.release()

	toothMask := detectToothMaskWithWorkspace(gray, width, height, workspace)
	boneMask := detectBoneLevelMaskWithWorkspace(gray, width, height, workspace)
	components := componentSizesAndBBoxes(
		toothMask,
		toothMask,
		width,
		height,
		minimumToothAreaPixels(width, height),
	)

	toothPixels := countMaskPixels(toothMask)
	bonePixels := countMaskPixels(boneMask)
	coverage := float64(toothPixels+bonePixels) / float64(maxInt(len(toothMask), 1))
	mode := "dynamic tooth and bone level overlay"
	if toothPixels < len(toothMask)/150 || len(components) == 0 {
		mode = "dynamic tooth and bone level overlay; no reliable tooth mask found"
	}
	if bonePixels < width/8 {
		mode = mode + "; no reliable bone level found"
	}

	return ToothOverlayResult{
		Preview: overlayMasksWithWorkspace(
			workspace,
			gray,
			toothMask,
			thickenBoneLineMaskWithWorkspace(boneMask, width, height, boneLineThicknessPixels, workspace),
			preview.Width,
			preview.Height,
		),
		ToothPixels:    toothPixels,
		BonePixels:     bonePixels,
		Coverage:       coverage,
		CandidateCount: len(components),
		Mode:           mode,
	}, nil
}

func detectToothMask(gray []uint8, width, height int) []uint8 {
	workspace := newMaskWorkspace()
	defer workspace.release()
	return append([]uint8(nil), detectToothMaskWithWorkspace(gray, width, height, workspace)...)
}

func detectToothMaskWithWorkspace(gray []uint8, width, height int, workspace *maskWorkspace) []uint8 {
	normalized := normalizeGray(gray)
	mask := workspace.getMask(len(normalized))
	work := workspace.getMask(len(normalized))
	scratch := workspace.getMask(len(normalized))
	counts := workspace.countScratch(width)

	featureTableToothMaskInto(mask, normalized, width, height)
	closeBinaryMaskInPlace(mask, work, scratch, counts, width, height, 1)
	openBinaryMaskInPlace(mask, work, scratch, counts, width, height, 1)
	fillHolesBinaryMaskInto(work, scratch, &workspace.queue, mask, width, height)
	mask, work = work, mask
	removeSmallMaskComponentsInto(
		work,
		workspace.componentLabelScratch(len(mask)),
		&workspace.queue,
		&workspace.components,
		mask,
		width,
		height,
		minimumToothAreaPixels(width, height),
	)
	mask = work
	return mask
}

func detectBoneLevelMask(gray []uint8, width, height int) []uint8 {
	workspace := newMaskWorkspace()
	defer workspace.release()
	return append([]uint8(nil), detectBoneLevelMaskWithWorkspace(gray, width, height, workspace)...)
}

func detectBoneLevelMaskWithWorkspace(gray []uint8, width, height int, workspace *maskWorkspace) []uint8 {
	if mask, ok := boneExemplarMask(gray, width, height); ok {
		pooled := workspace.getMask(len(mask))
		copy(pooled, mask)
		return pooled
	}

	normalized := normalizeGray(gray)
	gradient := gradientGray(boxBlurGray(normalized, width, height, 2), width, height)
	mask := workspace.getMask(len(normalized))
	work := workspace.getMask(len(normalized))
	scratch := workspace.getMask(len(normalized))
	counts := workspace.countScratch(width)

	boneFeatureTableMaskInto(mask, normalized, gradient, width, height)
	closeBinaryMaskInPlace(mask, work, scratch, counts, width, height, 1)
	removeSmallMaskComponentsInto(
		work,
		workspace.componentLabelScratch(len(mask)),
		&workspace.queue,
		&workspace.components,
		mask,
		width,
		height,
		minimumBoneAreaPixels(width, height),
	)
	mask = work
	return mask
}

func grayPixels(preview imaging.PreviewImage) ([]uint8, error) {
	switch preview.Format {
	case imaging.FormatGray8:
		return append([]uint8(nil), preview.Pixels...), nil
	case imaging.FormatRGBA8:
		gray := make([]uint8, len(preview.Pixels)/4)
		for index := range gray {
			base := index * 4
			gray[index] = uint8((299*uint32(preview.Pixels[base]) + 587*uint32(preview.Pixels[base+1]) + 114*uint32(preview.Pixels[base+2]) + 500) / 1000)
		}
		return gray, nil
	default:
		return nil, fmt.Errorf("unsupported preview format %q", preview.Format)
	}
}

func overlayMasks(gray []uint8, toothMask []uint8, boneMask []uint8, width, height uint32) imaging.PreviewImage {
	workspace := newMaskWorkspace()
	defer workspace.release()
	return overlayMasksWithWorkspace(workspace, gray, toothMask, boneMask, width, height)
}

func overlayMasksWithWorkspace(workspace *maskWorkspace, gray []uint8, toothMask []uint8, boneMask []uint8, width, height uint32) imaging.PreviewImage {
	rgba := make([]uint8, len(gray)*4)
	for index, value := range gray {
		base := index * 4
		rgba[base+0] = value
		rgba[base+1] = value
		rgba[base+2] = value
		rgba[base+3] = 255
	}

	widthInt := int(width)
	heightInt := int(height)
	drawSmoothedMaskContours(
		rgba,
		widthInt,
		heightInt,
		boneOutlineSourceMaskWithWorkspace(boneMask, widthInt, heightInt, workspace),
		boneOverlayRed,
		toothMask,
	)
	drawSmoothedMaskContours(rgba, widthInt, heightInt, toothMask, toothOverlayGreen, nil)
	return imaging.RGBAPreview(width, height, rgba)
}

func boneOutlineMask(toothMask []uint8, boneMask []uint8, width, height int) []uint8 {
	workspace := newMaskWorkspace()
	defer workspace.release()

	source := boneOutlineSourceMaskWithWorkspace(boneMask, width, height, workspace)
	outline := workspace.getMask(len(source))
	work := workspace.getMask(len(source))
	scratch := workspace.getMask(len(source))
	innerOutlineMaskInto(outline, work, scratch, workspace.countScratch(width), source, width, height, boneOutlineThicknessPixels)
	for index, value := range toothMask {
		if value != 0 {
			outline[index] = 0
		}
	}
	return append([]uint8(nil), outline...)
}

func boneOutlineSourceMask(boneMask []uint8, width, height int) []uint8 {
	workspace := newMaskWorkspace()
	defer workspace.release()
	return append([]uint8(nil), boneOutlineSourceMaskWithWorkspace(boneMask, width, height, workspace)...)
}

func boneOutlineSourceMaskWithWorkspace(boneMask []uint8, width, height int, workspace *maskWorkspace) []uint8 {
	source := workspace.getMask(len(boneMask))
	scratch := workspace.getMask(len(boneMask))
	filled := workspace.getMask(len(boneMask))
	removeSmallMaskComponentsInto(
		source,
		workspace.componentLabelScratch(len(boneMask)),
		&workspace.queue,
		&workspace.components,
		boneMask,
		width,
		height,
		minimumBoneOutlineAreaPixels(width, height),
	)
	fillHolesBinaryMaskInto(filled, scratch, &workspace.queue, source, width, height)
	return filled
}

func minimumBoneOutlineAreaPixels(width, height int) int {
	return minInt(maxInt(width*height/1000, 16), 128)
}

func drawSmoothedMaskContours(
	rgba []uint8,
	width int,
	height int,
	mask []uint8,
	color [3]uint8,
	excludeMask []uint8,
) {
	coverage := make([]float64, width*height)
	var scratch contourSmoothingScratch
	for _, contour := range maskBoundaryContours(mask, width, height) {
		if len(contour) < 4 {
			continue
		}
		contour = resampleClosedContour(contour, overlayContourPointSpacingPixels)
		contour = simplifyClosedContour(contour, overlayContourSimplifyTolerancePixels)
		contour = lowPassClosedContourWithScratch(contour, overlayContourLowPassIterations, &scratch)
		smoothed := smoothClosedContourWithScratch(contour, overlayContourSmoothingIterations, &scratch)
		addClosedStrokeCoverage(coverage, width, height, smoothed, overlayStrokeWidthPixels, excludeMask)
	}
	compositeOverlayCoverage(rgba, coverage, color)
}

func simplifyClosedContour(points []overlayPoint, tolerance float64) []overlayPoint {
	if len(points) < 4 || tolerance <= 0 {
		return points
	}

	anchor := 0
	for index := 1; index < len(points); index++ {
		if points[index].x < points[anchor].x ||
			(points[index].x == points[anchor].x && points[index].y < points[anchor].y) {
			anchor = index
		}
	}

	rotated := make([]overlayPoint, 0, len(points)+1)
	rotated = append(rotated, points[anchor:]...)
	rotated = append(rotated, points[:anchor]...)
	rotated = append(rotated, rotated[0])

	simplified := simplifyOpenPolyline(rotated, tolerance)
	if len(simplified) > 1 {
		simplified = simplified[:len(simplified)-1]
	}
	if len(simplified) < 4 {
		return points
	}
	return simplified
}

func simplifyOpenPolyline(points []overlayPoint, tolerance float64) []overlayPoint {
	if len(points) <= 2 {
		return append([]overlayPoint(nil), points...)
	}

	keep := make([]bool, len(points))
	keep[0] = true
	keep[len(points)-1] = true
	simplifyPolylineRange(points, tolerance*tolerance, 0, len(points)-1, keep)

	simplified := make([]overlayPoint, 0, len(points))
	for index, point := range points {
		if keep[index] {
			simplified = append(simplified, point)
		}
	}
	return simplified
}

func simplifyPolylineRange(points []overlayPoint, toleranceSquared float64, start int, end int, keep []bool) {
	if end <= start+1 {
		return
	}

	maxDistanceSquared := 0.0
	maxIndex := -1
	for index := start + 1; index < end; index++ {
		distance := distanceToSegment(points[index].x, points[index].y, points[start], points[end])
		distanceSquared := distance * distance
		if distanceSquared > maxDistanceSquared {
			maxDistanceSquared = distanceSquared
			maxIndex = index
		}
	}

	if maxIndex < 0 || maxDistanceSquared <= toleranceSquared {
		return
	}

	keep[maxIndex] = true
	simplifyPolylineRange(points, toleranceSquared, start, maxIndex, keep)
	simplifyPolylineRange(points, toleranceSquared, maxIndex, end, keep)
}

func lowPassClosedContour(points []overlayPoint, iterations int) []overlayPoint {
	var scratch contourSmoothingScratch
	return lowPassClosedContourWithScratch(points, iterations, &scratch)
}

func lowPassClosedContourWithScratch(points []overlayPoint, iterations int, scratch *contourSmoothingScratch) []overlayPoint {
	if len(points) < 24 {
		return points
	}

	filtered, next := scratch.prepare(points)
	for iteration := 0; iteration < iterations; iteration++ {
		if len(filtered) < 3 {
			return filtered
		}
		for index, point := range filtered {
			previous := filtered[(index+len(filtered)-1)%len(filtered)]
			following := filtered[(index+1)%len(filtered)]
			next[index] = overlayPoint{
				x: previous.x*0.25 + point.x*0.50 + following.x*0.25,
				y: previous.y*0.25 + point.y*0.50 + following.y*0.25,
			}
		}
		filtered, next = next, filtered
	}
	return filtered
}

func maskBoundaryContours(mask []uint8, width, height int) [][]overlayPoint {
	if len(mask) == 0 || width <= 0 || height <= 0 || len(mask) != width*height {
		return nil
	}

	segments := make([]boundarySegment, 0, width+height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			index := y*width + x
			if mask[index] == 0 {
				continue
			}
			if y == 0 || mask[index-width] == 0 {
				segments = append(segments, boundarySegment{
					start: gridPoint{x: x, y: y},
					end:   gridPoint{x: x + 1, y: y},
				})
			}
			if x+1 == width || mask[index+1] == 0 {
				segments = append(segments, boundarySegment{
					start: gridPoint{x: x + 1, y: y},
					end:   gridPoint{x: x + 1, y: y + 1},
				})
			}
			if y+1 == height || mask[index+width] == 0 {
				segments = append(segments, boundarySegment{
					start: gridPoint{x: x + 1, y: y + 1},
					end:   gridPoint{x: x, y: y + 1},
				})
			}
			if x == 0 || mask[index-1] == 0 {
				segments = append(segments, boundarySegment{
					start: gridPoint{x: x, y: y + 1},
					end:   gridPoint{x: x, y: y},
				})
			}
		}
	}

	starts := make(map[gridPoint][]int, len(segments))
	for index, segment := range segments {
		starts[segment.start] = append(starts[segment.start], index)
	}

	visited := make([]bool, len(segments))
	contours := make([][]overlayPoint, 0)
	for index, segment := range segments {
		if visited[index] {
			continue
		}

		currentIndex := index
		start := segment.start
		contour := make([]overlayPoint, 0, 64)
		for {
			if visited[currentIndex] {
				break
			}
			current := segments[currentIndex]
			visited[currentIndex] = true
			contour = append(contour, overlayPoint{
				x: float64(current.start.x),
				y: float64(current.start.y),
			})
			if current.end == start {
				break
			}

			nextIndex := -1
			for _, candidate := range starts[current.end] {
				if !visited[candidate] {
					nextIndex = candidate
					break
				}
			}
			if nextIndex < 0 {
				break
			}
			currentIndex = nextIndex
		}

		if len(contour) >= 4 {
			contours = append(contours, contour)
		}
	}

	return contours
}

func smoothClosedContour(points []overlayPoint, iterations int) []overlayPoint {
	var scratch contourSmoothingScratch
	return smoothClosedContourWithScratch(points, iterations, &scratch)
}

func smoothClosedContourWithScratch(points []overlayPoint, iterations int, scratch *contourSmoothingScratch) []overlayPoint {
	if len(points) < 24 {
		return points
	}

	smoothed, next := scratch.prepare(points)
	for iteration := 0; iteration < iterations; iteration++ {
		if len(smoothed) < 3 {
			return smoothed
		}
		for index, current := range smoothed {
			previous := smoothed[(index+len(smoothed)-1)%len(smoothed)]
			following := smoothed[(index+1)%len(smoothed)]
			next[index] = overlayPoint{
				x: previous.x*0.125 + current.x*0.75 + following.x*0.125,
				y: previous.y*0.125 + current.y*0.75 + following.y*0.125,
			}
		}
		smoothed, next = next, smoothed
	}
	smoothed = fairClosedContourWithScratch(
		smoothed,
		overlayContourFairingIterations,
		overlayContourFairingLambda,
		overlayContourFairingMu,
		scratch,
	)
	return subdivideClosedContourWithScratch(smoothed, scratch)
}

func fairClosedContour(points []overlayPoint, iterations int, lambda, mu float64) []overlayPoint {
	var scratch contourSmoothingScratch
	return fairClosedContourWithScratch(points, iterations, lambda, mu, &scratch)
}

func fairClosedContourWithScratch(
	points []overlayPoint,
	iterations int,
	lambda float64,
	mu float64,
	scratch *contourSmoothingScratch,
) []overlayPoint {
	faired, next := scratch.prepare(points)
	for iteration := 0; iteration < iterations; iteration++ {
		laplacianSmoothClosedContourInto(next, faired, lambda)
		faired, next = next, faired
		laplacianSmoothClosedContourInto(next, faired, mu)
		faired, next = next, faired
	}
	return faired
}

func laplacianSmoothClosedContour(points []overlayPoint, amount float64) []overlayPoint {
	if len(points) < 3 || amount == 0 {
		return append([]overlayPoint(nil), points...)
	}

	smoothed := make([]overlayPoint, len(points))
	laplacianSmoothClosedContourInto(smoothed, points, amount)
	return smoothed
}

func laplacianSmoothClosedContourInto(smoothed []overlayPoint, points []overlayPoint, amount float64) {
	if len(points) < 3 || amount == 0 {
		copy(smoothed, points)
		return
	}

	for index, point := range points {
		previous := points[(index+len(points)-1)%len(points)]
		next := points[(index+1)%len(points)]
		average := overlayPoint{
			x: (previous.x + next.x) / 2,
			y: (previous.y + next.y) / 2,
		}
		smoothed[index] = overlayPoint{
			x: point.x + amount*(average.x-point.x),
			y: point.y + amount*(average.y-point.y),
		}
	}
}

func subdivideClosedContourWithScratch(points []overlayPoint, scratch *contourSmoothingScratch) []overlayPoint {
	if len(points) < 3 {
		return points
	}

	subdivided := scratch.spare(points, len(points)*2)
	for index, current := range points {
		following := points[(index+1)%len(points)]
		base := index * 2
		subdivided[base] = overlayPoint{
			x: current.x*0.75 + following.x*0.25,
			y: current.y*0.75 + following.y*0.25,
		}
		subdivided[base+1] = overlayPoint{
			x: current.x*0.25 + following.x*0.75,
			y: current.y*0.25 + following.y*0.75,
		}
	}
	return subdivided
}

func (scratch *contourSmoothingScratch) prepare(points []overlayPoint) ([]overlayPoint, []overlayPoint) {
	if cap(scratch.a) < len(points) {
		scratch.a = make([]overlayPoint, len(points))
	}
	if cap(scratch.b) < len(points) {
		scratch.b = make([]overlayPoint, len(points))
	}

	first := scratch.a[:len(points)]
	second := scratch.b[:len(points)]
	copy(first, points)
	return first, second
}

func (scratch *contourSmoothingScratch) spare(points []overlayPoint, length int) []overlayPoint {
	if sameBacking(points, scratch.a) {
		if cap(scratch.b) < length {
			scratch.b = make([]overlayPoint, length)
		}
		return scratch.b[:length]
	}
	if cap(scratch.a) < length {
		scratch.a = make([]overlayPoint, length)
	}
	return scratch.a[:length]
}

func sameBacking(left []overlayPoint, right []overlayPoint) bool {
	return len(left) > 0 && len(right) > 0 && &left[0] == &right[0]
}

func resampleClosedContour(points []overlayPoint, spacing float64) []overlayPoint {
	if len(points) < 4 || spacing <= 1 {
		return points
	}

	resampled := make([]overlayPoint, 0, len(points)/int(math.Ceil(spacing))+1)
	resampled = append(resampled, points[0])
	last := points[0]
	for index := 1; index < len(points); index++ {
		current := points[index]
		if math.Hypot(current.x-last.x, current.y-last.y) < spacing {
			continue
		}
		resampled = append(resampled, current)
		last = current
	}

	if len(resampled) < 4 {
		return points
	}
	return resampled
}

func addClosedStrokeCoverage(
	coverage []float64,
	width int,
	height int,
	points []overlayPoint,
	strokeWidth float64,
	excludeMask []uint8,
) {
	if len(points) < 2 || strokeWidth <= 0 {
		return
	}

	radius := strokeWidth / 2
	for index, start := range points {
		end := points[(index+1)%len(points)]
		addStrokeSegmentCoverage(coverage, width, height, start, end, radius, excludeMask)
	}
}

func addStrokeSegmentCoverage(
	coverage []float64,
	width int,
	height int,
	start overlayPoint,
	end overlayPoint,
	radius float64,
	excludeMask []uint8,
) {
	minX := clampInt(int(math.Floor(minFloat(start.x, end.x)-radius-1)), 0, width-1)
	maxX := clampInt(int(math.Ceil(maxFloat(start.x, end.x)+radius+1)), 0, width-1)
	minY := clampInt(int(math.Floor(minFloat(start.y, end.y)-radius-1)), 0, height-1)
	maxY := clampInt(int(math.Ceil(maxFloat(start.y, end.y)+radius+1)), 0, height-1)
	if minX > maxX || minY > maxY {
		return
	}

	dx := end.x - start.x
	dy := end.y - start.y
	lengthSquared := dx*dx + dy*dy
	fullCoverageRadius := radius - 0.5
	fullCoverageRadiusSquared := fullCoverageRadius * fullCoverageRadius
	edgeCoverageRadius := radius + 0.5
	edgeCoverageRadiusSquared := edgeCoverageRadius * edgeCoverageRadius
	if lengthSquared == 0 {
		addStrokePointCoverage(
			coverage,
			width,
			minX,
			maxX,
			minY,
			maxY,
			start,
			radius,
			fullCoverageRadius,
			fullCoverageRadiusSquared,
			edgeCoverageRadiusSquared,
			excludeMask,
		)
		return
	}

	invLengthSquared := 1 / lengthSquared

	for y := minY; y <= maxY; y++ {
		pixelY := float64(y) + 0.5
		startY := pixelY - start.y
		endY := pixelY - end.y
		pixelStartX := float64(minX) + 0.5
		startX := pixelStartX - start.x
		endX := pixelStartX - end.x
		dot := startX*dx + startY*dy
		cross := startX*dy - startY*dx

		for x := minX; x <= maxX; x++ {
			distanceSquared := 0.0
			switch {
			case dot <= 0:
				distanceSquared = startX*startX + startY*startY
			case dot >= lengthSquared:
				distanceSquared = endX*endX + endY*endY
			default:
				distanceSquared = cross * cross * invLengthSquared
			}

			segmentCoverage := strokeCoverageFromDistanceSquared(
				distanceSquared,
				radius,
				fullCoverageRadius,
				fullCoverageRadiusSquared,
				edgeCoverageRadiusSquared,
			)
			if segmentCoverage > 0 {
				index := y*width + x
				if (len(excludeMask) != len(coverage) || excludeMask[index] == 0) &&
					segmentCoverage > coverage[index] {
					coverage[index] = segmentCoverage
				}
			}
			startX++
			endX++
			dot += dx
			cross += dy
		}
	}
}

func addStrokePointCoverage(
	coverage []float64,
	width int,
	minX int,
	maxX int,
	minY int,
	maxY int,
	point overlayPoint,
	radius float64,
	fullCoverageRadius float64,
	fullCoverageRadiusSquared float64,
	edgeCoverageRadiusSquared float64,
	excludeMask []uint8,
) {
	for y := minY; y <= maxY; y++ {
		pixelY := float64(y) + 0.5
		dy := pixelY - point.y
		for x := minX; x <= maxX; x++ {
			dx := float64(x) + 0.5 - point.x
			segmentCoverage := strokeCoverageFromDistanceSquared(
				dx*dx+dy*dy,
				radius,
				fullCoverageRadius,
				fullCoverageRadiusSquared,
				edgeCoverageRadiusSquared,
			)
			if segmentCoverage <= 0 {
				continue
			}
			index := y*width + x
			if len(excludeMask) == len(coverage) && excludeMask[index] != 0 {
				continue
			}
			if segmentCoverage > coverage[index] {
				coverage[index] = segmentCoverage
			}
		}
	}
}

func compositeOverlayCoverage(rgba []uint8, coverage []float64, color [3]uint8) {
	for index, value := range coverage {
		if value <= 0 {
			continue
		}
		base := index * 4
		if value >= 0.995 {
			rgba[base+0] = color[0]
			rgba[base+1] = color[1]
			rgba[base+2] = color[2]
			rgba[base+3] = 255
			continue
		}
		for channel := 0; channel < 3; channel++ {
			current := float64(rgba[base+channel])
			target := float64(color[channel])
			rgba[base+channel] = uint8(math.Round(current*(1-value) + target*value))
		}
		rgba[base+3] = 255
	}
}

func strokeCoverage(distance float64, radius float64) float64 {
	coverage := radius + 0.5 - distance
	if coverage <= 0 {
		return 0
	}
	if coverage >= 1 {
		return 1
	}
	return coverage
}

func strokeCoverageFromDistanceSquared(
	distanceSquared float64,
	radius float64,
	fullCoverageRadius float64,
	fullCoverageRadiusSquared float64,
	edgeCoverageRadiusSquared float64,
) float64 {
	if distanceSquared >= edgeCoverageRadiusSquared {
		return 0
	}
	if fullCoverageRadius >= 0 && distanceSquared <= fullCoverageRadiusSquared {
		return 1
	}
	return strokeCoverage(math.Sqrt(distanceSquared), radius)
}

func distanceToSegment(x float64, y float64, start overlayPoint, end overlayPoint) float64 {
	dx := end.x - start.x
	dy := end.y - start.y
	lengthSquared := dx*dx + dy*dy
	if lengthSquared == 0 {
		return math.Hypot(x-start.x, y-start.y)
	}

	t := ((x-start.x)*dx + (y-start.y)*dy) / lengthSquared
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	closestX := start.x + t*dx
	closestY := start.y + t*dy
	return math.Hypot(x-closestX, y-closestY)
}

func normalizeGray(pixels []uint8) []uint8 {
	low := percentileGray(pixels, 0.01)
	high := percentileGray(pixels, 0.99)
	if high <= low {
		return append([]uint8(nil), pixels...)
	}

	rangeValue := int(high) - int(low)
	normalized := make([]uint8, len(pixels))
	for index, value := range pixels {
		if value <= low {
			normalized[index] = 0
			continue
		}
		if value >= high {
			normalized[index] = 255
			continue
		}
		normalized[index] = uint8(((int(value)-int(low))*255 + rangeValue/2) / rangeValue)
	}
	return normalized
}

func percentileGray(pixels []uint8, percentile float64) uint8 {
	if len(pixels) == 0 {
		return 0
	}

	var histogram [256]int
	for _, value := range pixels {
		histogram[value]++
	}

	target := int(math.Round(float64(len(pixels)-1) * percentile))
	if target < 0 {
		target = 0
	}
	if target >= len(pixels) {
		target = len(pixels) - 1
	}

	cumulative := 0
	for value, count := range histogram {
		cumulative += count
		if cumulative > target {
			return uint8(value)
		}
	}
	return 255
}

func removeSmallMaskComponents(mask []uint8, width, height, minArea int) []uint8 {
	if minArea <= 1 || len(mask) == 0 {
		return append([]uint8(nil), mask...)
	}

	filtered := make([]uint8, len(mask))
	labels := make([]int32, len(mask))
	queue := make([]int, 0, 512)
	components := make([]componentMeta, 0, 512)
	removeSmallMaskComponentsInto(filtered, labels, &queue, &components, mask, width, height, minArea)
	return filtered
}

func removeSmallMaskComponentsInto(out []uint8, labels []int32, queue *[]int, components *[]componentMeta, mask []uint8, width, height, minArea int) {
	if minArea <= 1 || len(mask) == 0 {
		copy(out, mask)
		return
	}

	clear(out)
	metas := componentSizesAndBBoxesInto(labels, queue, components, mask, mask, width, height, 0)
	for index, label := range labels[:len(mask)] {
		if label <= 0 {
			continue
		}
		metaIndex := int(label - 1)
		if metaIndex < len(metas) && metas[metaIndex].area >= minArea {
			out[index] = 1
		}
	}
}

func componentSizesAndBBoxes(mask []uint8, seeds []uint8, width, height, minArea int) []componentMeta {
	if len(mask) == 0 {
		return nil
	}

	labels := make([]int32, len(mask))
	queue := make([]int, 0, 512)
	components := make([]componentMeta, 0)
	return componentSizesAndBBoxesInto(labels, &queue, &components, mask, seeds, width, height, minArea)
}

func componentSizesAndBBoxesInto(labels []int32, queue *[]int, components *[]componentMeta, mask []uint8, seeds []uint8, width, height, minArea int) []componentMeta {
	clear(labels[:len(mask)])
	q := (*queue)[:0]
	metas := (*components)[:0]
	nextLabel := int32(1)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			start := y*width + x
			if labels[start] != 0 || mask[start] == 0 {
				continue
			}

			label := nextLabel
			nextLabel++
			labels[start] = label
			q = append(q[:0], start)
			area := 0
			minX, maxX := x, x
			minY, maxY := y, y
			seedHit := false
			for head := 0; head < len(q); head++ {
				index := q[head]
				area++
				px := index % width
				py := index / width
				if px < minX {
					minX = px
				}
				if px > maxX {
					maxX = px
				}
				if py < minY {
					minY = py
				}
				if py > maxY {
					maxY = py
				}
				if seeds[index] != 0 {
					seedHit = true
				}

				for ny := maxInt(py-1, 0); ny <= minInt(py+1, height-1); ny++ {
					for nx := maxInt(px-1, 0); nx <= minInt(px+1, width-1); nx++ {
						neighbor := ny*width + nx
						if labels[neighbor] != 0 || mask[neighbor] == 0 {
							continue
						}
						labels[neighbor] = label
						q = append(q, neighbor)
					}
				}
			}

			meta := componentMeta{
				label:   label,
				minX:    minX,
				minY:    minY,
				maxX:    maxX,
				maxY:    maxY,
				area:    area,
				seedHit: seedHit,
			}
			if minArea > 1 && area < minArea {
				continue
			}
			metas = append(metas, meta)
		}
	}
	*queue = q
	*components = metas
	return metas
}

func minimumToothAreaPixels(width, height int) int {
	return maxInt(width*height/2000, minimumToothAreaFloorPixels)
}

func minimumBoneAreaPixels(width, height int) int {
	return maxInt(width*height/12000, 24)
}

func thickenBoneLineMask(mask []uint8, width, height, radius int) []uint8 {
	if radius <= 0 {
		return append([]uint8(nil), mask...)
	}
	return dilateBinaryMask(mask, width, height, radius)
}

func thickenBoneLineMaskWithWorkspace(mask []uint8, width, height, radius int, workspace *maskWorkspace) []uint8 {
	if radius <= 0 || len(mask) == 0 {
		return mask
	}
	out := workspace.getMask(len(mask))
	scratch := workspace.getMask(len(mask))
	dilateBinaryMaskInto(out, scratch, workspace.countScratch(width), mask, width, height, radius)
	return out
}

func collectComponents(mask []uint8, seeds []uint8, width, height, minArea int) []componentMeta {
	return componentSizesAndBBoxes(mask, seeds, width, height, minArea)
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

func boxBlurGray(pixels []uint8, width, height, radius int) []uint8 {
	if radius <= 0 || len(pixels) == 0 {
		return append([]uint8(nil), pixels...)
	}

	window := radius*2 + 1
	horizontal := make([]uint16, len(pixels))
	for y := 0; y < height; y++ {
		row := y * width
		sum := 0
		for x := -radius; x <= radius; x++ {
			sum += int(pixels[row+clampInt(x, 0, width-1)])
		}
		for x := 0; x < width; x++ {
			horizontal[row+x] = uint16((sum + window/2) / window)
			left := clampInt(x-radius, 0, width-1)
			right := clampInt(x+radius+1, 0, width-1)
			sum += int(pixels[row+right]) - int(pixels[row+left])
		}
	}

	blurred := make([]uint8, len(pixels))
	for x := 0; x < width; x++ {
		sum := 0
		for y := -radius; y <= radius; y++ {
			sum += int(horizontal[clampInt(y, 0, height-1)*width+x])
		}
		for y := 0; y < height; y++ {
			blurred[y*width+x] = uint8((sum + window/2) / window)
			top := clampInt(y-radius, 0, height-1)
			bottom := clampInt(y+radius+1, 0, height-1)
			sum += int(horizontal[bottom*width+x]) - int(horizontal[top*width+x])
		}
	}

	return blurred
}

func innerOutlineMask(mask []uint8, width, height, thickness int) []uint8 {
	if thickness <= 0 || len(mask) == 0 {
		return append([]uint8(nil), mask...)
	}

	outline := make([]uint8, len(mask))
	eroded := make([]uint8, len(mask))
	scratch := make([]uint8, len(mask))
	counts := make([]int, width)
	innerOutlineMaskInto(outline, eroded, scratch, counts, mask, width, height, thickness)
	return outline
}

func innerOutlineMaskInto(out []uint8, eroded []uint8, scratch []uint8, counts []int, mask []uint8, width, height, thickness int) {
	if thickness <= 0 || len(mask) == 0 {
		copy(out, mask)
		return
	}

	erodeBinaryMaskInto(eroded, scratch, counts, mask, width, height, thickness)
	for index, value := range mask {
		if value != 0 && eroded[index] == 0 {
			out[index] = 1
		} else {
			out[index] = 0
		}
	}
}

func closeBinaryMask(mask []uint8, width, height, radius int) []uint8 {
	if radius <= 0 || len(mask) == 0 {
		return append([]uint8(nil), mask...)
	}

	out := append([]uint8(nil), mask...)
	work := make([]uint8, len(mask))
	scratch := make([]uint8, len(mask))
	counts := make([]int, maxInt(width, 0))
	closeBinaryMaskInPlace(out, work, scratch, counts, width, height, radius)
	return out
}

func closeBinaryMaskInPlace(mask []uint8, work []uint8, scratch []uint8, counts []int, width, height, radius int) {
	if radius <= 0 || len(mask) == 0 {
		return
	}
	if width <= 0 || height <= 0 {
		clear(mask)
		return
	}

	dilateBinaryMaskInto(work, scratch, counts, mask, width, height, radius)
	erodeBinaryMaskInto(mask, scratch, counts, work, width, height, radius)
}

func openBinaryMask(mask []uint8, width, height, radius int) []uint8 {
	if radius <= 0 || len(mask) == 0 {
		return append([]uint8(nil), mask...)
	}

	out := append([]uint8(nil), mask...)
	work := make([]uint8, len(mask))
	scratch := make([]uint8, len(mask))
	counts := make([]int, maxInt(width, 0))
	openBinaryMaskInPlace(out, work, scratch, counts, width, height, radius)
	return out
}

func openBinaryMaskInPlace(mask []uint8, work []uint8, scratch []uint8, counts []int, width, height, radius int) {
	if radius <= 0 || len(mask) == 0 {
		return
	}
	if width <= 0 || height <= 0 {
		clear(mask)
		return
	}

	erodeBinaryMaskInto(work, scratch, counts, mask, width, height, radius)
	dilateBinaryMaskInto(mask, scratch, counts, work, width, height, radius)
}

func dilateBinaryMask(mask []uint8, width, height, radius int) []uint8 {
	if radius <= 0 || len(mask) == 0 {
		return append([]uint8(nil), mask...)
	}

	out := make([]uint8, len(mask))
	scratch := make([]uint8, len(mask))
	counts := make([]int, maxInt(width, 0))
	dilateBinaryMaskInto(out, scratch, counts, mask, width, height, radius)
	return out
}

func dilateBinaryMaskInto(out []uint8, scratch []uint8, counts []int, mask []uint8, width, height, radius int) {
	if width <= 0 || height <= 0 {
		clear(out)
		return
	}

	for y := 0; y < height; y++ {
		row := y * width
		count := 0
		for x := 0; x <= minInt(radius, width-1); x++ {
			if mask[row+x] != 0 {
				count++
			}
		}
		for x := 0; x < width; x++ {
			if count > 0 {
				scratch[row+x] = 1
			} else {
				scratch[row+x] = 0
			}
			if remove := x - radius; remove >= 0 && mask[row+remove] != 0 {
				count--
			}
			if add := x + radius + 1; add < width && mask[row+add] != 0 {
				count++
			}
		}
	}

	clear(counts[:width])
	for y := 0; y <= minInt(radius, height-1); y++ {
		row := y * width
		for x := 0; x < width; x++ {
			if scratch[row+x] != 0 {
				counts[x]++
			}
		}
	}
	for y := 0; y < height; y++ {
		row := y * width
		for x := 0; x < width; x++ {
			if counts[x] > 0 {
				out[row+x] = 1
			} else {
				out[row+x] = 0
			}
		}
		if remove := y - radius; remove >= 0 {
			row := remove * width
			for x := 0; x < width; x++ {
				if scratch[row+x] != 0 {
					counts[x]--
				}
			}
		}
		if add := y + radius + 1; add < height {
			row := add * width
			for x := 0; x < width; x++ {
				if scratch[row+x] != 0 {
					counts[x]++
				}
			}
		}
	}
}

func erodeBinaryMask(mask []uint8, width, height, radius int) []uint8 {
	if radius <= 0 || len(mask) == 0 {
		return append([]uint8(nil), mask...)
	}

	out := make([]uint8, len(mask))
	scratch := make([]uint8, len(mask))
	counts := make([]int, maxInt(width, 0))
	erodeBinaryMaskInto(out, scratch, counts, mask, width, height, radius)
	return out
}

func erodeBinaryMaskInto(out []uint8, scratch []uint8, counts []int, mask []uint8, width, height, radius int) {
	if width <= 0 || height <= 0 {
		clear(out)
		return
	}

	window := radius*2 + 1
	for y := 0; y < height; y++ {
		row := y * width
		count := 0
		for x := 0; x <= minInt(radius, width-1); x++ {
			if mask[row+x] != 0 {
				count++
			}
		}
		for x := 0; x < width; x++ {
			if count == window {
				scratch[row+x] = 1
			} else {
				scratch[row+x] = 0
			}
			if remove := x - radius; remove >= 0 && mask[row+remove] != 0 {
				count--
			}
			if add := x + radius + 1; add < width && mask[row+add] != 0 {
				count++
			}
		}
	}

	clear(counts[:width])
	for y := 0; y <= minInt(radius, height-1); y++ {
		row := y * width
		for x := 0; x < width; x++ {
			if scratch[row+x] != 0 {
				counts[x]++
			}
		}
	}
	for y := 0; y < height; y++ {
		row := y * width
		for x := 0; x < width; x++ {
			if counts[x] == window {
				out[row+x] = 1
			} else {
				out[row+x] = 0
			}
		}
		if remove := y - radius; remove >= 0 {
			row := remove * width
			for x := 0; x < width; x++ {
				if scratch[row+x] != 0 {
					counts[x]--
				}
			}
		}
		if add := y + radius + 1; add < height {
			row := add * width
			for x := 0; x < width; x++ {
				if scratch[row+x] != 0 {
					counts[x]++
				}
			}
		}
	}
}

func fillHolesBinaryMask(mask []uint8, width, height int) []uint8 {
	if len(mask) == 0 {
		return nil
	}

	filled := make([]uint8, len(mask))
	outside := make([]uint8, len(mask))
	queue := make([]int, 0, width*2+height*2)
	fillHolesBinaryMaskInto(filled, outside, &queue, mask, width, height)
	return filled
}

func fillHolesBinaryMaskInto(out []uint8, outside []uint8, queue *[]int, mask []uint8, width, height int) {
	if len(mask) == 0 {
		return
	}
	if width <= 0 || height <= 0 {
		copy(out, mask)
		return
	}

	clear(outside[:len(mask)])
	q := (*queue)[:0]
	push := func(index int) {
		if index < 0 || index >= len(mask) || outside[index] != 0 || mask[index] != 0 {
			return
		}
		outside[index] = 1
		q = append(q, index)
	}
	for x := 0; x < width; x++ {
		push(x)
		push((height-1)*width + x)
	}
	for y := 0; y < height; y++ {
		push(y * width)
		push(y*width + width - 1)
	}

	for head := 0; head < len(q); head++ {
		index := q[head]
		x := index % width
		y := index / width
		if x > 0 {
			push(index - 1)
		}
		if x+1 < width {
			push(index + 1)
		}
		if y > 0 {
			push(index - width)
		}
		if y+1 < height {
			push(index + width)
		}
	}

	copy(out, mask)
	for index := range out {
		if out[index] == 0 && outside[index] == 0 {
			out[index] = 1
		}
	}
	*queue = q
}

func fillSmallHolesBinaryMask(mask []uint8, width, height, maxArea int) []uint8 {
	if len(mask) == 0 || maxArea <= 0 {
		return append([]uint8(nil), mask...)
	}

	visited := make([]bool, len(mask))
	queue := make([]int, 0, 512)
	out := append([]uint8(nil), mask...)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			start := y*width + x
			if visited[start] || mask[start] != 0 {
				continue
			}

			visited[start] = true
			queue = append(queue[:0], start)
			pixels := make([]int, 0, 128)
			touchesBorder := false
			for head := 0; head < len(queue); head++ {
				index := queue[head]
				pixels = append(pixels, index)
				px := index % width
				py := index / width
				if px == 0 || py == 0 || px == width-1 || py == height-1 {
					touchesBorder = true
				}

				if px > 0 {
					neighbor := index - 1
					if !visited[neighbor] && mask[neighbor] == 0 {
						visited[neighbor] = true
						queue = append(queue, neighbor)
					}
				}
				if px+1 < width {
					neighbor := index + 1
					if !visited[neighbor] && mask[neighbor] == 0 {
						visited[neighbor] = true
						queue = append(queue, neighbor)
					}
				}
				if py > 0 {
					neighbor := index - width
					if !visited[neighbor] && mask[neighbor] == 0 {
						visited[neighbor] = true
						queue = append(queue, neighbor)
					}
				}
				if py+1 < height {
					neighbor := index + width
					if !visited[neighbor] && mask[neighbor] == 0 {
						visited[neighbor] = true
						queue = append(queue, neighbor)
					}
				}
			}

			if touchesBorder || len(pixels) > maxArea {
				continue
			}
			for _, index := range pixels {
				out[index] = 1
			}
		}
	}
	return out
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

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}
