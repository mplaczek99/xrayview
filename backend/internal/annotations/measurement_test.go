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

	if got, want := measurement.PixelLength, 5.0; got != want {
		t.Fatalf("PixelLength = %v, want %v", got, want)
	}
	if measurement.CalibratedLengthMM != nil {
		t.Fatalf("CalibratedLengthMM = %v, want nil", *measurement.CalibratedLengthMM)
	}
}

func TestMeasureLineMeasuresCalibratedLength(t *testing.T) {
	measurement := MeasureLine(
		contracts.AnnotationPoint{X: 10.0, Y: 8.0},
		contracts.AnnotationPoint{X: 14.0, Y: 11.0},
		&contracts.MeasurementScale{
			RowSpacingMM:    0.2,
			ColumnSpacingMM: 0.3,
			Source:          "PixelSpacing",
		},
	)

	if got, want := measurement.PixelLength, 5.0; got != want {
		t.Fatalf("PixelLength = %v, want %v", got, want)
	}
	if measurement.CalibratedLengthMM == nil {
		t.Fatal("CalibratedLengthMM = nil, want 1.3")
	}
	if got, want := *measurement.CalibratedLengthMM, 1.3; got != want {
		t.Fatalf("CalibratedLengthMM = %v, want %v", got, want)
	}
}

func TestMeasureLineRoundsHalfAwayFromZero(t *testing.T) {
	measurement := MeasureLine(
		contracts.AnnotationPoint{X: 0.0, Y: 0.0},
		contracts.AnnotationPoint{X: 0.05, Y: 0.0},
		nil,
	)

	if got, want := measurement.PixelLength, 0.1; got != want {
		t.Fatalf("PixelLength = %v, want %v", got, want)
	}
}
