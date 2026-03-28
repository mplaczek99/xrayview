package main

import (
	"flag"
	"fmt"
	"image"
	"os"
	"strings"

	"github.com/mplaczek99/xrayview/internal/colormap"
	"github.com/mplaczek99/xrayview/internal/filters"
	"github.com/mplaczek99/xrayview/internal/imageio"
)

type config struct {
	inputPath  string
	outputPath string
	invert     bool
	brightness int
	contrast   float64
	equalize   bool
	palette    string
}

type grayFilter func(*image.Gray) *image.Gray

func main() {
	cfg := parseFlags()

	img, format, err := imageio.Load(cfg.inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	output, mode := processImage(img, cfg)

	if err := imageio.SavePNG(cfg.outputPath, output); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	bounds := img.Bounds()
	fmt.Printf("loaded %s image: %dx%d\n", format, bounds.Dx(), bounds.Dy())
	fmt.Printf("saved %s png image: %s\n", mode, cfg.outputPath)
}

func processImage(img image.Image, cfg config) (image.Image, string) {
	output := filters.Grayscale(img)
	mode := "grayscale"
	pipeline := make([]grayFilter, 0, 4)

	if cfg.invert {
		pipeline = append(pipeline, filters.Invert)
		mode = "inverted grayscale"
	}
	if cfg.brightness != 0 {
		delta := cfg.brightness
		pipeline = append(pipeline, func(img *image.Gray) *image.Gray {
			return filters.AdjustBrightness(img, delta)
		})
		mode = fmt.Sprintf("%s with brightness %+d", mode, cfg.brightness)
	}
	if cfg.contrast != 1.0 {
		factor := cfg.contrast
		pipeline = append(pipeline, func(img *image.Gray) *image.Gray {
			return filters.AdjustContrast(img, factor)
		})
		mode = fmt.Sprintf("%s with contrast %g", mode, cfg.contrast)
	}
	if cfg.equalize {
		pipeline = append(pipeline, filters.EqualizeHistogram)
		mode = fmt.Sprintf("%s with histogram equalization", mode)
	}

	for _, filter := range pipeline {
		output = filter(output)
	}

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
	flag.StringVar(&cfg.outputPath, "output", "", "output image path")
	flag.BoolVar(&cfg.invert, "invert", false, "invert grayscale output")
	flag.IntVar(&cfg.brightness, "brightness", 0, "brightness delta for grayscale output")
	flag.Float64Var(&cfg.contrast, "contrast", 1.0, "contrast factor for grayscale output")
	flag.BoolVar(&cfg.equalize, "equalize", false, "apply histogram equalization")
	flag.StringVar(&cfg.palette, "palette", "none", "pseudocolor palette: none, hot, or bone")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of xrayview:\n")
		flag.PrintDefaults()
	}

	flag.Parse()
	cfg.palette = strings.ToLower(cfg.palette)

	if err := validateConfig(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		flag.Usage()
		os.Exit(2)
	}

	return cfg
}

func validateConfig(cfg config) error {
	if cfg.inputPath == "" || cfg.outputPath == "" {
		return fmt.Errorf("both -input and -output are required")
	}

	if !strings.HasSuffix(strings.ToLower(cfg.outputPath), ".png") {
		return fmt.Errorf("output path must end with .png")
	}
	if cfg.palette != "none" && cfg.palette != "hot" && cfg.palette != "bone" {
		return fmt.Errorf("palette must be one of: none, hot, bone")
	}

	return nil
}
