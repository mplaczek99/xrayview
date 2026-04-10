package render

import "xrayview/backend/internal/imaging"

type RenderPlan struct {
	Window WindowMode
}

func DefaultRenderPlan() RenderPlan {
	return RenderPlan{Window: DefaultWindowMode()}
}

func RenderSourceImage(source imaging.SourceImage, plan RenderPlan) imaging.PreviewImage {
	return imaging.GrayPreview(source.Width, source.Height, RenderGrayscalePixels(source, plan))
}

func RenderGrayscalePixels(source imaging.SourceImage, plan RenderPlan) []uint8 {
	pixels := make([]uint8, len(source.Pixels))
	window := ResolveWindow(source, plan.Window)

	if source.MinValue >= 0 && source.MaxValue <= 65535 {
		lut := buildRenderLUT(source, window)
		for index, value := range source.Pixels {
			pixels[index] = lut[uint16(value+0.5)]
		}
		return pixels
	}

	switch {
	case window != nil && source.Invert:
		for index, value := range source.Pixels {
			pixels[index] = 255 - window.Map(value)
		}
	case window != nil:
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

func buildRenderLUT(source imaging.SourceImage, window *WindowTransform) [65536]uint8 {
	var lut [65536]uint8
	for i := range 65536 {
		value := float32(i)
		var byteValue uint8
		if window != nil {
			byteValue = window.Map(value)
		} else {
			byteValue = MapLinear(value, source.MinValue, source.MaxValue)
		}
		if source.Invert {
			byteValue = 255 - byteValue
		}
		lut[i] = byteValue
	}
	return lut
}
