// Command xrayview applies visualization filters to a DICOM image and writes DICOM output.

package main

import (
	"flag"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/mplaczek99/xrayview/internal/colormap"
	"github.com/mplaczek99/xrayview/internal/filters"
	"github.com/mplaczek99/xrayview/internal/imageio"
)

// config holds resolved CLI settings.
type config struct {
	inputPath         string
	outputPath        string
	previewOutputPath string
	preset            string
	invert            bool
	brightness        int
	contrast          float64
	equalize          bool
	compare           bool
	pipeline          string
	palette           string
}

// presetConfig holds preset processing defaults.
type presetConfig struct {
	brightness int
	contrast   float64
	equalize   bool
	palette    string
}

var presetConfigs = map[string]presetConfig{
	"default": {
		brightness: 0,
		contrast:   1.0,
		equalize:   false,
		palette:    "none",
	},
	"xray": {
		brightness: 10,
		contrast:   1.4,
		equalize:   true,
		palette:    "bone",
	},
	"high-contrast": {
		brightness: 0,
		contrast:   1.8,
		equalize:   true,
		palette:    "none",
	},
}

var defaultPipelineOrder = []string{"grayscale", "invert", "brightness", "contrast", "equalize"}

func main() {
	cfg := parseFlags()

	loaded, err := imageio.Load(cfg.inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Keep the original grayscale image for comparison output.
	originalGray := filters.Grayscale(loaded.Image)
	output, mode := processGrayImage(originalGray, cfg)
	if cfg.compare {
		output = combineComparison(originalGray, output)
		mode = fmt.Sprintf("comparison of grayscale and %s", mode)
	}

	if cfg.previewOutputPath != "" {
		if err := imageio.SavePreviewPNG(cfg.previewOutputPath, output); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}

	if cfg.outputPath != "" {
		if err := imageio.SaveDICOM(cfg.outputPath, output, loaded); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}

	bounds := loaded.Image.Bounds()
	fmt.Printf("loaded %s image: %dx%d\n", loaded.Format, bounds.Dx(), bounds.Dy())
	if cfg.previewOutputPath != "" {
		fmt.Printf("saved %s preview image: %s\n", mode, cfg.previewOutputPath)
	}
	if cfg.outputPath != "" {
		fmt.Printf("saved %s dicom image: %s\n", mode, cfg.outputPath)
	}
}

func processImage(img image.Image, cfg config) (image.Image, string) {
	return processGrayImage(filters.Grayscale(img), cfg)
}

func processGrayImage(output *image.Gray, cfg config) (image.Image, string) {
	mode := "grayscale"

	steps, _ := pipelineSteps(cfg.pipeline)
	steps = effectivePipelineSteps(steps, cfg)
	lookup := identityLookupTable()
	pendingLookup := false
	flushLookup := func() {
		if !pendingLookup {
			return
		}
		output = filters.ApplyLookupTable(output, lookup)
		lookup = identityLookupTable()
		pendingLookup = false
	}

	for _, step := range steps {
		switch step {
		case "grayscale":
			// Already applied.
		case "invert":
			if cfg.invert {
				composeInvertLookup(&lookup)
				pendingLookup = true
				mode = strings.Replace(mode, "grayscale", "inverted grayscale", 1)
			}
		case "brightness":
			if cfg.brightness != 0 {
				composeBrightnessLookup(&lookup, cfg.brightness)
				pendingLookup = true
				mode = fmt.Sprintf("%s with brightness %+d", mode, cfg.brightness)
			}
		case "contrast":
			if cfg.contrast != 1.0 {
				composeContrastLookup(&lookup, cfg.contrast)
				pendingLookup = true
				mode = fmt.Sprintf("%s with contrast %g", mode, cfg.contrast)
			}
		case "equalize":
			if cfg.equalize {
				flushLookup()
				output = filters.EqualizeHistogram(output)
				mode = fmt.Sprintf("%s with histogram equalization", mode)
			}
		}
	}

	flushLookup()

	// Apply pseudocolor after grayscale filters.
	if cfg.palette == "hot" {
		return colormap.Hot(output), fmt.Sprintf("%s with hot palette", mode)
	}
	if cfg.palette == "bone" {
		return colormap.Bone(output), fmt.Sprintf("%s with bone palette", mode)
	}

	return output, mode
}

func identityLookupTable() [256]uint8 {
	var lut [256]uint8
	for i := range lut {
		lut[i] = uint8(i)
	}
	return lut
}

func composeInvertLookup(lut *[256]uint8) {
	for i, value := range lut {
		lut[i] = 255 - value
	}
}

func composeBrightnessLookup(lut *[256]uint8, delta int) {
	for i, value := range lut {
		lut[i] = clampLookupValue(int(value) + delta)
	}
}

func composeContrastLookup(lut *[256]uint8, factor float64) {
	for i, value := range lut {
		adjusted := 128 + factor*(float64(value)-128)
		lut[i] = clampLookupValue(int(math.Round(adjusted)))
	}
}

func clampLookupValue(value int) uint8 {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return uint8(value)
}

func parseFlags() config {
	var cfg config

	flag.StringVar(&cfg.inputPath, "input", "", "input DICOM path")
	flag.StringVar(&cfg.outputPath, "output", "", "output DICOM path (default: input_processed.dcm)")
	flag.StringVar(&cfg.previewOutputPath, "preview-output", "", "internal preview PNG path")
	flag.StringVar(&cfg.preset, "preset", "default", "preset: default, xray, or high-contrast")
	flag.BoolVar(&cfg.invert, "invert", false, "invert grayscale output")
	flag.IntVar(&cfg.brightness, "brightness", 0, "brightness delta for grayscale output")
	flag.Float64Var(&cfg.contrast, "contrast", 1.0, "contrast factor for grayscale output")
	flag.BoolVar(&cfg.equalize, "equalize", false, "apply histogram equalization")
	flag.BoolVar(&cfg.compare, "compare", false, "save grayscale and processed output side-by-side")
	flag.StringVar(&cfg.pipeline, "pipeline", "", "comma-separated grayscale steps")
	flag.StringVar(&cfg.palette, "palette", "none", "pseudocolor palette: none, hot, or bone")

	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprintln(out, "Usage of xrayview:")
		fmt.Fprintln(out, "  -input string")
		fmt.Fprintln(out, "        input DICOM path")
		fmt.Fprintln(out, "  -output string")
		fmt.Fprintln(out, "        output DICOM path (default: input_processed.dcm)")
		fmt.Fprintln(out, "  -preset string")
		fmt.Fprintln(out, "        preset: default, xray, or high-contrast")
		fmt.Fprintln(out, "  -invert")
		fmt.Fprintln(out, "        invert grayscale output")
		fmt.Fprintln(out, "  -brightness int")
		fmt.Fprintln(out, "        brightness delta for grayscale output")
		fmt.Fprintln(out, "  -contrast float")
		fmt.Fprintln(out, "        contrast factor for grayscale output")
		fmt.Fprintln(out, "  -equalize")
		fmt.Fprintln(out, "        apply histogram equalization")
		fmt.Fprintln(out, "  -compare")
		fmt.Fprintln(out, "        save grayscale and processed output side-by-side")
		fmt.Fprintln(out, "  -pipeline string")
		fmt.Fprintln(out, "        comma-separated grayscale steps")
		fmt.Fprintln(out, "  -palette string")
		fmt.Fprintln(out, "        pseudocolor palette: none, hot, or bone")
	}

	flag.Parse()

	// Track only flags explicitly set on the command line.
	explicit := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		explicit[f.Name] = true
	})

	cfg.palette = strings.ToLower(cfg.palette)
	cfg.preset = strings.ToLower(cfg.preset)

	// Fill in the default output path before validation.
	if cfg.outputPath == "" && cfg.previewOutputPath == "" && cfg.inputPath != "" {
		cfg.outputPath = defaultOutputPath(cfg.inputPath)
	}

	var err error
	cfg, err = applyPreset(cfg, explicit)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		flag.Usage()
		os.Exit(2)
	}

	if err := validateConfig(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		flag.Usage()
		os.Exit(2)
	}

	return cfg
}

func validateConfig(cfg config) error {
	if cfg.inputPath == "" {
		return fmt.Errorf("-input is required")
	}
	if !hasDICOMExtension(cfg.inputPath) {
		return fmt.Errorf("input path must end with .dcm or .dicom")
	}
	preset := cfg.preset
	if preset == "" {
		preset = "default"
	}
	if _, ok := presetConfigs[preset]; !ok {
		return fmt.Errorf("preset must be one of: default, xray, high-contrast")
	}

	if cfg.outputPath == "" && cfg.previewOutputPath == "" {
		return fmt.Errorf("either -output or internal preview output must be set")
	}
	if cfg.outputPath != "" && !hasDICOMExtension(cfg.outputPath) {
		return fmt.Errorf("output path must end with .dcm or .dicom")
	}
	if cfg.previewOutputPath != "" && !strings.HasSuffix(strings.ToLower(cfg.previewOutputPath), ".png") {
		return fmt.Errorf("preview output path must end with .png")
	}
	if math.IsNaN(cfg.contrast) || math.IsInf(cfg.contrast, 0) || cfg.contrast < 0 {
		return fmt.Errorf("contrast must be a finite value greater than or equal to 0")
	}
	if cfg.palette != "none" && cfg.palette != "hot" && cfg.palette != "bone" {
		return fmt.Errorf("palette must be one of: none, hot, bone")
	}
	if _, err := pipelineSteps(cfg.pipeline); err != nil {
		return err
	}

	return nil
}

func applyPreset(cfg config, explicit map[string]bool) (config, error) {
	if cfg.preset == "" {
		cfg.preset = "default"
	}

	preset, ok := presetConfigs[cfg.preset]
	if !ok {
		return cfg, fmt.Errorf("preset must be one of: default, xray, high-contrast")
	}

	if !explicit["brightness"] {
		cfg.brightness = preset.brightness
	}
	if !explicit["contrast"] {
		cfg.contrast = preset.contrast
	}
	if !explicit["equalize"] {
		cfg.equalize = preset.equalize
	}
	if !explicit["palette"] {
		cfg.palette = preset.palette
	}

	return cfg, nil
}

func pipelineSteps(pipeline string) ([]string, error) {
	if pipeline == "" {
		return nil, nil
	}

	parts := strings.Split(pipeline, ",")
	steps := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		step := strings.ToLower(strings.TrimSpace(part))
		if step == "" {
			return nil, fmt.Errorf("pipeline steps must not be empty")
		}

		switch step {
		case "grayscale", "invert", "brightness", "contrast", "equalize":
			if seen[step] {
				return nil, fmt.Errorf("duplicate pipeline step: %s", step)
			}
			seen[step] = true
			steps = append(steps, step)
		default:
			return nil, fmt.Errorf("unknown pipeline step: %s", step)
		}
	}

	return steps, nil
}

func effectivePipelineSteps(requested []string, cfg config) []string {
	if len(requested) == 0 {
		return append([]string(nil), defaultPipelineOrder...)
	}

	enabled := map[string]bool{
		"invert":     cfg.invert,
		"brightness": cfg.brightness != 0,
		"contrast":   cfg.contrast != 1.0,
		"equalize":   cfg.equalize,
	}
	steps := []string{"grayscale"}
	used := map[string]bool{"grayscale": true}

	for _, step := range requested {
		if step == "grayscale" || !enabled[step] || used[step] {
			continue
		}
		steps = append(steps, step)
		used[step] = true
	}

	for _, step := range defaultPipelineOrder[1:] {
		if enabled[step] && !used[step] {
			steps = append(steps, step)
		}
	}

	return steps
}

func defaultOutputPath(inputPath string) string {
	dir := filepath.Dir(inputPath)
	base := filepath.Base(inputPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if name == "" {
		name = base
	}

	return filepath.Join(dir, name+"_processed.dcm")
}

func hasDICOMExtension(path string) bool {
	lowered := strings.ToLower(path)
	return strings.HasSuffix(lowered, ".dcm") || strings.HasSuffix(lowered, ".dicom")
}
