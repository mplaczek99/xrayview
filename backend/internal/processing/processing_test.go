package processing

import (
	"errors"
	"strings"
	"testing"

	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/render"
)

func TestProcessGrayscaleAppliesControlsInOrder(t *testing.T) {
	pixels := []byte{0, 64, 128, 255}

	mode := ProcessGrayscalePixels(pixels, GrayscaleControls{
		Invert:     true,
		Brightness: 10,
		Contrast:   1.0,
		Equalize:   false,
	})

	if mode != "inverted grayscale with brightness +10" {
		t.Fatalf("mode = %q", mode)
	}
	if string(pixels) != string([]byte{255, 201, 137, 10}) {
		t.Fatalf("pixels = %v", pixels)
	}
}

func TestProcessRenderedPreviewAppliesPaletteAndCompare(t *testing.T) {
	output, err := ProcessRenderedPreview(
		render.Gray(2, 1, []byte{0, 255}),
		GrayscaleControls{Contrast: 1.0},
		PaletteHot,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}

	if output.Mode != "comparison of grayscale and grayscale with hot palette" {
		t.Fatalf("mode = %q", output.Mode)
	}
	if output.Preview.Width != 4 || output.Preview.Height != 1 || output.Preview.Format != render.RGBA8 {
		t.Fatalf("preview = %+v", output.Preview)
	}
}

func TestProcessRenderedPreviewRejectsMismatchedGrayBuffer(t *testing.T) {
	_, err := ProcessRenderedPreview(
		render.Gray(2, 2, []byte{0, 255}),
		GrayscaleControls{Contrast: 1.0},
		PaletteNone,
		false,
	)
	if !errors.Is(err, ErrInvalidPreviewPixels) {
		t.Fatalf("error = %v, want ErrInvalidPreviewPixels", err)
	}
}

func TestCombineComparisonRejectsMismatchedRightBuffer(t *testing.T) {
	left := render.Gray(2, 1, []byte{0, 255})
	right := render.RGBA(2, 1, []byte{0, 0, 0, 255})

	_, err := combineComparison(left, right)
	if !errors.Is(err, ErrInvalidPreviewPixels) {
		t.Fatalf("error = %v, want ErrInvalidPreviewPixels", err)
	}
}

func TestResolveProcessStudyCommandUsesPresetDefaults(t *testing.T) {
	resolved, backendErr := ResolveProcessStudyCommand(contracts.ProcessStudyCommand{
		StudyID:  "study-1",
		PresetID: "xray",
		Equalize: true,
	})
	if backendErr != nil {
		t.Fatal(backendErr)
	}

	if resolved.Controls.Brightness != 10 {
		t.Fatalf("brightness = %d, want 10", resolved.Controls.Brightness)
	}
	if resolved.Controls.Contrast != 1.4 {
		t.Fatalf("contrast = %v, want 1.4", resolved.Controls.Contrast)
	}
	if resolved.Palette != PaletteBone {
		t.Fatalf("palette = %v, want PaletteBone", resolved.Palette)
	}
}

func TestResolveProcessStudyCommandRejectsZeroContrast(t *testing.T) {
	contrast := 0.0
	_, backendErr := ResolveProcessStudyCommand(contracts.ProcessStudyCommand{
		StudyID:  "study-1",
		PresetID: "default",
		Contrast: &contrast,
	})
	if backendErr == nil {
		t.Fatal("expected backend error")
	}
	if backendErr.Code != contracts.InvalidInput {
		t.Fatalf("code = %s, want invalidInput", backendErr.Code)
	}
	if !strings.Contains(backendErr.Message, "contrast must be >= 0.1") {
		t.Fatalf("message = %q", backendErr.Message)
	}
}
