package main

import (
	"flag"
	"fmt"
	"image"
	"os"
	"strings"

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
}

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

func processImage(img image.Image, cfg config) (*image.Gray, string) {
	output := filters.Grayscale(img)
	mode := "grayscale"

	if cfg.invert {
		output = filters.Invert(output)
		mode = "inverted grayscale"
	}
	if cfg.brightness != 0 {
		output = filters.AdjustBrightness(output, cfg.brightness)
		mode = fmt.Sprintf("%s with brightness %+d", mode, cfg.brightness)
	}
	if cfg.contrast != 1.0 {
		output = filters.AdjustContrast(output, cfg.contrast)
		mode = fmt.Sprintf("%s with contrast %g", mode, cfg.contrast)
	}
	if cfg.equalize {
		output = filters.EqualizeHistogram(output)
		mode = fmt.Sprintf("%s with histogram equalization", mode)
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

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of xrayview:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

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

	return nil
}
