package analysis

// Overlay helpers paint analysis output onto preview images. The production
// analyze path uses the tooth trace overlay; the debug-dump build also uses a
// mask-region overlay to visualize connected components.

import (
	"fmt"
	"math"

	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/imaging"
)

var toothOverlayColor = [3]uint8{255, 238, 0}
var toothTraceColor = [3]uint8{255, 72, 72}

// overlayRegion carries the flat pixel indices (row-major) belonging to a
// single component. Kept internal — callers build regions from debug data.
type overlayRegion struct {
	pixels []int
}

func OverlayPreviewWithRegions(
	preview imaging.PreviewImage,
	regions []overlayRegion,
) (imaging.PreviewImage, error) {
	if err := preview.Validate(); err != nil {
		return imaging.PreviewImage{}, fmt.Errorf("validate preview image: %w", err)
	}

	rgba, err := rgbaPixelsFromPreview(preview)
	if err != nil {
		return imaging.PreviewImage{}, err
	}
	if len(regions) == 0 {
		return imaging.RGBAPreview(preview.Width, preview.Height, rgba), nil
	}

	width := int(preview.Width)
	height := int(preview.Height)
	mask := make([]uint8, width*height)
	for _, region := range regions {
		for _, index := range region.pixels {
			if index >= 0 && index < len(mask) {
				mask[index] = 1
			}
		}
	}
	// Two closes smooth jagged component edges before feathering; one open
	// despeckles the stray single-pixel hits left by the threshold pass.
	mask = closeBinaryMask(mask, width, height)
	mask = closeBinaryMask(mask, width, height)
	mask = openBinaryMask(mask, width, height)

	blendHighlightMask(rgba, mask, width, height)

	return imaging.RGBAPreview(preview.Width, preview.Height, rgba), nil
}

func OverlayPreviewWithToothTrace(
	preview imaging.PreviewImage,
	analysis contracts.ToothAnalysis,
) (imaging.PreviewImage, error) {
	if err := preview.Validate(); err != nil {
		return imaging.PreviewImage{}, fmt.Errorf("validate preview image: %w", err)
	}

	rgba, err := rgbaPixelsFromPreview(preview)
	if err != nil {
		return imaging.PreviewImage{}, err
	}

	traces, err := ExtractBlackGapTraces(preview)
	if err == nil && len(traces) > 0 {
		for _, trace := range traces {
			if trace.Closed {
				drawClosedPolyline(
					rgba,
					int(preview.Width),
					int(preview.Height),
					trace.Points,
					toothTraceColor,
					0.96,
					2,
				)
			} else {
				drawOpenPolyline(
					rgba,
					int(preview.Width),
					int(preview.Height),
					trace.Points,
					toothTraceColor,
					0.96,
					2,
				)
			}
		}
		return imaging.RGBAPreview(preview.Width, preview.Height, rgba), nil
	}

	if analysis.Tooth != nil && len(analysis.Tooth.Geometry.Outline) >= 2 {
		drawClosedPolyline(
			rgba,
			int(preview.Width),
			int(preview.Height),
			analysis.Tooth.Geometry.Outline,
			toothTraceColor,
			0.96,
			2,
		)
	}

	return imaging.RGBAPreview(preview.Width, preview.Height, rgba), nil
}

func blendHighlightMask(rgba []uint8, mask []uint8, width, height int) {
	feathered := blurBinaryMask(mask, width, height, 3)
	for index, strength := range feathered {
		if strength == 0 {
			continue
		}
		alpha := 0.94 * float64(strength) / 255.0
		if alpha < 0.03 {
			continue
		}

		base := index * 4
		rgba[base+0] = blendChannel(rgba[base+0], toothOverlayColor[0], alpha)
		rgba[base+1] = blendChannel(rgba[base+1], toothOverlayColor[1], alpha)
		rgba[base+2] = blendChannel(rgba[base+2], toothOverlayColor[2], alpha)
		rgba[base+3] = 255
	}
}

func rgbaPixelsFromPreview(preview imaging.PreviewImage) ([]uint8, error) {
	switch preview.Format {
	case imaging.FormatRGBA8:
		return append([]uint8(nil), preview.Pixels...), nil
	case imaging.FormatGray8:
		rgba := make([]uint8, len(preview.Pixels)*4)
		for index, value := range preview.Pixels {
			base := index * 4
			rgba[base+0] = value
			rgba[base+1] = value
			rgba[base+2] = value
			rgba[base+3] = 255
		}
		return rgba, nil
	default:
		return nil, fmt.Errorf("unsupported preview format %q", preview.Format)
	}
}

// blurBinaryMask runs a separable box blur of the given radius over a
// 0/1 mask, returning a grayscale feather used to soften the highlight edge.
func blurBinaryMask(mask []uint8, width, height, radius int) []uint8 {
	if radius <= 0 || len(mask) == 0 {
		return append([]uint8(nil), mask...)
	}

	window := radius*2 + 1
	expanded := make([]uint16, len(mask))
	for index, value := range mask {
		if value != 0 {
			expanded[index] = 255
		}
	}

	horizontal := make([]uint16, len(mask))
	for y := 0; y < height; y++ {
		row := y * width
		var sum uint32
		for x := -radius; x <= radius; x++ {
			sum += uint32(expanded[row+clampOverlayInt(x, 0, width-1)])
		}
		for x := 0; x < width; x++ {
			horizontal[row+x] = uint16(sum / uint32(window))
			left := clampOverlayInt(x-radius, 0, width-1)
			right := clampOverlayInt(x+radius+1, 0, width-1)
			sum += uint32(expanded[row+right])
			sum -= uint32(expanded[row+left])
		}
	}

	blurred := make([]uint8, len(mask))
	for x := 0; x < width; x++ {
		var sum uint32
		for y := -radius; y <= radius; y++ {
			sum += uint32(horizontal[clampOverlayInt(y, 0, height-1)*width+x])
		}
		for y := 0; y < height; y++ {
			blurred[y*width+x] = uint8(sum / uint32(window))
			top := clampOverlayInt(y-radius, 0, height-1)
			bottom := clampOverlayInt(y+radius+1, 0, height-1)
			sum += uint32(horizontal[bottom*width+x])
			sum -= uint32(horizontal[top*width+x])
		}
	}

	return blurred
}

func blendChannel(base uint8, overlay uint8, alpha float64) uint8 {
	return uint8(math.Round(float64(base)*(1.0-alpha) + float64(overlay)*alpha))
}

func drawClosedPolyline(
	rgba []uint8,
	width int,
	height int,
	points []contracts.Point,
	stroke [3]uint8,
	alpha float64,
	radius int,
) {
	if len(points) < 2 {
		return
	}

	for index := range points {
		start := points[index]
		end := points[(index+1)%len(points)]
		drawLine(rgba, width, height, start, end, stroke, alpha, radius)
	}
}

func drawOpenPolyline(
	rgba []uint8,
	width int,
	height int,
	points []contracts.Point,
	stroke [3]uint8,
	alpha float64,
	radius int,
) {
	if len(points) < 2 {
		return
	}

	for index := 0; index < len(points)-1; index++ {
		drawLine(rgba, width, height, points[index], points[index+1], stroke, alpha, radius)
	}
}

func drawLine(
	rgba []uint8,
	width int,
	height int,
	start contracts.Point,
	end contracts.Point,
	stroke [3]uint8,
	alpha float64,
	radius int,
) {
	x0 := int(start.X)
	y0 := int(start.Y)
	x1 := int(end.X)
	y1 := int(end.Y)

	dx := absInt(x1 - x0)
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	dy := -absInt(y1 - y0)
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	errValue := dx + dy

	for {
		blendStrokePoint(rgba, width, height, x0, y0, stroke, alpha, radius)
		if x0 == x1 && y0 == y1 {
			break
		}

		doubleErr := errValue * 2
		if doubleErr >= dy {
			errValue += dy
			x0 += sx
		}
		if doubleErr <= dx {
			errValue += dx
			y0 += sy
		}
	}
}

func blendStrokePoint(
	rgba []uint8,
	width int,
	height int,
	centerX int,
	centerY int,
	stroke [3]uint8,
	alpha float64,
	radius int,
) {
	for y := centerY - radius; y <= centerY+radius; y++ {
		if y < 0 || y >= height {
			continue
		}
		for x := centerX - radius; x <= centerX+radius; x++ {
			if x < 0 || x >= width {
				continue
			}
			if (x-centerX)*(x-centerX)+(y-centerY)*(y-centerY) > radius*radius {
				continue
			}

			base := (y*width + x) * 4
			rgba[base+0] = blendChannel(rgba[base+0], stroke[0], alpha)
			rgba[base+1] = blendChannel(rgba[base+1], stroke[1], alpha)
			rgba[base+2] = blendChannel(rgba[base+2], stroke[2], alpha)
			rgba[base+3] = 255
		}
	}
}

func clampOverlayInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
