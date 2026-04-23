package analysis

import (
	"math"
	"sort"

	"xrayview/backend/internal/bufpool"
	"xrayview/backend/internal/contracts"
)

type closeupSeparator struct {
	component maskComponent
	cx        uint32
}

type closeupBand struct {
	region  searchRegion
	partial bool
}

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

	wallBand := searchRegion{
		x:      search.x,
		y:      search.y + uint32(math.Round(float64(search.height)*0.50)),
		width:  search.width,
		height: maxUint32(uint32(math.Round(float64(search.height)*0.32)), 1),
	}
	wallBand = clampSearchRegionToSearch(wallBand, search)
	wallProfile := smoothFloatProfile(buildCloseupColumnProfile(darkness, width, wallBand, search), 11)
	if len(wallProfile) == 0 {
		return nil, nil, false
	}

	expectedWallOffset := clampInt(int(search.width/5), 120, 190)
	leftWallX := uint32(0)
	rightWallX := uint32(0)
	separators := detectCloseupSeparators(darkness, width, height, wallBand, borderPad)
	if centerIndex, ok := pickCloseupCentralSeparator(separators, gapX, searchMidX); ok &&
		centerIndex > 0 &&
		centerIndex < len(separators)-1 {
		leftWallX = separators[centerIndex-1].cx
		gapX = separators[centerIndex].cx
		rightWallX = separators[centerIndex+1].cx
	} else {
		leftWallX = pickCloseupProfilePeak(
			wallProfile,
			search,
			maxUint32(search.x, saturatingSubUint32(gapX, uint32(expectedWallOffset+90))),
			maxUint32(search.x, saturatingSubUint32(gapX, 70)),
			maxUint32(search.x, saturatingSubUint32(gapX, uint32(expectedWallOffset))),
			0.65,
		)
		rightWallX = pickCloseupProfilePeak(
			wallProfile,
			search,
			minUint32(gapX+70, search.x+search.width-1),
			minUint32(gapX+uint32(expectedWallOffset+90), search.x+search.width-1),
			minUint32(gapX+uint32(expectedWallOffset), search.x+search.width-1),
			0.65,
		)
	}
	if gapX <= leftWallX+70 || rightWallX <= gapX+70 {
		return nil, nil, false
	}

	walls := buildCloseupWallSet(search, borderPad, leftWallX, gapX, rightWallX)
	bands := buildCloseupBandsFromWalls(walls, search)
	if len(bands) == 0 {
		return nil, nil, false
	}

	candidates := make([]componentCandidate, 0, len(bands))
	geometries := make([]contracts.ToothGeometry, 0, len(bands))
	for _, band := range bands {
		if candidate, geometry, ok := detectCloseupBandCandidate(
			normalized,
			toothness,
			darkness,
			gradient,
			width,
			height,
			search,
			band,
			borderPad,
		); ok {
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
			relativeY := 0.0
			if search.height > 1 {
				relativeY = float64(y-search.y) / float64(search.height-1)
			}
			topPenalty := clamp01((0.34 - relativeY) / 0.34)
			bottomPenalty := clamp01((relativeY - 0.94) / 0.06)
			score := float64(band)*1.15 +
				float64(blurred[index])*0.92 -
				float64(dark)*1.65 -
				topPenalty*36.0 -
				bottomPenalty*38.0
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

func detectCloseupSeparators(
	darkness []uint8,
	width, height uint32,
	search searchRegion,
	borderPad int,
) []closeupSeparator {
	mask := make([]uint8, len(darkness))
	threshold := maxUint8(percentileInRegion(darkness, width, search, 0.88), 24)
	if threshold > 72 {
		threshold = 72
	}

	for y := search.y; y < search.y+search.height; y++ {
		for x := search.x; x < search.x+search.width; x++ {
			index := int(y*width + x)
			if darkness[index] >= threshold {
				mask[index] = 1
			}
		}
	}

	mask = closeBinaryMask(mask, int(width), int(height))
	mask = openBinaryMask(mask, int(width), int(height))

	components := collectMaskComponents(mask, width, height, search, 30)
	separators := make([]closeupSeparator, 0, len(components))
	for _, component := range components {
		if componentTouchesImageBorder(component.bbox, width, height, borderPad) {
			continue
		}
		bbox := component.bbox
		if bbox.Height < 50 {
			continue
		}
		if bbox.Width > 28 || bbox.Width == 0 {
			continue
		}
		if float64(bbox.Height)/float64(bbox.Width) < 2.6 {
			continue
		}
		separators = append(separators, closeupSeparator{
			component: component,
			cx:        bbox.X + bbox.Width/2,
		})
	}

	sort.Slice(separators, func(i, j int) bool {
		return separators[i].cx < separators[j].cx
	})
	return separators
}

func pickCloseupCentralSeparator(
	separators []closeupSeparator,
	gapX, searchMidX uint32,
) (int, bool) {
	if len(separators) == 0 {
		return 0, false
	}

	bestIndex := -1
	bestScore := math.Inf(-1)
	for index, separator := range separators {
		bbox := separator.component.bbox
		score := float64(bbox.Height)*1.2 -
			float64(absInt(int(separator.cx)-int(gapX)))*2.2 -
			float64(absInt(int(separator.cx)-int(searchMidX)))*0.4 -
			float64(bbox.Width)*6.0
		if score > bestScore {
			bestScore = score
			bestIndex = index
		}
	}
	return bestIndex, bestIndex >= 0
}

func buildCloseupBandRegion(
	leftSeparator closeupSeparator,
	rightSeparator closeupSeparator,
	search searchRegion,
) (searchRegion, bool) {
	leftX := leftSeparator.component.bbox.X + leftSeparator.component.bbox.Width + 4
	rightX := saturatingSubUint32(rightSeparator.component.bbox.X, 4)
	if rightX <= leftX {
		return searchRegion{}, false
	}

	region := searchRegion{
		x:      leftX,
		y:      search.y + 6,
		width:  rightX - leftX + 1,
		height: saturatingSubUint32(search.height, 12),
	}
	region = clampSearchRegionToSearch(region, search)
	if region.width < 70 || region.width > search.width/2 || region.height < search.height/2 {
		return searchRegion{}, false
	}
	return region, true
}

func buildCloseupBandRegionFromWalls(
	leftWallX uint32,
	rightWallX uint32,
	search searchRegion,
) (searchRegion, bool) {
	if rightWallX <= leftWallX+8 {
		return searchRegion{}, false
	}

	leftX := leftWallX + 6
	rightX := saturatingSubUint32(rightWallX, 6)
	if rightX <= leftX {
		return searchRegion{}, false
	}

	topInset := uint32(math.Round(float64(search.height) * 0.10))
	bottomInset := uint32(math.Round(float64(search.height) * 0.04))
	region := searchRegion{
		x:      leftX,
		y:      search.y + topInset,
		width:  rightX - leftX + 1,
		height: maxUint32(saturatingSubUint32(search.height, topInset+bottomInset), 1),
	}
	region = clampSearchRegionToSearch(region, search)
	if region.width < 55 || region.width > search.width/2 || region.height < search.height/2 {
		return searchRegion{}, false
	}
	return region, true
}

func buildCloseupWallSet(
	search searchRegion,
	borderPad int,
	leftWallX uint32,
	gapX uint32,
	rightWallX uint32,
) []uint32 {
	leftEdge := search.x + uint32(borderPad) + 6
	rightEdge := saturatingSubUint32(search.x+search.width-1, uint32(borderPad+6))
	walls := []uint32{leftEdge, leftWallX, gapX, rightWallX, rightEdge}
	sort.Slice(walls, func(i, j int) bool { return walls[i] < walls[j] })

	compacted := make([]uint32, 0, len(walls))
	for _, wall := range walls {
		if len(compacted) == 0 || wall > compacted[len(compacted)-1]+18 {
			compacted = append(compacted, wall)
			continue
		}
		compacted[len(compacted)-1] = (compacted[len(compacted)-1] + wall) / 2
	}
	return compacted
}

func buildCloseupBandsFromWalls(
	walls []uint32,
	search searchRegion,
) []closeupBand {
	if len(walls) < 2 {
		return nil
	}

	bands := make([]closeupBand, 0, len(walls)-1)
	for index := 0; index < len(walls)-1; index++ {
		region, ok := buildCloseupBandRegionFromWalls(walls[index], walls[index+1], search)
		if !ok {
			continue
		}
		bands = append(bands, closeupBand{
			region:  region,
			partial: index == 0 || index == len(walls)-2,
		})
	}
	return bands
}

func insetCloseupGrowRegion(region searchRegion) (searchRegion, bool) {
	xInset := uint32(clampInt(int(region.width/14), 6, 14))
	topInset := uint32(clampInt(int(region.height/18), 14, 28))
	bottomInset := uint32(clampInt(int(region.height/32), 8, 18))
	if region.width <= xInset*2+24 || region.height <= topInset+bottomInset+40 {
		return searchRegion{}, false
	}

	return searchRegion{
		x:      region.x + xInset,
		y:      region.y + topInset,
		width:  region.width - xInset*2,
		height: region.height - topInset - bottomInset,
	}, true
}

func detectCloseupBandCandidate(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width, height uint32,
	search searchRegion,
	band closeupBand,
	borderPad int,
) (componentCandidate, contracts.ToothGeometry, bool) {
	outerDirection := 0
	if band.partial {
		if band.region.x+band.region.width/2 > search.x+search.width/2 {
			outerDirection = 1
		} else {
			outerDirection = -1
		}
	}
	crownRegion, ok := buildCloseupCrownRegion(search, band.region)
	if !ok {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}
	seedX, seedY, ok := pickCloseupCrownSeed(normalized, toothness, darkness, gradient, width, search, crownRegion)
	if !ok {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}

	seedIndex := int(seedY*width + seedX)
	seedIntensity := normalized[seedIndex]
	seedToothness := toothness[seedIndex]
	if seedToothness < 72 {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}

	crownToothThreshold := maxUint8(
		percentileInRegion(toothness, width, crownRegion, 0.28),
		clampUint8FromInt(int(seedToothness)-52),
	)
	if crownToothThreshold < 58 {
		crownToothThreshold = 58
	}
	crownIntensityThreshold := maxUint8(
		percentileInRegion(normalized, width, crownRegion, 0.20),
		clampUint8FromInt(int(seedIntensity)-40),
	)
	if crownIntensityThreshold < 42 {
		crownIntensityThreshold = 42
	}
	crownDarkThreshold := maxUint8(percentileInRegion(darkness, width, crownRegion, 0.92), 72)
	if crownDarkThreshold > 148 {
		crownDarkThreshold = 148
	}
	crownGradientThreshold := maxUint8(percentileInRegion(gradient, width, crownRegion, 0.97), 72)
	if crownGradientThreshold > 164 {
		crownGradientThreshold = 164
	}

	crownMask := buildCloseupCrownMask(
		normalized,
		toothness,
		darkness,
		gradient,
		width,
		height,
		crownRegion,
		seedX,
		seedY,
		crownIntensityThreshold,
		crownToothThreshold,
		crownDarkThreshold,
		crownGradientThreshold,
		seedToothness,
	)
	seededCrownMask := growCloseupSeedMask(
		normalized,
		toothness,
		darkness,
		gradient,
		width,
		height,
		crownRegion,
		seedX,
		seedY,
		crownIntensityThreshold,
		crownToothThreshold,
		crownDarkThreshold,
		crownGradientThreshold,
		borderPad,
		seedToothness,
	)
	crownMask = orMasks(crownMask, seededCrownMask)
	crownMask = closeBinaryMask(crownMask, int(width), int(height))
	crownMask = closeBinaryMask(crownMask, int(width), int(height))
	crownMask = fillHolesBinaryMask(crownMask, int(width), int(height))

	crownComponent, ok := extractCloseupSeedComponent(
		crownMask,
		width,
		height,
		crownRegion,
		seedX,
		seedY,
		maxUint32(crownRegion.area()/220, 220),
	)
	if !ok {
		crownComponent, ok = extractCloseupBestComponent(
			crownMask,
			width,
			height,
			crownRegion,
			seedX,
			seedY,
			maxUint32(crownRegion.area()/260, 120),
		)
		if !ok {
			return componentCandidate{}, contracts.ToothGeometry{}, false
		}
	}
	if !isPlausibleCloseupCrown(crownComponent, crownRegion, band.partial) {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}

	extendRegion, ok := insetCloseupGrowRegion(band.region)
	if !ok {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}
	fullMask := traceCloseupBandContourMask(
		normalized,
		toothness,
		darkness,
		gradient,
		width,
		height,
		extendRegion,
		crownComponent,
		crownRegion,
		seedX,
		seedY,
		borderPad,
		band.partial,
		outerDirection,
	)
	if countMaskPixels(fullMask) == 0 {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}

	selected, ok := extractCloseupSeedComponent(
		fullMask,
		width,
		height,
		band.region,
		seedX,
		seedY,
		maxUint32(band.region.area()/240, 240),
	)
	if !ok {
		selected, ok = extractCloseupBestComponent(
			fullMask,
			width,
			height,
			band.region,
			seedX,
			seedY,
			maxUint32(band.region.area()/260, 180),
		)
	}
	if !ok {
		selected = crownComponent
	}
	if componentTouchesImageBorder(selected.bbox, width, height, borderPad) && !band.partial {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}
	selected = refineCloseupSelectedComponent(
		normalized,
		toothness,
		darkness,
		gradient,
		width,
		height,
		band.region,
		crownRegion,
		selected,
		seedX,
		seedY,
		band.partial,
		outerDirection,
	)

	geometry := geometryFromPixels(selected.pixels, selected.bbox, width)
	geometry = smoothCloseupToothGeometry(geometry, band.partial)
	if len(geometry.Outline) < 12 {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}

	shape := evaluateCloseupToothShape(selected.pixels, width, selected.bbox, crownRegion, band.partial)
	if !shape.ok && selected.bbox != crownComponent.bbox {
		selected = crownComponent
		geometry = geometryFromPixels(selected.pixels, selected.bbox, width)
		geometry = smoothCloseupToothGeometry(geometry, band.partial)
		if len(geometry.Outline) < 12 {
			return componentCandidate{}, contracts.ToothGeometry{}, false
		}
		shape = evaluateCloseupToothShape(selected.pixels, width, selected.bbox, crownRegion, band.partial)
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
	bandOccupancy := float64(selected.bbox.Width) / float64(maxUint32(band.region.width, 1))
	centerX := float64(selected.bbox.X) + float64(selected.bbox.Width)/2.0
	bandCenterX := float64(band.region.x) + float64(band.region.width)/2.0
	centerScore := 1.0 - math.Min(math.Abs(centerX-bandCenterX)/(float64(band.region.width)*0.60), 1.0)

	minWidthRatio := 0.10
	minHeightRatio := 0.20
	minBandOccupancy := 0.30
	maxAspectRatio := 7.8
	minArea := maxUint32(band.region.area()/52, 1500)
	minBBoxWidth := uint32(52)
	minBBoxHeight := search.height / 5
	minBodyRatio := 1.24
	if band.partial {
		minWidthRatio = 0.04
		minHeightRatio = 0.08
		minBandOccupancy = 0.10
		maxAspectRatio = 9.5
		minArea = maxUint32(band.region.area()/120, 280)
		minBBoxWidth = 18
		minBBoxHeight = maxUint32(search.height/3, 320)
		minBodyRatio = 1.08
	} else if band.region.x+band.region.width/2 > search.x+search.width/2 {
		minBodyRatio = 1.34
	}

	strict := widthRatio >= minWidthRatio &&
		widthRatio <= 0.34 &&
		heightRatio >= minHeightRatio &&
		heightRatio <= 0.98 &&
		aspectRatio >= 1.1 &&
		aspectRatio <= maxAspectRatio &&
		fillRatio >= 0.28 &&
		fillRatio <= 0.96 &&
		bandOccupancy >= minBandOccupancy &&
		bandOccupancy <= 0.96 &&
		meanDarkness <= 112 &&
		meanToothness >= 92 &&
		shape.taperScore >= 0.05 &&
		shape.bodyRatio >= minBodyRatio

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
	score = clamp01(score + shape.taperScore*0.18 + shape.crownCoverage*0.12 + clamp01(shape.bodyRatio-1.0)*0.08)
	if band.partial {
		score *= 0.72
	}
	if selected.area < minArea ||
		geometry.BoundingBox.Height < minBBoxHeight ||
		geometry.BoundingBox.Width < minBBoxWidth ||
		bandOccupancy < minBandOccupancy-0.03 ||
		bandOccupancy > 0.98 ||
		shape.bodyRatio < minBodyRatio ||
		score < 0.22 {
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

type closeupShapeMetrics struct {
	ok            bool
	bodyRatio     float64
	taperScore    float64
	crownCoverage float64
}

func buildCloseupCrownRegion(search searchRegion, bandRegion searchRegion) (searchRegion, bool) {
	top := search.y + uint32(math.Round(float64(search.height)*0.55))
	bottom := search.y + uint32(math.Round(float64(search.height)*0.92))
	if bottom <= top {
		return searchRegion{}, false
	}
	region := searchRegion{
		x:      bandRegion.x,
		y:      maxUint32(bandRegion.y, top),
		width:  bandRegion.width,
		height: minUint32(bandRegion.y+bandRegion.height-1, bottom) - maxUint32(bandRegion.y, top) + 1,
	}
	if region.width < 18 || region.height < 30 {
		return searchRegion{}, false
	}
	return region, true
}

func buildCloseupCrownMask(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width, height uint32,
	crownRegion searchRegion,
	seedX, seedY uint32,
	intensityThreshold uint8,
	toothThreshold uint8,
	darkThreshold uint8,
	gradientThreshold uint8,
	seedToothness uint8,
) []uint8 {
	crownness := make([]uint8, len(normalized))
	for y := crownRegion.y; y < crownRegion.y+crownRegion.height; y++ {
		rowStart := int(y * width)
		for x := crownRegion.x; x < crownRegion.x+crownRegion.width; x++ {
			index := rowStart + int(x)
			score := int(normalized[index])*7 +
				int(toothness[index])*4 -
				int(darkness[index])*5 -
				int(gradient[index])*2 +
				160
			crownness[index] = clampUint8FromInt(score / 4)
		}
	}

	crownScoreThreshold := maxUint8(
		percentileInRegion(crownness, width, crownRegion, 0.30),
		clampUint8FromInt(int(crownness[int(seedY*width+seedX)])-34),
	)
	if crownScoreThreshold < 74 {
		crownScoreThreshold = 74
	}

	mask := make([]uint8, len(normalized))
	for y := crownRegion.y; y < crownRegion.y+crownRegion.height; y++ {
		rowStart := int(y * width)
		for x := crownRegion.x; x < crownRegion.x+crownRegion.width; x++ {
			index := rowStart + int(x)
			if crownness[index] < crownScoreThreshold {
				continue
			}
			if normalized[index] < intensityThreshold {
				continue
			}
			if toothness[index] < toothThreshold {
				continue
			}
			if darkness[index] > darkThreshold {
				continue
			}
			if gradient[index] > gradientThreshold && toothness[index] < clampUint8FromInt(int(seedToothness)-10) {
				continue
			}
			mask[index] = 1
		}
	}

	mask = closeBinaryMask(mask, int(width), int(height))
	mask = closeBinaryMask(mask, int(width), int(height))
	mask = fillHolesBinaryMask(mask, int(width), int(height))
	return mask
}

func pickCloseupCrownSeed(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width uint32,
	search searchRegion,
	crownRegion searchRegion,
) (uint32, uint32, bool) {
	seedTop := search.y + uint32(math.Round(float64(search.height)*0.70))
	seedBottom := search.y + uint32(math.Round(float64(search.height)*0.84))
	seedBand := searchRegion{
		x:      crownRegion.x + minUint32(crownRegion.width/10, 10),
		y:      maxUint32(crownRegion.y, seedTop),
		width:  saturatingSubUint32(crownRegion.width, minUint32(crownRegion.width/5, 20)),
		height: minUint32(crownRegion.y+crownRegion.height-1, seedBottom) - maxUint32(crownRegion.y, seedTop) + 1,
	}
	if seedBand.width < 10 || seedBand.height < 10 {
		seedBand = crownRegion
	}

	bandCenterX := float64(crownRegion.x) + float64(crownRegion.width)/2.0
	bestScore := math.Inf(-1)
	var bestX uint32
	var bestY uint32
	for y := seedBand.y; y < seedBand.y+seedBand.height; y++ {
		for x := seedBand.x; x < seedBand.x+seedBand.width; x++ {
			index := int(y*width + x)
			score := float64(normalized[index])*1.25 +
				float64(toothness[index])*0.95 -
				float64(darkness[index])*1.10 -
				float64(gradient[index])*0.35 -
				math.Abs(float64(x)-bandCenterX)*0.55
			if score > bestScore {
				bestScore = score
				bestX = x
				bestY = y
			}
		}
	}
	if bestScore < 40 {
		return 0, 0, false
	}
	return bestX, bestY, true
}

func extractCloseupSeedComponent(
	mask []uint8,
	width, height uint32,
	region searchRegion,
	seedX, seedY uint32,
	minArea uint32,
) (maskComponent, bool) {
	components := collectMaskComponents(mask, width, height, region, minArea)
	if len(components) == 0 {
		return maskComponent{}, false
	}
	seedPixelIndex := int(seedY*width + seedX)
	for _, component := range components {
		if containsMaskPixel(component.pixels, seedPixelIndex) {
			return component, true
		}
	}
	return maskComponent{}, false
}

func isPlausibleCloseupCrown(
	component maskComponent,
	crownRegion searchRegion,
	partial bool,
) bool {
	widthRatio := float64(component.bbox.Width) / float64(maxUint32(crownRegion.width, 1))
	heightRatio := float64(component.bbox.Height) / float64(maxUint32(crownRegion.height, 1))
	fillRatio := float64(component.area) / float64(maxUint32(component.bbox.Width*component.bbox.Height, 1))
	minWidthRatio := 0.16
	minArea := maxUint32(crownRegion.area()/44, 220)
	if partial {
		minWidthRatio = 0.06
		minArea = maxUint32(crownRegion.area()/90, 60)
	}
	return component.area >= minArea &&
		widthRatio >= minWidthRatio &&
		heightRatio >= 0.10 &&
		fillRatio >= 0.14
}

func growCloseupMaskFromPixels(
	seedPixels []int,
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width, height uint32,
	region searchRegion,
	intensityThreshold uint8,
	growThreshold uint8,
	darkThreshold uint8,
	gradientThreshold uint8,
	borderPad int,
	seedToothness uint8,
) []uint8 {
	mask := make([]uint8, len(toothness))
	if len(seedPixels) == 0 {
		return mask
	}

	visited := make([]bool, len(toothness))
	queue := make([]int, 0, len(seedPixels)+int(region.area()/8))
	for _, index := range seedPixels {
		if index < 0 || index >= len(toothness) || visited[index] {
			continue
		}
		visited[index] = true
		queue = append(queue, index)
	}

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
		if normalized[index] < intensityThreshold {
			continue
		}
		if toothness[index] < growThreshold {
			continue
		}
		if darkness[index] > darkThreshold {
			continue
		}
		if gradient[index] > gradientThreshold && toothness[index] < clampUint8FromInt(int(seedToothness)-20) {
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

func traceCloseupBandContourMask(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width, height uint32,
	region searchRegion,
	crownComponent maskComponent,
	crownRegion searchRegion,
	seedX, seedY uint32,
	borderPad int,
	partial bool,
	outerDirection int,
) []uint8 {
	mask := make([]uint8, len(normalized))
	if region.width < 10 || region.height < 10 || len(crownComponent.pixels) == 0 {
		return mask
	}

	crownMask := maskFromComponent(crownComponent, len(normalized))
	rowLeft, rowRight, rowSeen := buildCloseupObservedRowSpans(crownMask, width, region)
	bottomProfile := estimateCloseupBottomProfile(
		normalized,
		toothness,
		darkness,
		width,
		region,
		crownRegion,
		crownComponent,
	)
	if len(bottomProfile) != int(region.width) {
		return mask
	}
	augmentCloseupLowerCrownSpans(
		rowLeft,
		rowRight,
		rowSeen,
		bottomProfile,
		normalized,
		toothness,
		darkness,
		gradient,
		width,
		region,
		partial,
	)
	anchorY, anchorLeft, anchorRight, ok := findCloseupContourAnchorRow(
		crownComponent,
		width,
		region,
		crownRegion,
		seedY,
		rowLeft,
		rowRight,
		rowSeen,
		partial,
	)
	if !ok {
		return mask
	}

	traceLeft := make([]uint32, region.height)
	traceRight := make([]uint32, region.height)
	traceSeen := make([]bool, region.height)
	anchorIndex := anchorY - region.y
	traceLeft[anchorIndex] = anchorLeft
	traceRight[anchorIndex] = anchorRight
	traceSeen[anchorIndex] = true

	traceCloseupContourDirection(
		traceLeft,
		traceRight,
		traceSeen,
		rowLeft,
		rowRight,
		rowSeen,
		bottomProfile,
		normalized,
		toothness,
		darkness,
		gradient,
		width,
		region,
		anchorY,
		anchorLeft,
		anchorRight,
		-1,
		partial,
		outerDirection,
	)
	traceCloseupContourDirection(
		traceLeft,
		traceRight,
		traceSeen,
		rowLeft,
		rowRight,
		rowSeen,
		bottomProfile,
		normalized,
		toothness,
		darkness,
		gradient,
		width,
		region,
		anchorY,
		anchorLeft,
		anchorRight,
		1,
		partial,
		outerDirection,
	)
	smoothCloseupContourBoundaries(traceLeft, traceRight, traceSeen, region)
	if !densifyCloseupContourBoundaries(traceLeft, traceRight, traceSeen) {
		return maskFromComponent(crownComponent, len(normalized))
	}

	topRow, _, ok := closeupContourVerticalExtent(traceSeen, region)
	if !ok {
		return maskFromComponent(crownComponent, len(normalized))
	}
	topIndex := topRow - region.y
	bottomShoulderRow, bottomLeft, bottomRight, ok := findCloseupBottomShoulderRow(
		traceLeft,
		traceRight,
		traceSeen,
		region,
		partial,
		outerDirection,
	)
	if !ok {
		return maskFromComponent(crownComponent, len(normalized))
	}
	bottomArc, ok := traceCloseupBottomCrownArc(
		normalized,
		toothness,
		darkness,
		gradient,
		width,
		height,
		region,
		bottomProfile,
		bottomLeft,
		bottomRight,
		bottomShoulderRow,
		bottomShoulderRow,
		partial,
		outerDirection,
	)
	if !ok {
		return maskFromComponent(crownComponent, len(normalized))
	}
	topArc, ok := traceCloseupTopRootArc(
		normalized,
		toothness,
		darkness,
		gradient,
		width,
		height,
		region,
		traceLeft[topIndex],
		traceRight[topIndex],
		topRow,
		topRow,
		partial,
	)
	if !ok {
		return maskFromComponent(crownComponent, len(normalized))
	}
	polygon, ok := buildCloseupContourPolygon(
		traceLeft,
		traceRight,
		traceSeen,
		region,
		topRow,
		bottomShoulderRow,
		topArc,
		bottomArc,
	)
	if !ok {
		return maskFromComponent(crownComponent, len(normalized))
	}

	seedIndex := int(seedY*width + seedX)
	mask = rasterizeCloseupContourPolygon(polygon, width, height, region)
	mask[seedIndex] = 1
	mask = openBinaryMask(mask, int(width), int(height))
	mask = closeBinaryMask(mask, int(width), int(height))
	mask = fillHolesBinaryMask(mask, int(width), int(height))

	selected, ok := extractCloseupSeedComponent(
		mask,
		width,
		height,
		region,
		seedX,
		seedY,
		maxUint32(region.area()/260, 180),
	)
	if !ok {
		return maskFromComponent(crownComponent, len(normalized))
	}
	if componentTouchesImageBorder(selected.bbox, width, height, borderPad) && !partial {
		return maskFromComponent(crownComponent, len(normalized))
	}
	return maskFromComponent(selected, len(normalized))
}

func buildCloseupObservedRowSpans(
	mask []uint8,
	width uint32,
	region searchRegion,
) ([]uint32, []uint32, []bool) {
	rowLeft := make([]uint32, region.height)
	rowRight := make([]uint32, region.height)
	rowSeen := make([]bool, region.height)
	regionRight := region.x + region.width - 1
	for rowIndex := range rowLeft {
		rowLeft[rowIndex] = regionRight
	}
	for y := region.y; y < region.y+region.height; y++ {
		rowStart := int(y * width)
		localY := y - region.y
		for x := region.x; x <= regionRight; x++ {
			if mask[rowStart+int(x)] == 0 {
				continue
			}
			rowSeen[localY] = true
			if x < rowLeft[localY] {
				rowLeft[localY] = x
			}
			if x > rowRight[localY] {
				rowRight[localY] = x
			}
		}
	}
	return rowLeft, rowRight, rowSeen
}

func augmentCloseupLowerCrownSpans(
	rowLeft []uint32,
	rowRight []uint32,
	rowSeen []bool,
	bottomProfile []uint32,
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width uint32,
	region searchRegion,
	partial bool,
) {
	bandCenterX := region.x + region.width/2
	startY := region.y + uint32(math.Round(float64(region.height)*0.60))
	endY := region.y + uint32(math.Round(float64(region.height)*0.94))
	if endY <= startY {
		return
	}

	lowerBand := searchRegion{
		x:      region.x,
		y:      startY,
		width:  region.width,
		height: endY - startY + 1,
	}
	lowerBand = clampSearchRegionToSearch(lowerBand, region)
	intensityThreshold := maxUint8(percentileInRegion(normalized, width, lowerBand, 0.18), 28)
	toothThreshold := maxUint8(percentileInRegion(toothness, width, lowerBand, 0.22), 82)
	darkThreshold := maxUint8(percentileInRegion(darkness, width, lowerBand, 0.90), 96)
	gradientThreshold := maxUint8(percentileInRegion(gradient, width, lowerBand, 0.92), 96)
	centerRadius := clampInt(int(region.width/5), 12, 40)
	if partial {
		centerRadius = clampInt(int(region.width/4), 10, 44)
	}
	for y := startY; y <= endY; y++ {
		rowIndex := y - region.y
		bestX := int(bandCenterX)
		bestScore := math.Inf(-1)
		for x := maxInt(int(region.x)+2, int(bandCenterX)-centerRadius); x <= minInt(int(region.x+region.width-3), int(bandCenterX)+centerRadius); x++ {
			index := int(y*width) + x
			if int(x-int(region.x)) < len(bottomProfile) && y > bottomProfile[x-int(region.x)] {
				continue
			}
			score := float64(toothness[index])*1.25 +
				float64(normalized[index])*0.55 -
				float64(darkness[index])*0.95 -
				float64(gradient[index])*0.30 -
				math.Abs(float64(x)-float64(bandCenterX))*0.45
			if score > bestScore {
				bestScore = score
				bestX = x
			}
		}
		if bestScore < 35 {
			continue
		}

		left := bestX
		for x := bestX - 1; x >= int(region.x)+1; x-- {
			index := int(y*width) + x
			if int(x-int(region.x)) < len(bottomProfile) && y > bottomProfile[x-int(region.x)] {
				break
			}
			if normalized[index] < intensityThreshold ||
				toothness[index] < toothThreshold ||
				darkness[index] > clampUint8FromInt(int(darkThreshold)+18) ||
				gradient[index] > clampUint8FromInt(int(gradientThreshold)+24) {
				break
			}
			left = x
		}
		right := bestX
		for x := bestX + 1; x <= int(region.x+region.width-2); x++ {
			index := int(y*width) + x
			if int(x-int(region.x)) < len(bottomProfile) && y > bottomProfile[x-int(region.x)] {
				break
			}
			if normalized[index] < intensityThreshold ||
				toothness[index] < toothThreshold ||
				darkness[index] > clampUint8FromInt(int(darkThreshold)+18) ||
				gradient[index] > clampUint8FromInt(int(gradientThreshold)+24) {
				break
			}
			right = x
		}

		spanWidth := right - left + 1
		minWidth := clampInt(int(region.width/3), 26, int(region.width)-8)
		if partial {
			minWidth = clampInt(int(region.width/5), 18, int(region.width)-8)
		}
		if spanWidth < minWidth {
			continue
		}
		if !rowSeen[rowIndex] || spanWidth > int(rowRight[rowIndex]-rowLeft[rowIndex])+4 {
			rowSeen[rowIndex] = true
			rowLeft[rowIndex] = uint32(left)
			rowRight[rowIndex] = uint32(right)
		}
	}
}

func findCloseupContourAnchorRow(
	component maskComponent,
	width uint32,
	region searchRegion,
	crownRegion searchRegion,
	seedY uint32,
	rowLeft []uint32,
	rowRight []uint32,
	rowSeen []bool,
	partial bool,
) (uint32, uint32, uint32, bool) {
	anchor, ok := findCloseupCrownTraceAnchor(component, width, region, crownRegion, partial)
	if !ok {
		return 0, 0, 0, false
	}

	bestRow := anchor.y
	bestLeft := anchor.left
	bestRight := anchor.right
	bestScore := math.Inf(-1)
	searchTop := maxUint32(crownRegion.y, component.bbox.Y)
	searchBottom := minUint32(region.y+region.height-1, component.bbox.Y+component.bbox.Height-1)
	for y := searchTop; y <= searchBottom; y++ {
		rowIndex := y - region.y
		if int(rowIndex) >= len(rowSeen) || !rowSeen[rowIndex] {
			continue
		}
		rowWidth := float64(rowRight[rowIndex] - rowLeft[rowIndex] + 1)
		lowerBias := 0.0
		if component.bbox.Height > 1 {
			lowerBias = float64(y-component.bbox.Y) / float64(component.bbox.Height-1)
		}
		widthScore := rowWidth*1.55 + lowerBias*32.0
		distancePenalty := math.Abs(float64(y)-float64(seedY)) * 0.18
		score := widthScore - distancePenalty
		if score > bestScore {
			bestScore = score
			bestRow = y
			bestLeft = rowLeft[rowIndex]
			bestRight = rowRight[rowIndex]
		}
	}
	if bestRight <= bestLeft {
		return 0, 0, 0, false
	}
	return bestRow, bestLeft, bestRight, true
}

func estimateCloseupBottomProfile(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	width uint32,
	region searchRegion,
	crownRegion searchRegion,
	component maskComponent,
) []uint32 {
	bottomProfile := make([]uint32, region.width)
	if region.width == 0 {
		return bottomProfile
	}

	bottomBand := searchRegion{
		x:      region.x,
		y:      region.y + uint32(math.Round(float64(region.height)*0.72)),
		width:  region.width,
		height: maxUint32(uint32(math.Round(float64(region.height)*0.24)), 1),
	}
	bottomBand = clampSearchRegionToSearch(bottomBand, region)
	intensityThreshold := maxUint8(percentileInRegion(normalized, width, bottomBand, 0.14), 18)
	toothThreshold := maxUint8(percentileInRegion(toothness, width, bottomBand, 0.24), 76)
	darkThreshold := maxUint8(percentileInRegion(darkness, width, bottomBand, 0.88), 84)
	searchTop := maxUint32(component.bbox.Y+component.bbox.Height/4, crownRegion.y)
	regionBottom := region.y + region.height - 1
	for x := region.x; x <= region.x+region.width-1; x++ {
		limit := regionBottom
		run := 0
		for y := int(regionBottom); y >= int(searchTop); y-- {
			index := y*int(width) + int(x)
			support := normalized[index] >= intensityThreshold ||
				(toothness[index] >= toothThreshold && darkness[index] <= clampUint8FromInt(int(darkThreshold)+24))
			if support {
				run++
				if run >= 2 {
					limit = uint32(minInt(y+1, int(regionBottom)))
					break
				}
				continue
			}
			run = 0
		}
		bottomProfile[x-region.x] = limit
	}

	smooth := make([]float64, len(bottomProfile))
	for index, value := range bottomProfile {
		smooth[index] = float64(value)
	}
	smooth = smoothFloatProfile(smooth, 8)
	for index, value := range smooth {
		bottomProfile[index] = minUint32(regionBottom, maxUint32(searchTop, uint32(math.Round(value))))
	}
	return bottomProfile
}

func traceCloseupContourDirection(
	traceLeft []uint32,
	traceRight []uint32,
	traceSeen []bool,
	rowLeft []uint32,
	rowRight []uint32,
	rowSeen []bool,
	bottomProfile []uint32,
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width uint32,
	region searchRegion,
	startY uint32,
	startLeft uint32,
	startRight uint32,
	step int,
	partial bool,
	outerDirection int,
) {
	prevLeft := startLeft
	prevRight := startRight
	prevWidth := float64(startRight - startLeft + 1)
	maxJump := clampInt(int(region.width/13), 6, 15)
	if partial {
		maxJump = clampInt(int(region.width/11), 7, 18)
	}
	if partial && outerDirection > 0 && step < 0 {
		maxJump = clampInt(int(region.width/10), 8, 20)
	}
	gapRows := 0
	scoreThreshold := 24.0
	rescueThreshold := 0.0
	maxGapRows := 10
	if step > 0 {
		maxGapRows = 5
	}
	forceContinueUntilY := int(region.y)
	if partial {
		scoreThreshold = 18.0
		rescueThreshold = 12.5
		maxGapRows = 14
		if step > 0 {
			scoreThreshold = 16.0
			rescueThreshold = 11.0
			maxGapRows = 8
		} else {
			forceContinueUntilY = int(region.y) + clampInt(int(math.Round(float64(region.height)*0.42)), 120, maxInt(int(region.height)-40, 120))
		}
	}
	if partial && outerDirection > 0 {
		if step < 0 {
			scoreThreshold = 14.0
			rescueThreshold = 8.5
			maxGapRows = 18
			forceContinueUntilY = int(region.y) + clampInt(int(math.Round(float64(region.height)*0.18)), 72, maxInt(int(region.height)-60, 72))
		} else {
			scoreThreshold = 15.0
			rescueThreshold = 10.0
			maxGapRows = 10
		}
	}
	for y := int(startY) + step; y >= int(region.y) && y < int(region.y+region.height); y += step {
		left, right, ok, score := traceCloseupContourRow(
			normalized,
			toothness,
			darkness,
			gradient,
			width,
			region,
			uint32(y),
			prevLeft,
			prevRight,
			prevWidth,
			rowLeft,
			rowRight,
			rowSeen,
			bottomProfile,
			maxJump,
			step,
			partial,
			outerDirection,
		)
		canRescue := partial &&
			step < 0 &&
			ok &&
			score >= rescueThreshold &&
			y > forceContinueUntilY
		if !ok || (score < scoreThreshold && !canRescue) {
			gapRows++
			if gapRows > maxGapRows {
				return
			}
			continue
		}
		gapRows = 0
		rowIndex := uint32(y) - region.y
		traceLeft[rowIndex] = left
		traceRight[rowIndex] = right
		traceSeen[rowIndex] = true
		prevLeft = left
		prevRight = right
		prevWidth = float64(right - left + 1)
	}
}

func traceCloseupContourRow(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width uint32,
	region searchRegion,
	y uint32,
	prevLeft uint32,
	prevRight uint32,
	prevWidth float64,
	rowLeft []uint32,
	rowRight []uint32,
	rowSeen []bool,
	bottomProfile []uint32,
	maxJump int,
	step int,
	partial bool,
	outerDirection int,
) (uint32, uint32, bool, float64) {
	regionRight := region.x + region.width - 1
	rowIndex := y - region.y
	leftCenter := prevLeft
	rightCenter := prevRight
	if int(rowIndex) < len(rowSeen) && rowSeen[rowIndex] {
		leftCenter = (leftCenter + rowLeft[rowIndex]) / 2
		rightCenter = (rightCenter + rowRight[rowIndex]) / 2
	}

	leftMin := uint32(maxInt(int(region.x), int(leftCenter)-maxJump))
	leftMax := uint32(minInt(int(regionRight-2), int(leftCenter)+maxJump))
	rightMin := uint32(maxInt(int(region.x+2), int(rightCenter)-maxJump))
	rightMax := uint32(minInt(int(regionRight), int(rightCenter)+maxJump))
	if leftMin > leftMax || rightMin > rightMax {
		return 0, 0, false, 0
	}

	minWidth := clampInt(int(math.Round(prevWidth*0.52)), 12, int(region.width)-4)
	maxWidth := clampInt(int(math.Round(prevWidth*1.42))+4, minWidth, int(region.width)-2)
	if step < 0 {
		maxWidth = clampInt(int(math.Round(prevWidth*1.20))+4, minWidth, int(region.width)-2)
	}
	if partial {
		minWidth = clampInt(int(math.Round(prevWidth*0.46)), 10, int(region.width)-4)
		maxWidth = clampInt(int(math.Round(prevWidth*1.54))+6, minWidth, int(region.width)-2)
		if step < 0 {
			maxWidth = clampInt(int(math.Round(prevWidth*1.34))+6, minWidth, int(region.width)-2)
		}
	}
	if partial && outerDirection > 0 && step < 0 {
		minWidth = clampInt(int(math.Round(prevWidth*0.30)), 8, int(region.width)-4)
		maxWidth = clampInt(int(math.Round(prevWidth*1.28))+6, minWidth, int(region.width)-2)
	}
	if int(rowIndex) < len(rowSeen) && rowSeen[rowIndex] {
		observedWidth := int(rowRight[rowIndex] - rowLeft[rowIndex] + 1)
		minWidth = minInt(minWidth, maxInt(observedWidth-18, 8))
		maxWidth = maxInt(maxWidth, minInt(observedWidth+24, int(region.width)-2))
		if step > 0 {
			minWidth = maxInt(minWidth, maxInt(observedWidth-8, 8))
		}
	}

	bestScore := math.Inf(-1)
	bestLeft := uint32(0)
	bestRight := uint32(0)
	for left := leftMin; left <= leftMax; left++ {
		for right := rightMin; right <= rightMax; right++ {
			if right <= left {
				continue
			}
			spanWidth := int(right - left + 1)
			if spanWidth < minWidth || spanWidth > maxWidth {
				continue
			}
			score := scoreCloseupContourSpan(
				normalized,
				toothness,
				darkness,
				gradient,
				width,
				region,
				y,
				left,
				right,
				prevLeft,
				prevRight,
				prevWidth,
				rowLeft,
				rowRight,
				rowSeen,
				bottomProfile,
				step,
				partial,
				outerDirection,
			)
			if score > bestScore {
				bestScore = score
				bestLeft = left
				bestRight = right
			}
		}
	}
	if math.IsInf(bestScore, -1) {
		return 0, 0, false, 0
	}
	return bestLeft, bestRight, true, bestScore
}

func scoreCloseupContourSpan(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width uint32,
	region searchRegion,
	y uint32,
	left uint32,
	right uint32,
	prevLeft uint32,
	prevRight uint32,
	prevWidth float64,
	rowLeft []uint32,
	rowRight []uint32,
	rowSeen []bool,
	bottomProfile []uint32,
	step int,
	partial bool,
	outerDirection int,
) float64 {
	rowStart := int(y * width)
	interiorBrightness := 0.0
	interiorToothness := 0.0
	separatorPenalty := 0.0
	interiorGradient := 0.0
	backgroundPenalty := 0.0
	count := float64(right - left + 1)
	for x := left; x <= right; x++ {
		index := rowStart + int(x)
		interiorBrightness += float64(normalized[index])
		interiorToothness += float64(toothness[index])
		separatorPenalty += float64(darkness[index])
		interiorGradient += float64(gradient[index])
		if int(x-region.x) < len(bottomProfile) && y > bottomProfile[x-region.x] {
			backgroundPenalty += 12.0
		}
	}
	interiorBrightness /= count
	interiorToothness /= count
	separatorPenalty /= count
	interiorGradient /= count

	leftBoundary := scoreCloseupSideBoundaryData(normalized, toothness, darkness, gradient, width, y, left, -1)
	rightBoundary := scoreCloseupSideBoundaryData(normalized, toothness, darkness, gradient, width, y, right, 1)
	spanWidth := float64(right - left + 1)
	bandCenter := float64(region.x) + float64(region.width)/2.0
	imageCenter := float64(width) / 2.0
	widthDelta := spanWidth - prevWidth
	widthPenalty := math.Abs(widthDelta) * 0.30
	if step < 0 && widthDelta > 0 {
		widthPenalty = widthDelta * 0.95
	} else if step > 0 && widthDelta < 0 {
		widthPenalty = math.Abs(widthDelta) * 0.72
	}
	jumpPenalty := float64(absInt(int(left)-int(prevLeft))+absInt(int(right)-int(prevRight))) * 0.62
	leftWallDistance := float64(left - region.x)
	rightWallDistance := float64(region.x + region.width - 1 - right)
	wallPenalty := 0.0
	minWallDistance := 12.0
	if partial {
		minWallDistance = 3.0
	}
	if leftWallDistance < minWallDistance {
		wallPenalty += (minWallDistance - leftWallDistance) * 1.35
	}
	if rightWallDistance < minWallDistance {
		wallPenalty += (minWallDistance - rightWallDistance) * 1.35
	}
	innerWallBonus := 0.0
	if !partial {
		if bandCenter < imageCenter {
			innerWallBonus = rightBoundary * 0.48
		} else {
			innerWallBonus = leftBoundary * 0.78
		}
	}
	continuationBonus := 0.0
	overlapLeft := maxUint32(left, prevLeft)
	overlapRight := minUint32(right, prevRight)
	if overlapRight >= overlapLeft {
		overlapRatio := float64(overlapRight-overlapLeft+1) / math.Max(spanWidth, prevWidth)
		continuationBonus = overlapRatio * 10.0
		if partial && step < 0 {
			continuationBonus += overlapRatio * 6.0
		}
	}
	bodyGrowthBonus := 0.0
	if !partial && step > 0 && widthDelta > 0 {
		bodyGrowthBonus = widthDelta * 0.24
		if bandCenter > float64(width)/2.0 {
			bodyGrowthBonus += widthDelta * 0.14
		}
	}
	partialOuterBonus := 0.0
	partialBoxPenalty := 0.0
	if partial && outerDirection > 0 {
		partialOuterBonus = rightBoundary*0.30 + leftBoundary*0.08
		if region.height > 1 {
			rowProgress := float64(y-region.y) / float64(region.height-1)
			if rowProgress >= 0.42 && rowProgress <= 0.86 {
				boxWidth := float64(region.width) * 0.62
				if spanWidth > boxWidth {
					partialBoxPenalty = (spanWidth - boxWidth) * 0.44
				}
			}
		}
		if step < 0 && widthDelta < 0 {
			partialOuterBonus += math.Abs(widthDelta) * 0.12
		}
	}
	midBodyPenalty := 0.0
	if !partial && region.height > 1 {
		rowProgress := float64(y-region.y) / float64(region.height-1)
		if rowProgress >= 0.34 && rowProgress <= 0.70 {
			targetWidth := float64(region.width) * 0.24
			if bandCenter > imageCenter {
				targetWidth = float64(region.width) * 0.28
			}
			if spanWidth < targetWidth {
				midBodyPenalty = (targetWidth - spanWidth) * 0.82
			}
		}
	}

	observedBonus := 0.0
	rowIndex := y - region.y
	if int(rowIndex) < len(rowSeen) && rowSeen[rowIndex] {
		overlapLeft := maxUint32(left, rowLeft[rowIndex])
		overlapRight := minUint32(right, rowRight[rowIndex])
		overlap := 0.0
		if overlapRight >= overlapLeft {
			overlap = float64(overlapRight-overlapLeft+1) / spanWidth
		}
		observedCenter := float64(rowLeft[rowIndex]+rowRight[rowIndex]) / 2.0
		candidateCenter := float64(left+right) / 2.0
		centerPenalty := math.Abs(candidateCenter-observedCenter) * 0.38
		observedBonus = overlap*26.0 - centerPenalty
		if step > 0 {
			observedWidth := float64(rowRight[rowIndex] - rowLeft[rowIndex] + 1)
			if spanWidth < observedWidth {
				observedBonus -= (observedWidth - spanWidth) * 1.10
			} else {
				observedBonus += math.Min(spanWidth-observedWidth, 14.0) * 0.55
			}
			observedBonus += overlap * 18.0
		}
	}

	return interiorBrightness*0.18 +
		interiorToothness*0.38 +
		leftBoundary*1.10 +
		rightBoundary*1.10 -
		separatorPenalty*0.36 -
		interiorGradient*0.10 -
		widthPenalty -
		partialBoxPenalty -
		midBodyPenalty -
		jumpPenalty -
		wallPenalty -
		backgroundPenalty +
		innerWallBonus +
		continuationBonus +
		bodyGrowthBonus +
		partialOuterBonus +
		observedBonus
}

func smoothCloseupContourBoundaries(
	traceLeft []uint32,
	traceRight []uint32,
	traceSeen []bool,
	region searchRegion,
) {
	if len(traceLeft) == 0 {
		return
	}
	smoothedLeft := append([]uint32(nil), traceLeft...)
	smoothedRight := append([]uint32(nil), traceRight...)
	for index, seen := range traceSeen {
		if !seen {
			continue
		}
		leftSum := 0.0
		rightSum := 0.0
		weightSum := 0.0
		for neighbor := maxInt(index-2, 0); neighbor <= minInt(index+2, len(traceSeen)-1); neighbor++ {
			if !traceSeen[neighbor] {
				continue
			}
			distance := math.Abs(float64(neighbor - index))
			weight := 1.0 / (1.0 + distance)
			leftSum += float64(traceLeft[neighbor]) * weight
			rightSum += float64(traceRight[neighbor]) * weight
			weightSum += weight
		}
		if weightSum == 0 {
			continue
		}
		smoothedLeft[index] = maxUint32(region.x, uint32(math.Round(leftSum/weightSum)))
		smoothedRight[index] = minUint32(region.x+region.width-1, uint32(math.Round(rightSum/weightSum)))
		if smoothedRight[index] <= smoothedLeft[index] {
			smoothedLeft[index] = traceLeft[index]
			smoothedRight[index] = traceRight[index]
		}
	}
	copy(traceLeft, smoothedLeft)
	copy(traceRight, smoothedRight)
}

func densifyCloseupContourBoundaries(
	traceLeft []uint32,
	traceRight []uint32,
	traceSeen []bool,
) bool {
	firstSeen := -1
	lastSeen := -1
	for index, seen := range traceSeen {
		if !seen {
			continue
		}
		if firstSeen < 0 {
			firstSeen = index
		}
		lastSeen = index
	}
	if firstSeen < 0 || lastSeen < firstSeen {
		return false
	}

	prevSeen := firstSeen
	for index := firstSeen + 1; index <= lastSeen; index++ {
		if !traceSeen[index] {
			continue
		}
		if index-prevSeen > 1 {
			gap := float64(index - prevSeen)
			for fill := prevSeen + 1; fill < index; fill++ {
				t := float64(fill-prevSeen) / gap
				traceLeft[fill] = uint32(math.Round((1.0-t)*float64(traceLeft[prevSeen]) + t*float64(traceLeft[index])))
				traceRight[fill] = uint32(math.Round((1.0-t)*float64(traceRight[prevSeen]) + t*float64(traceRight[index])))
				traceSeen[fill] = true
			}
		}
		prevSeen = index
	}
	return true
}

func closeupContourVerticalExtent(
	traceSeen []bool,
	region searchRegion,
) (uint32, uint32, bool) {
	firstSeen := -1
	lastSeen := -1
	for index, seen := range traceSeen {
		if !seen {
			continue
		}
		if firstSeen < 0 {
			firstSeen = index
		}
		lastSeen = index
	}
	if firstSeen < 0 || lastSeen < firstSeen {
		return 0, 0, false
	}
	return region.y + uint32(firstSeen), region.y + uint32(lastSeen), true
}

func findCloseupBottomShoulderRow(
	traceLeft []uint32,
	traceRight []uint32,
	traceSeen []bool,
	region searchRegion,
	partial bool,
	outerDirection int,
) (uint32, uint32, uint32, bool) {
	topRow, bottomRow, ok := closeupContourVerticalExtent(traceSeen, region)
	if !ok {
		return 0, 0, 0, false
	}
	searchStartRatio := 0.58
	if partial && outerDirection > 0 {
		searchStartRatio = 0.50
	}
	searchStart := topRow + uint32(math.Round(float64(bottomRow-topRow)*searchStartRatio))
	if searchStart > bottomRow {
		searchStart = topRow
	}

	bestRow := uint32(0)
	bestLeft := uint32(0)
	bestRight := uint32(0)
	bestScore := math.Inf(-1)
	for y := searchStart; y <= bottomRow; y++ {
		rowIndex := y - region.y
		if !traceSeen[rowIndex] {
			continue
		}
		widthValue := float64(traceRight[rowIndex] - traceLeft[rowIndex] + 1)
		lowerBias := 0.0
		if bottomRow > searchStart {
			lowerBias = float64(y-searchStart) / float64(bottomRow-searchStart)
		}
		score := widthValue*1.45 + lowerBias*18.0
		if partial {
			score = widthValue*1.25 + lowerBias*12.0
		}
		if partial && outerDirection > 0 {
			score = widthValue*1.12 + lowerBias*7.0
		}
		if score > bestScore {
			bestScore = score
			bestRow = y
			bestLeft = traceLeft[rowIndex]
			bestRight = traceRight[rowIndex]
		}
	}
	if bestScore <= 0 || bestRight <= bestLeft {
		return 0, 0, 0, false
	}
	return bestRow, bestLeft, bestRight, true
}

func traceCloseupBottomCrownArc(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width, height uint32,
	region searchRegion,
	bottomProfile []uint32,
	leftX uint32,
	rightX uint32,
	leftY uint32,
	rightY uint32,
	partial bool,
	outerDirection int,
) ([]uint32, bool) {
	if rightX <= leftX+2 {
		return nil, false
	}
	pointCount := int(rightX-leftX) + 1
	curveDepth := float64(clampInt(pointCount/5, 10, 32))
	if partial {
		curveDepth *= 0.65
	}
	if partial && outerDirection > 0 {
		curveDepth *= 1.25
	}
	maxStep := clampInt(pointCount/22, 2, 5)
	if partial {
		maxStep = clampInt(pointCount/18, 2, 6)
	}
	if partial && outerDirection > 0 {
		maxStep = clampInt(pointCount/20, 2, 5)
	}

	candidates := make([][]uint32, pointCount)
	expected := make([]float64, pointCount)
	for offset := 0; offset < pointCount; offset++ {
		x := leftX + uint32(offset)
		profileIndex := int(x - region.x)
		if profileIndex < 0 || profileIndex >= len(bottomProfile) {
			return nil, false
		}
		limitY := maxUint32(region.y+2, saturatingSubUint32(bottomProfile[profileIndex], 2))
		t := 0.0
		if pointCount > 1 {
			t = float64(offset) / float64(pointCount-1)
		}
		endpointY := (1.0-t)*float64(leftY) + t*float64(rightY)
		shape := 1.0 - math.Pow(2.0*t-1.0, 2)
		expectedY := math.Min(float64(limitY), endpointY+curveDepth*shape)
		expected[offset] = expectedY

		if offset == 0 {
			candidates[offset] = []uint32{minUint32(limitY, leftY)}
			continue
		}
		if offset == pointCount-1 {
			candidates[offset] = []uint32{minUint32(limitY, rightY)}
			continue
		}
		minY := maxInt(int(region.y)+1, int(math.Round(expectedY))-18)
		maxY := minInt(int(limitY), int(math.Round(expectedY))+14)
		if maxY < minY {
			return nil, false
		}
		column := make([]uint32, 0, maxY-minY+1)
		for y := minY; y <= maxY; y++ {
			column = append(column, uint32(y))
		}
		candidates[offset] = column
	}
	path := solveCloseupArcPath(
		candidates,
		expected,
		func(offset int, y uint32) float64 {
			x := leftX + uint32(offset)
			profileIndex := int(x - region.x)
			if profileIndex < 0 || profileIndex >= len(bottomProfile) || y >= bottomProfile[profileIndex] {
				return math.Inf(-1)
			}
			score := scoreCloseupBottomArcPoint(
				normalized,
				toothness,
				darkness,
				gradient,
				width,
				height,
				x,
				y,
				bottomProfile[profileIndex],
				expected[offset],
			)
			if !partial {
				centerDistance := math.Abs(float64(x) - (float64(region.x) + float64(region.width)/2.0))
				score -= centerDistance * 0.02
			}
			return score
		},
		maxStep,
	)
	if len(path) != pointCount {
		return nil, false
	}
	return path, true
}

func traceCloseupTopRootArc(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width, height uint32,
	region searchRegion,
	leftX uint32,
	rightX uint32,
	leftY uint32,
	rightY uint32,
	partial bool,
) ([]uint32, bool) {
	if rightX <= leftX+2 {
		return nil, false
	}
	pointCount := int(rightX-leftX) + 1
	curveDepth := float64(clampInt(pointCount/10, 4, 14))
	if partial {
		curveDepth *= 0.70
	}
	maxStep := clampInt(pointCount/28, 1, 4)
	minEndpointY := minUint32(leftY, rightY)

	candidates := make([][]uint32, pointCount)
	expected := make([]float64, pointCount)
	for offset := 0; offset < pointCount; offset++ {
		t := 0.0
		if pointCount > 1 {
			t = float64(offset) / float64(pointCount-1)
		}
		endpointY := (1.0-t)*float64(leftY) + t*float64(rightY)
		shape := 1.0 - math.Pow(2.0*t-1.0, 2)
		expectedY := math.Max(float64(region.y)+2, endpointY-curveDepth*shape)
		expected[offset] = expectedY

		if offset == 0 {
			candidates[offset] = []uint32{leftY}
			continue
		}
		if offset == pointCount-1 {
			candidates[offset] = []uint32{rightY}
			continue
		}
		minY := maxInt(int(region.y)+1, int(math.Round(expectedY))-10)
		maxY := minInt(int(minEndpointY)+10, int(math.Round(expectedY))+12)
		if maxY < minY {
			return nil, false
		}
		column := make([]uint32, 0, maxY-minY+1)
		for y := minY; y <= maxY; y++ {
			column = append(column, uint32(y))
		}
		candidates[offset] = column
	}
	path := solveCloseupArcPath(
		candidates,
		expected,
		func(offset int, y uint32) float64 {
			x := leftX + uint32(offset)
			score := scoreCloseupTopArcPoint(
				normalized,
				toothness,
				darkness,
				gradient,
				width,
				height,
				x,
				y,
				expected[offset],
			)
			if !partial {
				centerDistance := math.Abs(float64(x) - (float64(region.x) + float64(region.width)/2.0))
				score -= centerDistance * 0.01
			}
			return score
		},
		maxStep,
	)
	if len(path) != pointCount {
		return nil, false
	}
	return path, true
}

func solveCloseupArcPath(
	candidates [][]uint32,
	expected []float64,
	pointScore func(offset int, y uint32) float64,
	maxStep int,
) []uint32 {
	if len(candidates) == 0 {
		return nil
	}
	scores := make([][]float64, len(candidates))
	parents := make([][]int, len(candidates))
	for index, column := range candidates {
		scores[index] = make([]float64, len(column))
		parents[index] = make([]int, len(column))
		for candidateIndex := range column {
			scores[index][candidateIndex] = math.Inf(-1)
			parents[index][candidateIndex] = -1
		}
	}
	for candidateIndex, y := range candidates[0] {
		scores[0][candidateIndex] = pointScore(0, y)
	}
	for index := 1; index < len(candidates); index++ {
		for candidateIndex, y := range candidates[index] {
			point := pointScore(index, y)
			if math.IsInf(point, -1) {
				continue
			}
			bestScore := math.Inf(-1)
			bestParent := -1
			for previousIndex, previousY := range candidates[index-1] {
				if math.IsInf(scores[index-1][previousIndex], -1) {
					continue
				}
				if absInt(int(y)-int(previousY)) > maxStep {
					continue
				}
				score := scores[index-1][previousIndex] + point
				score -= float64(absInt(int(y)-int(previousY))) * 1.40
				score -= math.Abs(float64(y)-expected[index]) * 0.28
				if score > bestScore {
					bestScore = score
					bestParent = previousIndex
				}
			}
			scores[index][candidateIndex] = bestScore
			parents[index][candidateIndex] = bestParent
		}
	}

	bestIndex := -1
	bestScore := math.Inf(-1)
	lastColumn := len(candidates) - 1
	for candidateIndex, score := range scores[lastColumn] {
		if score > bestScore {
			bestScore = score
			bestIndex = candidateIndex
		}
	}
	if bestIndex < 0 || math.IsInf(bestScore, -1) {
		return nil
	}

	path := make([]uint32, len(candidates))
	current := bestIndex
	for index := lastColumn; index >= 0; index-- {
		path[index] = candidates[index][current]
		if index == 0 {
			break
		}
		current = parents[index][current]
		if current < 0 {
			return nil
		}
	}

	smoothed := make([]float64, len(path))
	for index, y := range path {
		smoothed[index] = float64(y)
	}
	smoothed = smoothFloatProfile(smoothed, 4)
	for index := range path {
		path[index] = uint32(math.Round(smoothed[index]*0.80 + expected[index]*0.20))
	}
	for index := 1; index+1 < len(path); index++ {
		leftJump := absInt(int(path[index]) - int(path[index-1]))
		rightJump := absInt(int(path[index+1]) - int(path[index]))
		if leftJump+rightJump > 8 {
			path[index] = uint32(math.Round((float64(path[index-1]) + float64(path[index]) + float64(path[index+1])) / 3.0))
		}
	}
	return path
}

func scoreCloseupBottomArcPoint(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width, height uint32,
	x uint32,
	y uint32,
	bottomLimit uint32,
	expectedY float64,
) float64 {
	index := int(y*width + x)
	boundary := closeupHorizontalBoundaryStrength(normalized, darkness, gradient, width, height, y, x, 1)
	insideBrightness := sampleCloseupVerticalMean(normalized, width, height, x, y, -4, -1)
	insideToothness := sampleCloseupVerticalMean(toothness, width, height, x, y, -4, -1)
	insideDarkness := sampleCloseupVerticalMean(darkness, width, height, x, y, -4, -1)
	belowDarkness := sampleCloseupVerticalMean(darkness, width, height, x, y, 1, 4)
	belowBrightness := sampleCloseupVerticalMean(normalized, width, height, x, y, 1, 4)
	limitPenalty := 0.0
	if y+1 >= bottomLimit {
		limitPenalty += 26.0
	}
	return boundary*0.95 +
		insideBrightness*0.09 +
		insideToothness*0.24 +
		belowDarkness*0.16 -
		insideDarkness*0.12 -
		belowBrightness*0.06 -
		math.Abs(float64(y)-expectedY)*0.90 -
		limitPenalty -
		float64(gradient[index])*0.04
}

func scoreCloseupTopArcPoint(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width, height uint32,
	x uint32,
	y uint32,
	expectedY float64,
) float64 {
	boundary := closeupHorizontalBoundaryStrength(normalized, darkness, gradient, width, height, y, x, -1)
	insideBrightness := sampleCloseupVerticalMean(normalized, width, height, x, y, 1, 4)
	insideToothness := sampleCloseupVerticalMean(toothness, width, height, x, y, 1, 4)
	outsideDarkness := sampleCloseupVerticalMean(darkness, width, height, x, y, -4, -1)
	outsideGradient := sampleCloseupVerticalMean(gradient, width, height, x, y, -3, 0)
	return boundary*0.82 +
		insideBrightness*0.08 +
		insideToothness*0.16 +
		outsideDarkness*0.05 -
		outsideGradient*0.10 -
		math.Abs(float64(y)-expectedY)*0.72
}

func sampleCloseupVerticalMean(
	values []uint8,
	width, height uint32,
	x uint32,
	y uint32,
	startOffset int,
	endOffset int,
) float64 {
	if startOffset > endOffset {
		startOffset, endOffset = endOffset, startOffset
	}
	sum := 0.0
	count := 0.0
	for offset := startOffset; offset <= endOffset; offset++ {
		targetY := int(y) + offset
		if targetY < 0 || targetY >= int(height) {
			continue
		}
		sum += float64(values[targetY*int(width)+int(x)])
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / count
}

func buildCloseupContourPolygon(
	traceLeft []uint32,
	traceRight []uint32,
	traceSeen []bool,
	region searchRegion,
	topRow uint32,
	bottomRow uint32,
	topArc []uint32,
	bottomArc []uint32,
) ([]contracts.Point, bool) {
	if topRow < region.y || bottomRow >= region.y+region.height || bottomRow < topRow {
		return nil, false
	}
	topIndex := topRow - region.y
	bottomIndex := bottomRow - region.y
	topLeftX := traceLeft[topIndex]
	topRightX := traceRight[topIndex]
	bottomLeftX := traceLeft[bottomIndex]
	bottomRightX := traceRight[bottomIndex]
	if len(topArc) != int(topRightX-topLeftX)+1 || len(bottomArc) != int(bottomRightX-bottomLeftX)+1 {
		return nil, false
	}

	points := make([]contracts.Point, 0, len(traceLeft)*2+len(topArc)+len(bottomArc))
	appendPoint := func(x, y uint32) {
		if len(points) > 0 && points[len(points)-1].X == x && points[len(points)-1].Y == y {
			return
		}
		points = append(points, contracts.Point{X: x, Y: y})
	}

	for y := topRow; y <= bottomRow; y++ {
		rowIndex := y - region.y
		if !traceSeen[rowIndex] {
			continue
		}
		appendPoint(traceLeft[rowIndex], y)
	}
	for offset, y := range bottomArc {
		appendPoint(bottomLeftX+uint32(offset), y)
	}
	for y := bottomRow; ; y-- {
		rowIndex := y - region.y
		if traceSeen[rowIndex] {
			appendPoint(traceRight[rowIndex], y)
		}
		if y == topRow {
			break
		}
	}
	for offset := len(topArc) - 1; offset >= 0; offset-- {
		appendPoint(topLeftX+uint32(offset), topArc[offset])
		if offset == 0 {
			break
		}
	}
	if len(points) < 8 {
		return nil, false
	}
	return points, true
}

func rasterizeCloseupContourPolygon(
	polygon []contracts.Point,
	width, height uint32,
	region searchRegion,
) []uint8 {
	mask := make([]uint8, int(width*height))
	if len(polygon) < 3 {
		return mask
	}

	minY := polygon[0].Y
	maxY := polygon[0].Y
	for _, point := range polygon[1:] {
		if point.Y < minY {
			minY = point.Y
		}
		if point.Y > maxY {
			maxY = point.Y
		}
	}
	minY = maxUint32(minY, region.y)
	maxY = minUint32(maxY, region.y+region.height-1)

	for y := minY; y <= maxY; y++ {
		scanY := float64(y) + 0.5
		intersections := make([]float64, 0, len(polygon))
		for index := range polygon {
			next := (index + 1) % len(polygon)
			y1 := float64(polygon[index].Y)
			y2 := float64(polygon[next].Y)
			if y1 == y2 {
				continue
			}
			minEdgeY := math.Min(y1, y2)
			maxEdgeY := math.Max(y1, y2)
			if scanY < minEdgeY || scanY >= maxEdgeY {
				continue
			}
			x1 := float64(polygon[index].X)
			x2 := float64(polygon[next].X)
			x := x1 + ((scanY-y1)*(x2-x1))/(y2-y1)
			intersections = append(intersections, x)
		}
		sort.Float64s(intersections)
		rowStart := int(y * width)
		for index := 0; index+1 < len(intersections); index += 2 {
			left := maxInt(int(region.x), int(math.Ceil(intersections[index])))
			right := minInt(int(region.x+region.width-1), int(math.Floor(intersections[index+1])))
			for x := left; x <= right; x++ {
				mask[rowStart+x] = 1
			}
		}
	}
	return mask
}

func smoothCloseupToothGeometry(
	geometry contracts.ToothGeometry,
	partial bool,
) contracts.ToothGeometry {
	if len(geometry.Outline) < 16 {
		return geometry
	}
	radius := 3
	if partial {
		radius = 2
	}
	smoothed := make([]contracts.Point, 0, len(geometry.Outline))
	appendPoint := func(point contracts.Point) {
		if len(smoothed) > 0 && smoothed[len(smoothed)-1] == point {
			return
		}
		smoothed = append(smoothed, point)
	}
	for index := range geometry.Outline {
		sumX := 0.0
		sumY := 0.0
		weightSum := 0.0
		for offset := -radius; offset <= radius; offset++ {
			neighbor := (index + offset + len(geometry.Outline)) % len(geometry.Outline)
			distance := math.Abs(float64(offset))
			weight := 1.0 / (1.0 + distance)
			sumX += float64(geometry.Outline[neighbor].X) * weight
			sumY += float64(geometry.Outline[neighbor].Y) * weight
			weightSum += weight
		}
		point := contracts.Point{
			X: minUint32(
				geometry.BoundingBox.X+geometry.BoundingBox.Width-1,
				maxUint32(geometry.BoundingBox.X, uint32(math.Round(sumX/weightSum))),
			),
			Y: minUint32(
				geometry.BoundingBox.Y+geometry.BoundingBox.Height-1,
				maxUint32(geometry.BoundingBox.Y, uint32(math.Round(sumY/weightSum))),
			),
		}
		appendPoint(point)
	}
	if len(smoothed) >= 12 {
		geometry.Outline = smoothed
	}
	return geometry
}

func refineCloseupSelectedComponent(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width, height uint32,
	bandRegion searchRegion,
	crownRegion searchRegion,
	component maskComponent,
	seedX, seedY uint32,
	partial bool,
	outerDirection int,
) maskComponent {
	if len(component.pixels) == 0 || component.bbox.Width < 12 || component.bbox.Height < 32 {
		return component
	}

	region := searchRegion{
		x:      maxUint32(bandRegion.x, saturatingSubUint32(component.bbox.X, 4)),
		y:      maxUint32(bandRegion.y, saturatingSubUint32(component.bbox.Y, 4)),
		width:  minUint32(component.bbox.Width+8, bandRegion.x+bandRegion.width-maxUint32(bandRegion.x, saturatingSubUint32(component.bbox.X, 4))),
		height: minUint32(component.bbox.Height+8, bandRegion.y+bandRegion.height-maxUint32(bandRegion.y, saturatingSubUint32(component.bbox.Y, 4))),
	}
	region = clampSearchRegionToSearch(region, bandRegion)
	if region.width < 12 || region.height < 24 {
		return component
	}

	mask := maskFromComponent(component, len(normalized))
	rowLeft, rowRight, rowSeen := buildCloseupObservedRowSpans(mask, width, region)
	if !densifyCloseupContourBoundaries(rowLeft, rowRight, rowSeen) {
		return component
	}
	bottomProfile := estimateCloseupBottomProfile(
		normalized,
		toothness,
		darkness,
		width,
		region,
		crownRegion,
		component,
	)
	if len(bottomProfile) != int(region.width) {
		return component
	}

	for pass := 0; pass < 4; pass++ {
		prevLeft := append([]uint32(nil), rowLeft...)
		prevRight := append([]uint32(nil), rowRight...)
		for rowIndex, seen := range rowSeen {
			if !seen {
				continue
			}
			y := region.y + uint32(rowIndex)
			left, right, ok := relaxCloseupContourRow(
				normalized,
				toothness,
				darkness,
				gradient,
				width,
				height,
				region,
				y,
				prevLeft,
				prevRight,
				rowSeen,
				bottomProfile,
				rowLeft[rowIndex],
				rowRight[rowIndex],
				partial,
				outerDirection,
			)
			if !ok {
				continue
			}
			rowLeft[rowIndex] = left
			rowRight[rowIndex] = right
		}
		smoothCloseupContourBoundaries(rowLeft, rowRight, rowSeen, region)
		densifyCloseupContourBoundaries(rowLeft, rowRight, rowSeen)
	}

	refinedMask := make([]uint8, len(mask))
	for rowIndex, seen := range rowSeen {
		if !seen {
			continue
		}
		y := region.y + uint32(rowIndex)
		left := rowLeft[rowIndex]
		right := rowRight[rowIndex]
		if right <= left {
			continue
		}
		if int(left-region.x) < len(bottomProfile) && y >= bottomProfile[left-region.x] {
			continue
		}
		if int(right-region.x) < len(bottomProfile) && y >= bottomProfile[right-region.x] {
			continue
		}
		fillCloseupRowSpan(refinedMask, width, y, left, right)
	}
	refinedMask = closeBinaryMask(refinedMask, int(width), int(height))
	refinedMask = fillHolesBinaryMask(refinedMask, int(width), int(height))
	refined, ok := extractCloseupSeedComponent(
		refinedMask,
		width,
		height,
		region,
		seedX,
		seedY,
		maxUint32(component.area/3, 160),
	)
	if !ok || refined.area < maxUint32(component.area/2, 180) {
		return component
	}
	return refined
}

func relaxCloseupContourRow(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width, height uint32,
	region searchRegion,
	y uint32,
	rowLeft []uint32,
	rowRight []uint32,
	rowSeen []bool,
	bottomProfile []uint32,
	currentLeft uint32,
	currentRight uint32,
	partial bool,
	outerDirection int,
) (uint32, uint32, bool) {
	rowIndex := y - region.y
	if int(rowIndex) >= len(rowSeen) || !rowSeen[rowIndex] {
		return 0, 0, false
	}
	prevIndex := findCloseupSeenNeighbor(rowSeen, int(rowIndex), -1)
	nextIndex := findCloseupSeenNeighbor(rowSeen, int(rowIndex), 1)
	leftMin := maxInt(int(region.x), int(currentLeft)-4)
	leftMax := minInt(int(region.x+region.width-2), int(currentLeft)+4)
	rightMin := maxInt(int(region.x+1), int(currentRight)-4)
	rightMax := minInt(int(region.x+region.width-1), int(currentRight)+4)
	bestScore := math.Inf(-1)
	bestLeft := currentLeft
	bestRight := currentRight
	currentWidth := float64(currentRight - currentLeft + 1)
	for left := leftMin; left <= leftMax; left++ {
		for right := rightMin; right <= rightMax; right++ {
			if right <= left+1 {
				continue
			}
			spanWidth := float64(right - left + 1)
			score := scoreCloseupSideBoundaryData(normalized, toothness, darkness, gradient, width, y, uint32(left), -1)*1.14 +
				scoreCloseupSideBoundaryData(normalized, toothness, darkness, gradient, width, y, uint32(right), 1)*1.14
			score += sampleCloseupHorizontalMean(normalized, width, height, uint32((left+right)/2), y, -1, 1) * 0.05
			score += sampleCloseupHorizontalMean(toothness, width, height, uint32((left+right)/2), y, -1, 1) * 0.08
			score -= sampleCloseupHorizontalMean(darkness, width, height, uint32((left+right)/2), y, -1, 1) * 0.05
			score -= math.Abs(spanWidth-currentWidth) * 0.28
			if prevIndex >= 0 {
				score -= math.Abs(float64(left)-float64(rowLeft[prevIndex])) * 0.42
				score -= math.Abs(float64(right)-float64(rowRight[prevIndex])) * 0.42
			}
			if nextIndex >= 0 {
				score -= math.Abs(float64(left)-float64(rowLeft[nextIndex])) * 0.42
				score -= math.Abs(float64(right)-float64(rowRight[nextIndex])) * 0.42
			}
			if prevIndex >= 0 && nextIndex >= 0 {
				score -= math.Abs(float64(left*2)-float64(rowLeft[prevIndex]+rowLeft[nextIndex])) * 0.16
				score -= math.Abs(float64(right*2)-float64(rowRight[prevIndex]+rowRight[nextIndex])) * 0.16
			}
			if int(uint32(left)-region.x) < len(bottomProfile) && y+1 >= bottomProfile[uint32(left)-region.x] {
				score -= 20.0
			}
			if int(uint32(right)-region.x) < len(bottomProfile) && y+1 >= bottomProfile[uint32(right)-region.x] {
				score -= 20.0
			}
			leftWallDistance := float64(uint32(left) - region.x)
			rightWallDistance := float64(region.x + region.width - 1 - uint32(right))
			if !partial {
				if leftWallDistance < 10 {
					score -= (10 - leftWallDistance) * 1.4
				}
				if rightWallDistance < 10 {
					score -= (10 - rightWallDistance) * 1.4
				}
			}
			if partial && outerDirection > 0 {
				rowProgress := float64(y-region.y) / math.Max(float64(region.height-1), 1.0)
				if rowProgress >= 0.40 && rowProgress <= 0.88 {
					boxTarget := float64(region.width) * 0.60
					if spanWidth > boxTarget {
						score -= (spanWidth - boxTarget) * 0.52
					}
				}
				score += closeupBoundaryStrength(normalized, darkness, gradient, width, y, uint32(right), 1) * 0.18
			}
			if score > bestScore {
				bestScore = score
				bestLeft = uint32(left)
				bestRight = uint32(right)
			}
		}
	}
	if math.IsInf(bestScore, -1) {
		return 0, 0, false
	}
	return bestLeft, bestRight, true
}

func findCloseupSeenNeighbor(rowSeen []bool, index int, direction int) int {
	for neighbor := index + direction; neighbor >= 0 && neighbor < len(rowSeen); neighbor += direction {
		if rowSeen[neighbor] {
			return neighbor
		}
	}
	return -1
}

func closeupHorizontalBoundaryStrength(
	normalized []uint8,
	darkness []uint8,
	gradient []uint8,
	width, height uint32,
	y uint32,
	x uint32,
	direction int,
) float64 {
	rowStart := int(y * width)
	index := rowStart + int(x)
	score := float64(gradient[index])*0.78 + float64(darkness[index])*0.18
	if direction < 0 && y > 0 {
		inside := rowStart + int(x)
		outside := int((y-1)*width + x)
		score += math.Abs(float64(normalized[inside])-float64(normalized[outside])) * 0.62
	}
	if direction > 0 && y+1 < height {
		inside := rowStart + int(x)
		outside := int((y+1)*width + x)
		score += math.Abs(float64(normalized[inside])-float64(normalized[outside])) * 0.62
	}
	return score
}

func scoreCloseupSideBoundaryData(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width uint32,
	y uint32,
	x uint32,
	direction int,
) float64 {
	height := uint32(len(normalized)) / width
	boundary := closeupBoundaryStrength(normalized, darkness, gradient, width, y, x, direction)
	insideStart := 0
	insideEnd := 2
	outsideStart := -2
	outsideEnd := -1
	if direction > 0 {
		insideStart = -2
		insideEnd = 0
		outsideStart = 1
		outsideEnd = 2
	}
	insideBrightness := sampleCloseupHorizontalMean(normalized, width, height, x, y, insideStart, insideEnd)
	insideToothness := sampleCloseupHorizontalMean(toothness, width, height, x, y, insideStart, insideEnd)
	outsideDarkness := sampleCloseupHorizontalMean(darkness, width, height, x, y, outsideStart, outsideEnd)
	outsideBrightness := sampleCloseupHorizontalMean(normalized, width, height, x, y, outsideStart, outsideEnd)
	outsideGradient := sampleCloseupHorizontalMean(gradient, width, height, x, y, outsideStart, outsideEnd)
	return boundary*1.12 +
		insideBrightness*0.26 +
		insideToothness*0.34 +
		outsideDarkness*0.18 -
		outsideBrightness*0.06 -
		outsideGradient*0.05
}

func sampleCloseupHorizontalMean(
	values []uint8,
	width, height uint32,
	x uint32,
	y uint32,
	startOffset int,
	endOffset int,
) float64 {
	if startOffset > endOffset {
		startOffset, endOffset = endOffset, startOffset
	}
	if y >= height {
		return 0
	}
	sum := 0.0
	count := 0.0
	for offset := startOffset; offset <= endOffset; offset++ {
		targetX := int(x) + offset
		if targetX < 0 || targetX >= int(width) {
			continue
		}
		sum += float64(values[int(y)*int(width)+targetX])
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / count
}

type closeupTraceAnchor struct {
	y     uint32
	left  uint32
	right uint32
}

func constrainCloseupMaskToEnvelope(
	mask []uint8,
	width uint32,
	region searchRegion,
	crownBBox contracts.BoundingBox,
	partial bool,
) []uint8 {
	if countMaskPixels(mask) == 0 {
		return mask
	}

	leftPad := uint32(clampInt(int(crownBBox.Width/3), 18, 34))
	rightPad := leftPad
	if partial {
		leftPad = uint32(clampInt(int(crownBBox.Width/2), 24, 46))
		rightPad = leftPad
	}
	left := maxUint32(region.x, saturatingSubUint32(crownBBox.X, leftPad))
	right := minUint32(region.x+region.width-1, crownBBox.X+crownBBox.Width-1+rightPad)
	top := region.y
	bottom := region.y + region.height - 1
	constrained := make([]uint8, len(mask))
	for y := top; y <= bottom; y++ {
		rowStart := int(y * width)
		for x := left; x <= right; x++ {
			index := rowStart + int(x)
			if mask[index] != 0 {
				constrained[index] = 1
			}
		}
	}
	return constrained
}

func traceCloseupCrownExtension(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width uint32,
	region searchRegion,
	crownComponent maskComponent,
	crownRegion searchRegion,
	partial bool,
) []uint8 {
	mask := make([]uint8, len(normalized))
	anchor, ok := findCloseupCrownTraceAnchor(crownComponent, width, region, crownRegion, partial)
	if !ok {
		return mask
	}

	prevLeft := anchor.left
	prevRight := anchor.right
	prevWidth := float64(prevRight - prevLeft + 1)
	maxJump := clampInt(int(region.width/16), 4, 8)
	if partial {
		maxJump = clampInt(int(region.width/14), 5, 10)
	}
	badRows := 0
	maxBadRows := 7
	if partial {
		maxBadRows = 9
	}

	for y := int(anchor.y) - 1; y >= int(region.y); y-- {
		left, right, ok, score := traceCloseupExtensionRow(
			normalized,
			toothness,
			darkness,
			gradient,
			width,
			region,
			uint32(y),
			prevLeft,
			prevRight,
			prevWidth,
			maxJump,
			partial,
		)
		if !ok || score < 26.0 {
			badRows++
			if badRows > maxBadRows {
				break
			}
			continue
		}

		fillCloseupRowSpan(mask, width, uint32(y), left, right)
		prevLeft = left
		prevRight = right
		prevWidth = float64(right - left + 1)
		badRows = 0
	}

	return mask
}

func findCloseupCrownTraceAnchor(
	component maskComponent,
	width uint32,
	region searchRegion,
	crownRegion searchRegion,
	partial bool,
) (closeupTraceAnchor, bool) {
	if len(component.pixels) == 0 || component.bbox.Height == 0 {
		return closeupTraceAnchor{}, false
	}

	rowLeft := make([]int, component.bbox.Height)
	rowRight := make([]int, component.bbox.Height)
	for index := range rowLeft {
		rowLeft[index] = -1
		rowRight[index] = -1
	}
	for _, pixel := range component.pixels {
		y := uint32(pixel / int(width))
		x := uint32(pixel % int(width))
		if y < component.bbox.Y || y >= component.bbox.Y+component.bbox.Height {
			continue
		}
		rowIndex := y - component.bbox.Y
		localIndex := int(rowIndex)
		if rowLeft[localIndex] < 0 || x < uint32(rowLeft[localIndex]) {
			rowLeft[localIndex] = int(x)
		}
		if rowRight[localIndex] < 0 || x > uint32(rowRight[localIndex]) {
			rowRight[localIndex] = int(x)
		}
	}

	minStableWidth := clampInt(int(component.bbox.Width/3), 16, 34)
	if partial {
		minStableWidth = clampInt(int(component.bbox.Width/4), 10, 24)
	}
	topLimit := minUint32(component.bbox.Y+component.bbox.Height-1, crownRegion.y+crownRegion.height/2)
	anchorRows := make([]closeupTraceAnchor, 0, 4)
	for y := maxUint32(component.bbox.Y, crownRegion.y); y <= topLimit; y++ {
		rowIndex := int(y - component.bbox.Y)
		if rowIndex < 0 || rowIndex >= len(rowLeft) || rowLeft[rowIndex] < 0 || rowRight[rowIndex] < 0 {
			continue
		}
		rowWidth := rowRight[rowIndex] - rowLeft[rowIndex] + 1
		if rowWidth < minStableWidth {
			continue
		}
		anchorRows = append(anchorRows, closeupTraceAnchor{
			y:     y,
			left:  uint32(rowLeft[rowIndex]),
			right: uint32(rowRight[rowIndex]),
		})
		if len(anchorRows) == 4 {
			break
		}
	}
	if len(anchorRows) == 0 {
		return closeupTraceAnchor{}, false
	}

	var sumLeft uint32
	var sumRight uint32
	var sumY uint32
	for _, row := range anchorRows {
		sumLeft += row.left
		sumRight += row.right
		sumY += row.y
	}
	anchor := closeupTraceAnchor{
		y:     sumY / uint32(len(anchorRows)),
		left:  sumLeft / uint32(len(anchorRows)),
		right: sumRight / uint32(len(anchorRows)),
	}
	if anchor.left < region.x {
		anchor.left = region.x
	}
	regionRight := region.x + region.width - 1
	if anchor.right > regionRight {
		anchor.right = regionRight
	}
	if anchor.right <= anchor.left {
		return closeupTraceAnchor{}, false
	}
	return anchor, true
}

func traceCloseupExtensionRow(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width uint32,
	region searchRegion,
	y uint32,
	prevLeft uint32,
	prevRight uint32,
	prevWidth float64,
	maxJump int,
	partial bool,
) (uint32, uint32, bool, float64) {
	regionRight := region.x + region.width - 1
	leftMin := uint32(maxInt(int(region.x), int(prevLeft)-maxJump))
	leftMax := uint32(minInt(int(regionRight-2), int(prevLeft)+maxJump))
	rightMin := uint32(maxInt(int(region.x+2), int(prevRight)-maxJump))
	rightMax := uint32(minInt(int(regionRight), int(prevRight)+maxJump))
	if leftMin > leftMax || rightMin > rightMax {
		return 0, 0, false, 0
	}

	minWidth := clampInt(int(math.Round(prevWidth*0.48)), 12, int(region.width)-4)
	maxWidth := clampInt(int(math.Round(prevWidth*1.08))+2, minWidth, int(region.width)-2)
	if partial {
		minWidth = clampInt(int(math.Round(prevWidth*0.40)), 8, int(region.width)-4)
		maxWidth = clampInt(int(math.Round(prevWidth*1.14))+3, minWidth, int(region.width)-2)
	}

	bestScore := math.Inf(-1)
	bestLeft := uint32(0)
	bestRight := uint32(0)
	for left := leftMin; left <= leftMax; left++ {
		for right := rightMin; right <= rightMax; right++ {
			if right <= left {
				continue
			}
			spanWidth := int(right - left + 1)
			if spanWidth < minWidth || spanWidth > maxWidth {
				continue
			}
			if left > prevRight+uint32(maxJump+2) || right+uint32(maxJump+2) < prevLeft {
				continue
			}
			score := scoreCloseupExtensionSpan(
				normalized,
				toothness,
				darkness,
				gradient,
				width,
				region,
				y,
				left,
				right,
				prevLeft,
				prevRight,
				prevWidth,
				partial,
			)
			if score > bestScore {
				bestScore = score
				bestLeft = left
				bestRight = right
			}
		}
	}
	if !math.IsInf(bestScore, -1) {
		return bestLeft, bestRight, true, bestScore
	}
	return 0, 0, false, 0
}

func scoreCloseupExtensionSpan(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width uint32,
	region searchRegion,
	y uint32,
	left uint32,
	right uint32,
	prevLeft uint32,
	prevRight uint32,
	prevWidth float64,
	partial bool,
) float64 {
	rowStart := int(y * width)
	interiorBrightness := 0.0
	interiorToothness := 0.0
	separatorPenalty := 0.0
	interiorGradient := 0.0
	count := float64(right - left + 1)
	for x := left; x <= right; x++ {
		index := rowStart + int(x)
		interiorBrightness += float64(normalized[index])
		interiorToothness += float64(toothness[index])
		separatorPenalty += float64(darkness[index])
		interiorGradient += float64(gradient[index])
	}
	interiorBrightness /= count
	interiorToothness /= count
	separatorPenalty /= count
	interiorGradient /= count

	leftBoundary := closeupBoundaryStrength(normalized, darkness, gradient, width, y, left, -1)
	rightBoundary := closeupBoundaryStrength(normalized, darkness, gradient, width, y, right, 1)
	spanWidth := float64(right - left + 1)
	widthDelta := spanWidth - prevWidth
	widthPenalty := math.Abs(widthDelta) * 0.85
	if widthDelta > 0 {
		widthPenalty = widthDelta * 1.55
	}
	jumpPenalty := float64(absInt(int(left)-int(prevLeft))+absInt(int(right)-int(prevRight))) * 1.25
	leftWallDistance := float64(left - region.x)
	rightWallDistance := float64(region.x + region.width - 1 - right)
	wallPenalty := 0.0
	minWallDistance := 10.0
	if partial {
		minWallDistance = 4.0
	}
	if leftWallDistance < minWallDistance {
		wallPenalty += (minWallDistance - leftWallDistance) * 2.1
	}
	if rightWallDistance < minWallDistance {
		wallPenalty += (minWallDistance - rightWallDistance) * 2.1
	}

	return interiorBrightness*0.16 +
		interiorToothness*0.34 +
		leftBoundary*0.55 +
		rightBoundary*0.55 -
		separatorPenalty*0.44 -
		interiorGradient*0.16 -
		widthPenalty -
		jumpPenalty -
		wallPenalty
}

func closeupBoundaryStrength(
	normalized []uint8,
	darkness []uint8,
	gradient []uint8,
	width uint32,
	y uint32,
	x uint32,
	direction int,
) float64 {
	rowStart := int(y * width)
	index := rowStart + int(x)
	score := float64(gradient[index])*0.90 + float64(darkness[index])*0.55
	if direction < 0 && x > 0 {
		inside := rowStart + int(x)
		outside := rowStart + int(x-1)
		score += math.Abs(float64(normalized[inside])-float64(normalized[outside])) * 0.45
	}
	if direction > 0 && x+1 < width {
		inside := rowStart + int(x)
		outside := rowStart + int(x+1)
		score += math.Abs(float64(normalized[inside])-float64(normalized[outside])) * 0.45
	}
	return score
}

func maskFromComponent(component maskComponent, size int) []uint8 {
	mask := make([]uint8, size)
	for _, index := range component.pixels {
		if index >= 0 && index < size {
			mask[index] = 1
		}
	}
	return mask
}

func extractCloseupBestComponent(
	mask []uint8,
	width, height uint32,
	region searchRegion,
	targetX, targetY uint32,
	minArea uint32,
) (maskComponent, bool) {
	components := collectMaskComponents(mask, width, height, region, minArea)
	if len(components) == 0 {
		return maskComponent{}, false
	}

	bestIndex := -1
	bestScore := math.Inf(-1)
	for index, component := range components {
		centerX := float64(component.bbox.X) + float64(component.bbox.Width)/2.0
		centerY := float64(component.bbox.Y) + float64(component.bbox.Height)/2.0
		score := float64(component.bbox.Width)*1.6 +
			float64(component.area)/220.0 -
			math.Abs(centerX-float64(targetX))*0.9 -
			math.Abs(centerY-float64(targetY))*0.45
		if score > bestScore {
			bestScore = score
			bestIndex = index
		}
	}
	if bestIndex < 0 {
		return maskComponent{}, false
	}
	return components[bestIndex], true
}

func evaluateCloseupToothShape(
	pixels []int,
	width uint32,
	bbox contracts.BoundingBox,
	crownRegion searchRegion,
	partial bool,
) closeupShapeMetrics {
	if len(pixels) == 0 || bbox.Width == 0 || bbox.Height == 0 {
		return closeupShapeMetrics{}
	}

	rowWidths := make([]uint32, bbox.Height)
	for _, index := range pixels {
		y := uint32(index / int(width))
		x := uint32(index % int(width))
		if y < bbox.Y || y >= bbox.Y+bbox.Height || x < bbox.X || x >= bbox.X+bbox.Width {
			continue
		}
		rowWidths[y-bbox.Y]++
	}

	upperWidths := make([]float64, 0, bbox.Height/2)
	lowerWidths := make([]float64, 0, bbox.Height/2)
	var lowerMax float64
	var crownRows uint32
	var crownCovered uint32
	crownTop := maxUint32(crownRegion.y, bbox.Y)
	crownBottom := minUint32(crownRegion.y+crownRegion.height-1, bbox.Y+bbox.Height-1)
	for rowIndex, rowWidth := range rowWidths {
		if rowWidth == 0 {
			continue
		}
		y := bbox.Y + uint32(rowIndex)
		if y <= bbox.Y+bbox.Height/2 {
			upperWidths = append(upperWidths, float64(rowWidth))
		} else {
			lowerWidths = append(lowerWidths, float64(rowWidth))
			lowerMax = math.Max(lowerMax, float64(rowWidth))
		}
		if y >= crownTop && y <= crownBottom {
			crownRows++
			if rowWidth >= maxUint32(bbox.Width/5, 14) {
				crownCovered++
			}
		}
	}
	if len(lowerWidths) == 0 {
		return closeupShapeMetrics{}
	}
	upperMedian := lowerMax
	if len(upperWidths) > 0 {
		sort.Float64s(upperWidths)
		upperMedian = upperWidths[len(upperWidths)/2]
	}
	lowerRatio := lowerMax / math.Max(upperMedian, 1.0)
	minLowerRatio := 1.35
	if partial {
		minLowerRatio = 1.10
	}
	crownCoverage := 0.0
	if crownRows > 0 {
		crownCoverage = float64(crownCovered) / float64(crownRows)
	}
	ok := lowerRatio >= minLowerRatio &&
		lowerMax >= func() float64 {
			if partial {
				return 22.0
			}
			return 42.0
		}() &&
		crownCoverage >= func() float64 {
			if partial {
				return 0.28
			}
			return 0.42
		}()

	taperScore := clamp01((lowerRatio - 1.0) / 0.9)
	return closeupShapeMetrics{
		ok:            ok,
		bodyRatio:     lowerRatio,
		taperScore:    taperScore,
		crownCoverage: crownCoverage,
	}
}

func pickCloseupBandSeed(
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width uint32,
	region searchRegion,
) (uint32, uint32, bool) {
	seedBand := searchRegion{
		x:      region.x + minUint32(region.width/10, 12),
		y:      region.y + uint32(math.Round(float64(region.height)*0.66)),
		width:  saturatingSubUint32(region.width, minUint32(region.width/5, 24)),
		height: maxUint32(uint32(math.Round(float64(region.height)*0.18)), 1),
	}
	seedBand = clampSearchRegionToSearch(seedBand, region)
	if seedBand.width < 12 || seedBand.height < 8 {
		return 0, 0, false
	}

	bandCenterX := float64(region.x) + float64(region.width)/2.0
	bestScore := math.Inf(-1)
	bestX := uint32(0)
	bestY := uint32(0)
	for y := seedBand.y; y < seedBand.y+seedBand.height; y++ {
		for x := seedBand.x; x < seedBand.x+seedBand.width; x++ {
			index := int(y*width + x)
			if toothness[index] < 90 || darkness[index] > 150 {
				continue
			}
			score := float64(toothness[index]) -
				float64(darkness[index])*1.15 -
				float64(gradient[index])*0.45 -
				math.Abs(float64(x)-bandCenterX)*0.8
			if score > bestScore {
				bestScore = score
				bestX = x
				bestY = y
			}
		}
	}

	if bestScore < 20 {
		return 0, 0, false
	}
	return bestX, bestY, true
}

func pickCloseupBandSeeds(
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width uint32,
	region searchRegion,
) []contracts.Point {
	seedBand := searchRegion{
		x:      region.x + minUint32(region.width/12, 10),
		y:      region.y + uint32(math.Round(float64(region.height)*0.56)),
		width:  saturatingSubUint32(region.width, minUint32(region.width/6, 20)),
		height: maxUint32(uint32(math.Round(float64(region.height)*0.24)), 1),
	}
	seedBand = clampSearchRegionToSearch(seedBand, region)
	if seedBand.width < 8 || seedBand.height < 8 {
		if seedX, seedY, ok := pickCloseupBandSeed(toothness, darkness, gradient, width, region); ok {
			return []contracts.Point{{X: seedX, Y: seedY}}
		}
		return nil
	}

	type closeupSeedScore struct {
		x     uint32
		y     uint32
		score float64
	}
	candidates := make([]closeupSeedScore, 0, int(seedBand.area()/12))
	bandCenterX := float64(region.x) + float64(region.width)/2.0
	for y := seedBand.y; y < seedBand.y+seedBand.height; y++ {
		for x := seedBand.x; x < seedBand.x+seedBand.width; x++ {
			index := int(y*width + x)
			if toothness[index] < 84 || darkness[index] > 165 {
				continue
			}
			score := float64(toothness[index]) -
				float64(darkness[index])*1.10 -
				float64(gradient[index])*0.40 -
				math.Abs(float64(x)-bandCenterX)*0.65
			candidates = append(candidates, closeupSeedScore{x: x, y: y, score: score})
		}
	}
	if len(candidates) == 0 {
		if seedX, seedY, ok := pickCloseupBandSeed(toothness, darkness, gradient, width, region); ok {
			return []contracts.Point{{X: seedX, Y: seedY}}
		}
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	seeds := make([]contracts.Point, 0, 4)
	for _, candidate := range candidates {
		keep := true
		for _, seed := range seeds {
			if absInt(int(candidate.x)-int(seed.X)) <= 18 &&
				absInt(int(candidate.y)-int(seed.Y)) <= 28 {
				keep = false
				break
			}
		}
		if !keep {
			continue
		}
		seeds = append(seeds, contracts.Point{X: candidate.x, Y: candidate.y})
		if len(seeds) == 4 {
			break
		}
	}
	return seeds
}

func detectCloseupSeedCandidate(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width, height uint32,
	search searchRegion,
	growRegion searchRegion,
	seedX, seedY uint32,
	partialBand bool,
	borderPad int,
) (componentCandidate, contracts.ToothGeometry, bool) {
	seedIndex := int(seedY*width + seedX)
	seedIntensity := normalized[seedIndex]
	seedToothness := toothness[seedIndex]
	seedDarkness := darkness[seedIndex]
	if seedToothness < 90 || seedDarkness > 150 {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}

	growThreshold := maxUint8(
		percentileInRegion(toothness, width, growRegion, 0.38),
		clampUint8FromInt(int(seedToothness)-68),
	)
	if growThreshold < 62 {
		growThreshold = 62
	}
	intensityThreshold := clampUint8FromInt(int(seedIntensity) - 45)
	if intensityThreshold < 45 {
		intensityThreshold = 45
	}
	darkThreshold := maxUint8(percentileInRegion(darkness, width, growRegion, 0.92), 78)
	if darkThreshold > 140 {
		darkThreshold = 140
	}
	gradientThreshold := maxUint8(percentileInRegion(gradient, width, growRegion, 0.96), 78)
	if gradientThreshold > 132 {
		gradientThreshold = 132
	}

	mask := traceCloseupSeedMask(
		normalized,
		toothness,
		darkness,
		gradient,
		width,
		growRegion,
		seedX,
		seedY,
		intensityThreshold,
		growThreshold,
		darkThreshold,
		gradientThreshold,
		partialBand,
	)
	growMask := growCloseupSeedMask(
		normalized,
		toothness,
		darkness,
		gradient,
		width,
		height,
		growRegion,
		seedX,
		seedY,
		intensityThreshold,
		growThreshold,
		darkThreshold,
		gradientThreshold,
		borderPad,
		seedToothness,
	)
	if countMaskPixels(mask) > 0 && countMaskPixels(growMask) > 0 {
		mask = mergeCloseupMasksWithinEnvelope(mask, growMask, width, growRegion)
	} else if countMaskPixels(mask) == 0 {
		mask = growMask
	}
	if countMaskPixels(mask) == 0 {
		return componentCandidate{}, contracts.ToothGeometry{}, false
	}

	mask = closeBinaryMask(mask, int(width), int(height))
	mask = closeBinaryMask(mask, int(width), int(height))
	mask = fillHolesBinaryMask(mask, int(width), int(height))

	components := collectMaskComponents(mask, width, height, growRegion, maxUint32(growRegion.area()/500, 250))
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
	merged := mergeCloseupSeedNeighbors(*selected, components, growRegion)
	selected = &merged
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
	bandOccupancy := float64(selected.bbox.Width) / float64(maxUint32(growRegion.width, 1))
	centerX := float64(selected.bbox.X) + float64(selected.bbox.Width)/2.0
	bandCenterX := float64(growRegion.x) + float64(growRegion.width)/2.0
	centerScore := 1.0 - math.Min(math.Abs(centerX-bandCenterX)/(float64(growRegion.width)*0.55), 1.0)

	minWidthRatio := 0.08
	minHeightRatio := 0.38
	minBandOccupancy := 0.26
	maxAspectRatio := 9.0
	minArea := maxUint32(growRegion.area()/80, 1200)
	minBBoxWidth := uint32(40)
	minBBoxHeight := search.height / 3
	if partialBand {
		minWidthRatio = 0.06
		minHeightRatio = 0.12
		minBandOccupancy = 0.08
		maxAspectRatio = 10.0
		minArea = maxUint32(growRegion.area()/180, 220)
		minBBoxWidth = 12
		minBBoxHeight = search.height / 8
	}

	strict := widthRatio >= minWidthRatio &&
		widthRatio <= 0.34 &&
		heightRatio >= minHeightRatio &&
		heightRatio <= 0.98 &&
		aspectRatio >= 1.4 &&
		aspectRatio <= maxAspectRatio &&
		fillRatio >= 0.30 &&
		fillRatio <= 0.95 &&
		bandOccupancy >= minBandOccupancy &&
		bandOccupancy <= 0.94 &&
		meanDarkness <= 105 &&
		meanToothness >= 112

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
	if partialBand {
		score *= 0.65
	}
	if selected.area < minArea ||
		geometry.BoundingBox.Height < minBBoxHeight ||
		geometry.BoundingBox.Width < minBBoxWidth ||
		widthRatio > 0.36 ||
		bandOccupancy < minBandOccupancy-0.04 ||
		bandOccupancy > 0.98 ||
		aspectRatio > maxAspectRatio+0.4 ||
		score < 0.20 {
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

func traceCloseupSeedMask(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width uint32,
	region searchRegion,
	seedX, seedY uint32,
	intensityThreshold uint8,
	growThreshold uint8,
	darkThreshold uint8,
	gradientThreshold uint8,
	partialBand bool,
) []uint8 {
	mask := make([]uint8, len(toothness))
	left, right, ok := traceCloseupRowSpan(
		normalized,
		toothness,
		darkness,
		gradient,
		width,
		region,
		seedY,
		seedX,
		seedX,
		intensityThreshold,
		growThreshold,
		darkThreshold,
		gradientThreshold,
		partialBand,
	)
	if !ok {
		return mask
	}

	fillCloseupRowSpan(mask, width, seedY, left, right)
	traceCloseupSeedDirection(mask, normalized, toothness, darkness, gradient, width, region, seedY, left, right, -1, intensityThreshold, growThreshold, darkThreshold, gradientThreshold, partialBand)
	traceCloseupSeedDirection(mask, normalized, toothness, darkness, gradient, width, region, seedY, left, right, 1, intensityThreshold, growThreshold, darkThreshold, gradientThreshold, partialBand)
	return mask
}

func traceCloseupSeedDirection(
	mask []uint8,
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width uint32,
	region searchRegion,
	startY uint32,
	startLeft uint32,
	startRight uint32,
	step int,
	intensityThreshold uint8,
	growThreshold uint8,
	darkThreshold uint8,
	gradientThreshold uint8,
	partialBand bool,
) {
	prevLeft := startLeft
	prevRight := startRight
	gapRows := 0
	for y := int(startY) + step; y >= int(region.y) && y < int(region.y+region.height); y += step {
		left, right, ok := traceCloseupRowSpan(
			normalized,
			toothness,
			darkness,
			gradient,
			width,
			region,
			uint32(y),
			prevLeft,
			prevRight,
			intensityThreshold,
			growThreshold,
			darkThreshold,
			gradientThreshold,
			partialBand,
		)
		if !ok {
			gapRows++
			if gapRows > 18 {
				return
			}
			continue
		}

		gapRows = 0
		fillCloseupRowSpan(mask, width, uint32(y), left, right)
		prevLeft = left
		prevRight = right
	}
}

func traceCloseupRowSpan(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width uint32,
	region searchRegion,
	y uint32,
	preferredLeft uint32,
	preferredRight uint32,
	intensityThreshold uint8,
	growThreshold uint8,
	darkThreshold uint8,
	gradientThreshold uint8,
	partialBand bool,
) (uint32, uint32, bool) {
	regionRight := region.x + region.width - 1
	preferredCenter := int((preferredLeft + preferredRight) / 2)
	searchRadius := clampInt(int(region.width/7), 10, 24)
	bestX := -1
	bestScore := math.Inf(-1)
	for x := maxInt(preferredCenter-searchRadius, int(region.x)+2); x <= minInt(preferredCenter+searchRadius, int(regionRight)-2); x++ {
		index := int(y*width + uint32(x))
		score := closeupRowPixelScore(normalized[index], toothness[index], darkness[index], gradient[index])
		if toothness[index] < clampUint8FromInt(int(growThreshold)-30) || darkness[index] > clampUint8FromInt(int(darkThreshold)+18) {
			continue
		}
		if score > bestScore {
			bestScore = score
			bestX = x
		}
	}
	if bestX < 0 {
		return 0, 0, false
	}

	left := bestX
	right := bestX
	badRun := 0
	for x := bestX - 1; x >= int(region.x)+2; x-- {
		index := int(y*width + uint32(x))
		if closeupRowPixelAcceptable(normalized[index], toothness[index], darkness[index], gradient[index], intensityThreshold, growThreshold, darkThreshold, gradientThreshold) {
			left = x
			badRun = 0
			continue
		}
		badRun++
		if badRun > 1 {
			break
		}
	}

	badRun = 0
	for x := bestX + 1; x <= int(regionRight)-2; x++ {
		index := int(y*width + uint32(x))
		if closeupRowPixelAcceptable(normalized[index], toothness[index], darkness[index], gradient[index], intensityThreshold, growThreshold, darkThreshold, gradientThreshold) {
			right = x
			badRun = 0
			continue
		}
		badRun++
		if badRun > 1 {
			break
		}
	}

	minSpanWidth := 18
	if !partialBand {
		minSpanWidth = 24
	}
	if right-left+1 < minSpanWidth {
		return 0, 0, false
	}
	if absInt(((left+right)/2)-preferredCenter) > clampInt(int(region.width/7), 18, 36) {
		return 0, 0, false
	}
	return uint32(left), uint32(right), true
}

func closeupRowPixelScore(
	intensity uint8,
	toothness uint8,
	darkness uint8,
	gradient uint8,
) float64 {
	return float64(toothness)*1.15 +
		float64(intensity)*0.28 -
		float64(darkness)*0.95 -
		float64(gradient)*0.42
}

func closeupRowPixelAcceptable(
	intensity uint8,
	toothness uint8,
	darkness uint8,
	gradient uint8,
	intensityThreshold uint8,
	growThreshold uint8,
	darkThreshold uint8,
	gradientThreshold uint8,
) bool {
	if intensity < clampUint8FromInt(int(intensityThreshold)-18) {
		return false
	}
	if toothness < clampUint8FromInt(int(growThreshold)-32) {
		return false
	}
	if darkness > clampUint8FromInt(int(darkThreshold)+18) {
		return false
	}
	if gradient > clampUint8FromInt(int(gradientThreshold)+24) && toothness < clampUint8FromInt(int(growThreshold)+6) {
		return false
	}
	return closeupRowPixelScore(intensity, toothness, darkness, gradient) > 8.0
}

func fillCloseupRowSpan(mask []uint8, width uint32, y uint32, left uint32, right uint32) {
	rowStart := int(y * width)
	for x := left; x <= right; x++ {
		mask[rowStart+int(x)] = 1
	}
}

func mergeCloseupMasksWithinEnvelope(
	primary []uint8,
	supplement []uint8,
	width uint32,
	region searchRegion,
) []uint8 {
	minX, minY, maxX, maxY, ok := maskBoundsInRegion(primary, width, region)
	if !ok {
		return primary
	}

	padX := uint32(clampInt(int((maxX-minX+1)/6), 14, 26))
	padUp := uint32(clampInt(int(region.height/4), 140, 260))
	padDown := uint32(clampInt(int(region.height/10), 40, 100))
	left := maxUint32(region.x, saturatingSubUint32(minX, padX))
	right := minUint32(region.x+region.width-1, maxX+padX)
	top := maxUint32(region.y, saturatingSubUint32(minY, padUp))
	bottom := minUint32(region.y+region.height-1, maxY+padDown)
	merged := append([]uint8(nil), primary...)
	for y := top; y <= bottom; y++ {
		rowStart := int(y * width)
		for x := left; x <= right; x++ {
			index := rowStart + int(x)
			if supplement[index] != 0 {
				merged[index] = 1
			}
		}
	}
	return merged
}

func maskBoundsInRegion(
	mask []uint8,
	width uint32,
	region searchRegion,
) (uint32, uint32, uint32, uint32, bool) {
	found := false
	minX := region.x + region.width - 1
	minY := region.y + region.height - 1
	maxX := region.x
	maxY := region.y
	for y := region.y; y < region.y+region.height; y++ {
		rowStart := int(y * width)
		for x := region.x; x < region.x+region.width; x++ {
			if mask[rowStart+int(x)] == 0 {
				continue
			}
			found = true
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	return minX, minY, maxX, maxY, found
}

func growCloseupSeedMask(
	normalized []uint8,
	toothness []uint8,
	darkness []uint8,
	gradient []uint8,
	width, height uint32,
	region searchRegion,
	seedX, seedY uint32,
	intensityThreshold uint8,
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
		if normalized[index] < intensityThreshold {
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

func componentTouchesRegionBorder(bbox contracts.BoundingBox, region searchRegion, pad uint32) bool {
	regionRight := region.x + region.width - 1
	regionBottom := region.y + region.height - 1
	return bbox.X <= region.x+pad ||
		bbox.Y <= region.y+pad ||
		bbox.X+bbox.Width-1 >= saturatingSubUint32(regionRight, pad) ||
		bbox.Y+bbox.Height-1 >= saturatingSubUint32(regionBottom, pad)
}

func mergeCloseupSeedNeighbors(
	seed maskComponent,
	components []maskComponent,
	region searchRegion,
) maskComponent {
	merged := seed
	used := make([]bool, len(components))
	for index := range components {
		if sameMaskComponent(components[index], seed) {
			used[index] = true
			break
		}
	}

	for {
		changed := false
		for index := range components {
			if used[index] {
				continue
			}
			if !shouldMergeCloseupComponent(merged.bbox, components[index].bbox, region) {
				continue
			}
			merged = mergeMaskComponent(merged, components[index])
			used[index] = true
			changed = true
		}
		if !changed {
			return merged
		}
	}
}

func sameMaskComponent(left, right maskComponent) bool {
	return left.area == right.area &&
		left.bbox == right.bbox &&
		len(left.pixels) == len(right.pixels)
}

func shouldMergeCloseupComponent(
	current contracts.BoundingBox,
	candidate contracts.BoundingBox,
	region searchRegion,
) bool {
	if componentTouchesRegionBorder(candidate, region, 1) {
		return false
	}

	overlapLeft := maxUint32(current.X, candidate.X)
	overlapRight := minUint32(current.X+current.Width-1, candidate.X+candidate.Width-1)
	overlapWidth := uint32(0)
	if overlapRight >= overlapLeft {
		overlapWidth = overlapRight - overlapLeft + 1
	}
	minWidth := minUint32(current.Width, candidate.Width)
	if overlapWidth < maxUint32(minWidth/3, 18) {
		return false
	}

	currentTop := current.Y
	currentBottom := current.Y + current.Height - 1
	candidateTop := candidate.Y
	candidateBottom := candidate.Y + candidate.Height - 1
	if candidateTop > currentBottom {
		return candidateTop-currentBottom <= 96
	}
	if currentTop > candidateBottom {
		return currentTop-candidateBottom <= 96
	}
	return true
}

func mergeMaskComponent(left, right maskComponent) maskComponent {
	pixels := make([]int, 0, len(left.pixels)+len(right.pixels))
	pixels = append(pixels, left.pixels...)
	pixels = append(pixels, right.pixels...)

	minX := minUint32(left.bbox.X, right.bbox.X)
	minY := minUint32(left.bbox.Y, right.bbox.Y)
	maxX := maxUint32(left.bbox.X+left.bbox.Width-1, right.bbox.X+right.bbox.Width-1)
	maxY := maxUint32(left.bbox.Y+left.bbox.Height-1, right.bbox.Y+right.bbox.Height-1)
	return maskComponent{
		pixels: pixels,
		bbox: contracts.BoundingBox{
			X:      minX,
			Y:      minY,
			Width:  maxX - minX + 1,
			Height: maxY - minY + 1,
		},
		area: left.area + right.area,
	}
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
