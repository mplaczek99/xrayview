package filters

import "image"

func Invert(src *image.Gray) *image.Gray {
	// Returning a new image keeps each filter stage independent, which makes the
	// pipeline easier to reason about and avoids mutating an earlier result.
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
