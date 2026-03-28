package filters

import (
	"image"
	"image/color"
	"testing"
)

func TestAdjustContrast(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 4, 1))
	src.SetGray(0, 0, color.Gray{Y: 0})
	src.SetGray(1, 0, color.Gray{Y: 100})
	src.SetGray(2, 0, color.Gray{Y: 128})
	src.SetGray(3, 0, color.Gray{Y: 255})

	unchanged := AdjustContrast(src, 1.0)
	for x := 0; x < 4; x++ {
		if got, want := unchanged.GrayAt(x, 0), src.GrayAt(x, 0); got != want {
			t.Fatalf("no-op pixel (%d,0) = %+v, want %+v", x, got, want)
		}
	}

	higher := AdjustContrast(src, 2.0)
	if got := higher.GrayAt(0, 0); got != (color.Gray{Y: 0}) {
		t.Fatalf("contrast pixel (0,0) = %+v, want %+v", got, color.Gray{Y: 0})
	}
	if got := higher.GrayAt(1, 0); got != (color.Gray{Y: 72}) {
		t.Fatalf("contrast pixel (1,0) = %+v, want %+v", got, color.Gray{Y: 72})
	}
	if got := higher.GrayAt(2, 0); got != (color.Gray{Y: 128}) {
		t.Fatalf("contrast pixel (2,0) = %+v, want %+v", got, color.Gray{Y: 128})
	}
	if got := higher.GrayAt(3, 0); got != (color.Gray{Y: 255}) {
		t.Fatalf("contrast pixel (3,0) = %+v, want %+v", got, color.Gray{Y: 255})
	}
}
