package analysis

import (
	"fmt"
	"math"

	"xrayview/backend/internal/imaging"
)

var toothOverlayGreen = [3]uint8{102, 255, 0}

// AnalyzeAlgorithmVersion is part of the Analyze result cache key. Change it
// only when the generated overlay semantics intentionally change.
const AnalyzeAlgorithmVersion = "outline-scaled-components-v1"

const minimumToothAreaFloorPixels = 51
const toothOutlineThicknessPixels = 2

type ToothOverlayResult struct {
	Preview        imaging.PreviewImage
	ToothPixels    int
	Coverage       float64
	CandidateCount int
	Mode           string
}

type component struct {
	pixels  []int
	minX    int
	minY    int
	maxX    int
	maxY    int
	area    int
	seedHit bool
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

	mask := detectToothMask(gray, width, height)
	components := collectComponents(mask, mask, width, height, minimumToothAreaPixels(width, height))

	toothPixels := countMaskPixels(mask)
	coverage := float64(toothPixels) / float64(maxInt(len(mask), 1))
	mode := "dynamic tooth outline overlay"
	if toothPixels < len(mask)/150 || len(components) == 0 {
		mode = "dynamic tooth outline overlay; no reliable tooth mask found"
	}

	return ToothOverlayResult{
		Preview:        overlayMask(gray, innerOutlineMask(mask, width, height, toothOutlineThicknessPixels), preview.Width, preview.Height),
		ToothPixels:    toothPixels,
		Coverage:       coverage,
		CandidateCount: len(components),
		Mode:           mode,
	}, nil
}

func detectToothMask(gray []uint8, width, height int) []uint8 {
	normalized := normalizeGray(gray)
	mask := featureTableToothMask(normalized, width, height)
	mask = closeBinaryMask(mask, width, height, 1)
	mask = openBinaryMask(mask, width, height, 1)
	mask = fillHolesBinaryMask(mask, width, height)
	mask = removeSmallMaskComponents(mask, width, height, minimumToothAreaPixels(width, height))
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

func overlayMask(gray []uint8, mask []uint8, width, height uint32) imaging.PreviewImage {
	rgba := make([]uint8, len(gray)*4)
	for index, value := range gray {
		base := index * 4
		if mask[index] != 0 {
			rgba[base+0] = toothOverlayGreen[0]
			rgba[base+1] = toothOverlayGreen[1]
			rgba[base+2] = toothOverlayGreen[2]
			rgba[base+3] = 255
			continue
		}

		rgba[base+0] = value
		rgba[base+1] = value
		rgba[base+2] = value
		rgba[base+3] = 255
	}
	return imaging.RGBAPreview(width, height, rgba)
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
	for _, candidate := range collectComponents(mask, mask, width, height, minArea) {
		for _, index := range candidate.pixels {
			filtered[index] = 1
		}
	}
	return filtered
}

func minimumToothAreaPixels(width, height int) int {
	return maxInt(width*height/2000, minimumToothAreaFloorPixels)
}

func collectComponents(mask []uint8, seeds []uint8, width, height, minArea int) []component {
	visited := make([]bool, len(mask))
	queue := make([]int, 0, 512)
	components := make([]component, 0)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			start := y*width + x
			if visited[start] || mask[start] == 0 {
				continue
			}

			visited[start] = true
			queue = append(queue[:0], start)
			pixels := make([]int, 0, 512)
			minX, maxX := x, x
			minY, maxY := y, y
			seedHit := false
			for head := 0; head < len(queue); head++ {
				index := queue[head]
				pixels = append(pixels, index)
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
						if visited[neighbor] || mask[neighbor] == 0 {
							continue
						}
						visited[neighbor] = true
						queue = append(queue, neighbor)
					}
				}
			}

			if len(pixels) < minArea {
				continue
			}
			components = append(components, component{
				pixels:  pixels,
				minX:    minX,
				minY:    minY,
				maxX:    maxX,
				maxY:    maxY,
				area:    len(pixels),
				seedHit: seedHit,
			})
		}
	}

	return components
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

	eroded := erodeBinaryMask(mask, width, height, thickness)
	outline := make([]uint8, len(mask))
	for index, value := range mask {
		if value != 0 && eroded[index] == 0 {
			outline[index] = 1
		}
	}
	return outline
}

func closeBinaryMask(mask []uint8, width, height, radius int) []uint8 {
	return erodeBinaryMask(dilateBinaryMask(mask, width, height, radius), width, height, radius)
}

func openBinaryMask(mask []uint8, width, height, radius int) []uint8 {
	return dilateBinaryMask(erodeBinaryMask(mask, width, height, radius), width, height, radius)
}

func dilateBinaryMask(mask []uint8, width, height, radius int) []uint8 {
	if radius <= 0 || len(mask) == 0 {
		return append([]uint8(nil), mask...)
	}

	out := make([]uint8, len(mask))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			filled := false
			for ny := maxInt(y-radius, 0); ny <= minInt(y+radius, height-1) && !filled; ny++ {
				for nx := maxInt(x-radius, 0); nx <= minInt(x+radius, width-1); nx++ {
					if mask[ny*width+nx] != 0 {
						filled = true
						break
					}
				}
			}
			if filled {
				out[y*width+x] = 1
			}
		}
	}
	return out
}

func erodeBinaryMask(mask []uint8, width, height, radius int) []uint8 {
	if radius <= 0 || len(mask) == 0 {
		return append([]uint8(nil), mask...)
	}

	out := make([]uint8, len(mask))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			filled := true
			for ny := y - radius; ny <= y+radius && filled; ny++ {
				for nx := x - radius; nx <= x+radius; nx++ {
					if nx < 0 || nx >= width || ny < 0 || ny >= height || mask[ny*width+nx] == 0 {
						filled = false
						break
					}
				}
			}
			if filled {
				out[y*width+x] = 1
			}
		}
	}
	return out
}

func fillHolesBinaryMask(mask []uint8, width, height int) []uint8 {
	if len(mask) == 0 {
		return nil
	}

	outside := make([]bool, len(mask))
	queue := make([]int, 0, width*2+height*2)
	push := func(index int) {
		if index < 0 || index >= len(mask) || outside[index] || mask[index] != 0 {
			return
		}
		outside[index] = true
		queue = append(queue, index)
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

	filled := append([]uint8(nil), mask...)
	for index := range filled {
		if filled[index] == 0 && !outside[index] {
			filled[index] = 1
		}
	}
	return filled
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
