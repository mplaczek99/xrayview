package main

import (
	"image"
	"image/draw"
)

// combineComparison places the original and processed images side by side.
func combineComparison(left *image.Gray, right image.Image) *image.RGBA {
	leftBounds := left.Bounds()
	w := leftBounds.Dx()
	h := leftBounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, 2*w, h))
	draw.Draw(dst, image.Rect(0, 0, w, h), left, leftBounds.Min, draw.Src)
	draw.Draw(dst, image.Rect(w, 0, 2*w, h), right, right.Bounds().Min, draw.Src)

	return dst
}
