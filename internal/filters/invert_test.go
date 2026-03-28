package filters

import (
	"image"
	"image/color"
	"testing"
)

func TestInvert(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 2, 1))
	src.SetGray(0, 0, color.Gray{Y: 0})
	src.SetGray(1, 0, color.Gray{Y: 200})

	inverted := Invert(src)

	if got := inverted.GrayAt(0, 0); got != (color.Gray{Y: 255}) {
		t.Fatalf("pixel (0,0) = %+v, want %+v", got, color.Gray{Y: 255})
	}

	if got := inverted.GrayAt(1, 0); got != (color.Gray{Y: 55}) {
		t.Fatalf("pixel (1,0) = %+v, want %+v", got, color.Gray{Y: 55})
	}
}
