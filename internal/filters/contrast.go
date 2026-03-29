package filters

import (
	"image"
	"math"
)

// AdjustContrast scales contrast around mid-gray.
func AdjustContrast(src *image.Gray, factor float64) *image.Gray {
	bounds := src.Bounds()
	dst := image.NewGray(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			value := 128 + factor*(float64(src.GrayAt(x, y).Y)-128)

			// Clamp to 0..255.
			if value < 0 {
				value = 0
			}
			if value > 255 {
				value = 255
			}
			dst.Pix[dst.PixOffset(x, y)] = uint8(math.Round(value))
		}
	}

	return dst
}
