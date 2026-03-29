package filters

import "image"

var invertLookup = func() [256]uint8 {
	var lut [256]uint8
	for i := range lut {
		lut[i] = 255 - uint8(i)
	}
	return lut
}()

// Invert returns a grayscale image with inverted pixel values.
func Invert(src *image.Gray) *image.Gray {
	return ApplyLookupTable(src, invertLookup)
}
