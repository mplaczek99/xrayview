package processing

import (
	"fmt"
	"math"
	"strings"

	"xrayview/backend/internal/contracts"
)

type ResolvedProcessStudy struct {
	Controls GrayscaleControls
	Palette  string
	Compare  bool
}

func ResolveProcessStudyCommand(
	command contracts.ProcessStudyCommand,
) (ResolvedProcessStudy, error) {
	manifest := contracts.DefaultProcessingManifest()
	presetID := strings.ToLower(strings.TrimSpace(command.PresetID))
	preset, ok := lookupPreset(manifest, presetID)
	if !ok {
		return ResolvedProcessStudy{}, contracts.InvalidInput(
			fmt.Sprintf("preset must be one of: %s", supportedPresetList(manifest)),
		)
	}

	if command.Brightness != nil {
		value := *command.Brightness
		if value < -256 || value > 256 {
			return ResolvedProcessStudy{}, contracts.InvalidInput(
				fmt.Sprintf("brightness must be between -256 and 256, got %d", value),
			)
		}
	}

	if command.Contrast != nil {
		value := *command.Contrast
		if !isFiniteNumber(value) || value < 0.0 {
			return ResolvedProcessStudy{}, contracts.InvalidInput(
				fmt.Sprintf("contrast must be >= 0.0, got %v", value),
			)
		}
	}

	if command.Palette != nil {
		if _, err := NormalizePaletteName(string(*command.Palette)); err != nil {
			return ResolvedProcessStudy{}, contracts.InvalidInput(err.Error())
		}
	}

	palette := string(preset.Controls.Palette)
	if command.Palette != nil {
		palette = string(*command.Palette)
	}

	normalizedPalette, err := NormalizePaletteName(palette)
	if err != nil {
		return ResolvedProcessStudy{}, contracts.InvalidInput(err.Error())
	}

	return ResolvedProcessStudy{
		Controls: GrayscaleControls{
			Invert:     command.Invert || preset.Controls.Invert,
			Brightness: valueOr(command.Brightness, preset.Controls.Brightness),
			Contrast:   valueOr(command.Contrast, preset.Controls.Contrast),
			Equalize:   command.Equalize || preset.Controls.Equalize,
		},
		Palette: normalizedPalette,
		Compare: command.Compare,
	}, nil
}

func lookupPreset(
	manifest contracts.ProcessingManifest,
	presetID string,
) (contracts.ProcessingPreset, bool) {
	for _, preset := range manifest.Presets {
		if preset.ID == presetID {
			return preset, true
		}
	}

	return contracts.ProcessingPreset{}, false
}

func supportedPresetList(manifest contracts.ProcessingManifest) string {
	supported := make([]string, 0, len(manifest.Presets))
	for _, preset := range manifest.Presets {
		supported = append(supported, preset.ID)
	}

	return strings.Join(supported, ", ")
}

func valueOr[T any](value *T, fallback T) T {
	if value == nil {
		return fallback
	}

	return *value
}

func isFiniteNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
