package filters

import (
	"image"
	"math"
)

func AdjustContrast(src *image.Gray, factor float64) *image.Gray {
	// Contrast is adjusted around mid-gray so a factor of 1.0 is a no-op and the
	// midpoint remains visually stable while darker and lighter tones spread out.
	bounds := src.Bounds()
	dst := image.NewGray(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			value := 128 + factor*(float64(src.GrayAt(x, y).Y)-128)

			// Clamp before writing so strong factors cannot produce invalid bytes.
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
