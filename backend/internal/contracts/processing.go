package contracts

const DefaultProcessingPresetID = "default"

type PaletteName string

const (
	PaletteNone PaletteName = "none"
	PaletteHot  PaletteName = "hot"
	PaletteBone PaletteName = "bone"
)

type ProcessingControls struct {
	Brightness int         `json:"brightness"`
	Contrast   float64     `json:"contrast"`
	Invert     bool        `json:"invert"`
	Equalize   bool        `json:"equalize"`
	Palette    PaletteName `json:"palette"`
}

type ProcessingPreset struct {
	ID       string             `json:"id"`
	Controls ProcessingControls `json:"controls"`
}

type ProcessingManifest struct {
	DefaultPresetID string             `json:"defaultPresetId"`
	Presets         []ProcessingPreset `json:"presets"`
}

var defaultProcessingPresets = []ProcessingPreset{
	{
		ID: DefaultProcessingPresetID,
		Controls: ProcessingControls{
			Brightness: 0,
			Contrast:   1.0,
			Invert:     false,
			Equalize:   false,
			Palette:    PaletteNone,
		},
	},
	{
		ID: "xray",
		Controls: ProcessingControls{
			Brightness: 10,
			Contrast:   1.4,
			Invert:     false,
			Equalize:   true,
			Palette:    PaletteBone,
		},
	},
	{
		ID: "high-contrast",
		Controls: ProcessingControls{
			Brightness: 0,
			Contrast:   1.8,
			Invert:     false,
			Equalize:   true,
			Palette:    PaletteNone,
		},
	},
}

func DefaultProcessingManifest() ProcessingManifest {
	presets := append([]ProcessingPreset(nil), defaultProcessingPresets...)

	return ProcessingManifest{
		DefaultPresetID: DefaultProcessingPresetID,
		Presets:         presets,
	}
}
