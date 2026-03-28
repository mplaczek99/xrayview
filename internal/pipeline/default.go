package pipeline

import (
	"image"

	"github.com/mplaczek99/xrayview/internal/colormap"
	"github.com/mplaczek99/xrayview/internal/filters"
)

// ProcessDefault applies the project's current default visualization path.
//
// Keeping this in a shared package lets the GUI reuse the same image logic as
// the rest of the project instead of quietly growing its own copy of the
// filter sequence.
func ProcessDefault(src image.Image, invert bool, brightness int, contrast float64, equalize bool, palette string) image.Image {
	// This helper wires one control at a time so the shared path stays easy to
	// reason about while the GUI grows incrementally.
	gray := filters.Grayscale(src)

	// Invert is applied immediately after grayscale so it flips the base intensity
	// values before any later tone shaping occurs. That keeps the rest of the
	// pipeline operating on the same kind of gray image regardless of whether
	// inversion is enabled.
	if invert {
		gray = filters.Invert(gray)
	}

	// Brightness is applied after grayscale because the current default pipeline
	// treats brightness as a gray-level adjustment, not a color operation.
	if brightness != 0 {
		gray = filters.AdjustBrightness(gray, brightness)
	}

	// Contrast follows brightness so it reshapes the already adjusted tones. The
	// full order stays centralized here so GUI controls produce predictable results
	// instead of relying on each caller to remember filter sequencing.
	if contrast != 1.0 {
		gray = filters.AdjustContrast(gray, contrast)
	}

	// Equalization follows contrast so it redistributes the final gray values after
	// the simpler tone adjustments have already been applied. Keeping this ordering
	// here preserves one centralized definition of the default pipeline.
	if equalize {
		gray = filters.EqualizeHistogram(gray)
	}

	// Palette mapping stays separate from grayscale processing because it changes
	// how final intensities are visualized rather than how those intensities are
	// computed. Applying it last ensures the color map reflects the finished gray
	// result, and keeping every supported palette here keeps GUI and CLI output aligned.
	if palette == "hot" {
		return colormap.Hot(gray)
	}
	if palette == "bone" {
		return colormap.Bone(gray)
	}

	return gray
}
