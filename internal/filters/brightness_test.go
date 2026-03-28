package filters

import (
	"image"
	"image/color"
	"testing"
)

func TestAdjustBrightness(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 3, 1))
	src.SetGray(0, 0, color.Gray{Y: 0})
	src.SetGray(1, 0, color.Gray{Y: 100})
	src.SetGray(2, 0, color.Gray{Y: 250})

	brighter := AdjustBrightness(src, 20)
	if got := brighter.GrayAt(0, 0); got != (color.Gray{Y: 20}) {
		t.Fatalf("bright pixel (0,0) = %+v, want %+v", got, color.Gray{Y: 20})
	}
	if got := brighter.GrayAt(1, 0); got != (color.Gray{Y: 120}) {
		t.Fatalf("bright pixel (1,0) = %+v, want %+v", got, color.Gray{Y: 120})
	}
	if got := brighter.GrayAt(2, 0); got != (color.Gray{Y: 255}) {
		t.Fatalf("bright pixel (2,0) = %+v, want %+v", got, color.Gray{Y: 255})
	}

	darker := AdjustBrightness(src, -30)
	if got := darker.GrayAt(0, 0); got != (color.Gray{Y: 0}) {
		t.Fatalf("dark pixel (0,0) = %+v, want %+v", got, color.Gray{Y: 0})
	}
	if got := darker.GrayAt(1, 0); got != (color.Gray{Y: 70}) {
		t.Fatalf("dark pixel (1,0) = %+v, want %+v", got, color.Gray{Y: 70})
	}
	if got := darker.GrayAt(2, 0); got != (color.Gray{Y: 220}) {
		t.Fatalf("dark pixel (2,0) = %+v, want %+v", got, color.Gray{Y: 220})
	}
}
