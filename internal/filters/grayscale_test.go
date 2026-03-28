package filters

import (
	"image"
	"image/color"
	"testing"
)

func TestGrayscale(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	src.Set(1, 0, color.RGBA{R: 0, G: 255, B: 0, A: 255})

	gray := Grayscale(src)

	if gray.Bounds() != src.Bounds() {
		t.Fatalf("bounds = %v, want %v", gray.Bounds(), src.Bounds())
	}

	want0 := color.GrayModel.Convert(src.At(0, 0)).(color.Gray)
	want1 := color.GrayModel.Convert(src.At(1, 0)).(color.Gray)

	if got := gray.GrayAt(0, 0); got != want0 {
		t.Fatalf("pixel (0,0) = %+v, want %+v", got, want0)
	}

	if got := gray.GrayAt(1, 0); got != want1 {
		t.Fatalf("pixel (1,0) = %+v, want %+v", got, want1)
	}
}
