package processing

import (
	"fmt"
	"math"

	"xrayview/backend/internal/imaging"
)

type GrayscaleControls struct {
	Invert     bool
	Brightness int
	Contrast   float64
	Equalize   bool
}

func ProcessPreviewImage(
	preview imaging.PreviewImage,
	controls GrayscaleControls,
) (imaging.PreviewImage, string, error) {
	if err := preview.Validate(); err != nil {
		return imaging.PreviewImage{}, "", fmt.Errorf("validate preview image: %w", err)
	}
	if preview.Format != imaging.FormatGray8 {
		return imaging.PreviewImage{}, "", fmt.Errorf("grayscale processing requires %q preview input", imaging.FormatGray8)
	}

	pixels := append([]uint8(nil), preview.Pixels...)
	mode := ProcessGrayscalePixels(pixels, controls)

	return imaging.GrayPreview(preview.Width, preview.Height, pixels), mode, nil
}

func ProcessGrayscalePixels(pixels []uint8, controls GrayscaleControls) string {
	mode := "grayscale"
	lookup := identityLookupTable()
	pendingLookup := false

	flushLookup := func() {
		if !pendingLookup {
			return
		}

		applyLookupInPlace(pixels, &lookup)
		lookup = identityLookupTable()
		pendingLookup = false
	}

	// Keep the Rust behavior exactly: invert, brightness, contrast, then
	// histogram equalization.
	if controls.Invert {
		composeInvertLookup(&lookup)
		pendingLookup = true
		mode = "inverted grayscale"
	}
	if controls.Brightness != 0 {
		composeBrightnessLookup(&lookup, controls.Brightness)
		pendingLookup = true
		mode = fmt.Sprintf("%s with brightness %+d", mode, controls.Brightness)
	}
	if controls.Contrast != 1.0 {
		composeContrastLookup(&lookup, controls.Contrast)
		pendingLookup = true
		mode = fmt.Sprintf("%s with contrast %v", mode, controls.Contrast)
	}
	if controls.Equalize {
		flushLookup()
		equalizeHistogramInPlace(pixels)
		mode = fmt.Sprintf("%s with histogram equalization", mode)
	}

	flushLookup()
	return mode
}

func identityLookupTable() [256]uint8 {
	var lookup [256]uint8
	for index := range lookup {
		lookup[index] = uint8(index)
	}

	return lookup
}

func composeInvertLookup(lookup *[256]uint8) {
	for index := range lookup {
		lookup[index] = 255 - lookup[index]
	}
}

func composeBrightnessLookup(lookup *[256]uint8, delta int) {
	for index := range lookup {
		lookup[index] = clampLookupValue(int(lookup[index]) + delta)
	}
}

func composeContrastLookup(lookup *[256]uint8, factor float64) {
	for index := range lookup {
		adjusted := 128.0 + factor*(float64(lookup[index])-128.0)
		lookup[index] = clampLookupValue(int(math.Round(adjusted)))
	}
}

func clampLookupValue(value int) uint8 {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}

	return uint8(value)
}

func applyLookupInPlace(pixels []uint8, lookup *[256]uint8) {
	i := 0
	n := len(pixels)

	// Process 8 pixels per iteration with sub-slice windowing.
	// The bounded sub-slice lets the compiler prove all accesses are
	// in-bounds, eliminating per-access bounds checks.
	for ; i+8 <= n; i += 8 {
		p := pixels[i : i+8 : i+8]
		p[0] = lookup[p[0]]
		p[1] = lookup[p[1]]
		p[2] = lookup[p[2]]
		p[3] = lookup[p[3]]
		p[4] = lookup[p[4]]
		p[5] = lookup[p[5]]
		p[6] = lookup[p[6]]
		p[7] = lookup[p[7]]
	}

	for ; i < n; i++ {
		pixels[i] = lookup[pixels[i]]
	}
}

func equalizeHistogramInPlace(pixels []uint8) {
	if len(pixels) == 0 {
		return
	}

	var histogram [256]int
	for _, value := range pixels {
		histogram[value]++
	}

	total := len(pixels)
	cdf := 0
	cdfMin := 0
	found := false

	for _, count := range histogram {
		cdf += count
		if !found && count != 0 {
			cdfMin = cdf
			found = true
		}
	}

	if cdfMin == total {
		return
	}

	var lookup [256]uint8
	cdf = 0
	denom := total - cdfMin
	for index, count := range histogram {
		cdf += count
		if cdf <= cdfMin {
			continue
		}

		value := ((cdf-cdfMin)*255 + denom/2) / denom
		lookup[index] = uint8(value)
	}

	applyLookupInPlace(pixels, &lookup)
}
