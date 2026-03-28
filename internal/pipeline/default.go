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
func ProcessDefault(src image.Image, brightness int) *image.Gray {
	// This helper intentionally wires only brightness for now. Adding one control
	// at a time keeps the shared path easy to reason about while the GUI grows.
	gray := filters.Grayscale(src)

	// Brightness is applied after grayscale because the current default pipeline
	// treats brightness as a gray-level adjustment, not a color operation.
	if brightness != 0 {
		gray = filters.AdjustBrightness(gray, brightness)
	}

	return gray
}
