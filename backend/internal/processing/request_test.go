package processing

import (
	"testing"

	"xrayview/backend/internal/contracts"
)

func TestResolveProcessStudyCommandAppliesPresetAndExplicitOverrides(t *testing.T) {
	brightness := 24
	contrast := 2.25
	palette := contracts.PaletteHot

	resolved, err := ResolveProcessStudyCommand(contracts.ProcessStudyCommand{
		PresetID:   "xray",
		Invert:     true,
		Brightness: &brightness,
		Contrast:   &contrast,
		Equalize:   false,
		Compare:    true,
		Palette:    &palette,
	})
	if err != nil {
		t.Fatalf("ResolveProcessStudyCommand returned error: %v", err)
	}

	if got, want := resolved.Controls.Invert, true; got != want {
		t.Fatalf("Invert = %v, want %v", got, want)
	}
	if got, want := resolved.Controls.Brightness, brightness; got != want {
		t.Fatalf("Brightness = %d, want %d", got, want)
	}
	if got, want := resolved.Controls.Contrast, contrast; got != want {
		t.Fatalf("Contrast = %v, want %v", got, want)
	}
	if got, want := resolved.Controls.Equalize, true; got != want {
		t.Fatalf("Equalize = %v, want %v", got, want)
	}
	if got, want := resolved.Palette, PaletteHot; got != want {
		t.Fatalf("Palette = %q, want %q", got, want)
	}
	if got, want := resolved.Compare, true; got != want {
		t.Fatalf("Compare = %v, want %v", got, want)
	}
}

func TestResolveProcessStudyCommandRejectsUnsupportedPreset(t *testing.T) {
	_, err := ResolveProcessStudyCommand(contracts.ProcessStudyCommand{
		PresetID: "missing",
	})
	if err == nil {
		t.Fatal("ResolveProcessStudyCommand returned nil error, want invalid preset failure")
	}

	backendErr, ok := err.(contracts.BackendError)
	if !ok {
		t.Fatalf("error type = %T, want contracts.BackendError", err)
	}
	if got, want := backendErr.Code, contracts.BackendErrorCodeInvalidInput; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
}

func TestResolveProcessStudyCommandRejectsOutOfRangeBrightness(t *testing.T) {
	brightness := 999

	_, err := ResolveProcessStudyCommand(contracts.ProcessStudyCommand{
		PresetID:   "default",
		Brightness: &brightness,
	})
	if err == nil {
		t.Fatal("ResolveProcessStudyCommand returned nil error, want brightness validation failure")
	}
}

func TestResolveProcessStudyCommandRejectsInvalidPalette(t *testing.T) {
	palette := contracts.PaletteName("magma")

	_, err := ResolveProcessStudyCommand(contracts.ProcessStudyCommand{
		PresetID: "default",
		Palette:  &palette,
	})
	if err == nil {
		t.Fatal("ResolveProcessStudyCommand returned nil error, want palette validation failure")
	}
}
