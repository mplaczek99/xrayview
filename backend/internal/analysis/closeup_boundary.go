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

	enhanced := claheGray(normalized, int(width), int(height), 48, 2.2)
	defer bufpool.PutUint8(enhanced)

	blurred := gaussianBlurGrayFast(enhanced, width, height, 1.2)
	defer bufpool.PutUint8(blurred)

	smallBlur, largeBlur := dualGaussianBlurGray(blurred, width, height, 1.5, 9.0)
	defer bufpool.PutUint8(smallBlur)
	defer bufpool.PutUint8(largeBlur)

	gradient := gradientMagnitudeGray(blurred, width, height)
	defer bufpool.PutUint8(gradient)

	toothness, darkness := buildCloseupToothMaps(blurred, smallBlur, largeBlur, width, height, search)
	borderPad := clampInt(int(minUint32(width, height)/80), 8, 15)
	suppressCloseupFrame(toothness, darkness, width, height, borderPad)

	crownGapBand := searchRegion{
		x:      search.x,
		y:      search.y + uint32(math.Round(float64(search.height)*0.56)),
		width:  search.width,
		height: maxUint32(uint32(math.Round(float64(search.height)*0.18)), 1),
	}
	crownGapBand = clampSearchRegionToSearch(crownGapBand, search)
	gapProfile := smoothFloatProfile(buildCloseupColumnProfile(darkness, width, crownGapBand, search), 9)
	if len(gapProfile) == 0 {
		return nil, nil, false
	}

	searchMidX := search.x + search.width/2
	centerWindowHalf := maxUint32(search.width/10, 36)
	gapX := pickCloseupProfilePeak(
		gapProfile,
		search,
		maxUint32(saturatingSubUint32(searchMidX, centerWindowHalf), search.x),
		minUint32(searchMidX+centerWindowHalf, search.x+search.width-1),
		searchMidX,
		1.5,
	)

	seedBand := searchRegion{
		x:      search.x,
		y:      search.y + uint32(math.Round(float64(search.height)*0.60)),
		width:  search.width,
		height: maxUint32(uint32(math.Round(float64(search.height)*0.26)), 1),
	}
	seedBand = clampSearchRegionToSearch(seedBand, search)
	toothProfile := smoothFloatProfile(buildCloseupColumnProfile(toothness, width, seedBand, search), 7)
	if len(toothProfile) == 0 {
		return nil, nil, false
	}

	expectedHalfWidth := clampInt(int(search.width/8), 60, 110)
	leftTargetX := maxUint32(search.x, saturatingSubUint32(gapX, uint32(expectedHalfWidth)))
	rightTargetX := minUint32(gapX+uint32(expectedHalfWidth), search.x+search.width-1)
	leftSeedX := pickCloseupProfilePeak(
		toothProfile,
		search,
		maxUint32(search.x, saturatingSubUint32(leftTargetX, 30)),
		minUint32(leftTargetX+30, saturatingSubUint32(gapX, 35)),
		leftTargetX,
		0.7,
	)
	rightSeedX := pickCloseupProfilePeak(
		toothProfile,
		search,
		maxUint32(gapX+35, saturatingSubUint32(rightTargetX, 30)),
		minUint32(rightTargetX+30, search.x+search.width-1),
		rightTargetX,
		0.7,
	)
	if leftSeedX >= gapX || rightSeedX <= gapX {
		return nil, nil, false
	}

	leftSeedY, ok := pickCloseupSeedY(toothness, darkness, width, seedBand, leftSeedX)
	if !ok {
		return nil, nil, false
	}
	rightSeedY, ok := pickCloseupSeedY(toothness, darkness, width, seedBand, rightSeedX)
	if !ok {
		return nil, nil, false
	}

	candidates := make([]componentCandidate, 0, 2)
	geometries := make([]contracts.ToothGeometry, 0, 2)
	if candidate, geometry, ok := detectCloseupSeedCandidate(
		normalized,
		toothness,
		darkness,
		gradient,
		width,
		height,
		search,
		leftSeedX,
		leftSeedY,
		gapX,
		false,
		borderPad,
	); ok {
		candidates = append(candidates, candidate)
		geometries = append(geometries, geometry)
	}
	if candidate, geometry, ok := detectCloseupSeedCandidate(
		normalized,
		toothness,
		darkness,
		gradient,
		width,
		height,
		search,
		rightSeedX,
		rightSeedY,
		gapX,
		true,
		borderPad,
	); ok {
		candidates = append(candidates, candidate)
		geometries = append(geometries, geometry)
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

func buildCloseupToothMaps(
	blurred []uint8,
	smallBlur []uint8,
	largeBlur []uint8,
	width, height uint32,
	search searchRegion,
) ([]uint8, []uint8) {
	toothness := make([]uint8, len(blurred))
	darkness := make([]uint8, len(blurred))

	rawToothness := make([]float64, len(blurred))
	maxToothness := 1.0
	integral := buildIntegralImage(blurred, width, search)

	for y := search.y; y < search.y+search.height; y++ {
		for x := search.x; x < search.x+search.width; x++ {
			index := int(y*width + x)
			band := maxInt(int(smallBlur[index])-int(largeBlur[index]), 0)
			localMean := meanInIntegralBox(integral, search, x, y, 5, 22)
			dark := maxInt(localMean-int(blurred[index]), 0)
			score := float64(band)*1.2 + float64(blurred[index])*0.8 - float64(dark)*1.5
			if score < 0 {
				score = 0
			}
			rawToothness[index] = score
			darkness[index] = clampUint8FromInt(dark * 3)
			if score > maxToothness {
				maxToothness = score
			}
		}
	}

	for y := search.y; y < search.y+search.height; y++ {
		for x := search.x; x < search.x+search.width; x++ {
			index := int(y*width + x)
			toothness[index] = uint8(math.Round(rawToothness[index] * 255.0 / maxToothness))
		}
	}

	return toothness, darkness
}

func suppressCloseupFrame(
	toothness []uint8,
	darkness []uint8,
	width, height uint32,
	borderPad int,
) {
	if borderPad <= 0 {
		return
	}
	heightInt := int(height)
	widthInt := int(width)
	for y := 0; y < heightInt; y++ {
		for x := 0; x < widthInt; x++ {
			if x >= borderPad && x < widthInt-borderPad && y >= borderPad && y < heightInt-borderPad {
				continue
			}
			index := y*widthInt + x
			toothness[index] = 0
			darkness[index] = 255
		}
	}
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

func pickCloseupSeedY(
	toothness []uint8,
	darkness []uint8,
	width uint32,
	band searchRegion,
	centerX uint32,
) (uint32, bool) {
	if band.height == 0 {
		return 0, false
	}

	windowHalf := uint32(8)
	bestY := band.y
	bestScore := -1.0
	for y := band.y; y < band.y+band.height; y++ {
		sum := 0.0
		count := 0.0
		for yy := maxUint32(y-4, band.y); yy <= minUint32(y+4, band.y+band.height-1); yy++ {
			for xx := maxUint32(centerX-windowHalf, band.x); xx <= minUint32(centerX+windowHalf, band.x+band.width-1); xx++ {
				index := int(yy*width + xx)
				sum += float64(toothness[index]) - float64(darkness[index])*0.9
				count++
			}
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

	if bestScore < 0 {
		return 0, false
	}
	return bestY, true
}

func detectCloseupSeedCandidate(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width, height uint32,
	search searchRegion,
	seedX, seedY, gapX uint32,
	isRight bool,
	borderPad int,
) (componentCandidate, contracts.ToothGeometry, bool) {
	maxHalfWidth := maxUint32(search.width/7, 110)
	minGrowX := search.x
	maxGrowX := search.x + search.width - 1
	if isRight {
		minGrowX = gapX + 8
		maxGrowX = minUint32(seedX+maxHalfWidth, maxGrowX)
	} else {
		minGrowX = maxUint32(search.x, saturatingSubUint32(seedX, maxHalfWidth))
		maxGrowX = saturatingSubUint32(gapX, 8)
	}
	if maxGrowX <= minGrowX {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}

	growRegion := searchRegion{
		x:      minGrowX,
		y:      search.y,
		width:  maxUint32(saturatingSubUint32(maxGrowX, minGrowX)+1, 1),
		height: search.height,
	}
	seedIndex := int(seedY*width + seedX)
	seedToothness := toothness[seedIndex]
	seedDarkness := darkness[seedIndex]
	if seedToothness < 90 || seedDarkness > 150 {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}

	growThreshold := maxUint8(
		percentileInRegion(toothness, width, growRegion, 0.45),
		clampUint8FromInt(int(seedToothness)-70),
	)
	if growThreshold < 60 {
		growThreshold = 60
	}
	darkThreshold := maxUint8(percentileInRegion(darkness, width, growRegion, 0.90), 90)
	if darkThreshold > 150 {
		darkThreshold = 150
	}
	gradientThreshold := maxUint8(percentileInRegion(gradient, width, growRegion, 0.94), 90)

	mask := growCloseupSeedMask(
		toothness,
		darkness,
		gradient,
		width,
		height,
		growRegion,
		seedX,
		seedY,
		growThreshold,
		darkThreshold,
		gradientThreshold,
		borderPad,
		seedToothness,
	)
	if countMaskPixels(mask) == 0 {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}

	mask = closeBinaryMask(mask, int(width), int(height))
	mask = closeBinaryMask(mask, int(width), int(height))
	mask = fillHolesBinaryMask(mask, int(width), int(height))
	mask = openBinaryMask(mask, int(width), int(height))

	components := collectMaskComponents(mask, width, height, growRegion, maxUint32(growRegion.area()/90, 3500))
	if len(components) == 0 {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}

	seedPixelIndex := int(seedY*width + seedX)
	var selected *maskComponent
	for index := range components {
		if containsMaskPixel(components[index].pixels, seedPixelIndex) {
			selected = &components[index]
			break
		}
	}
	if selected == nil {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}
	if componentTouchesImageBorder(selected.bbox, width, height, borderPad) {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}

	geometry := geometryFromPixels(selected.pixels, selected.bbox, width)
	if len(geometry.Outline) < 12 {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}

	var intensitySum uint64
	var toothnessSum uint64
	var darknessSum uint64
	for _, index := range selected.pixels {
		intensitySum += uint64(normalized[index])
		toothnessSum += uint64(toothness[index])
		darknessSum += uint64(darkness[index])
	}
	meanIntensity := float64(intensitySum) / float64(maxUint32(selected.area, 1))
	meanToothness := float64(toothnessSum) / float64(maxUint32(selected.area, 1))
	meanDarkness := float64(darknessSum) / float64(maxUint32(selected.area, 1))
	fillRatio := float64(selected.area) / float64(maxUint32(selected.bbox.Width*selected.bbox.Height, 1))
	aspectRatio := float64(selected.bbox.Height) / float64(maxUint32(selected.bbox.Width, 1))
	widthRatio := float64(selected.bbox.Width) / float64(maxUint32(search.width, 1))
	heightRatio := float64(selected.bbox.Height) / float64(maxUint32(search.height, 1))
	centerX := float64(selected.bbox.X) + float64(selected.bbox.Width)/2.0
	targetCenterX := float64(seedX)
	if isRight {
		targetCenterX = math.Max(targetCenterX, float64(gapX)+float64(selected.bbox.Width)*0.35)
	} else {
		targetCenterX = math.Min(targetCenterX, float64(gapX)-float64(selected.bbox.Width)*0.35)
	}
	centerScore := 1.0 - math.Min(math.Abs(centerX-targetCenterX)/(float64(search.width)*0.18), 1.0)

	strict := widthRatio >= 0.14 &&
		widthRatio <= 0.34 &&
		heightRatio >= 0.62 &&
		heightRatio <= 0.98 &&
		aspectRatio >= 2.1 &&
		aspectRatio <= 6.0 &&
		fillRatio >= 0.45 &&
		fillRatio <= 0.92 &&
		meanDarkness <= 110 &&
		meanToothness >= 120

	score := scoreCloseupCandidate(
		meanIntensity,
		meanToothness,
		meanDarkness,
		widthRatio,
		heightRatio,
		aspectRatio,
		fillRatio,
		centerScore,
		strict,
	)
	if selected.area < 9000 ||
		geometry.BoundingBox.Height < search.height/2 ||
		widthRatio > 0.36 ||
		score < 0.34 {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}

	return componentCandidate{
		pixels: selected.pixels,
		bbox:   selected.bbox,
		area:   selected.area,
		score:  score,
		strict: strict,
	}, geometry, true
}

func growCloseupSeedMask(
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width, height uint32,
	region searchRegion,
	seedX, seedY uint32,
	growThreshold uint8,
	darkThreshold uint8,
	gradientThreshold uint8,
	borderPad int,
	seedToothness uint8,
) []uint8 {
	mask := make([]uint8, len(toothness))
	visited := make([]bool, len(toothness))
	queue := make([]int, 0, int(region.area()/8))

	seedIndex := int(seedY*width + seedX)
	queue = append(queue, seedIndex)
	visited[seedIndex] = true
	widthInt := int(width)
	heightInt := int(height)
	regionRight := region.x + region.width - 1
	regionBottom := region.y + region.height - 1

	for head := 0; head < len(queue); head++ {
		index := queue[head]
		x := uint32(index % widthInt)
		y := uint32(index / widthInt)
		if x < region.x || x > regionRight || y < region.y || y > regionBottom {
			continue
		}
		if x < uint32(borderPad) || x+uint32(borderPad) >= width || y < uint32(borderPad) || y+uint32(borderPad) >= height {
			continue
		}
		if toothness[index] < growThreshold {
			continue
		}
		if darkness[index] > darkThreshold {
			continue
		}
		if gradient[index] > gradientThreshold && toothness[index] < clampUint8FromInt(int(seedToothness)-18) {
			continue
		}

		mask[index] = 1
		for ny := maxInt(int(y)-1, 0); ny <= minInt(int(y)+1, heightInt-1); ny++ {
			for nx := maxInt(int(x)-1, 0); nx <= minInt(int(x)+1, widthInt-1); nx++ {
				neighbor := ny*widthInt + nx
				if visited[neighbor] {
					continue
				}
				visited[neighbor] = true
				queue = append(queue, neighbor)
			}
		}
	}

	return mask
}

func containsMaskPixel(pixels []int, target int) bool {
	for _, index := range pixels {
		if index == target {
			return true
		}
	}
	return false
}

func componentTouchesImageBorder(bbox contracts.BoundingBox, width, height uint32, borderPad int) bool {
	pad := uint32(maxInt(borderPad, 0))
	return bbox.X <= pad ||
		bbox.Y <= pad ||
		bbox.X+bbox.Width >= width-pad ||
		bbox.Y+bbox.Height >= height-pad
}

func scoreCloseupCandidate(
	meanIntensity float64,
	meanToothness float64,
	meanDarkness float64,
	widthRatio float64,
	heightRatio float64,
	aspectRatio float64,
	fillRatio float64,
	centerScore float64,
	strict bool,
) float64 {
	brightnessScore := clamp01((meanIntensity - 60.0) / 120.0)
	toothnessScore := clamp01((meanToothness - 100.0) / 110.0)
	darkPenalty := clamp01((meanDarkness - 70.0) / 80.0)
	widthScore := 1.0 - math.Min(math.Abs(widthRatio-0.22)/0.14, 1.0)
	heightScore := clamp01((heightRatio - 0.60) / 0.32)
	aspectScore := 1.0 - math.Min(math.Abs(aspectRatio-3.7)/2.3, 1.0)
	fillScore := 1.0 - math.Min(math.Abs(fillRatio-0.66)/0.28, 1.0)

	score := 0.22*brightnessScore +
		0.24*toothnessScore +
		0.15*widthScore +
		0.14*heightScore +
		0.08*aspectScore +
		0.08*fillScore +
		0.09*centerScore -
		0.18*darkPenalty
	if !strict {
		score *= 0.9
	}
	return clamp01(score)
}
