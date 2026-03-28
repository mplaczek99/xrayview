package colormap

import (
	"image"
	"image/color"
	"testing"
)

func TestHot(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 4, 1))
	src.SetGray(0, 0, color.Gray{Y: 0})
	src.SetGray(1, 0, color.Gray{Y: 85})
	src.SetGray(2, 0, color.Gray{Y: 170})
	src.SetGray(3, 0, color.Gray{Y: 255})

	got := Hot(src)

	if c := got.RGBAAt(0, 0); c != (color.RGBA{A: 255}) {
		t.Fatalf("pixel (0,0) = %+v, want %+v", c, color.RGBA{A: 255})
	}
	if c := got.RGBAAt(1, 0); c != (color.RGBA{R: 255, A: 255}) {
		t.Fatalf("pixel (1,0) = %+v, want %+v", c, color.RGBA{R: 255, A: 255})
	}
	if c := got.RGBAAt(2, 0); c != (color.RGBA{R: 255, G: 255, A: 255}) {
		t.Fatalf("pixel (2,0) = %+v, want %+v", c, color.RGBA{R: 255, G: 255, A: 255})
	}
	if c := got.RGBAAt(3, 0); c != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatalf("pixel (3,0) = %+v, want %+v", c, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	}
}
