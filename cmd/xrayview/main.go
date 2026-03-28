// Command xrayview loads an image, applies visualization steps, and writes a
// PNG result.
//
// It is the CLI entry point that wires together flag parsing, image loading,
// grayscale processing, optional pseudocolor, and output generation.

package main

import (
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

	"github.com/mplaczek99/xrayview/internal/colormap"
	"github.com/mplaczek99/xrayview/internal/filters"
	"github.com/mplaczek99/xrayview/internal/imageio"
)

type config struct {
	// config stores the fully resolved runtime settings after flags, defaults,
	// and presets have been merged. The rest of the program can then process
	// images without needing to know where a value originally came from.
	inputPath  string
	outputPath string
	preset     string
	invert     bool
	brightness int
	contrast   float64
	equalize   bool
	compare    bool
	pipeline   string
	palette    string
}

// grayFilter keeps the pipeline limited to single-channel transforms.
// Pseudocolor stays outside this pipeline because it changes the image into
// RGBA and should only happen after grayscale processing is finished.
type grayFilter func(*image.Gray) *image.Gray

type presetConfig struct {
	// presetConfig only covers visualization knobs that make sense as reusable
	// defaults. Output paths, comparison mode, and pipeline ordering stay outside
	// presets so a preset does not silently change file handling or control flow.
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

func main() {
	cfg := parseFlags()

	img, format, err := imageio.Load(cfg.inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Comparison mode always uses the untouched grayscale baseline on the left,
	// even if the processed output later becomes color.
	originalGray := filters.Grayscale(img)
	output, mode := processImage(img, cfg)
	if cfg.compare {
		output = combineComparison(originalGray, output)
		mode = fmt.Sprintf("comparison of grayscale and %s", mode)
	}

	if err := imageio.SavePNG(cfg.outputPath, output); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	bounds := img.Bounds()
	fmt.Printf("loaded %s image: %dx%d\n", format, bounds.Dx(), bounds.Dy())
	fmt.Printf("saved %s png image: %s\n", mode, cfg.outputPath)
}

func processImage(img image.Image, cfg config) (image.Image, string) {
	// Grayscale is always first because every configurable filter in the pipeline
	// operates on a single intensity channel. This also gives comparison mode a
	// stable baseline regardless of the input file's original color model.
	output := filters.Grayscale(img)
	mode := "grayscale"
	pipeline := make([]grayFilter, 0, 4)

	// An explicit pipeline only changes ordering. The existing flags still decide
	// whether a step is active and what parameters it uses.
	steps, _ := pipelineSteps(cfg.pipeline)
	if len(steps) == 0 {
		steps = []string{"grayscale", "invert", "brightness", "contrast", "equalize"}
	}

	for _, step := range steps {
		switch step {
		case "grayscale":
			// The grayscale step is already applied before pipeline assembly. Keeping
			// it in the allowed list makes explicit pipelines easier to read.
		case "invert":
			if cfg.invert {
				pipeline = append(pipeline, filters.Invert)
				// Only the first "grayscale" token is replaced so later suffixes such as
				// brightness or contrast descriptions stay intact.
				mode = strings.Replace(mode, "grayscale", "inverted grayscale", 1)
			}
		case "brightness":
			if cfg.brightness != 0 {
				// Capture the current value in a local variable so the closure keeps the
				// chosen flag or preset value when the pipeline runs later.
				delta := cfg.brightness
				pipeline = append(pipeline, func(img *image.Gray) *image.Gray {
					return filters.AdjustBrightness(img, delta)
				})
				mode = fmt.Sprintf("%s with brightness %+d", mode, cfg.brightness)
			}
		case "contrast":
			if cfg.contrast != 1.0 {
				// Capture the factor for the same reason as brightness: the function is
				// stored now and executed later when the pipeline is applied.
				factor := cfg.contrast
				pipeline = append(pipeline, func(img *image.Gray) *image.Gray {
					return filters.AdjustContrast(img, factor)
				})
				mode = fmt.Sprintf("%s with contrast %g", mode, cfg.contrast)
			}
		case "equalize":
			if cfg.equalize {
				pipeline = append(pipeline, filters.EqualizeHistogram)
				mode = fmt.Sprintf("%s with histogram equalization", mode)
			}
		}
	}

	// The loop is the actual execution point. Building the pipeline first keeps
	// ordering decisions separate from the filter implementations themselves.
	for _, filter := range pipeline {
		output = filter(output)
	}

	// Pseudocolor is intentionally last so the palette reflects the final gray
	// intensities after all grayscale processing has already been decided.
	if cfg.palette == "hot" {
		return colormap.Hot(output), fmt.Sprintf("%s with hot palette", mode)
	}
	if cfg.palette == "bone" {
		return colormap.Bone(output), fmt.Sprintf("%s with bone palette", mode)
	}

	return output, mode
}

func parseFlags() config {
	var cfg config

	flag.StringVar(&cfg.inputPath, "input", "", "input image path")
	flag.StringVar(&cfg.outputPath, "output", "", "output PNG path (default: input_processed.png)")
	flag.StringVar(&cfg.preset, "preset", "default", "preset: default, xray, or high-contrast")
	flag.BoolVar(&cfg.invert, "invert", false, "invert grayscale output")
	flag.IntVar(&cfg.brightness, "brightness", 0, "brightness delta for grayscale output")
	flag.Float64Var(&cfg.contrast, "contrast", 1.0, "contrast factor for grayscale output")
	flag.BoolVar(&cfg.equalize, "equalize", false, "apply histogram equalization")
	flag.BoolVar(&cfg.compare, "compare", false, "save grayscale and processed output side-by-side")
	flag.StringVar(&cfg.pipeline, "pipeline", "", "comma-separated grayscale steps")
	flag.StringVar(&cfg.palette, "palette", "none", "pseudocolor palette: none, hot, or bone")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of xrayview:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	// Visit reports only flags the user actually set. That lets presets supply
	// defaults first while still allowing explicit CLI values to win.
	explicit := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		explicit[f.Name] = true
	})

	cfg.palette = strings.ToLower(cfg.palette)
	cfg.preset = strings.ToLower(cfg.preset)

	// Default output generation happens before validation so the rest of the code
	// can treat the output path as already resolved.
	if cfg.outputPath == "" && cfg.inputPath != "" {
		cfg.outputPath = defaultOutputPath(cfg.inputPath)
	}

	// Presets are applied before validation so derived values such as palette or
	// contrast are checked in their final form.
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
	// Validation runs after defaults and presets have been resolved so it checks
	// the exact configuration that processing will use.
	if cfg.inputPath == "" {
		return fmt.Errorf("-input is required")
	}
	preset := cfg.preset
	if preset == "" {
		preset = "default"
	}
	if _, ok := presetConfigs[preset]; !ok {
		return fmt.Errorf("preset must be one of: default, xray, high-contrast")
	}

	if !strings.HasSuffix(strings.ToLower(cfg.outputPath), ".png") {
		return fmt.Errorf("output path must end with .png")
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
	// Presets provide convenient starting points, but any flag the user typed
	// explicitly should override the preset rather than be overwritten by it.
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
	// Returning nil for an empty value lets processImage keep a single default
	// ordering path instead of duplicating that order in validation code.
	if pipeline == "" {
		return nil, nil
	}

	parts := strings.Split(pipeline, ",")
	steps := make([]string, 0, len(parts))
	for _, part := range parts {
		step := strings.ToLower(strings.TrimSpace(part))
		if step == "" {
			return nil, fmt.Errorf("pipeline steps must not be empty")
		}

		switch step {
		case "grayscale", "invert", "brightness", "contrast", "equalize":
			steps = append(steps, step)
		default:
			return nil, fmt.Errorf("unknown pipeline step: %s", step)
		}
	}

	return steps, nil
}

func defaultOutputPath(inputPath string) string {
	// Using the input directory avoids surprising output locations when -output is
	// omitted, and the suffix makes it clear the file is derived rather than raw.
	dir := filepath.Dir(inputPath)
	base := filepath.Base(inputPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if name == "" {
		name = base
	}

	return filepath.Join(dir, name+"_processed.png")
}
