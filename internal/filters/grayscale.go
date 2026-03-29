package filters

import (
	"image"
	"image/color"
)

// Grayscale converts any image to grayscale.
func Grayscale(src image.Image) *image.Gray {
	bounds := src.Bounds()
	dst := image.NewGray(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gray := color.GrayModel.Convert(src.At(x, y)).(color.Gray)
			dst.SetGray(x, y, gray)
		}
	}

	return dst
}
