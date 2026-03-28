package pipeline

import (
	"image"

	"github.com/mplaczek99/xrayview/internal/filters"
)

// ProcessDefault applies the project's current default visualization path.
//
// Keeping this in a shared package lets the GUI reuse the same image logic as
// the rest of the project instead of quietly growing its own copy of the
// filter sequence.
func ProcessDefault(src image.Image, brightness int, contrast float64) *image.Gray {
	// This helper wires one control at a time so the shared path stays easy to
	// reason about while the GUI grows incrementally.
	gray := filters.Grayscale(src)

	// Brightness is applied after grayscale because the current default pipeline
	// treats brightness as a gray-level adjustment, not a color operation.
	if brightness != 0 {
		gray = filters.AdjustBrightness(gray, brightness)
	}

	// Contrast follows brightness so it reshapes the already adjusted tones. That
	// ordering matches the current GUI flow and keeps pipeline behavior centralized
	// instead of scattering filter order decisions across multiple callers.
	if contrast != 1.0 {
		gray = filters.AdjustContrast(gray, contrast)
	}

	return gray
}
