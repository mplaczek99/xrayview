package filters

import "image"

// EqualizeHistogram spreads grayscale values across the available range.
func EqualizeHistogram(src *image.Gray) *image.Gray {
	bounds := src.Bounds()
	dst := image.NewGray(bounds)
	width := bounds.Dx()
	total := width * bounds.Dy()
	if total == 0 {
		return dst
	}

	var hist [256]int
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		rowStart := src.PixOffset(bounds.Min.X, y)
		row := src.Pix[rowStart : rowStart+width]
		for _, value := range row {
			hist[value]++
		}
	}

	cdf := 0
	cdfMin := 0
	found := false
	var lut [256]uint8
	for _, count := range hist {
		cdf += count
		if !found && count != 0 {
			cdfMin = cdf
			found = true
		}
	}

	// Leave flat images unchanged.
	if cdfMin == total {
		copyGrayRows(dst, src)
		return dst
	}

	cdf = 0
	denom := total - cdfMin
	// Build the normalized cumulative histogram lookup table.
	for i, count := range hist {
		cdf += count
		if cdf <= cdfMin {
			continue
		}
		value := ((cdf-cdfMin)*255 + denom/2) / denom
		lut[i] = uint8(value)
	}

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
