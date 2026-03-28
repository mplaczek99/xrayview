package main

import (
	"image"
	"image/draw"
)

func combineComparison(left *image.Gray, right image.Image) *image.RGBA {
	// The comparison output keeps the original grayscale baseline on the left so a
	// user can visually judge the effect of the chosen processing steps in one file.
	leftBounds := left.Bounds()
	w := leftBounds.Dx()
	h := leftBounds.Dy()

	// The helper assumes both images share the same height, which is true for the
	// current flow because the processed image is derived from the same input.
	dst := image.NewRGBA(image.Rect(0, 0, 2*w, h))
	draw.Draw(dst, image.Rect(0, 0, w, h), left, leftBounds.Min, draw.Src)
	draw.Draw(dst, image.Rect(w, 0, 2*w, h), right, right.Bounds().Min, draw.Src)

	return dst
}
