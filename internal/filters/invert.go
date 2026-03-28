package filters

import "image"

func Invert(src *image.Gray) *image.Gray {
	bounds := src.Bounds()
	dst := image.NewGray(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gray := src.GrayAt(x, y)
			gray.Y = 255 - gray.Y
			dst.SetGray(x, y, gray)
		}
	}

	return dst
}
