package colormap

import (
	"image"
	"image/color"
)

var bonePalette = buildPalette(boneColor)

// Bone maps grayscale values to a cool X-ray-style palette.
func Bone(src *image.Gray) *image.RGBA {
	return applyPalette(src, bonePalette)
}

func boneColor(v int) color.RGBA {
	// Bright tones get a white boost.
	whiteBoost := v - 128
	if whiteBoost < 0 {
		whiteBoost = 0
	}

	r := clamp8((v*7)/8 + whiteBoost)
	g := clamp8((v*7)/8 + whiteBoost + v/16)
	b := clamp8(v + whiteBoost/2)

	return color.RGBA{R: r, G: g, B: b, A: 255}
}

func clamp8(v int) uint8 {
	// Clamp to 0..255.
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
