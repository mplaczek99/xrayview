package filters

import "image"

// AdjustBrightness adds delta to each grayscale pixel.
func AdjustBrightness(src *image.Gray, delta int) *image.Gray {
	bounds := src.Bounds()
	dst := image.NewGray(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			value := int(src.GrayAt(x, y).Y) + delta

			// Clamp to 0..255.
			if value < 0 {
				value = 0
			}
			if value > 255 {
				value = 255
			}
			dst.Pix[dst.PixOffset(x, y)] = uint8(value)
		}
	}

	return dst
}
