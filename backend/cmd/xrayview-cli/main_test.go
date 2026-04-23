package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/processing"
	"xrayview/backend/internal/render"
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

func TestLegacyCLIDescribeCommandsReturnJSON(t *testing.T) {
	samplePath := sampleDicomPath(t)

	manifestStdout, manifestStderr, err := runLegacyCommand(
		"--describe-presets",
	)
	if err != nil {
		t.Fatalf("describe-presets returned error: %v\nstderr:\n%s", err, manifestStderr)
	}

	var manifest struct {
		DefaultPresetID string `json:"defaultPresetId"`
		Presets         []struct {
			ID string `json:"id"`
		} `json:"presets"`
	}
	if err := json.Unmarshal([]byte(manifestStdout), &manifest); err != nil {
		t.Fatalf("parse manifest stdout: %v\nstdout:\n%s", err, manifestStdout)
	}
	if got, want := manifest.DefaultPresetID, contracts.DefaultProcessingPresetID; got != want {
		t.Fatalf("DefaultPresetID = %q, want %q", got, want)
	}
	if got, want := len(manifest.Presets), 3; got != want {
		t.Fatalf("len(Presets) = %d, want %d", got, want)
	}

	studyStdout, studyStderr, err := runLegacyCommand(
		"--input", samplePath,
		"--describe-study",
	)
	if err != nil {
		t.Fatalf("describe-study returned error: %v\nstderr:\n%s", err, studyStderr)
	}

	var study map[string]any
	if err := json.Unmarshal([]byte(studyStdout), &study); err != nil {
		t.Fatalf("parse study stdout: %v\nstdout:\n%s", err, studyStdout)
	}
}

func TestLegacyCLIAllowsLeadingArgumentSeparator(t *testing.T) {
	stdout, stderr, err := runLegacyCommand(
		"--",
		"--describe-presets",
	)
	if err != nil {
		t.Fatalf("leading separator command returned error: %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, `"defaultPresetId":"default"`) {
		t.Fatalf("stdout missing manifest payload:\n%s", stdout)
	}
}

func TestLegacyCLIPreviewAndProcessWriteExpectedArtifacts(t *testing.T) {
	samplePath := sampleDicomPath(t)
	tempDir := t.TempDir()
	previewPath := filepath.Join(tempDir, "preview.png")
	processedPreviewPath := filepath.Join(tempDir, "processed-preview.png")
	outputPath := filepath.Join(tempDir, "processed.dcm")

	previewStdout, previewStderr, err := runLegacyCommand(
		"--input", samplePath,
		"--preview-output", previewPath,
	)
	if err != nil {
		t.Fatalf("preview command returned error: %v\nstderr:\n%s", err, previewStderr)
	}
	if _, err := os.Stat(previewPath); err != nil {
		t.Fatalf("preview output missing: %v", err)
	}
	if !strings.Contains(previewStdout, "loaded dicom image:") {
		t.Fatalf("preview stdout missing load summary:\n%s", previewStdout)
	}
	if !strings.Contains(previewStdout, "saved grayscale preview image:") {
		t.Fatalf("preview stdout missing preview summary:\n%s", previewStdout)
	}

	processStdout, processStderr, err := runLegacyCommand(
		"--input", samplePath,
		"--preview-output", processedPreviewPath,
		"--output", outputPath,
		"--preset", "xray",
	)
	if err != nil {
		t.Fatalf("process command returned error: %v\nstderr:\n%s", err, processStderr)
	}
	if _, err := os.Stat(processedPreviewPath); err != nil {
		t.Fatalf("processed preview output missing: %v", err)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("processed dicom output missing: %v", err)
	}
	if !strings.Contains(processStdout, "loaded dicom image:") {
		t.Fatalf("process stdout missing load summary:\n%s", processStdout)
	}
	if !strings.Contains(processStdout, "preview image:") {
		t.Fatalf("process stdout missing preview summary:\n%s", processStdout)
	}
	if !strings.Contains(processStdout, "dicom image:") {
		t.Fatalf("process stdout missing dicom summary:\n%s", processStdout)
	}
}

func TestLegacyCLIUsesDefaultOutputPathWhenNoOutputsProvided(t *testing.T) {
	sourcePath := sampleDicomPath(t)
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read sample source: %v", err)
	}

	tempDir := t.TempDir()
	inputPath := filepath.Join(tempDir, "study.dcm")
	if err := os.WriteFile(inputPath, sourceBytes, 0o644); err != nil {
		t.Fatalf("write copied study: %v", err)
	}

	stdout, stderr, err := runLegacyCommand(
		"--input", inputPath,
	)
	if err != nil {
		t.Fatalf("default-output process command returned error: %v\nstderr:\n%s", err, stderr)
	}

	defaultOutputPath := filepath.Join(tempDir, "study_processed.dcm")
	if _, err := os.Stat(defaultOutputPath); err != nil {
		t.Fatalf("default processed output missing: %v", err)
	}
	if !strings.Contains(stdout, defaultOutputPath) {
		t.Fatalf("stdout missing default output path %q:\n%s", defaultOutputPath, stdout)
	}
}

func runLegacyCommand(args ...string) (string, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func sampleDicomPath(t *testing.T) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current file path")
	}

	return filepath.Clean(
		filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "images", "sample-dental-radiograph.dcm"),
	)
}
