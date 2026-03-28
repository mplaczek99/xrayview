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
func ProcessDefault(src image.Image) *image.Gray {
	// This helper intentionally stays on the default path only. GUI controls for
	// alternate settings are added later so this step can wire in processing
	// without also deciding how every option should be represented in the UI.
	return filters.Grayscale(src)
}
