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

func TestGrayscaleYCbCrMatchesColorModel(t *testing.T) {
	src := image.NewYCbCr(image.Rect(0, 0, 4, 1), image.YCbCrSubsampleRatio444)
	src.Y[0], src.Cb[0], src.Cr[0] = 20, 10, 240
	src.Y[1], src.Cb[1], src.Cr[1] = 80, 128, 128
	src.Y[2], src.Cb[2], src.Cr[2] = 160, 240, 16
	src.Y[3], src.Cb[3], src.Cr[3] = 250, 90, 180

	gray := Grayscale(src)

	for x := 0; x < 4; x++ {
		want := color.GrayModel.Convert(src.At(x, 0)).(color.Gray)
		if got := gray.GrayAt(x, 0); got != want {
			t.Fatalf("pixel (%d,0) = %+v, want %+v", x, got, want)
		}
	}
}
