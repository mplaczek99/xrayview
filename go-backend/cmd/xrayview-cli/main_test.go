package main

import (
	"testing"

	"xrayview/go-backend/internal/processing"
	"xrayview/go-backend/internal/render"
)

func TestParseProcessPreviewArgsAcceptsPaletteAndCompare(t *testing.T) {
	plan, controls, palette, compare, inputPath, outputPath, err := parseProcessPreviewArgs([]string{
		"--full-range",
		"--invert",
		"--brightness", "10",
		"--contrast", "1.4",
		"--equalize",
		"--palette", "bone",
		"--compare",
		"input.dcm",
		"output.png",
	})
	if err != nil {
		t.Fatalf("parseProcessPreviewArgs returned error: %v", err)
	}

	if got, want := plan.Window.Kind, render.WindowModeFullRange; got != want {
		t.Fatalf("Window.Kind = %v, want %v", got, want)
	}
	if !controls.Invert || !controls.Equalize {
		t.Fatalf("controls = %#v, want invert and equalize enabled", controls)
	}
	if got, want := controls.Brightness, 10; got != want {
		t.Fatalf("Brightness = %d, want %d", got, want)
	}
	if got, want := controls.Contrast, 1.4; got != want {
		t.Fatalf("Contrast = %v, want %v", got, want)
	}
	if got, want := palette, processing.PaletteBone; got != want {
		t.Fatalf("palette = %q, want %q", got, want)
	}
	if !compare {
		t.Fatal("compare = false, want true")
	}
	if got, want := inputPath, "input.dcm"; got != want {
		t.Fatalf("inputPath = %q, want %q", got, want)
	}
	if got, want := outputPath, "output.png"; got != want {
		t.Fatalf("outputPath = %q, want %q", got, want)
	}
}

func TestParseProcessPreviewArgsRejectsUnknownPalette(t *testing.T) {
	_, _, _, _, _, _, err := parseProcessPreviewArgs([]string{
		"--palette", "rainbow",
		"input.dcm",
		"output.png",
	})
	if err == nil {
		t.Fatal("parseProcessPreviewArgs returned nil error for unknown palette")
	}
}
