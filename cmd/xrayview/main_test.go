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
