package main

import "testing"

func TestDefaultOutputPath(t *testing.T) {
	got := defaultOutputPath("images/scan.jpg")
	want := "images/scan_processed.png"

	if got != want {
		t.Fatalf("output path = %q, want %q", got, want)
	}
}

func TestValidateConfigRejectsNonPNGOutput(t *testing.T) {
	err := validateConfig(config{
		inputPath:  "input.jpg",
		outputPath: "output.jpg",
	})
	if err == nil {
		t.Fatal("expected validation error for non-png output")
	}

	if err.Error() != "output path must end with .png" {
		t.Fatalf("error = %q, want %q", err.Error(), "output path must end with .png")
	}
}

func TestPipelineStepsParsesExplicitOrder(t *testing.T) {
	got, err := pipelineSteps("grayscale, contrast, invert")
	if err != nil {
		t.Fatalf("pipelineSteps returned error: %v", err)
	}

	want := []string{"grayscale", "contrast", "invert"}
	if len(got) != len(want) {
		t.Fatalf("len(steps) = %d, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("steps[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestValidateConfigRejectsUnknownPipelineStep(t *testing.T) {
	err := validateConfig(config{
		inputPath:  "input.jpg",
		outputPath: "output.png",
		preset:     "default",
		palette:    "none",
		pipeline:   "grayscale,sharpen",
	})
	if err == nil {
		t.Fatal("expected validation error for unknown pipeline step")
	}

	if err.Error() != "unknown pipeline step: sharpen" {
		t.Fatalf("error = %q, want %q", err.Error(), "unknown pipeline step: sharpen")
	}
}

func TestApplyPresetUsesPresetAndKeepsExplicitOverrides(t *testing.T) {
	cfg, err := applyPreset(config{
		preset:     "xray",
		brightness: 5,
	}, map[string]bool{"brightness": true})
	if err != nil {
		t.Fatalf("applyPreset returned error: %v", err)
	}

	if cfg.brightness != 5 {
		t.Fatalf("brightness = %d, want %d", cfg.brightness, 5)
	}
	if cfg.contrast != 1.4 {
		t.Fatalf("contrast = %g, want %g", cfg.contrast, 1.4)
	}
	if !cfg.equalize {
		t.Fatal("equalize = false, want true")
	}
	if cfg.palette != "bone" {
		t.Fatalf("palette = %q, want %q", cfg.palette, "bone")
	}
}

func TestValidateConfigRejectsUnknownPreset(t *testing.T) {
	err := validateConfig(config{
		inputPath:  "input.jpg",
		outputPath: "output.png",
		preset:     "unknown",
		palette:    "none",
	})
	if err == nil {
		t.Fatal("expected validation error for unknown preset")
	}

	if err.Error() != "preset must be one of: default, xray, high-contrast" {
		t.Fatalf("error = %q, want %q", err.Error(), "preset must be one of: default, xray, high-contrast")
	}
}
