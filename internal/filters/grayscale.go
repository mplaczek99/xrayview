package filters

import (
	"image"
	"image/color"
)

func Grayscale(src image.Image) *image.Gray {
	// Converting once to Gray gives every later filter the same single-channel
	// input regardless of whether the original file was color or grayscale.
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
