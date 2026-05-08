package render

import (
	"xrayview/backend/internal/bufpool"
	"xrayview/backend/internal/imaging"
)

type RenderPlan struct {
	Window WindowMode
}

func DefaultRenderPlan() RenderPlan {
	return RenderPlan{Window: DefaultWindowMode()}
}

func RenderSourceImage(source imaging.SourceImage, plan RenderPlan) imaging.PreviewImage {
	return imaging.GrayPreviewWithRelease(
		source.Width,
		source.Height,
		RenderGrayscalePixels(source, plan),
		bufpool.PutUint8,
	)
}

func RenderGrayscalePixels(source imaging.SourceImage, plan RenderPlan) []uint8 {
	pixels := bufpool.GetUint8(len(source.Pixels))
	window, hasWindow := resolveWindow(source, plan.Window)

	// LUT fast path. When the source's modality values fit a uint16 index
	// we precompute a 65k-entry table once and turn each per-pixel
	// window/invert/map into a single cache-friendly read. The else branch
	// below handles sources whose range doesn't fit (signed CT, rescaled
	// PET) with the old branchy per-pixel path.
	if source.MinValue >= 0 && source.MaxValue <= 65535 {
		lut := buildRenderLUT(source, window, hasWindow)
		for index, value := range source.Pixels {
			pixels[index] = lut[uint16(value+0.5)]
		}
		return pixels
	}

	switch {
	case hasWindow && source.Invert:
		for index, value := range source.Pixels {
			pixels[index] = 255 - window.Map(value)
		}
	case hasWindow:
		for index, value := range source.Pixels {
			pixels[index] = window.Map(value)
		}
	case source.Invert:
		for index, value := range source.Pixels {
			pixels[index] = 255 - MapLinear(value, source.MinValue, source.MaxValue)
		}
	default:
		for index, value := range source.Pixels {
			pixels[index] = MapLinear(value, source.MinValue, source.MaxValue)
		}
	}

	return pixels
}

func buildRenderLUT(source imaging.SourceImage, window WindowTransform, hasWindow bool) [65536]uint8 {
	var lut [65536]uint8

	// Step 1.3: hoist window-nil and invert checks outside the loop.
	// Each branch runs a branchless inner loop — no per-entry conditionals.
	switch {
	case hasWindow && source.Invert:
		for i := range 65536 {
			lut[i] = 255 - window.Map(float32(i))
		}
	case hasWindow:
		for i := range 65536 {
			lut[i] = window.Map(float32(i))
		}
	case source.Invert:
		for i := range 65536 {
			lut[i] = 255 - MapLinear(float32(i), source.MinValue, source.MaxValue)
		}
	default:
		for i := range 65536 {
			lut[i] = MapLinear(float32(i), source.MinValue, source.MaxValue)
		}
	}

	return lut
}
