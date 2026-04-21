package analysis

import (
	"fmt"

	"xrayview/backend/internal/bufpool"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/imaging"
)

type BoundaryTrace struct {
	Points []contracts.Point
	Closed bool
}

type maskComponent struct {
	pixels []int
	bbox   contracts.BoundingBox
	area   uint32
}

func ExtractBlackGapTraces(preview imaging.PreviewImage) ([]BoundaryTrace, error) {
	if preview.Format != imaging.FormatGray8 {
		return nil, fmt.Errorf("gap tracing currently requires an 8-bit grayscale preview")
	}

	return extractBlackGapTraces(preview.Width, preview.Height, preview.Pixels)
}

func extractBlackGapTraces(width, height uint32, pixels []uint8) ([]BoundaryTrace, error) {
	expectedLen := int(width) * int(height)
	if len(pixels) != expectedLen {
		return nil, fmt.Errorf("gap tracing expects %d pixels for dimensions %dx%d, got %d", expectedLen, width, height, len(pixels))
	}
	if width < 16 || height < 16 {
		return nil, nil
	}

	normalized := normalizePixels(pixels)
	defer bufpool.PutUint8(normalized)

	search := defaultSearchRegion(width, height)
	search = searchRegion{
		x:      0,
		y:      search.y,
		width:  width,
		height: search.height,
	}
	if search.y >= height || search.height == 0 {
		return nil, nil
	}
	if search.y+search.height > height {
		search.height = height - search.y
	}

	blackThreshold := maxUint8(percentileInRegion(normalized, width, search, 0.08), 20)
	if blackThreshold > 52 {
		blackThreshold = 52
	}

	blackMask := make([]uint8, len(normalized))
	for y := search.y; y < search.y+search.height; y++ {
		rowStart := int(y * width)
		for x := search.x; x < search.x+search.width; x++ {
			index := rowStart + int(x)
			if normalized[index] <= blackThreshold {
				blackMask[index] = 1
			}
		}
	}

	blackMask = closeBinaryMask(blackMask, int(width), int(height))
	blackMask = openBinaryMask(blackMask, int(width), int(height))

	components := collectMaskComponents(blackMask, width, height, search, 80)
	traces := make([]BoundaryTrace, 0, len(components))
	for _, component := range components {
		if !shouldKeepBlackGapComponent(component, search) {
			continue
		}
		componentTraces := traceBlackComponentBoundaries(component, blackMask, width, height, search)
		for _, trace := range componentTraces {
			minPoints := 2
			if trace.Closed {
				minPoints = 3
			}
			if len(trace.Points) < minPoints || pathLength(trace.Points) < 12 {
				continue
			}
			traces = append(traces, trace)
		}
	}

	return traces, nil
}

func shouldKeepBlackGapComponent(component maskComponent, search searchRegion) bool {
	bbox := component.bbox
	touchesSide := bbox.X <= search.x ||
		bbox.Y <= search.y ||
		bbox.X+bbox.Width >= search.x+search.width ||
		bbox.Y+bbox.Height >= search.y+search.height
	if touchesSide {
		return true
	}
	if component.area >= 140 {
		return true
	}
	return bbox.Width >= 8
}

func collectMaskComponents(
	mask []uint8,
	width, height uint32,
	search searchRegion,
	minArea uint32,
) []maskComponent {
	widthInt := int(width)
	heightInt := int(height)
	visited := make([]bool, len(mask))
	queue := make([]int, 0, 256)
	components := make([]maskComponent, 0)

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

			for head < len(queue) {
				index := queue[head]
				head++
				px := uint32(index % widthInt)
				py := uint32(index / widthInt)
				minX = minUint32(minX, px)
				maxX = maxUint32(maxX, px)
				minY = minUint32(minY, py)
				maxY = maxUint32(maxY, py)

				for ny := maxInt(int(py)-1, 0); ny <= minInt(int(py)+1, heightInt-1); ny++ {
					for nx := maxInt(int(px)-1, 0); nx <= minInt(int(px)+1, widthInt-1); nx++ {
						neighbor := ny*widthInt + nx
						if visited[neighbor] || mask[neighbor] == 0 {
							continue
						}
						visited[neighbor] = true
						queue = append(queue, neighbor)
					}
				}
			}

			area := uint32(len(queue))
			if area < minArea {
				continue
			}
			componentPixels := make([]int, len(queue))
			copy(componentPixels, queue)
			components = append(components, maskComponent{
				pixels: componentPixels,
				bbox: contracts.BoundingBox{
					X:      minX,
					Y:      minY,
					Width:  maxX - minX + 1,
					Height: maxY - minY + 1,
				},
				area: area,
			})
		}
	}

	return components
}

func traceBlackComponentBoundaries(
	component maskComponent,
	mask []uint8,
	width, height uint32,
	search searchRegion,
) []BoundaryTrace {
	if len(component.pixels) == 0 || component.bbox.Width == 0 || component.bbox.Height == 0 {
		return nil
	}

	localWidth := int(component.bbox.Width)
	localHeight := int(component.bbox.Height)
	localMask := make([]bool, localWidth*localHeight)
	widthInt := int(width)
	for _, index := range component.pixels {
		x := uint32(index % widthInt)
		y := uint32(index / widthInt)
		localX := int(x - component.bbox.X)
		localY := int(y - component.bbox.Y)
		if localX < 0 || localX >= localWidth || localY < 0 || localY >= localHeight {
			continue
		}
		localMask[localY*localWidth+localX] = true
	}

	segments := make([]outlineSegment, 0, len(component.pixels)*2)
	addSegment := func(startX, startY, endX, endY uint32) {
		segments = append(segments, outlineSegment{
			start: contracts.Point{X: component.bbox.X + startX, Y: component.bbox.Y + startY},
			end:   contracts.Point{X: component.bbox.X + endX, Y: component.bbox.Y + endY},
		})
	}
	isFilled := func(x, y int) bool {
		return x >= 0 && x < localWidth && y >= 0 && y < localHeight && localMask[y*localWidth+x]
	}
	isTraceNeighbor := func(x, y int) bool {
		if x < int(search.x) || x >= int(search.x+search.width) || y < int(search.y) || y >= int(search.y+search.height) {
			return false
		}
		return mask[y*widthInt+x] == 0
	}

	for y := 0; y < localHeight; y++ {
		for x := 0; x < localWidth; x++ {
			if !localMask[y*localWidth+x] {
				continue
			}

			globalX := int(component.bbox.X) + x
			globalY := int(component.bbox.Y) + y
			if !isFilled(x, y-1) && isTraceNeighbor(globalX, globalY-1) {
				addSegment(uint32(x), uint32(y), uint32(x+1), uint32(y))
			}
			if !isFilled(x+1, y) && isTraceNeighbor(globalX+1, globalY) {
				addSegment(uint32(x+1), uint32(y), uint32(x+1), uint32(y+1))
			}
			if !isFilled(x, y+1) && isTraceNeighbor(globalX, globalY+1) {
				addSegment(uint32(x+1), uint32(y+1), uint32(x), uint32(y+1))
			}
			if !isFilled(x-1, y) && isTraceNeighbor(globalX-1, globalY) {
				addSegment(uint32(x), uint32(y+1), uint32(x), uint32(y))
			}
		}
	}

	return traceBoundaryPaths(segments)
}

func traceBoundaryPaths(segments []outlineSegment) []BoundaryTrace {
	if len(segments) == 0 {
		return nil
	}

	adjacency := make(map[uint64][]int, len(segments)*2)
	for index, segment := range segments {
		adjacency[pointKey(segment.start)] = append(adjacency[pointKey(segment.start)], index)
		adjacency[pointKey(segment.end)] = append(adjacency[pointKey(segment.end)], index)
	}

	used := make([]bool, len(segments))
	traces := make([]BoundaryTrace, 0, len(segments)/2)

	for key, indexes := range adjacency {
		if len(indexes) != 1 {
			continue
		}
		index := indexes[0]
		if used[index] {
			continue
		}
		points, closed := walkBoundaryPath(pointFromKey(key), index, segments, adjacency, used)
		if len(points) >= 2 {
			traces = append(traces, BoundaryTrace{
				Points: simplifyPointPath(points, closed),
				Closed: closed,
			})
		}
	}

	for index := range segments {
		if used[index] {
			continue
		}
		points, closed := walkBoundaryPath(segments[index].start, index, segments, adjacency, used)
		if len(points) >= 2 {
			traces = append(traces, BoundaryTrace{
				Points: simplifyPointPath(points, closed),
				Closed: closed,
			})
		}
	}

	return traces
}

func walkBoundaryPath(
	start contracts.Point,
	startSegment int,
	segments []outlineSegment,
	adjacency map[uint64][]int,
	used []bool,
) ([]contracts.Point, bool) {
	points := []contracts.Point{start}
	current := start
	segmentIndex := startSegment
	prevSegment := -1
	closed := false

	for segmentIndex >= 0 {
		if used[segmentIndex] {
			break
		}
		used[segmentIndex] = true
		nextPoint := otherSegmentPoint(segments[segmentIndex], current)
		points = append(points, nextPoint)
		current = nextPoint
		if current == start {
			closed = true
			break
		}
		prevSegment = segmentIndex
		segmentIndex = nextUnusedBoundarySegment(current, adjacency, used, prevSegment)
	}

	return points, closed
}

func nextUnusedBoundarySegment(
	point contracts.Point,
	adjacency map[uint64][]int,
	used []bool,
	prevSegment int,
) int {
	options := adjacency[pointKey(point)]
	for _, index := range options {
		if used[index] || index == prevSegment {
			continue
		}
		return index
	}
	return -1
}

func otherSegmentPoint(segment outlineSegment, current contracts.Point) contracts.Point {
	if segment.start == current {
		return segment.end
	}
	return segment.start
}

func simplifyPointPath(points []contracts.Point, closed bool) []contracts.Point {
	if closed {
		return simplifyClosedPointLoop(points)
	}
	if len(points) == 0 {
		return []contracts.Point{}
	}

	simplified := make([]contracts.Point, 0, len(points))
	for _, point := range points {
		if len(simplified) == 0 || simplified[len(simplified)-1] != point {
			simplified = append(simplified, point)
		}
	}
	if len(simplified) < 3 {
		return simplified
	}

	next := make([]contracts.Point, 0, len(simplified))
	next = append(next, simplified[0])
	for index := 1; index < len(simplified)-1; index++ {
		if isCollinear(simplified[index-1], simplified[index], simplified[index+1]) {
			continue
		}
		next = append(next, simplified[index])
	}
	next = append(next, simplified[len(simplified)-1])
	return next
}

func pointFromKey(key uint64) contracts.Point {
	return contracts.Point{
		X: uint32(key >> 32),
		Y: uint32(key),
	}
}

func pathLength(points []contracts.Point) uint32 {
	if len(points) < 2 {
		return 0
	}

	var total uint32
	for index := 1; index < len(points); index++ {
		total += lineSegmentLength(contracts.LineSegment{
			Start: points[index-1],
			End:   points[index],
		})
	}
	return total
}
