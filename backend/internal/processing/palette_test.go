package processing

import (
	"testing"

	"xrayview/backend/internal/imaging"
)

func TestNormalizePaletteNameAcceptsKnownNamesAndEmptyAsNone(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: PaletteNone},
		{name: "none", input: "none", want: PaletteNone},
		{name: "trimmed hot", input: " Hot ", want: PaletteHot},
		{name: "bone", input: "BONE", want: PaletteBone},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := NormalizePaletteName(testCase.input)
			if err != nil {
				t.Fatalf("NormalizePaletteName returned error: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("NormalizePaletteName(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
		})
	}
}

func TestNormalizePaletteNameRejectsUnknownNames(t *testing.T) {
	_, err := NormalizePaletteName("rainbow")
	if err == nil {
		t.Fatal("NormalizePaletteName returned nil error for unknown palette")
	}
}

func TestApplyNamedPaletteRequiresGrayPreviewInput(t *testing.T) {
	_, err := ApplyNamedPalette(imaging.RGBAPreview(1, 1, []uint8{0, 0, 0, 255}), PaletteBone)
	if err == nil {
		t.Fatal("ApplyNamedPalette returned nil error, want gray8 validation failure")
	}
}

func TestHotPaletteBreakpoints(t *testing.T) {
	if got, want := hotColor(0), [4]uint8{0, 0, 0, 255}; got != want {
		t.Fatalf("hotColor(0) = %v, want %v", got, want)
	}
	if got, want := hotColor(84), [4]uint8{252, 0, 0, 255}; got != want {
		t.Fatalf("hotColor(84) = %v, want %v", got, want)
	}
	if got, want := hotColor(85), [4]uint8{255, 0, 0, 255}; got != want {
		t.Fatalf("hotColor(85) = %v, want %v", got, want)
	}
	if got, want := hotColor(170), [4]uint8{255, 255, 0, 255}; got != want {
		t.Fatalf("hotColor(170) = %v, want %v", got, want)
	}
	if got, want := hotColor(255), [4]uint8{255, 255, 255, 255}; got != want {
		t.Fatalf("hotColor(255) = %v, want %v", got, want)
	}
}

func TestBonePaletteFormula(t *testing.T) {
	if got, want := boneColor(0), [4]uint8{0, 0, 0, 255}; got != want {
		t.Fatalf("boneColor(0) = %v, want %v", got, want)
	}
	if got, want := boneColor(128), [4]uint8{112, 120, 128, 255}; got != want {
		t.Fatalf("boneColor(128) = %v, want %v", got, want)
	}
	if got, want := boneColor(255), [4]uint8{255, 255, 255, 255}; got != want {
		t.Fatalf("boneColor(255) = %v, want %v", got, want)
	}
}

func TestApplyNamedPalettePromotesGrayPreviewToRGBA(t *testing.T) {
	preview := imaging.GrayPreview(2, 1, []uint8{0, 128})

	got, err := ApplyNamedPalette(preview, PaletteBone)
	if err != nil {
		t.Fatalf("ApplyNamedPalette returned error: %v", err)
	}

	if got.Format != imaging.FormatRGBA8 {
		t.Fatalf("Format = %q, want %q", got.Format, imaging.FormatRGBA8)
	}
	if got.Width != 2 || got.Height != 1 {
		t.Fatalf("size = %dx%d, want 2x1", got.Width, got.Height)
	}
	if want := []uint8{0, 0, 0, 255, 112, 120, 128, 255}; !equalBytes(got.Pixels, want) {
		t.Fatalf("Pixels = %v, want %v", got.Pixels, want)
	}
}
