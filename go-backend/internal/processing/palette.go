package processing

import (
	"fmt"
	"strings"

	"xrayview/go-backend/internal/imaging"
)

const (
	PaletteNone = "none"
	PaletteHot  = "hot"
	PaletteBone = "bone"
)

func NormalizePaletteName(name string) (string, error) {
	switch normalized := strings.ToLower(strings.TrimSpace(name)); normalized {
	case "", PaletteNone:
		return PaletteNone, nil
	case PaletteHot, PaletteBone:
		return normalized, nil
	default:
		return "", fmt.Errorf("palette must be one of: none, hot, bone")
	}
}

func ApplyNamedPalette(
	preview imaging.PreviewImage,
	name string,
) (imaging.PreviewImage, error) {
	if err := preview.Validate(); err != nil {
		return imaging.PreviewImage{}, fmt.Errorf("validate preview image: %w", err)
	}
	if preview.Format != imaging.FormatGray8 {
		return imaging.PreviewImage{}, fmt.Errorf(
			"pseudocolor palettes require %q preview input",
			imaging.FormatGray8,
		)
	}

	palette, err := NormalizePaletteName(name)
	if err != nil {
		return imaging.PreviewImage{}, err
	}

	var colorFn func(uint8) [4]uint8
	switch palette {
	case PaletteHot:
		colorFn = hotColor
	case PaletteBone:
		colorFn = boneColor
	default:
		return imaging.PreviewImage{}, fmt.Errorf("palette must be one of: none, hot, bone")
	}

	var lookup [256][4]uint8
	for index := range lookup {
		lookup[index] = colorFn(uint8(index))
	}

	pixels := make([]uint8, len(preview.Pixels)*4)
	for index, value := range preview.Pixels {
		base := index * 4
		copy(pixels[base:base+4], lookup[value][:])
	}

	return imaging.RGBAPreview(preview.Width, preview.Height, pixels), nil
}

func hotColor(value uint8) [4]uint8 {
	switch {
	case value <= 84:
		return [4]uint8{value * 3, 0, 0, 255}
	case value <= 169:
		return [4]uint8{255, (value - 85) * 3, 0, 255}
	default:
		return [4]uint8{255, 255, (value - 170) * 3, 255}
	}
}

func boneColor(value uint8) [4]uint8 {
	intValue := int(value)
	whiteBoost := max(intValue-128, 0)

	return [4]uint8{
		clampPaletteValue((intValue*7)/8 + whiteBoost),
		clampPaletteValue((intValue*7)/8 + whiteBoost + intValue/16),
		clampPaletteValue(intValue + whiteBoost/2),
		255,
	}
}

func clampPaletteValue(value int) uint8 {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}

	return uint8(value)
}
