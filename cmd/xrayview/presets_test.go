package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestProcessingManifestDataUsesBackendDefaults(t *testing.T) {
	manifest := processingManifestData()
	if manifest.DefaultPresetID != defaultPresetID {
		t.Fatalf("default preset = %q, want %q", manifest.DefaultPresetID, defaultPresetID)
	}

	if len(manifest.Presets) != 3 {
		t.Fatalf("len(presets) = %d, want %d", len(manifest.Presets), 3)
	}

	xray, ok := lookupProcessingPreset("xray")
	if !ok {
		t.Fatal("expected xray preset to exist")
	}

	if xray.Controls.Brightness != 10 {
		t.Fatalf("brightness = %d, want %d", xray.Controls.Brightness, 10)
	}
	if xray.Controls.Contrast != 1.4 {
		t.Fatalf("contrast = %g, want %g", xray.Controls.Contrast, 1.4)
	}
	if !xray.Controls.Equalize {
		t.Fatal("equalize = false, want true")
	}
	if xray.Controls.Palette != "bone" {
		t.Fatalf("palette = %q, want %q", xray.Controls.Palette, "bone")
	}
	if xray.Controls.Invert {
		t.Fatal("invert = true, want false")
	}
}

func TestWriteProcessingManifestProducesJSON(t *testing.T) {
	var output bytes.Buffer
	if err := writeProcessingManifest(&output); err != nil {
		t.Fatalf("writeProcessingManifest returned error: %v", err)
	}

	var manifest processingManifest
	if err := json.Unmarshal(output.Bytes(), &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	if manifest.DefaultPresetID != defaultPresetID {
		t.Fatalf("default preset = %q, want %q", manifest.DefaultPresetID, defaultPresetID)
	}
	if len(manifest.Presets) != len(processingPresets) {
		t.Fatalf("len(presets) = %d, want %d", len(manifest.Presets), len(processingPresets))
	}
}
