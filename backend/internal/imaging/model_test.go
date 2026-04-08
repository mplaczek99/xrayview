package imaging

import (
	"encoding/json"
	"testing"
)

func TestSourceImageUnmarshalDefaultsFormatForHelperCompatibility(t *testing.T) {
	var image SourceImage
	if err := json.Unmarshal([]byte(`{
		"width": 2,
		"height": 2,
		"pixels": [0, 64, 128, 255],
		"minValue": 0,
		"maxValue": 255,
		"invert": false
	}`), &image); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}

	if got, want := image.Format, FormatGrayFloat32; got != want {
		t.Fatalf("Format = %q, want %q", got, want)
	}
}

func TestSourceImageValidateAcceptsSharedDecodeModel(t *testing.T) {
	image := SourceImage{
		Width:    2,
		Height:   2,
		Format:   FormatGrayFloat32,
		Pixels:   []float32{0, 64, 128, 255},
		MinValue: 0,
		MaxValue: 255,
		DefaultWindow: &WindowLevel{
			Center: 127.5,
			Width:  255,
		},
	}

	if err := image.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestSourceImageValidateRejectsInvalidShape(t *testing.T) {
	image := SourceImage{
		Width:    2,
		Height:   2,
		Format:   FormatGrayFloat32,
		Pixels:   []float32{0, 64, 128},
		MinValue: 0,
		MaxValue: 255,
	}

	err := image.Validate()
	if err == nil {
		t.Fatal("Validate returned nil error, want pixel-count failure")
	}
}

func TestPreviewImageValidateAcceptsGrayAndRGBAFormats(t *testing.T) {
	gray := GrayPreview(2, 2, []uint8{0, 64, 128, 255})
	if err := gray.Validate(); err != nil {
		t.Fatalf("gray Validate returned error: %v", err)
	}

	rgba := RGBAPreview(1, 1, []uint8{1, 2, 3, 255})
	if err := rgba.Validate(); err != nil {
		t.Fatalf("rgba Validate returned error: %v", err)
	}
}

func TestPreviewImageValidateRejectsUnknownFormat(t *testing.T) {
	image := PreviewImage{
		Width:  1,
		Height: 1,
		Format: FormatGrayFloat32,
		Pixels: []uint8{0},
	}

	err := image.Validate()
	if err == nil {
		t.Fatal("Validate returned nil error, want format failure")
	}
}
