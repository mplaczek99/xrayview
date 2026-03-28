package colormap

import (
	"image"
	"image/color"
)

func Bone(src *image.Gray) *image.RGBA {
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
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
