package colormap

import (
	"image"
	"image/color"
)

func Bone(src *image.Gray) *image.RGBA {
	// Bone keeps a cooler, X-ray-like appearance while still preserving monotonic
	// brightness, which makes it a reasonable default pseudocolor preset choice.
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			v := int(src.GrayAt(x, y).Y)
			dst.SetRGBA(x, y, boneColor(v))
		}
	}

	return dst
}

func boneColor(v int) color.RGBA {
	// The extra white boost in brighter regions nudges the palette toward the pale
	// highlights people often expect from an X-ray-style view.
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
	// Keeping the helper local avoids repeating clamping logic while preserving a
	// predictable 0..255 output for the palette math.
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
