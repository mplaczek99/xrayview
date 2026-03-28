package main

import (
	"flag"
	"fmt"
	"os"
)

type config struct {
	inputPath  string
	outputPath string
}

func main() {
	cfg := parseFlags()
	fmt.Printf("xrayview starting: input=%s output=%s\n", cfg.inputPath, cfg.outputPath)
}

func parseFlags() config {
	var cfg config

	flag.StringVar(&cfg.inputPath, "input", "", "input image path")
	flag.StringVar(&cfg.outputPath, "output", "", "output image path")

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
