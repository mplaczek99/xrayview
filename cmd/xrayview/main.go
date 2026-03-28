package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mplaczek99/xrayview/internal/filters"
	"github.com/mplaczek99/xrayview/internal/imageio"
)

type config struct {
	inputPath  string
	outputPath string
	invert     bool
	brightness int
	contrast   float64
}

func main() {
	cfg := parseFlags()

	img, format, err := imageio.Load(cfg.inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	gray := filters.Grayscale(img)
	output := gray
	mode := "grayscale"
	if cfg.invert {
		output = filters.Invert(gray)
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

	if err := imageio.SavePNG(cfg.outputPath, output); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	bounds := img.Bounds()
	fmt.Printf("loaded %s image: %dx%d\n", format, bounds.Dx(), bounds.Dy())
	fmt.Printf("saved %s png image: %s\n", mode, cfg.outputPath)
}

func parseFlags() config {
	var cfg config

	flag.StringVar(&cfg.inputPath, "input", "", "input image path")
	flag.StringVar(&cfg.outputPath, "output", "", "output image path")
	flag.BoolVar(&cfg.invert, "invert", false, "invert grayscale output")
	flag.IntVar(&cfg.brightness, "brightness", 0, "brightness delta for grayscale output")
	flag.Float64Var(&cfg.contrast, "contrast", 1.0, "contrast factor for grayscale output")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of xrayview:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if cfg.inputPath == "" || cfg.outputPath == "" {
		fmt.Fprintln(os.Stderr, "both -input and -output are required")
		flag.Usage()
		os.Exit(2)
	}

	return cfg
}
