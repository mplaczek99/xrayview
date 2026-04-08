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

	for index, value := range source.Pixels {
		var byteValue uint8
		if window != nil {
			byteValue = window.Map(value)
		} else {
			byteValue = MapLinear(value, source.MinValue, source.MaxValue)
		}

		if source.Invert {
			byteValue = 255 - byteValue
		}

		pixels[index] = byteValue
	}

	return pixels
}
