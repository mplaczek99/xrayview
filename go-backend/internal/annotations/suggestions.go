package annotations

import (
	"fmt"

	"xrayview/go-backend/internal/contracts"
)

func SuggestedAnnotations(analysis *contracts.ToothAnalysis) contracts.AnnotationBundle {
	if analysis == nil || len(analysis.Teeth) == 0 {
		return emptyAnnotationBundle()
	}

	measurementScale := analysis.Calibration.MeasurementScale
	lines := make([]contracts.LineAnnotation, 0, len(analysis.Teeth)*2)
	rectangles := make([]contracts.RectangleAnnotation, 0, len(analysis.Teeth))

	for index, tooth := range analysis.Teeth {
		toothNumber := index + 1
		widthStart := annotationPoint(tooth.Geometry.WidthLine.Start)
		widthEnd := annotationPoint(tooth.Geometry.WidthLine.End)
		heightStart := annotationPoint(tooth.Geometry.HeightLine.Start)
		heightEnd := annotationPoint(tooth.Geometry.HeightLine.End)

		lines = append(lines,
			contracts.LineAnnotation{
				ID:          fmt.Sprintf("auto-tooth-%d-width", toothNumber),
				Label:       fmt.Sprintf("Tooth %d width", toothNumber),
				Source:      contracts.AnnotationSourceAutoTooth,
				Start:       widthStart,
				End:         widthEnd,
				Editable:    true,
				Confidence:  pointerTo(tooth.Confidence),
				Measurement: pointerTo(MeasureLine(widthStart, widthEnd, measurementScale)),
			},
			contracts.LineAnnotation{
				ID:          fmt.Sprintf("auto-tooth-%d-height", toothNumber),
				Label:       fmt.Sprintf("Tooth %d height", toothNumber),
				Source:      contracts.AnnotationSourceAutoTooth,
				Start:       heightStart,
				End:         heightEnd,
				Editable:    true,
				Confidence:  pointerTo(tooth.Confidence),
				Measurement: pointerTo(MeasureLine(heightStart, heightEnd, measurementScale)),
			},
		)

		rectangles = append(rectangles, contracts.RectangleAnnotation{
			ID:         fmt.Sprintf("auto-tooth-%d-bounding-box", toothNumber),
			Label:      fmt.Sprintf("Tooth %d bounding box", toothNumber),
			Source:     contracts.AnnotationSourceAutoTooth,
			X:          float64(tooth.Geometry.BoundingBox.X),
			Y:          float64(tooth.Geometry.BoundingBox.Y),
			Width:      float64(tooth.Geometry.BoundingBox.Width),
			Height:     float64(tooth.Geometry.BoundingBox.Height),
			Editable:   false,
			Confidence: pointerTo(tooth.Confidence),
		})
	}

	return contracts.AnnotationBundle{
		Lines:      lines,
		Rectangles: rectangles,
	}
}

func annotationPoint(point contracts.Point) contracts.AnnotationPoint {
	return contracts.AnnotationPoint{
		X: float64(point.X),
		Y: float64(point.Y),
	}
}

func emptyAnnotationBundle() contracts.AnnotationBundle {
	return contracts.AnnotationBundle{
		Lines:      []contracts.LineAnnotation{},
		Rectangles: []contracts.RectangleAnnotation{},
	}
}
