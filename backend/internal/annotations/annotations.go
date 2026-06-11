package annotations

import (
	"math"

	"xrayview/backend/internal/contracts"
)

// MeasureLineAnnotation attaches a measurement to a line annotation and
// returns the updated value. The input is passed by value so callers can chain
// it without mutating shared annotation state.
func MeasureLineAnnotation(annotation contracts.LineAnnotation, scale *contracts.MeasurementScale) contracts.LineAnnotation {
	measurement := MeasureLine(annotation.Start, annotation.End, scale)
	annotation.Measurement = &measurement
	return annotation
}

// MeasureLine computes pixel distance and, when calibration is available, the
// physical distance in millimeters. Row and column spacing are applied before
// hypot so anisotropic pixels are handled correctly.
func MeasureLine(start, end contracts.AnnotationPoint, scale *contracts.MeasurementScale) contracts.LineMeasurement {
	dx := end.X - start.X
	dy := end.Y - start.Y
	pixelLength := roundMeasurement(math.Hypot(dx, dy))

	var calibrated *float64
	if scale != nil {
		value := roundMeasurement(math.Hypot(dx*scale.ColumnSpacingMM, dy*scale.RowSpacingMM))
		calibrated = &value
	}

	return contracts.LineMeasurement{
		PixelLength:        pixelLength,
		CalibratedLengthMM: calibrated,
	}
}

// roundMeasurement keeps the one-decimal-place display contract used by the
// Rust implementation and the frontend.
func roundMeasurement(value float64) float64 {
	return math.Round(value*10.0) / 10.0
}
