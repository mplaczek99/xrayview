package analysis

import (
	"math"

	"xrayview/backend/internal/bufpool"
	"xrayview/backend/internal/contracts"
)

func shouldUseCloseupBoundaryAnalysis(width, height uint32) bool {
	return width >= 640 && width <= 1200 && height >= width
}

func detectCloseupCentralIncisorCandidates(
	normalized []uint8,
	width, height uint32,
) ([]componentCandidate, []contracts.ToothGeometry, bool) {
	search := estimateCloseupSearchRegion(normalized, width, height)
	if search.width < width/3 || search.height < height/2 {
		return nil, nil, false
	}

	enhanced := claheGray(normalized, int(width), int(height), 48, 2.5)
	defer bufpool.PutUint8(enhanced)

	blurred := gaussianBlurGrayFast(enhanced, width, height, 1.2)
	defer bufpool.PutUint8(blurred)

	boundary, darkness := buildCloseupBoundaryMaps(blurred, width, height, search)

	crownGapBand := searchRegion{
		x:      search.x,
		y:      search.y + uint32(math.Round(float64(search.height)*0.50)),
		width:  search.width,
		height: maxUint32(uint32(math.Round(float64(search.height)*0.34)), 1),
	}
	crownGapBand = clampSearchRegionToSearch(crownGapBand, search)
	gapProfile := buildCloseupColumnProfile(darkness, width, crownGapBand, search)
	gapProfile = smoothFloatProfile(gapProfile, 9)
	if len(gapProfile) == 0 {
		return nil, nil, false
	}

	searchMidX := search.x + search.width/2
	centerWindowHalf := maxUint32(search.width/10, 36)
	gapLeft := maxUint32(saturatingSubUint32(searchMidX, centerWindowHalf), search.x)
	gapRight := minUint32(searchMidX+centerWindowHalf, search.x+search.width-1)
	gapX := pickCloseupProfilePeak(gapProfile, search, gapLeft, gapRight, searchMidX, 1.5)

	crownBand := searchRegion{
		x:      search.x,
		y:      search.y + uint32(math.Round(float64(search.height)*0.60)),
		width:  search.width,
		height: maxUint32(uint32(math.Round(float64(search.height)*0.24)), 1),
	}
	crownBand = clampSearchRegionToSearch(crownBand, search)
	brightnessProfile := smoothFloatProfile(buildCloseupColumnProfile(blurred, width, crownBand, search), 7)
	if len(brightnessProfile) == 0 {
		return nil, nil, false
	}

	expectedHalfWidth := clampInt(int(search.width/7), 70, 150)
	peakWindowHalf := clampInt(expectedHalfWidth/2, 28, 72)
	leftCenter := pickCloseupProfilePeak(
		brightnessProfile,
		search,
		maxUint32(search.x, saturatingSubUint32(gapX, uint32(expectedHalfWidth+peakWindowHalf))),
		maxUint32(search.x, saturatingSubUint32(gapX, uint32(maxInt(expectedHalfWidth-peakWindowHalf, 20)))),
		saturatingSubUint32(gapX, uint32(expectedHalfWidth)),
		0.75,
	)
	rightCenter := pickCloseupProfilePeak(
		brightnessProfile,
		search,
		minUint32(gapX+uint32(maxInt(expectedHalfWidth-peakWindowHalf, 20)), search.x+search.width-1),
		minUint32(gapX+uint32(expectedHalfWidth+peakWindowHalf), search.x+search.width-1),
		minUint32(gapX+uint32(expectedHalfWidth), search.x+search.width-1),
		0.75,
	)
	if leftCenter >= gapX || rightCenter <= gapX {
		return nil, nil, false
	}

	rowBand := searchRegion{
		x:      search.x,
		y:      search.y + uint32(math.Round(float64(search.height)*0.02)),
		width:  search.width,
		height: maxUint32(uint32(math.Round(float64(search.height)*0.90)), 1),
	}
	rowBand = clampSearchRegionToSearch(rowBand, search)

	gapWindowHalf := clampInt(int(search.width/32), 18, 30)
	gapPath, gapStrength := traceCloseupVerticalPath(
		darkness,
		width,
		rowBand,
		maxUint32(search.x, saturatingSubUint32(gapX, uint32(gapWindowHalf))),
		minUint32(gapX+uint32(gapWindowHalf), search.x+search.width-1),
		gapX,
		3,
		11.0,
		1.25,
	)
	if len(gapPath) == 0 || gapStrength < 72 {
		return nil, nil, false
	}

	leftHalfWidth := maxUint32(saturatingSubUint32(gapX, leftCenter), 1)
	rightHalfWidth := maxUint32(saturatingSubUint32(rightCenter, gapX), 1)
	leftOuterTarget := maxUint32(search.x, saturatingSubUint32(leftCenter, leftHalfWidth))
	rightOuterTarget := minUint32(rightCenter+rightHalfWidth, search.x+search.width-1)
	outerWindowHalf := clampInt(int(search.width/24), 22, 44)

	leftOuterPath, leftStrength := traceCloseupVerticalPath(
		boundary,
		width,
		rowBand,
		maxUint32(search.x, saturatingSubUint32(leftOuterTarget, uint32(outerWindowHalf))),
		minUint32(leftOuterTarget+uint32(outerWindowHalf), gapX-4),
		leftOuterTarget,
		4,
		10.0,
		0.35,
	)
	rightOuterPath, rightStrength := traceCloseupVerticalPath(
		boundary,
		width,
		rowBand,
		maxUint32(gapX+4, saturatingSubUint32(rightOuterTarget, uint32(outerWindowHalf))),
		minUint32(rightOuterTarget+uint32(outerWindowHalf), search.x+search.width-1),
		rightOuterTarget,
		4,
		10.0,
		0.35,
	)

	candidates := make([]componentCandidate, 0, 2)
	geometries := make([]contracts.ToothGeometry, 0, 2)
	if leftStrength >= 42 {
		candidate, geometry, ok := buildCloseupCandidateFromPaths(
			normalized,
			boundary,
			width,
			height,
			search,
			rowBand,
			leftOuterPath,
			gapPath,
			gapX,
			false,
		)
		if ok {
			candidates = append(candidates, candidate)
			geometries = append(geometries, geometry)
		}
	}
	if rightStrength >= 42 {
		candidate, geometry, ok := buildCloseupCandidateFromPaths(
			normalized,
			boundary,
			width,
			height,
			search,
			rowBand,
			gapPath,
			rightOuterPath,
			gapX,
			true,
		)
		if ok {
			candidates = append(candidates, candidate)
			geometries = append(geometries, geometry)
		}
	}
	if len(candidates) == 0 {
		return nil, nil, false
	}

	sortDetectedCandidates(candidates)
	sortedGeometries := make([]contracts.ToothGeometry, len(geometries))
	for index, candidate := range candidates {
		for geometryIndex := range geometries {
			if geometries[geometryIndex].BoundingBox == candidate.bbox {
				sortedGeometries[index] = geometries[geometryIndex]
				break
			}
		}
		if sortedGeometries[index].BoundingBox.Width == 0 {
			sortedGeometries[index] = geometries[index]
		}
	}

	return candidates, sortedGeometries, true
}

func estimateCloseupSearchRegion(normalized []uint8, width, height uint32) searchRegion {
	xMargin := maxUint32(width/16, 24)
	top := maxUint32(uint32(math.Round(float64(height)*0.06)), 24)
	preliminaryBottom := minUint32(uint32(math.Round(float64(height)*0.97)), height-1)
	preliminary := searchRegion{
		x:      xMargin,
		y:      top,
		width:  maxUint32(saturatingSubUint32(width, xMargin*2), 1),
		height: maxUint32(saturatingSubUint32(preliminaryBottom, top)+1, 1),
	}

	bottom := estimateCloseupBottomY(normalized, width, height, preliminary)
	if bottom <= top {
		bottom = preliminaryBottom
	}
	return searchRegion{
		x:      preliminary.x,
		y:      preliminary.y,
		width:  preliminary.width,
		height: maxUint32(saturatingSubUint32(bottom, preliminary.y)+1, 1),
	}
}

func estimateCloseupBottomY(
	normalized []uint8,
	width, height uint32,
	search searchRegion,
) uint32 {
	bandLeft := search.x + search.width/4
	bandRight := search.x + search.width*3/4
	if bandRight <= bandLeft {
		return search.y + search.height - 1
	}

	const brightnessThreshold = 30.0
	streak := 0
	for y := int(minUint32(search.y+search.height-1, height-1)); y >= int(search.y); y-- {
		rowStart := y * int(width)
		sum := 0.0
		count := 0.0
		for x := bandLeft; x <= bandRight; x++ {
			sum += float64(normalized[rowStart+int(x)])
			count++
		}
		if count == 0 {
			continue
		}
		if sum/count >= brightnessThreshold {
			streak++
			if streak >= 12 {
				return minUint32(uint32(y+20), height-1)
			}
			continue
		}
		streak = 0
	}
	return search.y + search.height - 1
}

func buildCloseupBoundaryMaps(
	blurred []uint8,
	width, height uint32,
	search searchRegion,
) ([]uint8, []uint8) {
	rawBoundary := make([]uint16, len(blurred))
	rawDarkness := make([]uint16, len(blurred))
	integral := buildIntegralImage(blurred, width, search)

	maxBoundary := uint16(1)
	maxDarkness := uint16(1)
	for y := search.y; y < search.y+search.height; y++ {
		for x := search.x; x < search.x+search.width; x++ {
			index := int(y*width + x)
			gx := uint16(sobelXAbsGray(blurred, width, height, x, y))
			localMean := meanInIntegralBox(integral, search, x, y, 6, 28)
			darkness := uint16(maxInt(localMean-int(blurred[index]), 0))
			combined := uint16(minInt(int(math.Round(float64(gx)*0.7+float64(darkness)*1.3)), math.MaxUint16))
			rawDarkness[index] = darkness
			rawBoundary[index] = combined
			if combined > maxBoundary {
				maxBoundary = combined
			}
			if darkness > maxDarkness {
				maxDarkness = darkness
			}
		}
	}

	boundary := make([]uint8, len(blurred))
	darkness := make([]uint8, len(blurred))
	for y := search.y; y < search.y+search.height; y++ {
		for x := search.x; x < search.x+search.width; x++ {
			index := int(y*width + x)
			boundary[index] = uint8((uint32(rawBoundary[index]) * 255) / uint32(maxBoundary))
			darkness[index] = uint8((uint32(rawDarkness[index]) * 255) / uint32(maxDarkness))
		}
	}

	return boundary, darkness
}

func sobelXAbsGray(pixels []uint8, width, height, x, y uint32) int {
	leftX := saturatingSubUint32(x, 1)
	rightX := minUint32(x+1, width-1)
	topY := saturatingSubUint32(y, 1)
	bottomY := minUint32(y+1, height-1)

	topLeft := int(pixels[int(topY*width+leftX)])
	midLeft := int(pixels[int(y*width+leftX)])
	bottomLeft := int(pixels[int(bottomY*width+leftX)])
	topRight := int(pixels[int(topY*width+rightX)])
	midRight := int(pixels[int(y*width+rightX)])
	bottomRight := int(pixels[int(bottomY*width+rightX)])

	gx := -topLeft - 2*midLeft - bottomLeft + topRight + 2*midRight + bottomRight
	return absInt(gx)
}

func meanInIntegralBox(
	integral []int,
	region searchRegion,
	x, y uint32,
	radiusX, radiusY int,
) int {
	localWidth := int(region.width)
	localHeight := int(region.height)
	localX := int(x - region.x)
	localY := int(y - region.y)
	minX := maxInt(localX-radiusX, 0)
	maxX := minInt(localX+radiusX, localWidth-1)
	minY := maxInt(localY-radiusY, 0)
	maxY := minInt(localY+radiusY, localHeight-1)
	sum := integral[(maxY+1)*(localWidth+1)+(maxX+1)] -
		integral[minY*(localWidth+1)+(maxX+1)] -
		integral[(maxY+1)*(localWidth+1)+minX] +
		integral[minY*(localWidth+1)+minX]
	area := (maxX - minX + 1) * (maxY - minY + 1)
	if area <= 0 {
		return 0
	}
	return sum / area
}

func buildCloseupColumnProfile(
	values []uint8,
	width uint32,
	band searchRegion,
	search searchRegion,
) []float64 {
	profile := make([]float64, search.width)
	if band.width == 0 || band.height == 0 {
		return profile
	}

	for x := band.x; x < band.x+band.width; x++ {
		sum := 0.0
		count := 0.0
		for y := band.y; y < band.y+band.height; y++ {
			sum += float64(values[int(y*width+x)])
			count++
		}
		if count > 0 {
			profile[x-search.x] = sum / count
		}
	}
	return profile
}

func pickCloseupProfilePeak(
	profile []float64,
	search searchRegion,
	startX, endX, targetX uint32,
	distancePenalty float64,
) uint32 {
	if len(profile) == 0 {
		return targetX
	}
	start := clampInt(int(startX-search.x), 0, len(profile)-1)
	end := clampInt(int(endX-search.x), start, len(profile)-1)
	target := clampInt(int(targetX-search.x), start, end)
	bestIndex := target
	bestScore := profile[target]
	for index := start; index <= end; index++ {
		score := profile[index] - float64(absInt(index-target))*distancePenalty
		if score > bestScore {
			bestScore = score
			bestIndex = index
		}
	}
	return search.x + uint32(bestIndex)
}

func traceCloseupVerticalPath(
	response []uint8,
	width uint32,
	region searchRegion,
	minX, maxX, targetX uint32,
	maxStep int,
	jumpPenalty float64,
	centerPenalty float64,
) ([]uint32, float64) {
	if region.width == 0 || region.height == 0 {
		return nil, 0
	}
	if maxX < minX {
		return nil, 0
	}

	minX = maxUint32(minX, region.x)
	maxX = minUint32(maxX, region.x+region.width-1)
	if maxX < minX {
		return nil, 0
	}

	cols := int(maxX-minX) + 1
	rows := int(region.height)
	back := make([]int, rows*cols)
	prev := make([]float64, cols)
	curr := make([]float64, cols)
	for index := range prev {
		prev[index] = math.Inf(1)
		curr[index] = math.Inf(1)
	}

	for col := 0; col < cols; col++ {
		x := minX + uint32(col)
		responseValue := response[int(region.y*width+x)]
		prev[col] = float64(255-responseValue) + math.Abs(float64(int(x)-int(targetX)))*centerPenalty
		back[col] = -1
	}

	for row := 1; row < rows; row++ {
		y := region.y + uint32(row)
		for col := 0; col < cols; col++ {
			x := minX + uint32(col)
			unary := float64(255-response[int(y*width+x)]) + math.Abs(float64(int(x)-int(targetX)))*centerPenalty
			bestCost := math.Inf(1)
			bestPrev := col
			prevStart := maxInt(col-maxStep, 0)
			prevEnd := minInt(col+maxStep, cols-1)
			for prevCol := prevStart; prevCol <= prevEnd; prevCol++ {
				cost := prev[prevCol] + unary + float64(absInt(col-prevCol))*jumpPenalty
				if cost < bestCost {
					bestCost = cost
					bestPrev = prevCol
				}
			}
			curr[col] = bestCost
			back[row*cols+col] = bestPrev
		}
		prev, curr = curr, prev
		for index := range curr {
			curr[index] = math.Inf(1)
		}
	}

	bestCol := 0
	bestCost := prev[0]
	for col := 1; col < cols; col++ {
		if prev[col] < bestCost {
			bestCost = prev[col]
			bestCol = col
		}
	}

	path := make([]uint32, rows)
	col := bestCol
	totalResponse := 0.0
	for row := rows - 1; row >= 0; row-- {
		x := minX + uint32(col)
		path[row] = x
		y := region.y + uint32(row)
		totalResponse += float64(response[int(y*width+x)])
		if row == 0 {
			break
		}
		col = back[row*cols+col]
		if col < 0 {
			col = 0
		}
	}

	return smoothCloseupPath(path, 5), totalResponse / float64(rows)
}

func smoothCloseupPath(path []uint32, radius int) []uint32 {
	if len(path) == 0 || radius <= 0 {
		return append([]uint32(nil), path...)
	}

	prefix := make([]uint64, len(path)+1)
	for index, value := range path {
		prefix[index+1] = prefix[index] + uint64(value)
	}
	smoothed := make([]uint32, len(path))
	for index := range path {
		start := maxInt(index-radius, 0)
		end := minInt(index+radius+1, len(path))
		count := uint64(end - start)
		smoothed[index] = uint32((prefix[end] - prefix[start]) / count)
	}
	return smoothed
}

func buildCloseupCandidateFromPaths(
	normalized []uint8,
	boundary []uint8,
	width, height uint32,
	search searchRegion,
	rowBand searchRegion,
	leftPath []uint32,
	rightPath []uint32,
	gapX uint32,
	isRight bool,
) (componentCandidate, contracts.ToothGeometry, bool) {
	if len(leftPath) != len(rightPath) || len(leftPath) != int(rowBand.height) {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}

	mask := make([]uint8, len(normalized))
	for row := 0; row < len(leftPath); row++ {
		y := rowBand.y + uint32(row)
		left := minUint32(leftPath[row], rightPath[row])
		right := maxUint32(leftPath[row], rightPath[row])
		if right <= left+4 {
			continue
		}
		if !isRight {
			right = saturatingSubUint32(right, 1)
		} else {
			left = minUint32(left+1, right)
		}
		if right <= left+4 {
			continue
		}
		for x := left; x <= right; x++ {
			mask[int(y*width+x)] = 1
		}
	}

	mask = closeBinaryMask(mask, int(width), int(height))
	mask = closeBinaryMask(mask, int(width), int(height))
	mask = fillHolesBinaryMask(mask, int(width), int(height))
	mask = openBinaryMask(mask, int(width), int(height))

	components := collectMaskComponents(mask, width, height, search, maxUint32(search.area()/80, 4000))
	if len(components) == 0 {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}

	best := components[0]
	for _, component := range components[1:] {
		if component.area > best.area {
			best = component
		}
	}

	var intensitySum uint64
	var boundarySum uint64
	for _, index := range best.pixels {
		intensitySum += uint64(normalized[index])
		boundarySum += uint64(boundary[index])
	}
	meanIntensity := float64(intensitySum) / float64(maxUint32(best.area, 1))
	meanBoundary := float64(boundarySum) / float64(maxUint32(best.area, 1))
	fillRatio := float64(best.area) / float64(maxUint32(best.bbox.Width*best.bbox.Height, 1))
	aspectRatio := float64(best.bbox.Height) / float64(maxUint32(best.bbox.Width, 1))
	widthRatio := float64(best.bbox.Width) / float64(maxUint32(search.width, 1))
	heightRatio := float64(best.bbox.Height) / float64(maxUint32(search.height, 1))
	centerX := float64(best.bbox.X) + float64(best.bbox.Width)/2.0
	targetCenterX := float64(gapX)
	if !isRight {
		targetCenterX -= float64(best.bbox.Width) * 0.45
	} else {
		targetCenterX += float64(best.bbox.Width) * 0.45
	}
	centerScore := 1.0 - math.Min(math.Abs(centerX-targetCenterX)/(float64(search.width)*0.22), 1.0)
	strict := widthRatio >= 0.12 &&
		widthRatio <= 0.38 &&
		heightRatio >= 0.62 &&
		heightRatio <= 0.98 &&
		aspectRatio >= 2.2 &&
		aspectRatio <= 7.0 &&
		fillRatio >= 0.42 &&
		fillRatio <= 0.95 &&
		meanBoundary >= 36
	score := scoreCloseupCandidate(meanIntensity, meanBoundary, widthRatio, heightRatio, aspectRatio, fillRatio, centerScore, strict)
	geometry := geometryFromPixels(best.pixels, best.bbox, width)

	if best.area < 7000 || geometry.BoundingBox.Height < search.height/2 || len(geometry.Outline) < 8 {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}

	return componentCandidate{
		pixels: best.pixels,
		bbox:   best.bbox,
		area:   best.area,
		score:  score,
		strict: strict,
	}, geometry, true
}

func scoreCloseupCandidate(
	meanIntensity float64,
	meanBoundary float64,
	widthRatio float64,
	heightRatio float64,
	aspectRatio float64,
	fillRatio float64,
	centerScore float64,
	strict bool,
) float64 {
	boundaryScore := clamp01((meanBoundary - 32.0) / 96.0)
	brightnessScore := clamp01((meanIntensity - 52.0) / 140.0)
	widthScore := 1.0 - math.Min(math.Abs(widthRatio-0.20)/0.16, 1.0)
	heightScore := clamp01((heightRatio - 0.55) / 0.35)
	aspectScore := 1.0 - math.Min(math.Abs(aspectRatio-4.0)/3.0, 1.0)
	fillScore := 1.0 - math.Min(math.Abs(fillRatio-0.68)/0.34, 1.0)

	score := 0.28*boundaryScore +
		0.18*brightnessScore +
		0.16*widthScore +
		0.16*heightScore +
		0.10*aspectScore +
		0.07*fillScore +
		0.05*centerScore
	if !strict {
		score *= 0.88
	}
	return score
}
