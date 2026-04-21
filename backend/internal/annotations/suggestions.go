package annotations

import "xrayview/backend/internal/contracts"

// SuggestedAnnotations turns a tooth analysis into the trace annotation the UI
// renders. The backend emits one stable auto-analysis trace for the primary
// detected white region so re-analysis can diff-patch it without recreating
// manual measurements.
func SuggestedAnnotations(analysis *contracts.ToothAnalysis) contracts.AnnotationBundle {
	if analysis == nil || analysis.Tooth == nil || len(analysis.Tooth.Geometry.Outline) < 3 {
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
				Points:     annotationPoints(analysis.Tooth.Geometry.Outline),
				Closed:     true,
				Editable:   false,
				Confidence: pointerTo(analysis.Tooth.Confidence),
			},
		},
	}
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
