package main

import (
	"image"
	"image/color"
	"testing"
)

func TestCombineComparison(t *testing.T) {
	left := image.NewGray(image.Rect(0, 0, 2, 1))
	left.SetGray(0, 0, color.Gray{Y: 10})
	left.SetGray(1, 0, color.Gray{Y: 20})

	right := image.NewRGBA(image.Rect(0, 0, 2, 1))
	right.SetRGBA(0, 0, color.RGBA{R: 100, G: 110, B: 120, A: 255})
	right.SetRGBA(1, 0, color.RGBA{R: 200, G: 210, B: 220, A: 255})

	got := combineComparison(left, right)

	if got.Bounds().Dx() != 4 || got.Bounds().Dy() != 1 {
		t.Fatalf("bounds = %v, want 4x1", got.Bounds())
	}

	if c := got.RGBAAt(0, 0); c != (color.RGBA{R: 10, G: 10, B: 10, A: 255}) {
		t.Fatalf("left pixel = %+v, want %+v", c, color.RGBA{R: 10, G: 10, B: 10, A: 255})
	}
	if c := got.RGBAAt(3, 0); c != (color.RGBA{R: 200, G: 210, B: 220, A: 255}) {
		t.Fatalf("right pixel = %+v, want %+v", c, color.RGBA{R: 200, G: 210, B: 220, A: 255})
	}
}
