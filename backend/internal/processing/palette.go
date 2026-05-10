package processing

import (
	"fmt"
	"strings"
	"unsafe"

	"xrayview/backend/internal/bufpool"
	"xrayview/backend/internal/imaging"
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

	var lookup [256]uint32
	for index := range lookup {
		lookup[index] = rgbaUint32(colorFn(uint8(index)))
	}

	pixels := bufpool.GetUint8(len(preview.Pixels) * 4)
	if len(preview.Pixels) > 0 {
		rgbaPixels := unsafe.Slice((*uint32)(unsafe.Pointer(&pixels[0])), len(preview.Pixels))
		for index, value := range preview.Pixels {
			rgbaPixels[index] = lookup[value]
		}
	}

	return imaging.RGBAPreviewWithRelease(preview.Width, preview.Height, pixels, bufpool.PutUint8), nil
}

func rgbaUint32(rgba [4]uint8) uint32 {
	return uint32(rgba[0]) |
		uint32(rgba[1])<<8 |
		uint32(rgba[2])<<16 |
		uint32(rgba[3])<<24
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
