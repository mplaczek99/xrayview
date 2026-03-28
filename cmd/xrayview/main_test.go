package main

import "testing"

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
