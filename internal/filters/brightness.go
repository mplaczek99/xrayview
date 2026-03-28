package filters

import "image"

func AdjustBrightness(src *image.Gray, delta int) *image.Gray {
	// Brightness is expressed as an additive delta because it maps cleanly to the
	// CLI flag and composes predictably with the other grayscale-only stages.
	bounds := src.Bounds()
	dst := image.NewGray(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			value := int(src.GrayAt(x, y).Y) + delta

			// Clamp into the byte range so later stages always receive valid gray values.
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
