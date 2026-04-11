package render

import (
	"fmt"

	"xrayview/backend/internal/imaging"
)

// PreviewExtension returns "jpeg" for Gray8 previews, "png" for RGBA8.
// Gray8 uses JPEG because it encodes 5-10x faster and produces smaller files.
// RGBA8 uses PNG to preserve alpha channel and pixel-exact palette colors.
func PreviewExtension(format imaging.ImageFormat) string {
	switch format {
	case imaging.FormatGray8:
		return "jpeg"
	default:
		return "png"
	}
}

// SavePreview encodes a preview image to disk using the optimal format:
// JPEG quality 92 for Gray8, PNG BestSpeed for RGBA8.
func SavePreview(path string, preview imaging.PreviewImage) error {
	switch preview.Format {
	case imaging.FormatGray8:
		return SavePreviewJPEG(path, preview)
	case imaging.FormatRGBA8:
		return SavePreviewPNG(path, preview)
	default:
		return fmt.Errorf("unsupported preview format %q", preview.Format)
	}
}
