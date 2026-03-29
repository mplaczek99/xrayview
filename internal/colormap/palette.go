package colormap

import (
	"image"
	"image/color"
)

func applyPalette(src *image.Gray, palette [256]color.RGBA) *image.RGBA {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	width := bounds.Dx()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		srcStart := src.PixOffset(bounds.Min.X, y)
		dstStart := dst.PixOffset(bounds.Min.X, y)
		srcRow := src.Pix[srcStart : srcStart+width]
		dstRow := dst.Pix[dstStart : dstStart+width*4]

		for x, value := range srcRow {
			c := palette[value]
			i := x * 4
			dstRow[i] = c.R
			dstRow[i+1] = c.G
			dstRow[i+2] = c.B
			dstRow[i+3] = c.A
		}
	}

	return dst
}

func buildPalette(colorFn func(int) color.RGBA) [256]color.RGBA {
	var palette [256]color.RGBA
	for i := range palette {
		palette[i] = colorFn(i)
	}
	return palette
}
