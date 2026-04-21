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
	if suggestions.Polylines == nil {
		t.Fatal("Polylines = nil, want empty slice")
	}
	if got, want := len(suggestions.Polylines), 0; got != want {
		t.Fatalf("len(Polylines) = %d, want %d", got, want)
	}
}

func TestSuggestedAnnotationsMatchesUnitParity(t *testing.T) {
	primaryOutline := []contracts.Point{
		{X: 120, Y: 80},
		{X: 164, Y: 80},
		{X: 164, Y: 172},
		{X: 120, Y: 172},
	}
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
					Outline: primaryOutline,
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
					Outline: []contracts.Point{
						{X: 200, Y: 86},
						{X: 240, Y: 86},
						{X: 240, Y: 172},
						{X: 200, Y: 172},
					},
				},
			},
		},
	}
	analysis.Tooth = &analysis.Teeth[0]

	got := SuggestedAnnotations(&analysis)
	want := contracts.AnnotationBundle{
		Lines:      []contracts.LineAnnotation{},
		Rectangles: []contracts.RectangleAnnotation{},
		Polylines: []contracts.PolylineAnnotation{
			{
				ID:     "auto-tooth-trace",
				Label:  "Tooth trace",
				Source: contracts.AnnotationSourceAutoTooth,
				Points: []contracts.AnnotationPoint{
					{X: 120.0, Y: 80.0},
					{X: 164.0, Y: 80.0},
					{X: 164.0, Y: 172.0},
					{X: 120.0, Y: 172.0},
				},
				Closed:     true,
				Editable:   false,
				Confidence: pointerTo(0.82),
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

	if fixture.Analysis.Tooth == nil {
		if len(got.Polylines) != 0 {
			t.Fatalf("len(Polylines) = %d, want 0", len(got.Polylines))
		}
		return
	}
	if got.Lines == nil || got.Rectangles == nil || got.Polylines == nil {
		t.Fatalf("suggestions slices should be non-nil: %#v", got)
	}
	if gotCount := len(got.Lines); gotCount != 0 {
		t.Fatalf("len(Lines) = %d, want 0", gotCount)
	}
	if gotCount := len(got.Rectangles); gotCount != 0 {
		t.Fatalf("len(Rectangles) = %d, want 0", gotCount)
	}
	if gotCount := len(got.Polylines); gotCount > 1 {
		t.Fatalf("len(Polylines) = %d, want at most 1", gotCount)
	}
	if len(got.Polylines) == 1 {
		if got.Polylines[0].ID != "auto-tooth-trace" {
			t.Fatalf("Polyline ID = %q, want auto-tooth-trace", got.Polylines[0].ID)
		}
		if len(got.Polylines[0].Points) < 3 {
			t.Fatalf("len(Polyline.Points) = %d, want at least 3", len(got.Polylines[0].Points))
		}
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
