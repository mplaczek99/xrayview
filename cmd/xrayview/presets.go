package main

import (
	"encoding/json"
	"io"
	"strings"
)

type processingControls struct {
	Brightness int     `json:"brightness"`
	Contrast   float64 `json:"contrast"`
	Invert     bool    `json:"invert"`
	Equalize   bool    `json:"equalize"`
	Palette    string  `json:"palette"`
}

type processingPreset struct {
	ID       string             `json:"id"`
	Controls processingControls `json:"controls"`
}

type processingManifest struct {
	DefaultPresetID string             `json:"defaultPresetId"`
	Presets         []processingPreset `json:"presets"`
}

const defaultPresetID = "default"

var processingPresets = []processingPreset{
	{
		ID: defaultPresetID,
		Controls: processingControls{
			Brightness: 0,
			Contrast:   1.0,
			Invert:     false,
			Equalize:   false,
			Palette:    "none",
		},
	},
	{
		ID: "xray",
		Controls: processingControls{
			Brightness: 10,
			Contrast:   1.4,
			Invert:     false,
			Equalize:   true,
			Palette:    "bone",
		},
	},
	{
		ID: "high-contrast",
		Controls: processingControls{
			Brightness: 0,
			Contrast:   1.8,
			Invert:     false,
			Equalize:   true,
			Palette:    "none",
		},
	},
}

func processingManifestData() processingManifest {
	presets := make([]processingPreset, len(processingPresets))
	copy(presets, processingPresets)

	return processingManifest{
		DefaultPresetID: defaultPresetID,
		Presets:         presets,
	}
}

func writeProcessingManifest(w io.Writer) error {
	encoder := json.NewEncoder(w)
	return encoder.Encode(processingManifestData())
}

func lookupProcessingPreset(id string) (processingPreset, bool) {
	for _, preset := range processingPresets {
		if preset.ID == id {
			return preset, true
		}
	}

	return processingPreset{}, false
}

func supportedPresetIDs() []string {
	ids := make([]string, 0, len(processingPresets))
	for _, preset := range processingPresets {
		ids = append(ids, preset.ID)
	}

	return ids
}

func supportedPresetList() string {
	return strings.Join(supportedPresetIDs(), ", ")
}
