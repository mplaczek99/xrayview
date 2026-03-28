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
