package filters

import "image"

// AdjustBrightness adds delta to each grayscale pixel.
func AdjustBrightness(src *image.Gray, delta int) *image.Gray {
	var lut [256]uint8
	for i := range lut {
		lut[i] = clampToUint8(i + delta)
	}

	return ApplyLookupTable(src, lut)
}

func clampToUint8(value int) uint8 {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return uint8(value)
}
