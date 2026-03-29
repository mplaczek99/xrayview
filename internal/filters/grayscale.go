package filters

import (
	"image"
	"image/color"
)

// Grayscale converts any image to grayscale.
func Grayscale(src image.Image) *image.Gray {
	switch src := src.(type) {
	case *image.Gray:
		return cloneGray(src)
	case *image.YCbCr:
		bounds := src.Bounds()
		dst := image.NewGray(bounds)
		width := bounds.Dx()

		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			rowStart := dst.PixOffset(bounds.Min.X, y)
			row := dst.Pix[rowStart : rowStart+width]

			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				yy := src.Y[src.YOffset(x, y)]
				cbcrOffset := src.COffset(x, y)
				cb := src.Cb[cbcrOffset]
				cr := src.Cr[cbcrOffset]
				row[x-bounds.Min.X] = grayFromYCbCr(yy, cb, cr)
			}
		}

		return dst
	}

	bounds := src.Bounds()
	dst := image.NewGray(bounds)
	width := bounds.Dx()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		rowStart := dst.PixOffset(bounds.Min.X, y)
		row := dst.Pix[rowStart : rowStart+width]

		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gray := color.GrayModel.Convert(src.At(x, y)).(color.Gray)
			row[x-bounds.Min.X] = gray.Y
		}
	}

	return dst
}

func grayFromYCbCr(y, cb, cr uint8) uint8 {
	r, g, b, _ := color.YCbCr{Y: y, Cb: cb, Cr: cr}.RGBA()
	return uint8((19595*r + 38470*g + 7471*b + 1<<15) >> 24)
}
