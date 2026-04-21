package annotations

import (
	"fmt"

	"xrayview/backend/internal/analysis"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/imaging"
)

// SuggestedAnnotations turns the preview into the black-gap trace set the UI
// renders. If extraction fails or returns nothing, it falls back to the
// primary outline already present on the analysis payload.
func SuggestedAnnotations(
	preview imaging.PreviewImage,
	analysisResult *contracts.ToothAnalysis,
) contracts.AnnotationBundle {
	polylines := traceAnnotationsFromPreview(preview)
	if len(polylines) > 0 {
		return contracts.AnnotationBundle{
			Lines:      []contracts.LineAnnotation{},
			Rectangles: []contracts.RectangleAnnotation{},
			Polylines:  polylines,
		}
	}

	if analysisResult == nil || analysisResult.Tooth == nil || len(analysisResult.Tooth.Geometry.Outline) < 3 {
		return emptyAnnotationBundle()
	}

	return contracts.AnnotationBundle{
		Lines:      []contracts.LineAnnotation{},
		Rectangles: []contracts.RectangleAnnotation{},
		Polylines: []contracts.PolylineAnnotation{
			{
				ID:         "auto-tooth-trace",
				Label:      "Tooth trace",
				Source:     contracts.AnnotationSourceAutoTooth,
				Points:     annotationPoints(analysisResult.Tooth.Geometry.Outline),
				Closed:     true,
				Editable:   false,
				Confidence: pointerTo(analysisResult.Tooth.Confidence),
			},
		},
	}
}

func traceAnnotationsFromPreview(preview imaging.PreviewImage) []contracts.PolylineAnnotation {
	traces, err := analysis.ExtractBlackGapTraces(preview)
	if err != nil || len(traces) == 0 {
		return nil
	}

	polylines := make([]contracts.PolylineAnnotation, 0, len(traces))
	for index, trace := range traces {
		if len(trace.Points) < 2 {
			continue
		}
		if trace.Closed && len(trace.Points) < 3 {
			continue
		}
		polylines = append(polylines, contracts.PolylineAnnotation{
			ID:         fmt.Sprintf("auto-tooth-trace-%d", index),
			Label:      "Tooth trace",
			Source:     contracts.AnnotationSourceAutoTooth,
			Points:     annotationPoints(trace.Points),
			Closed:     trace.Closed,
			Editable:   false,
			Confidence: nil,
		})
	}

	return polylines
}

func annotationPoints(points []contracts.Point) []contracts.AnnotationPoint {
	converted := make([]contracts.AnnotationPoint, 0, len(points))
	for _, point := range points {
		converted = append(converted, contracts.AnnotationPoint{
			X: float64(point.X),
			Y: float64(point.Y),
		})
	}
	return converted
}

func emptyAnnotationBundle() contracts.AnnotationBundle {
	return contracts.AnnotationBundle{
		Lines:      []contracts.LineAnnotation{},
		Rectangles: []contracts.RectangleAnnotation{},
		Polylines:  []contracts.PolylineAnnotation{},
	}
}
