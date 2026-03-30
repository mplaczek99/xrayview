package main

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestDefaultOutputPath(t *testing.T) {
	got := defaultOutputPath("images/scan.dcm")
	want := "images/scan_processed.dcm"

	if got != want {
		t.Fatalf("output path = %q, want %q", got, want)
	}
}

func TestValidateConfigRejectsUnsupportedOutput(t *testing.T) {
	err := validateConfig(config{
		inputPath:  "input.dcm",
		outputPath: "output.jpg",
	})
	if err == nil {
		t.Fatal("expected validation error for unsupported output")
	}

	if err.Error() != "output path must end with .dcm or .dicom" {
		t.Fatalf("error = %q, want %q", err.Error(), "output path must end with .dcm or .dicom")
	}
}

func TestValidateConfigAllowsDICOMOutput(t *testing.T) {
	err := validateConfig(config{
		inputPath:  "input.dcm",
		outputPath: "output.dcm",
		preset:     "default",
		palette:    "none",
	})
	if err != nil {
		t.Fatalf("validateConfig returned error: %v", err)
	}
}

func TestValidateConfigRejectsNonDICOMInput(t *testing.T) {
	err := validateConfig(config{
		inputPath:  "input.jpg",
		outputPath: "output.dcm",
		preset:     "default",
		palette:    "none",
	})
	if err == nil {
		t.Fatal("expected validation error for non-dicom input")
	}

	if err.Error() != "input path must end with .dcm or .dicom" {
		t.Fatalf("error = %q, want %q", err.Error(), "input path must end with .dcm or .dicom")
	}
}

func TestValidateConfigAllowsPreviewOutputWithoutDICOMSave(t *testing.T) {
	err := validateConfig(config{
		inputPath:         "input.dcm",
		previewOutputPath: "preview.png",
		preset:            "default",
		palette:           "none",
	})
	if err != nil {
		t.Fatalf("validateConfig returned error: %v", err)
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
		inputPath:  "input.dcm",
		outputPath: "output.dcm",
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
		inputPath:  "input.dcm",
		outputPath: "output.dcm",
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

func TestValidateConfigRejectsNonFiniteContrast(t *testing.T) {
	err := validateConfig(config{
		inputPath:  "input.dcm",
		outputPath: "output.dcm",
		preset:     "default",
		palette:    "none",
		contrast:   math.NaN(),
	})
	if err == nil {
		t.Fatal("expected validation error for non-finite contrast")
	}

	if err.Error() != "contrast must be a finite value greater than or equal to 0" {
		t.Fatalf("error = %q, want %q", err.Error(), "contrast must be a finite value greater than or equal to 0")
	}
}

func TestPipelineStepsRejectsDuplicateSteps(t *testing.T) {
	_, err := pipelineSteps("grayscale,contrast,contrast")
	if err == nil {
		t.Fatal("expected validation error for duplicate pipeline steps")
	}

	if err.Error() != "duplicate pipeline step: contrast" {
		t.Fatalf("error = %q, want %q", err.Error(), "duplicate pipeline step: contrast")
	}
}

func TestProcessImageCustomPipelineKeepsOmittedEnabledFilters(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	src.Set(0, 0, color.RGBA{R: 100, G: 100, B: 100, A: 255})

	output, _ := processImage(src, config{
		brightness: 20,
		contrast:   2.0,
		pipeline:   "contrast",
	})

	gray, ok := output.(*image.Gray)
	if !ok {
		t.Fatalf("output type = %T, want *image.Gray", output)
	}

	if got := gray.GrayAt(0, 0).Y; got != 92 {
		t.Fatalf("pixel = %d, want %d", got, 92)
	}
}
