package annotations

import (
	"math"

	"xrayview/backend/internal/contracts"
)

func MeasureLineAnnotation(
	annotation contracts.LineAnnotation,
	measurementScale *contracts.MeasurementScale,
) contracts.LineAnnotation {
	annotation.Measurement = pointerTo(MeasureLine(annotation.Start, annotation.End, measurementScale))
	return annotation
}

func MeasureLine(
	start contracts.AnnotationPoint,
	end contracts.AnnotationPoint,
	measurementScale *contracts.MeasurementScale,
) contracts.LineMeasurement {
	dx := end.X - start.X
	dy := end.Y - start.Y

	measurement := contracts.LineMeasurement{
		PixelLength: roundMeasurement(math.Hypot(dx, dy)),
	}

	if measurementScale != nil {
		calibratedLengthMM := roundMeasurement(
			math.Hypot(dx*measurementScale.ColumnSpacingMM, dy*measurementScale.RowSpacingMM),
		)
		measurement.CalibratedLengthMM = &calibratedLengthMM
	}

	return measurement
}

func roundMeasurement(value float64) float64 {
	return math.Round(value*10.0) / 10.0
}

func pointerTo[T any](value T) *T {
	return &value
}
