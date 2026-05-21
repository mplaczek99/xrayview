package contractv1

const DefaultProcessingPresetID = "default"

func NewBackendError(code BackendErrorCode, message string) BackendError {
	return BackendError{
		Code:        code,
		Message:     message,
		Recoverable: code != BackendErrorCodeInternal,
	}
}

func (err BackendError) Error() string {
	return err.Message
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
