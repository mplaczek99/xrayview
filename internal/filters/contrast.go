package filters

import (
	"image"
	"math"
)

// AdjustContrast scales contrast around mid-gray.
func AdjustContrast(src *image.Gray, factor float64) *image.Gray {
	var lut [256]uint8
	for i := range lut {
		value := 128 + factor*(float64(i)-128)
		lut[i] = clampToUint8(int(math.Round(value)))
	}

	return ApplyLookupTable(src, lut)
}
