package filters

import (
	"image"
	"image/color"
	"testing"
)

func TestEqualizeHistogram(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 4, 1))
	src.SetGray(0, 0, color.Gray{Y: 50})
	src.SetGray(1, 0, color.Gray{Y: 50})
	src.SetGray(2, 0, color.Gray{Y: 100})
	src.SetGray(3, 0, color.Gray{Y: 100})

	equalized := EqualizeHistogram(src)

	if got := equalized.GrayAt(0, 0); got != (color.Gray{Y: 0}) {
		t.Fatalf("pixel (0,0) = %+v, want %+v", got, color.Gray{Y: 0})
	}
	if got := equalized.GrayAt(1, 0); got != (color.Gray{Y: 0}) {
		t.Fatalf("pixel (1,0) = %+v, want %+v", got, color.Gray{Y: 0})
	}
	if got := equalized.GrayAt(2, 0); got != (color.Gray{Y: 255}) {
		t.Fatalf("pixel (2,0) = %+v, want %+v", got, color.Gray{Y: 255})
	}
	if got := equalized.GrayAt(3, 0); got != (color.Gray{Y: 255}) {
		t.Fatalf("pixel (3,0) = %+v, want %+v", got, color.Gray{Y: 255})
	}
}
