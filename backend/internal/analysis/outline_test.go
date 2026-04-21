package analysis

import (
	"reflect"
	"testing"

	"xrayview/backend/internal/contracts"
)

func TestGeometryFromPixelsTracesClosedOutline(t *testing.T) {
	geometry := geometryFromPixels(
		[]int{
			3*10 + 2,
			3*10 + 3,
			4*10 + 2,
			4*10 + 3,
		},
		contracts.BoundingBox{
			X:      2,
			Y:      3,
			Width:  2,
			Height: 2,
		},
		10,
	)

	want := []contracts.Point{
		{X: 2, Y: 3},
		{X: 4, Y: 3},
		{X: 4, Y: 5},
		{X: 2, Y: 5},
	}
	if !reflect.DeepEqual(geometry.Outline, want) {
		t.Fatalf("Outline = %#v, want %#v", geometry.Outline, want)
	}
}
