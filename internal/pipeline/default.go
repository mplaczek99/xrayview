package pipeline

import (
	"image"

	"github.com/mplaczek99/xrayview/internal/colormap"
	"github.com/mplaczek99/xrayview/internal/filters"
)

// ProcessDefault applies the shared default grayscale pipeline and optional
// palette mapping.
func ProcessDefault(src image.Image, invert bool, brightness int, contrast float64, equalize bool, palette string) image.Image {
	// Start in grayscale so the rest of the pipeline operates on one channel.
	gray := filters.Grayscale(src)

	// Keep the default tone adjustments in one place so every caller uses the same
	// ordering: grayscale, invert, brightness, contrast, equalize.
	if invert {
		gray = filters.Invert(gray)
	}

	if brightness != 0 {
		gray = filters.AdjustBrightness(gray, brightness)
	}

	if contrast != 1.0 {
		gray = filters.AdjustContrast(gray, contrast)
	}

	if equalize {
		gray = filters.EqualizeHistogram(gray)
	}

	// Apply the palette after tone mapping so color reflects the final gray values.
	if palette == "hot" {
		return colormap.Hot(gray)
	}
	if palette == "bone" {
		return colormap.Bone(gray)
	}

	return gray
}
