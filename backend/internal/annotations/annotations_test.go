package annotations

import (
	"testing"

	"xrayview/backend/internal/contracts"
)

func TestMeasureLineMeasuresPixelLength(t *testing.T) {
	measurement := MeasureLine(
		contracts.AnnotationPoint{X: 12.0, Y: 18.0},
		contracts.AnnotationPoint{X: 15.0, Y: 22.0},
		nil,
	)

	if measurement.PixelLength != 5.0 {
		t.Fatalf("pixel length = %v, want 5.0", measurement.PixelLength)
	}
	if measurement.CalibratedLengthMM != nil {
		t.Fatalf("calibrated length = %v, want nil", *measurement.CalibratedLengthMM)
	}
}

func TestMeasureLineMeasuresCalibratedLength(t *testing.T) {
	scale := contracts.MeasurementScale{
		RowSpacingMM:    0.2,
		ColumnSpacingMM: 0.3,
		Source:          "manualCalibration",
	}

	measurement := MeasureLine(
		contracts.AnnotationPoint{X: 10.0, Y: 8.0},
		contracts.AnnotationPoint{X: 14.0, Y: 11.0},
		&scale,
	)

	if measurement.PixelLength != 5.0 {
		t.Fatalf("pixel length = %v, want 5.0", measurement.PixelLength)
	}
	if measurement.CalibratedLengthMM == nil || *measurement.CalibratedLengthMM != 1.3 {
		t.Fatalf("calibrated length = %v, want 1.3", measurement.CalibratedLengthMM)
	}
}

func TestMeasureLineRoundsHalfAwayFromZero(t *testing.T) {
	measurement := MeasureLine(
		contracts.AnnotationPoint{X: 0.0, Y: 0.0},
		contracts.AnnotationPoint{X: 0.05, Y: 0.0},
		nil,
	)

	if measurement.PixelLength != 0.1 {
		t.Fatalf("pixel length = %v, want 0.1", measurement.PixelLength)
	}
}

func TestMeasureLineAnnotationAttachesMeasurement(t *testing.T) {
	annotation := contracts.LineAnnotation{
		ID:       "line-1",
		Label:    "Measurement",
		Source:   contracts.AnnotationManual,
		Start:    contracts.AnnotationPoint{X: 1, Y: 1},
		End:      contracts.AnnotationPoint{X: 4, Y: 5},
		Editable: true,
	}

	measured := MeasureLineAnnotation(annotation, nil)
	if measured.Measurement == nil {
		t.Fatal("measurement was not attached")
	}
	if measured.Measurement.PixelLength != 5.0 {
		t.Fatalf("pixel length = %v, want 5.0", measured.Measurement.PixelLength)
	}
	if annotation.Measurement != nil {
		t.Fatal("input annotation was mutated")
	}
}
