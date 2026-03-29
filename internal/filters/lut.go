package filters

import "image"

// ApplyLookupTable applies a grayscale lookup table to src.
func ApplyLookupTable(src *image.Gray, lut [256]uint8) *image.Gray {
	bounds := src.Bounds()
	dst := image.NewGray(bounds)
	width := bounds.Dx()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		srcStart := src.PixOffset(bounds.Min.X, y)
		dstStart := dst.PixOffset(bounds.Min.X, y)
		srcRow := src.Pix[srcStart : srcStart+width]
		dstRow := dst.Pix[dstStart : dstStart+width]

		for x, value := range srcRow {
			dstRow[x] = lut[value]
		}
	}

	return dst
}

func cloneGray(src *image.Gray) *image.Gray {
	dst := image.NewGray(src.Bounds())
	copyGrayRows(dst, src)
	return dst
}

func copyGrayRows(dst, src *image.Gray) {
	bounds := src.Bounds()
	width := bounds.Dx()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		srcStart := src.PixOffset(bounds.Min.X, y)
		dstStart := dst.PixOffset(bounds.Min.X, y)
		copy(dst.Pix[dstStart:dstStart+width], src.Pix[srcStart:srcStart+width])
	}
}
