package processing

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/render"
)

const MinContrast = 0.1

// GrayscaleControls are applied in this exact order: invert, brightness,
// contrast, then optional histogram equalization.
type GrayscaleControls struct {
	Invert     bool
	Brightness int
	Contrast   float64
	Equalize   bool
}

// ResolvedProcessStudy is a command after preset defaults have been merged.
type ResolvedProcessStudy struct {
	Controls GrayscaleControls
	Palette  Palette
	Compare  bool
}

// PipelineOutput contains the processed preview plus the mode string shown in
// the UI footer and CLI output.
type PipelineOutput struct {
	Preview render.PreviewImage
	Mode    string
}

// Processing errors match the Rust error messages so tests and user-facing
// command failures remain stable as packages migrate.
var (
	ErrNonGray8Input            = errors.New("grayscale processing requires Gray8 preview input")
	ErrPaletteRequiresGray8     = errors.New("pseudocolor palettes require Gray8 preview input")
	ErrCompareRequiresGray8Left = errors.New("compare preview requires Gray8 source on the left side")
	ErrCompareDimensionMismatch = errors.New("compare preview requires matching image dimensions")
	ErrCompareWidthOverflow     = errors.New("compare preview width overflow")
	ErrCompareSizeOverflow      = errors.New("compare preview size overflow")
	ErrInvalidPreviewPixels     = errors.New("preview pixel length does not match dimensions")
	ErrUnknownPalette           = errors.New("palette must be one of: none, hot, bone")
)

// Palette is the internal palette representation used by the processing
// pipeline.
type Palette int

const (
	PaletteNone Palette = iota
	PaletteHot
	PaletteBone
)

func (palette Palette) Label() string {
	switch palette {
	case PaletteNone:
		return "none"
	case PaletteHot:
		return "hot"
	case PaletteBone:
		return "bone"
	default:
		return "unknown"
	}
}

// ResolveProcessStudyCommand validates a process command and merges unset
// optional fields with the selected preset defaults.
func ResolveProcessStudyCommand(command contracts.ProcessStudyCommand) (ResolvedProcessStudy, *contracts.BackendError) {
	manifest := contracts.DefaultProcessingManifest()
	presetID := strings.ToLower(strings.TrimSpace(command.PresetID))

	var preset *contracts.ProcessingPreset
	for index := range manifest.Presets {
		if manifest.Presets[index].ID == presetID {
			preset = &manifest.Presets[index]
			break
		}
	}
	if preset == nil {
		ids := make([]string, len(manifest.Presets))
		for index, preset := range manifest.Presets {
			ids[index] = preset.ID
		}
		err := contracts.InvalidInputError(fmt.Sprintf("preset must be one of: %s", strings.Join(ids, ", ")))
		return ResolvedProcessStudy{}, &err
	}

	if command.Brightness != nil && (*command.Brightness < -256 || *command.Brightness > 256) {
		err := contracts.InvalidInputError(fmt.Sprintf("brightness must be between -256 and 256, got %d", *command.Brightness))
		return ResolvedProcessStudy{}, &err
	}
	if command.Contrast != nil && (math.IsNaN(*command.Contrast) || math.IsInf(*command.Contrast, 0) || *command.Contrast < MinContrast) {
		err := contracts.InvalidInputError(fmt.Sprintf("contrast must be >= %g, got %v", MinContrast, *command.Contrast))
		return ResolvedProcessStudy{}, &err
	}

	brightness := preset.Controls.Brightness
	if command.Brightness != nil {
		brightness = *command.Brightness
	}
	contrast := preset.Controls.Contrast
	if command.Contrast != nil {
		contrast = *command.Contrast
	}
	paletteName := preset.Controls.Palette
	if command.Palette != nil {
		paletteName = *command.Palette
	}

	return ResolvedProcessStudy{
		Controls: GrayscaleControls{
			Invert:     command.Invert,
			Brightness: brightness,
			Contrast:   contrast,
			Equalize:   command.Equalize,
		},
		Palette: paletteFromContract(paletteName),
		Compare: command.Compare,
	}, nil
}

// ProcessRenderedPreview runs the full processing pipeline: controls, palette,
// then optional side-by-side comparison.
func ProcessRenderedPreview(sourcePreview render.PreviewImage, controls GrayscaleControls, palette Palette, compare bool) (PipelineOutput, error) {
	if sourcePreview.Format != render.Gray8 {
		return PipelineOutput{}, ErrNonGray8Input
	}
	if err := validatePreviewPixels(sourcePreview); err != nil {
		return PipelineOutput{}, err
	}

	workingPixels := append([]byte(nil), sourcePreview.Pixels...)
	working := render.Gray(sourcePreview.Width, sourcePreview.Height, workingPixels)
	var sourceForCompare *render.PreviewImage
	if compare {
		snapshotPixels := append([]byte(nil), sourcePreview.Pixels...)
		snapshot := render.Gray(sourcePreview.Width, sourcePreview.Height, snapshotPixels)
		sourceForCompare = &snapshot
	}

	mode := ProcessGrayscalePixels(working.Pixels, controls)
	if palette != PaletteNone {
		mode = fmt.Sprintf("%s with %s palette", mode, palette.Label())
		paletted, err := applyNamedPalette(working, palette)
		if err != nil {
			return PipelineOutput{}, err
		}
		working = paletted
	}
	if sourceForCompare != nil {
		combined, err := combineComparison(*sourceForCompare, working)
		if err != nil {
			return PipelineOutput{}, err
		}
		working = combined
		mode = fmt.Sprintf("comparison of grayscale and %s", mode)
	}

	return PipelineOutput{Preview: working, Mode: mode}, nil
}

// ProcessGrayscalePixels applies controls in place and returns the stable
// human-readable mode string.
func ProcessGrayscalePixels(pixels []byte, controls GrayscaleControls) string {
	modeParts := []string{"grayscale"}
	lookup := identityLookupTable()
	pendingLookup := false

	if controls.Invert {
		for index := range lookup {
			lookup[index] = 255 - lookup[index]
		}
		pendingLookup = true
		modeParts[0] = "inverted grayscale"
	}
	if controls.Brightness != 0 {
		for index := range lookup {
			lookup[index] = clampLookupValue(int(lookup[index]) + controls.Brightness)
		}
		pendingLookup = true
		modeParts = append(modeParts, fmt.Sprintf("brightness %+d", controls.Brightness))
	}
	if controls.Contrast != 1.0 {
		for index := range lookup {
			adjusted := 128.0 + controls.Contrast*(float64(lookup[index])-128.0)
			lookup[index] = clampLookupValue(int(math.Round(adjusted)))
		}
		pendingLookup = true
		modeParts = append(modeParts, fmt.Sprintf("contrast %v", controls.Contrast))
	}
	if controls.Equalize {
		if pendingLookup {
			applyLookupInPlace(pixels, lookup)
			lookup = identityLookupTable()
			pendingLookup = false
		}
		equalizeHistogramInPlace(pixels)
		modeParts = append(modeParts, "histogram equalization")
	}
	if pendingLookup {
		applyLookupInPlace(pixels, lookup)
	}
	return strings.Join(modeParts, " with ")
}

// NormalizePaletteName parses user-facing palette names, including the empty
// string that the frontend may send for an explicit no-palette choice.
func NormalizePaletteName(name string) (Palette, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "none":
		return PaletteNone, nil
	case "hot":
		return PaletteHot, nil
	case "bone":
		return PaletteBone, nil
	default:
		return PaletteNone, ErrUnknownPalette
	}
}

func applyNamedPalette(preview render.PreviewImage, palette Palette) (render.PreviewImage, error) {
	if preview.Format != render.Gray8 {
		return render.PreviewImage{}, ErrPaletteRequiresGray8
	}
	if err := validatePreviewPixels(preview); err != nil {
		return render.PreviewImage{}, err
	}

	var colorFn func(byte) [4]byte
	switch palette {
	case PaletteHot:
		colorFn = hotColor
	case PaletteBone:
		colorFn = boneColor
	case PaletteNone:
		return preview, nil
	default:
		return render.PreviewImage{}, ErrUnknownPalette
	}

	pixels := make([]byte, 0, len(preview.Pixels)*4)
	for _, value := range preview.Pixels {
		color := colorFn(value)
		pixels = append(pixels, color[:]...)
	}
	return render.RGBA(preview.Width, preview.Height, pixels), nil
}

func combineComparison(left, right render.PreviewImage) (render.PreviewImage, error) {
	if left.Format != render.Gray8 {
		return render.PreviewImage{}, ErrCompareRequiresGray8Left
	}
	if left.Width != right.Width || left.Height != right.Height {
		return render.PreviewImage{}, ErrCompareDimensionMismatch
	}
	if err := validatePreviewPixels(left); err != nil {
		return render.PreviewImage{}, err
	}
	if err := validatePreviewPixels(right); err != nil {
		return render.PreviewImage{}, err
	}

	if left.Width > math.MaxUint32/2 {
		return render.PreviewImage{}, ErrCompareWidthOverflow
	}
	combinedWidth := left.Width * 2
	outputLen := uint64(combinedWidth) * uint64(left.Height) * 4
	if outputLen > uint64(maxInt) {
		return render.PreviewImage{}, ErrCompareSizeOverflow
	}
	pixels := make([]byte, 0, int(outputLen))
	width := int(left.Width)
	for row := 0; row < int(left.Height); row++ {
		leftStart := row * width
		for x := 0; x < width; x++ {
			value := left.Pixels[leftStart+x]
			pixels = append(pixels, value, value, value, 255)
		}

		switch right.Format {
		case render.Gray8:
			rightStart := row * width
			for x := 0; x < width; x++ {
				value := right.Pixels[rightStart+x]
				pixels = append(pixels, value, value, value, 255)
			}
		case render.RGBA8:
			rightStart := row * width * 4
			pixels = append(pixels, right.Pixels[rightStart:rightStart+width*4]...)
		default:
			return render.PreviewImage{}, ErrInvalidPreviewPixels
		}
	}
	return render.RGBA(combinedWidth, left.Height, pixels), nil
}

func validatePreviewPixels(preview render.PreviewImage) error {
	channels := uint64(1)
	switch preview.Format {
	case render.Gray8:
		channels = 1
	case render.RGBA8:
		channels = 4
	default:
		return ErrInvalidPreviewPixels
	}
	expected := uint64(preview.Width) * uint64(preview.Height) * channels
	if expected > uint64(maxInt) || len(preview.Pixels) != int(expected) {
		return ErrInvalidPreviewPixels
	}
	return nil
}

func identityLookupTable() [256]byte {
	var lookup [256]byte
	for index := range lookup {
		lookup[index] = byte(index)
	}
	return lookup
}

func applyLookupInPlace(pixels []byte, lookup [256]byte) {
	for index, value := range pixels {
		pixels[index] = lookup[value]
	}
}

func equalizeHistogramInPlace(pixels []byte) {
	if len(pixels) == 0 {
		return
	}
	var histogram [256]int
	for _, value := range pixels {
		histogram[value]++
	}
	lookup, ok := equalizeLookup(histogram, len(pixels))
	if !ok {
		return
	}
	applyLookupInPlace(pixels, lookup)
}

func equalizeLookup(histogram [256]int, total int) ([256]byte, bool) {
	cdf := 0
	cdfMin := 0
	found := false
	for _, count := range histogram {
		cdf += count
		if !found && count != 0 {
			cdfMin = cdf
			found = true
		}
	}
	if cdfMin == total {
		return [256]byte{}, false
	}

	denom := total - cdfMin
	var lookup [256]byte
	cdf = 0
	for index, count := range histogram {
		cdf += count
		if cdf <= cdfMin {
			continue
		}
		lookup[index] = byte(((cdf-cdfMin)*255 + denom/2) / denom)
	}
	return lookup, true
}

func clampLookupValue(value int) byte {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return byte(value)
}

func hotColor(value byte) [4]byte {
	switch {
	case value <= 84:
		return [4]byte{value * 3, 0, 0, 255}
	case value <= 169:
		return [4]byte{255, (value - 85) * 3, 0, 255}
	default:
		return [4]byte{255, 255, (value - 170) * 3, 255}
	}
}

func boneColor(value byte) [4]byte {
	valueInt := int(value)
	whiteBoost := max(valueInt-128, 0)
	return [4]byte{
		clampLookupValue((valueInt*7)/8 + whiteBoost),
		clampLookupValue((valueInt*7)/8 + whiteBoost + valueInt/16),
		clampLookupValue(valueInt + whiteBoost/2),
		255,
	}
}

func paletteFromContract(name contracts.PaletteName) Palette {
	switch name {
	case contracts.PaletteHot:
		return PaletteHot
	case contracts.PaletteBone:
		return PaletteBone
	default:
		return PaletteNone
	}
}

const maxInt = int(^uint(0) >> 1)
