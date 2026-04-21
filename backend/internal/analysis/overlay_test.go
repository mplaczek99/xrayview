package analysis

import (
	"testing"

	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/imaging"
)

func TestOverlayPreviewWithToothTraceDrawsRedOutline(t *testing.T) {
	preview := imaging.GrayPreview(8, 8, []uint8{
		20, 20, 20, 20, 20, 20, 20, 20,
		20, 20, 20, 20, 20, 20, 20, 20,
		20, 20, 20, 20, 20, 20, 20, 20,
		20, 20, 20, 20, 20, 20, 20, 20,
		20, 20, 20, 20, 20, 20, 20, 20,
		20, 20, 20, 20, 20, 20, 20, 20,
		20, 20, 20, 20, 20, 20, 20, 20,
		20, 20, 20, 20, 20, 20, 20, 20,
	})
	analysisResult := contracts.ToothAnalysis{
		Tooth: &contracts.ToothCandidate{
			Geometry: contracts.ToothGeometry{
				Outline: []contracts.Point{
					{X: 2, Y: 2},
					{X: 5, Y: 2},
					{X: 5, Y: 5},
					{X: 2, Y: 5},
				},
			},
		},
	}

	overlay, err := OverlayPreviewWithToothTrace(preview, analysisResult)
	if err != nil {
		t.Fatalf("OverlayPreviewWithToothTrace returned error: %v", err)
	}
	if got, want := overlay.Format, imaging.FormatRGBA8; got != want {
		t.Fatalf("overlay format = %q, want %q", got, want)
	}

	topEdgeBase := (2*int(overlay.Width) + 3) * 4
	if overlay.Pixels[topEdgeBase+0] <= overlay.Pixels[topEdgeBase+1] {
		t.Fatalf("top edge pixel not tinted red enough: rgba=%v", overlay.Pixels[topEdgeBase:topEdgeBase+4])
	}

	outsideBase := 0
	if overlay.Pixels[outsideBase+0] != 20 || overlay.Pixels[outsideBase+1] != 20 || overlay.Pixels[outsideBase+2] != 20 {
		t.Fatalf("outside pixel unexpectedly changed: rgba=%v", overlay.Pixels[outsideBase:outsideBase+4])
	}
}
