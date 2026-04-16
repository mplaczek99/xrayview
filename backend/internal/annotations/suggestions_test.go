package annotations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"xrayview/backend/internal/contracts"
)

func TestSuggestedAnnotationsReturnsEmptyBundleWithoutTeeth(t *testing.T) {
	suggestions := SuggestedAnnotations(&contracts.ToothAnalysis{})

	if suggestions.Lines == nil {
		t.Fatal("Lines = nil, want empty slice")
	}
	if suggestions.Rectangles == nil {
		t.Fatal("Rectangles = nil, want empty slice")
	}
	if got, want := len(suggestions.Lines), 0; got != want {
		t.Fatalf("len(Lines) = %d, want %d", got, want)
	}
	if got, want := len(suggestions.Rectangles), 0; got != want {
		t.Fatalf("len(Rectangles) = %d, want %d", got, want)
	}
}

func TestSuggestedAnnotationsMatchesUnitParity(t *testing.T) {
	analysis := contracts.ToothAnalysis{
		Image: contracts.ToothImageMetadata{
			Width:  640,
			Height: 480,
		},
		Calibration: contracts.ToothCalibration{
			PixelUnits: "px",
			MeasurementScale: &contracts.MeasurementScale{
				RowSpacingMM:    0.2,
				ColumnSpacingMM: 0.3,
				Source:          "PixelSpacing",
			},
			RealWorldMeasurementsAvailable: true,
		},
		Teeth: []contracts.ToothCandidate{
			{
				Confidence:     0.82,
				MaskAreaPixels: 1200,
				Measurements: contracts.ToothMeasurementBundle{
					Pixel: contracts.ToothMeasurementValues{
						ToothWidth:        40.0,
						ToothHeight:       80.0,
						BoundingBoxWidth:  42.0,
						BoundingBoxHeight: 88.0,
						Units:             "px",
					},
				},
				Geometry: contracts.ToothGeometry{
					BoundingBox: contracts.BoundingBox{
						X:      120,
						Y:      80,
						Width:  44,
						Height: 92,
					},
					WidthLine: contracts.LineSegment{
						Start: contracts.Point{X: 122, Y: 96},
						End:   contracts.Point{X: 160, Y: 96},
					},
					HeightLine: contracts.LineSegment{
						Start: contracts.Point{X: 141, Y: 84},
						End:   contracts.Point{X: 141, Y: 170},
					},
				},
			},
			{
				Confidence:     0.77,
				MaskAreaPixels: 990,
				Measurements: contracts.ToothMeasurementBundle{
					Pixel: contracts.ToothMeasurementValues{
						ToothWidth:        36.0,
						ToothHeight:       76.0,
						BoundingBoxWidth:  40.0,
						BoundingBoxHeight: 84.0,
						Units:             "px",
					},
				},
				Geometry: contracts.ToothGeometry{
					BoundingBox: contracts.BoundingBox{
						X:      200,
						Y:      86,
						Width:  40,
						Height: 86,
					},
					WidthLine: contracts.LineSegment{
						Start: contracts.Point{X: 202, Y: 104},
						End:   contracts.Point{X: 236, Y: 104},
					},
					HeightLine: contracts.LineSegment{
						Start: contracts.Point{X: 220, Y: 90},
						End:   contracts.Point{X: 220, Y: 172},
					},
				},
			},
		},
	}

	got := SuggestedAnnotations(&analysis)
	want := contracts.AnnotationBundle{
		Lines: []contracts.LineAnnotation{
			{
				ID:         "auto-tooth-1-width",
				Label:      "Tooth 1 width",
				Source:     contracts.AnnotationSourceAutoTooth,
				Start:      contracts.AnnotationPoint{X: 122.0, Y: 96.0},
				End:        contracts.AnnotationPoint{X: 160.0, Y: 96.0},
				Editable:   true,
				Confidence: pointerTo(0.82),
				Measurement: pointerTo(contracts.LineMeasurement{
					PixelLength:        38.0,
					CalibratedLengthMM: pointerTo(11.4),
				}),
			},
			{
				ID:         "auto-tooth-1-height",
				Label:      "Tooth 1 height",
				Source:     contracts.AnnotationSourceAutoTooth,
				Start:      contracts.AnnotationPoint{X: 141.0, Y: 84.0},
				End:        contracts.AnnotationPoint{X: 141.0, Y: 170.0},
				Editable:   true,
				Confidence: pointerTo(0.82),
				Measurement: pointerTo(contracts.LineMeasurement{
					PixelLength:        86.0,
					CalibratedLengthMM: pointerTo(17.2),
				}),
			},
			{
				ID:         "auto-tooth-2-width",
				Label:      "Tooth 2 width",
				Source:     contracts.AnnotationSourceAutoTooth,
				Start:      contracts.AnnotationPoint{X: 202.0, Y: 104.0},
				End:        contracts.AnnotationPoint{X: 236.0, Y: 104.0},
				Editable:   true,
				Confidence: pointerTo(0.77),
				Measurement: pointerTo(contracts.LineMeasurement{
					PixelLength:        34.0,
					CalibratedLengthMM: pointerTo(10.2),
				}),
			},
			{
				ID:         "auto-tooth-2-height",
				Label:      "Tooth 2 height",
				Source:     contracts.AnnotationSourceAutoTooth,
				Start:      contracts.AnnotationPoint{X: 220.0, Y: 90.0},
				End:        contracts.AnnotationPoint{X: 220.0, Y: 172.0},
				Editable:   true,
				Confidence: pointerTo(0.77),
				Measurement: pointerTo(contracts.LineMeasurement{
					PixelLength:        82.0,
					CalibratedLengthMM: pointerTo(16.4),
				}),
			},
		},
		Rectangles: []contracts.RectangleAnnotation{
			{
				ID:         "auto-tooth-1-bounding-box",
				Label:      "Tooth 1 bounding box",
				Source:     contracts.AnnotationSourceAutoTooth,
				X:          120.0,
				Y:          80.0,
				Width:      44.0,
				Height:     92.0,
				Editable:   false,
				Confidence: pointerTo(0.82),
			},
			{
				ID:         "auto-tooth-2-bounding-box",
				Label:      "Tooth 2 bounding box",
				Source:     contracts.AnnotationSourceAutoTooth,
				X:          200.0,
				Y:          86.0,
				Width:      40.0,
				Height:     86.0,
				Editable:   false,
				Confidence: pointerTo(0.77),
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("suggestions = %#v, want %#v", got, want)
	}
}

func TestSuggestedAnnotationsMatchesFixtureOutput(t *testing.T) {
	fixture := loadAnalyzeFixture(t)

	got := SuggestedAnnotations(&fixture.Analysis)

	if !reflect.DeepEqual(got, fixture.SuggestedAnnotations) {
		t.Fatalf("suggestions = %#v, want %#v", got, fixture.SuggestedAnnotations)
	}
}

type analyzeFixture struct {
	Analysis             contracts.ToothAnalysis    `json:"analysis"`
	SuggestedAnnotations contracts.AnnotationBundle `json:"suggestedAnnotations"`
}

func loadAnalyzeFixture(t *testing.T) analyzeFixture {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	fixturePath := filepath.Join(
		filepath.Dir(file),
		"..",
		"..",
		"..",
		"backend",
		"tests",
		"fixtures",
		"parity",
		"sample-dental-radiograph",
		"analyze-study.json",
	)

	raw, err := os.ReadFile(filepath.Clean(fixturePath))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var fixture analyzeFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	return fixture
}
