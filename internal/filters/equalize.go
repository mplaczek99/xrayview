package filters

import "image"

func EqualizeHistogram(src *image.Gray) *image.Gray {
	bounds := src.Bounds()
	dst := image.NewGray(bounds)
	total := bounds.Dx() * bounds.Dy()
	if total == 0 {
		return dst
	}

	var hist [256]int
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			hist[src.GrayAt(x, y).Y]++
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

	if cdfMin == total {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				dst.SetGray(x, y, src.GrayAt(x, y))
			}
		}
		return dst
	}

	cdf = 0
	denom := total - cdfMin
	for i, count := range hist {
		cdf += count
		if cdf <= cdfMin {
			continue
		}
		value := ((cdf-cdfMin)*255 + denom/2) / denom
		lut[i] = uint8(value)
	}

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gray := src.GrayAt(x, y)
			gray.Y = lut[gray.Y]
			dst.SetGray(x, y, gray)
		}
	}

	return dst
}
