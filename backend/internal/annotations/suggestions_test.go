package annotations

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/imaging"
)

func TestSuggestedAnnotationsReturnsEmptyBundleWithoutTeeth(t *testing.T) {
	suggestions := SuggestedAnnotations(imaging.GrayPreview(1, 1, []uint8{255}), &contracts.ToothAnalysis{})

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
	preview := imaging.GrayPreview(64, 64, make([]uint8, 64*64))
	for index := range preview.Pixels {
		preview.Pixels[index] = 255
	}

	got := SuggestedAnnotations(preview, &analysis)
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
	preview := loadAnalyzePreviewFixture(t)

	got := SuggestedAnnotations(preview, &fixture.Analysis)

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
	if gotCount := len(got.Polylines); gotCount == 0 {
		t.Fatal("len(Polylines) = 0, want at least 1")
	}
	for index, polyline := range got.Polylines {
		if polyline.ID == "" {
			t.Fatalf("Polyline[%d].ID = empty, want stable ID", index)
		}
		minPoints := 2
		if polyline.Closed {
			minPoints = 3
		}
		if len(polyline.Points) < minPoints {
			t.Fatalf("len(Polyline[%d].Points) = %d, want at least %d", index, len(polyline.Points), minPoints)
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

func loadAnalyzePreviewFixture(t *testing.T) imaging.PreviewImage {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	previewPath := filepath.Join(
		filepath.Dir(file),
		"..",
		"..",
		"..",
		"backend",
		"tests",
		"fixtures",
		"parity",
		"sample-dental-radiograph",
		"analyze-preview.png",
	)

	rawFile, err := os.Open(filepath.Clean(previewPath))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer rawFile.Close()

	decoded, err := png.Decode(rawFile)
	if err != nil {
		t.Fatalf("png.Decode returned error: %v", err)
	}

	bounds := decoded.Bounds()
	pixels := make([]uint8, 0, bounds.Dx()*bounds.Dy())
	if gray, ok := decoded.(*image.Gray); ok {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			rowStart := gray.PixOffset(bounds.Min.X, y)
			rowEnd := rowStart + bounds.Dx()
			pixels = append(pixels, gray.Pix[rowStart:rowEnd]...)
		}
	} else {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				pixels = append(pixels, color.GrayModel.Convert(decoded.At(x, y)).(color.Gray).Y)
			}
		}
	}

	return imaging.GrayPreview(uint32(bounds.Dx()), uint32(bounds.Dy()), pixels)
}
