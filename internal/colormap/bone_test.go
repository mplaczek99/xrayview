package colormap

import (
	"image"
	"image/color"
	"testing"
)

func TestBone(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 3, 1))
	src.SetGray(0, 0, color.Gray{Y: 0})
	src.SetGray(1, 0, color.Gray{Y: 128})
	src.SetGray(2, 0, color.Gray{Y: 255})

	got := Bone(src)

	if c := got.RGBAAt(0, 0); c != (color.RGBA{A: 255}) {
		t.Fatalf("pixel (0,0) = %+v, want %+v", c, color.RGBA{A: 255})
	}
	if c := got.RGBAAt(1, 0); c != (color.RGBA{R: 112, G: 120, B: 128, A: 255}) {
		t.Fatalf("pixel (1,0) = %+v, want %+v", c, color.RGBA{R: 112, G: 120, B: 128, A: 255})
	}
	if c := got.RGBAAt(2, 0); c != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatalf("pixel (2,0) = %+v, want %+v", c, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	}
}
