package analysis

import (
	_ "embed"
	"fmt"
	"runtime"
	"sync"

	"xrayview/backend/internal/render"
)

const (
	toothGreen = byte(120)
	boneRed    = byte(255)

	sectionFillAlpha = byte(115)

	minimumBoneAreaFloorPixels  = 24
	minimumToothAreaFloorPixels = 4

	toothOutlineThicknessPixels = 2
	boneOutlineThicknessPixels  = 2
	toothMaskCloseRadiusPixels  = 2

	boneToothCutoutBridgeRadiusPixels = 24
	radiographBackgroundMaxGray       = 2
	boneEdgeSnapRadiusPixels          = 14

	// Forest score at/above this is the class. The trainer regresses toward
	// {0, 1}; both cuts are where balanced accuracy peaks on the labeled set.
	// Tooth 0.55 stops greening the isodense bone; bone 0.50 is balanced.
	toothScoreThreshold = 0.55
	boneScoreThreshold  = 0.50
)

// Gradient-boosted forests (XVLM2), trained offline on position-free texture
// features (see tooth_model.go). Baked into the binary via go:embed.
//
//go:embed assets/learned_model.bin
var toothModelData []byte

//go:embed assets/bone_model.bin
var boneModelData []byte

var (
	toothModelOnce sync.Once
	toothModel     *toothForest
	toothModelErr  error

	boneModelOnce sync.Once
	boneModel     *toothForest
	boneModelErr  error
)

// OverlayResult is the public analysis output. Preview is the outline view;
// FilledPreview is the translucent sections view.
type OverlayResult struct {
	Preview        render.PreviewImage
	FilledPreview  render.PreviewImage
	ToothPixels    int
	BonePixels     int
	Coverage       float64
	CandidateCount int
	Mode           string
}

// GenerateToothOverlay runs the full tooth and bone overlay pipeline.
func GenerateToothOverlay(preview render.PreviewImage) (OverlayResult, error) {
	if preview.Format != render.Gray8 {
		return OverlayResult{}, fmt.Errorf("tooth analysis requires Gray8 preview input")
	}
	width := int(preview.Width)
	height := int(preview.Height)
	if width < 8 || height < 8 {
		return OverlayResult{}, fmt.Errorf("image is too small for tooth analysis: %dx%d", preview.Width, preview.Height)
	}
	expectedPixels, ok := checkedPixelCount(width, height)
	if !ok {
		return OverlayResult{}, fmt.Errorf("preview dimensions overflow")
	}
	if len(preview.Pixels) != expectedPixels {
		return OverlayResult{}, fmt.Errorf("preview pixel length = %d, want %d", len(preview.Pixels), expectedPixels)
	}

	// Normalize once, then build the texture feature planes once — both the
	// tooth and bone forests score off the same integral images.
	normalized := normalizeGray(preview.Pixels)
	planes := buildFeaturePlanes(normalized, width, height)
	buffers := newMaskBuffers(expectedPixels)
	toothMask := detectToothMask(planes, width, height, buffers)
	boneMask := detectBoneLineMask(planes, width, height, buffers)

	toothPixels := countMask(toothMask)
	bonePixels := countMask(boneMask)
	coverage := float64(toothPixels+bonePixels) / float64(max(len(preview.Pixels), 1))
	candidateCount := countComponents(toothMask, width, height, buffers.visited)

	mode := "dynamic tooth and bone level overlay"
	if toothPixels < len(preview.Pixels)/150 || candidateCount == 0 {
		mode += "; no reliable tooth mask found"
	}
	if bonePixels < width/8 {
		mode += "; no reliable bone level found"
	}

	boneSection := boneSectionMaskWithIgnoredCutouts(preview.Pixels, boneMask, toothMask, width, height, buffers)
	snapMaskToImageEdge(boneSection, width, height, boneEdgeSnapRadiusPixels, buffers)

	return OverlayResult{
		Preview:        overlayPreview(preview.Pixels, preview.Width, preview.Height, toothMask, boneSection, false, buffers),
		FilledPreview:  overlayPreview(preview.Pixels, preview.Width, preview.Height, toothMask, boneSection, true, buffers),
		ToothPixels:    toothPixels,
		BonePixels:     bonePixels,
		Coverage:       coverage,
		CandidateCount: candidateCount,
		Mode:           mode,
	}, nil
}

type maskBuffers struct {
	a       []bool
	b       []bool
	scratch []bool
	visited []bool
}

func newMaskBuffers(length int) *maskBuffers {
	return &maskBuffers{
		a:       make([]bool, length),
		b:       make([]bool, length),
		scratch: make([]bool, length),
		visited: make([]bool, length),
	}
}

// detectToothMask runs the gradient-boosted forest over texture features (the
// discriminator that actually separates isodense tooth from bone), then hands
// the raw mask to cleanToothMask for morphological polish. If the forest asset
// fails to load it falls back to a high-percentile intensity threshold — which
// CANNOT separate tooth from bone, so it is a last resort to avoid an empty
// overlay, not a real detector.
func detectToothMask(planes *featurePlanes, width, height int, buffers *maskBuffers) []bool {
	var mask []bool
	if forest, ok := loadedToothModel(); ok {
		mask = forestScoreMask(forest, planes, toothScoreThreshold)
	} else {
		normalized := planes.normalized
		threshold := max(int(percentile(normalized, 82)), 24)
		mask = make([]bool, len(normalized))
		for index, value := range normalized {
			mask[index] = int(value) >= threshold
		}
	}
	return cleanToothMask(mask, width, height, buffers)
}

// forestScoreMask thresholds the forest score at every pixel. Per-pixel work is
// independent and integral-image feature lookups are O(1), so it parallelizes
// over row ranges.
func forestScoreMask(forest *toothForest, planes *featurePlanes, threshold float64) []bool {
	width := planes.width
	height := planes.height
	mask := make([]bool, width*height)
	workers := min(max(runtime.NumCPU(), 1), max(height, 1))
	chunk := (height + workers - 1) / workers
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		yStart := w * chunk
		if yStart >= height {
			break
		}
		yEnd := min(yStart+chunk, height)
		wg.Add(1)
		go func(yStart, yEnd int) {
			defer wg.Done()
			for y := yStart; y < yEnd; y++ {
				row := y * width
				for x := 0; x < width; x++ {
					features := planes.features(x, y)
					mask[row+x] = forest.score(&features) >= threshold
				}
			}
		}(yStart, yEnd)
	}
	wg.Wait()
	return mask
}

func cleanToothMask(mask []bool, width, height int, buffers *maskBuffers) []bool {
	if width == 0 || height == 0 || len(mask) != width*height {
		return append([]bool(nil), mask...)
	}
	minArea := minimumToothAreaPixels(width, height)
	removeSmallComponentsInto(mask, width, height, minArea, buffers.a, buffers.visited)
	cleaned := append([]bool(nil), buffers.a...)
	closeRadius := toothMaskCloseRadius(width, height)
	if closeRadius > 0 {
		closeMaskInto(cleaned, width, height, closeRadius, buffers)
		copy(cleaned, buffers.b)
	}
	fillHolesInto(cleaned, width, height, buffers.a)
	removeSmallComponentsInto(buffers.a, width, height, minArea, buffers.b, buffers.visited)
	return append([]bool(nil), buffers.b...)
}

func toothMaskCloseRadius(width, height int) int {
	return min(toothMaskCloseRadiusPixels, min(width, height)/24)
}

func minimumToothAreaPixels(width, height int) int {
	return clamp(width*height/1000, minimumToothAreaFloorPixels, 2048)
}

// detectBoneLineMask uses the same position-free texture forest as the tooth
// detector, trained on the red (bone) label. Returns an empty mask if the asset
// can't load (bone is an optional overlay — better blank than a bad guess).
func detectBoneLineMask(planes *featurePlanes, width, height int, buffers *maskBuffers) []bool {
	forest, ok := loadedBoneModel()
	if !ok {
		return make([]bool, width*height)
	}
	mask := forestScoreMask(forest, planes, boneScoreThreshold)

	// Light cleanup: close hairline gaps, drop specks, fill interior holes. The
	// heavier section shaping happens later in boneSectionMaskWithIgnoredCutouts.
	closeMaskInto(mask, width, height, 1, buffers)
	copy(mask, buffers.b)
	removeSmallComponentsInto(mask, width, height, minimumBoneAreaPixels(width, height), buffers.a, buffers.visited)
	copy(mask, buffers.a)
	fillHolesInto(mask, width, height, buffers.a)
	copy(mask, buffers.a)
	return mask
}

func overlayPreview(gray []byte, width, height uint32, toothMask, boneMask []bool, fillSections bool, buffers *maskBuffers) render.PreviewImage {
	widthN := int(width)
	heightN := int(height)
	pixels := grayscaleRGBA(gray)
	if fillSections {
		blendMaskFill(pixels, boneMask, [4]byte{boneRed, 0, 0, 255}, sectionFillAlpha, toothMask)
		blendMaskFill(pixels, toothMask, [4]byte{toothGreen, 255, 0, 255}, sectionFillAlpha, nil)
	}
	boneOutline := centeredOutlineMask(boneMask, widthN, heightN, boneOutlineThicknessPixels, buffers)
	compositeMaskFill(pixels, boneOutline, [4]byte{boneRed, 0, 0, 255}, toothMask)
	toothOutline := innerOutlineMask(toothMask, widthN, heightN, toothOutlineThicknessPixels, buffers)
	compositeMaskFill(pixels, toothOutline, [4]byte{toothGreen, 255, 0, 255}, nil)
	return render.RGBA(width, height, pixels)
}

func grayscaleRGBA(gray []byte) []byte {
	pixels := make([]byte, 0, len(gray)*4)
	for _, value := range gray {
		pixels = append(pixels, value, value, value, 255)
	}
	return pixels
}

func compositeMaskFill(pixels []byte, mask []bool, color [4]byte, excludeMask []bool) {
	if len(mask)*4 != len(pixels) {
		return
	}
	for index, value := range mask {
		if !value || boolAt(excludeMask, index) {
			continue
		}
		base := index * 4
		pixels[base] = color[0]
		pixels[base+1] = color[1]
		pixels[base+2] = color[2]
		pixels[base+3] = 255
	}
}

func blendMaskFill(pixels []byte, mask []bool, color [4]byte, alpha byte, excludeMask []bool) {
	if len(mask)*4 != len(pixels) {
		return
	}
	a := uint32(alpha)
	inv := uint32(255) - a
	for index, value := range mask {
		if !value || boolAt(excludeMask, index) {
			continue
		}
		base := index * 4
		pixels[base] = blendChannel(pixels[base], color[0], a, inv)
		pixels[base+1] = blendChannel(pixels[base+1], color[1], a, inv)
		pixels[base+2] = blendChannel(pixels[base+2], color[2], a, inv)
		pixels[base+3] = 255
	}
}

func blendChannel(dst, src byte, alpha, invAlpha uint32) byte {
	return byte((uint32(src)*alpha + uint32(dst)*invAlpha + 127) / 255)
}

func boneSectionMaskWithIgnoredCutouts(gray []byte, boneMask, toothMask []bool, width, height int, buffers *maskBuffers) []bool {
	if len(boneMask) == 0 || len(toothMask) != len(boneMask) {
		return append([]bool(nil), boneMask...)
	}
	sectionMask := append([]bool(nil), boneMask...)
	radius := boneToothCutoutBridgeRadius(width, height)
	if radius == 0 || width < 8 || height < 8 {
		return sectionMask
	}
	dilateMaskInto(boneMask, width, height, radius, buffers.scratch, buffers.a)
	dilateMaskInto(toothMask, width, height, radius, buffers.scratch, buffers.b)
	for index := range sectionMask {
		if buffers.b[index] && buffers.a[index] {
			sectionMask[index] = true
		}
	}
	closeMaskInto(sectionMask, width, height, clamp(radius/2, 1, 8), buffers)
	fillHolesInto(buffers.b, width, height, buffers.a)
	removeSmallComponentsInto(buffers.a, width, height, minimumBoneOutlineAreaPixels(width, height), buffers.b, buffers.visited)
	cleaned := append([]bool(nil), buffers.b...)
	clearBorderBackgroundFromMask(cleaned, gray, width, height, buffers.visited)
	return cleaned
}

func snapMaskToImageEdge(mask []bool, width, height, snapRadius int, buffers *maskBuffers) {
	if snapRadius == 0 || width == 0 || height == 0 || len(mask) != width*height || len(buffers.a) != len(mask) {
		return
	}
	dilateMaskInto(mask, width, height, snapRadius, buffers.scratch, buffers.a)
	strip := min(snapRadius, height)
	for y := 0; y < strip; y++ {
		for x := 0; x < width; x++ {
			if buffers.a[y*width+x] {
				mask[y*width+x] = true
			}
		}
	}
	for y := max(height-strip, 0); y < height; y++ {
		for x := 0; x < width; x++ {
			if buffers.a[y*width+x] {
				mask[y*width+x] = true
			}
		}
	}
	hStrip := min(snapRadius, width)
	for y := 0; y < height; y++ {
		row := y * width
		for x := 0; x < hStrip; x++ {
			if buffers.a[row+x] {
				mask[row+x] = true
			}
		}
		for x := max(width-hStrip, 0); x < width; x++ {
			if buffers.a[row+x] {
				mask[row+x] = true
			}
		}
	}
}

func boneToothCutoutBridgeRadius(width, height int) int {
	return min(boneToothCutoutBridgeRadiusPixels, max(boneOutlineThicknessPixels, min(width, height)/32))
}

func minimumBoneOutlineAreaPixels(width, height int) int {
	return clamp(width*height/1000, 16, 128)
}

func innerOutlineMask(mask []bool, width, height, thickness int, buffers *maskBuffers) []bool {
	if thickness == 0 || len(mask) == 0 {
		return append([]bool(nil), mask...)
	}
	notMask := invertMask(mask)
	dilateMaskInto(notMask, width, height, thickness, buffers.scratch, buffers.a)
	outline := make([]bool, len(mask))
	for index := range outline {
		outline[index] = mask[index] && buffers.a[index]
	}
	return outline
}

func centeredOutlineMask(mask []bool, width, height, thickness int, buffers *maskBuffers) []bool {
	if thickness == 0 || len(mask) == 0 {
		return append([]bool(nil), mask...)
	}
	dilateMaskInto(mask, width, height, thickness, buffers.scratch, buffers.a)
	notMask := invertMask(mask)
	dilateMaskInto(notMask, width, height, thickness, buffers.scratch, buffers.b)
	outline := make([]bool, len(mask))
	for index := range outline {
		outline[index] = buffers.a[index] && buffers.b[index]
	}
	return outline
}

func clearBorderBackgroundFromMask(mask []bool, gray []byte, width, height int, visited []bool) {
	if len(mask) == 0 || len(gray) != len(mask) || len(visited) != len(mask) || width == 0 || height == 0 {
		return
	}
	threshold := radiographBackgroundThreshold(gray)
	clear(visited)
	queue := make([]int, 0)
	push := func(index int) {
		if index >= len(mask) || visited[index] || gray[index] > threshold {
			return
		}
		visited[index] = true
		mask[index] = false
		queue = append(queue, index)
	}
	for x := 0; x < width; x++ {
		push(x)
		push((height-1)*width + x)
	}
	for y := 1; y < height; y++ {
		push(y * width)
		push(y*width + width - 1)
	}
	for head := 0; head < len(queue); head++ {
		index := queue[head]
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
}

func radiographBackgroundThreshold(gray []byte) byte {
	if percentileFraction(gray, 0.01) > radiographBackgroundMaxGray {
		return 0
	}
	return radiographBackgroundMaxGray
}

func dilateMaskInto(mask []bool, width, height, radius int, scratch, output []bool) {
	if radius == 0 || len(mask) == 0 {
		copy(output, mask)
		return
	}
	if width == 0 || height == 0 || len(mask) != width*height {
		clear(output)
		return
	}
	clear(scratch)
	for y := 0; y < height; y++ {
		row := y * width
		count := 0
		for x := 0; x <= min(radius, width-1); x++ {
			if mask[row+x] {
				count++
			}
		}
		for x := 0; x < width; x++ {
			scratch[row+x] = count > 0
			if x >= radius && mask[row+x-radius] {
				count--
			}
			add := x + radius + 1
			if add < width && mask[row+add] {
				count++
			}
		}
	}
	counts := make([]int, width)
	for y := 0; y <= min(radius, height-1); y++ {
		row := y * width
		for x := 0; x < width; x++ {
			if scratch[row+x] {
				counts[x]++
			}
		}
	}
	clear(output)
	for y := 0; y < height; y++ {
		row := y * width
		for x := 0; x < width; x++ {
			output[row+x] = counts[x] > 0
		}
		if y >= radius {
			removeRow := (y - radius) * width
			for x := 0; x < width; x++ {
				if scratch[removeRow+x] {
					counts[x]--
				}
			}
		}
		add := y + radius + 1
		if add < height {
			addRow := add * width
			for x := 0; x < width; x++ {
				if scratch[addRow+x] {
					counts[x]++
				}
			}
		}
	}
}

func erodeMaskInto(mask []bool, width, height, radius int, scratch, output []bool) {
	if radius == 0 || len(mask) == 0 {
		copy(output, mask)
		return
	}
	if width == 0 || height == 0 || len(mask) != width*height {
		clear(output)
		return
	}
	window := radius*2 + 1
	clear(scratch)
	for y := 0; y < height; y++ {
		row := y * width
		count := 0
		for x := 0; x <= min(radius, width-1); x++ {
			if mask[row+x] {
				count++
			}
		}
		for x := 0; x < width; x++ {
			scratch[row+x] = count == window
			if x >= radius && mask[row+x-radius] {
				count--
			}
			add := x + radius + 1
			if add < width && mask[row+add] {
				count++
			}
		}
	}
	counts := make([]int, width)
	for y := 0; y <= min(radius, height-1); y++ {
		row := y * width
		for x := 0; x < width; x++ {
			if scratch[row+x] {
				counts[x]++
			}
		}
	}
	clear(output)
	for y := 0; y < height; y++ {
		row := y * width
		for x := 0; x < width; x++ {
			output[row+x] = counts[x] == window
		}
		if y >= radius {
			removeRow := (y - radius) * width
			for x := 0; x < width; x++ {
				if scratch[removeRow+x] {
					counts[x]--
				}
			}
		}
		add := y + radius + 1
		if add < height {
			addRow := add * width
			for x := 0; x < width; x++ {
				if scratch[addRow+x] {
					counts[x]++
				}
			}
		}
	}
}

func closeMaskInto(mask []bool, width, height, radius int, buffers *maskBuffers) {
	dilateMaskInto(mask, width, height, radius, buffers.scratch, buffers.a)
	erodeMaskInto(buffers.a, width, height, radius, buffers.scratch, buffers.b)
}

func removeSmallComponentsInto(mask []bool, width, height, minArea int, output, visited []bool) {
	if minArea <= 1 || width == 0 || height == 0 || len(mask) != width*height || len(visited) != len(mask) {
		copy(output, mask)
		return
	}
	clear(output)
	clear(visited)
	stack := make([]int, 0)
	component := make([]int, 0)
	for start := range mask {
		if visited[start] || !mask[start] {
			continue
		}
		visited[start] = true
		stack = append(stack, start)
		component = component[:0]
		for len(stack) > 0 {
			index := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			component = append(component, index)
			x := index % width
			y := index / width
			for yy := max(y-1, 0); yy <= min(y+1, height-1); yy++ {
				for xx := max(x-1, 0); xx <= min(x+1, width-1); xx++ {
					neighbor := yy*width + xx
					if visited[neighbor] || !mask[neighbor] {
						continue
					}
					visited[neighbor] = true
					stack = append(stack, neighbor)
				}
			}
		}
		if len(component) >= minArea {
			for _, index := range component {
				output[index] = true
			}
		}
	}
}

func fillHolesInto(mask []bool, width, height int, output []bool) {
	if width == 0 || height == 0 || len(mask) != width*height {
		copy(output, mask)
		return
	}
	clear(output)
	queue := make([]int, 0)
	push := func(index int) {
		if !mask[index] && !output[index] {
			output[index] = true
			queue = append(queue, index)
		}
	}
	for x := 0; x < width; x++ {
		push(x)
		push((height-1)*width + x)
	}
	for y := 0; y < height; y++ {
		push(y * width)
		push(y*width + width - 1)
	}
	for head := 0; head < len(queue); head++ {
		index := queue[head]
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
	for index := range output {
		outside := output[index]
		output[index] = mask[index] || !outside
	}
}

func minimumBoneAreaPixels(width, height int) int {
	return max(width*height/12000, minimumBoneAreaFloorPixels)
}

func normalizeGray(pixels []byte) []byte {
	low, high := percentileFractionBounds(pixels, 0.01, 0.99)
	if high <= low {
		return append([]byte(nil), pixels...)
	}
	valueRange := int(high) - int(low)
	var lut [256]byte
	for value := range lut {
		switch {
		case value <= int(low):
			lut[value] = 0
		case value >= int(high):
			lut[value] = 255
		default:
			lut[value] = byte(((value-int(low))*255 + valueRange/2) / valueRange)
		}
	}
	normalized := make([]byte, len(pixels))
	for index, value := range pixels {
		normalized[index] = lut[value]
	}
	return normalized
}

func percentileFractionBounds(pixels []byte, lowPercentile, highPercentile float64) (byte, byte) {
	if len(pixels) == 0 {
		return 0, 0
	}
	var histogram [256]int
	for _, value := range pixels {
		histogram[value]++
	}
	lowTarget := percentileFractionTarget(len(pixels), lowPercentile)
	highTarget := percentileFractionTarget(len(pixels), highPercentile)
	lowValue := -1
	cumulative := 0
	for value, count := range histogram {
		cumulative += count
		if lowValue < 0 && cumulative > lowTarget {
			lowValue = value
		}
		if cumulative > highTarget {
			if lowValue < 0 {
				lowValue = value
			}
			return byte(lowValue), byte(value)
		}
	}
	if lowValue < 0 {
		lowValue = 255
	}
	return byte(lowValue), 255
}

func percentile(pixels []byte, value int) byte {
	return percentileFraction(pixels, float64(value)/100)
}

func percentileFraction(pixels []byte, percentile float64) byte {
	if len(pixels) == 0 {
		return 0
	}
	var histogram [256]int
	for _, value := range pixels {
		histogram[value]++
	}
	target := percentileFractionTarget(len(pixels), percentile)
	cumulative := 0
	for value, count := range histogram {
		cumulative += count
		if cumulative > target {
			return byte(value)
		}
	}
	return 255
}

func percentileFractionTarget(length int, percentile float64) int {
	target := roundFloat(float64(length-1) * percentile)
	return clamp(target, 0, length-1)
}

func gradientGray(pixels []byte, width, height int) []byte {
	gradient := make([]byte, len(pixels))
	if width < 3 || height < 3 {
		return gradient
	}
	for y := 1; y < height-1; y++ {
		for x := 1; x < width-1; x++ {
			value := absInt(int(pixels[y*width+x+1])-int(pixels[y*width+x-1])) +
				absInt(int(pixels[(y+1)*width+x])-int(pixels[(y-1)*width+x]))
			gradient[y*width+x] = byte(min(value, 255))
		}
	}
	return gradient
}

func loadedToothModel() (*toothForest, bool) {
	toothModelOnce.Do(func() {
		toothModel, toothModelErr = decodeToothForest(toothModelData)
	})
	return toothModel, toothModelErr == nil
}

func loadedBoneModel() (*toothForest, bool) {
	boneModelOnce.Do(func() {
		boneModel, boneModelErr = decodeToothForest(boneModelData)
	})
	return boneModel, boneModelErr == nil
}

func countMask(mask []bool) int {
	count := 0
	for _, value := range mask {
		if value {
			count++
		}
	}
	return count
}

func countComponents(mask []bool, width, height int, visited []bool) int {
	if width == 0 || height == 0 || len(mask) != width*height || len(visited) != len(mask) {
		return 0
	}
	clear(visited)
	count := 0
	stack := make([]int, 0)
	for start := range mask {
		if visited[start] || !mask[start] {
			continue
		}
		count++
		visited[start] = true
		stack = append(stack, start)
		for len(stack) > 0 {
			index := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x := index % width
			y := index / width
			for yy := max(y-1, 0); yy <= min(y+1, height-1); yy++ {
				for xx := max(x-1, 0); xx <= min(x+1, width-1); xx++ {
					neighbor := yy*width + xx
					if visited[neighbor] || !mask[neighbor] {
						continue
					}
					visited[neighbor] = true
					stack = append(stack, neighbor)
				}
			}
		}
	}
	return count
}

func invertMask(mask []bool) []bool {
	out := make([]bool, len(mask))
	for index, value := range mask {
		out[index] = !value
	}
	return out
}

func boolAt(values []bool, index int) bool {
	return index >= 0 && index < len(values) && values[index]
}

func checkedPixelCount(width, height int) (int, bool) {
	if width < 0 || height < 0 || (width != 0 && height > int(^uint(0)>>1)/width) {
		return 0, false
	}
	return width * height, true
}

func clamp(value, low, high int) int {
	return min(max(value, low), high)
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func roundFloat(value float64) int {
	if value < 0 {
		return int(value - 0.5)
	}
	return int(value + 0.5)
}
