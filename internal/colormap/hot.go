package colormap

import (
	"image"
	"image/color"
)

func Hot(src *image.Gray) *image.RGBA {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			v := int(src.GrayAt(x, y).Y)
			dst.SetRGBA(x, y, hotColor(v))
		}
	}

	return dst
}

func hotColor(v int) color.RGBA {
	switch {
	case v < 85:
		return color.RGBA{R: uint8(v * 3), A: 255}
	case v < 170:
		return color.RGBA{R: 255, G: uint8((v - 85) * 3), A: 255}
	default:
		return color.RGBA{R: 255, G: 255, B: uint8((v - 170) * 3), A: 255}
	}
}
