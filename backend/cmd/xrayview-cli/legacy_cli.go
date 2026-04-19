package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"xrayview/backend/internal/analysis"
	"xrayview/backend/internal/contracts"
	dicommeta "xrayview/backend/internal/dicommeta"
	dicomexport "xrayview/backend/internal/export"
	"xrayview/backend/internal/processing"
	"xrayview/backend/internal/render"
)

type legacyCLIOptions struct {
	input           string
	output          string
	previewOutput   string
	describePresets bool
	describeStudy   bool
	analyzeTooth    bool
	preset          string
	invert          bool
	brightness      optionalIntFlag
	contrast        optionalFloatFlag
	equalize        bool
	compare         bool
	palette         string
}

type legacyStudyDescription struct {
	MeasurementScale *contracts.MeasurementScale `json:"measurementScale,omitempty"`
}

type legacyToothAnalysis struct {
	Image       contracts.ToothImageMetadata `json:"image"`
	Calibration contracts.ToothCalibration   `json:"calibration"`
	Tooth       *contracts.ToothCandidate    `json:"tooth"`
	Teeth       []contracts.ToothCandidate   `json:"teeth"`
	Warnings    []string                     `json:"warnings"`
}

type optionalIntFlag struct {
	value int
	set   bool
}

func (flagValue *optionalIntFlag) String() string {
	if !flagValue.set {
		return ""
	}

	return strconv.Itoa(flagValue.value)
}

func (flagValue *optionalIntFlag) Set(value string) error {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return err
	}

	flagValue.value = parsed
	flagValue.set = true
	return nil
}

type optionalFloatFlag struct {
	value float64
	set   bool
}

func (flagValue *optionalFloatFlag) String() string {
	if !flagValue.set {
		return ""
	}

	return strconv.FormatFloat(flagValue.value, 'f', -1, 64)
}

func (flagValue *optionalFloatFlag) Set(value string) error {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return err
	}

	flagValue.value = parsed
	flagValue.set = true
	return nil
}

func runLegacyCLI(args []string, stdout, stderr io.Writer) error {
	options, err := parseLegacyCLIArgs(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return err
	}

	return executeLegacyCLI(options, stdout)
}

func parseLegacyCLIArgs(args []string, stderr io.Writer) (legacyCLIOptions, error) {
	options := legacyCLIOptions{
		preset: contracts.DefaultProcessingPresetID,
	}

	flagSet := flag.NewFlagSet("xrayview-cli", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	flagSet.Usage = func() {
		printLegacyUsage(stderr)
	}

	flagSet.StringVar(&options.input, "input", "", "Path to the source DICOM study")
	flagSet.StringVar(&options.output, "output", "", "Output DICOM path")
	flagSet.StringVar(&options.previewOutput, "preview-output", "", "PNG preview output path")
	flagSet.BoolVar(&options.describePresets, "describe-presets", false, "Print processing preset metadata as JSON")
	flagSet.BoolVar(&options.describeStudy, "describe-study", false, "Print study measurement metadata as JSON")
	flagSet.BoolVar(&options.analyzeTooth, "analyze-tooth", false, "Analyze the study and return automatic tooth measurements as JSON")
	flagSet.StringVar(&options.preset, "preset", contracts.DefaultProcessingPresetID, "Processing preset")
	flagSet.BoolVar(&options.invert, "invert", false, "Invert grayscale")
	flagSet.Var(&options.brightness, "brightness", "Brightness adjustment (-256 to 256)")
	flagSet.Var(&options.contrast, "contrast", "Contrast multiplier (>= 0.0)")
	flagSet.BoolVar(&options.equalize, "equalize", false, "Apply histogram equalization")
	flagSet.BoolVar(&options.compare, "compare", false, "Show before/after comparison")
	flagSet.StringVar(&options.palette, "palette", "", "Color palette (none, hot, bone)")

	if err := flagSet.Parse(args); err != nil {
		return legacyCLIOptions{}, err
	}

	if flagSet.NArg() != 0 {
		return legacyCLIOptions{}, fmt.Errorf(
			"unexpected positional arguments: %s",
			strings.Join(flagSet.Args(), " "),
		)
	}

	return options, nil
}

func executeLegacyCLI(options legacyCLIOptions, stdout io.Writer) error {
	if err := validateLegacyModeSelection(options); err != nil {
		return err
	}

	if options.describePresets {
		return writeJSON(stdout, contracts.DefaultProcessingManifest())
	}

	inputPath, err := requiredInputPath(options)
	if err != nil {
		return err
	}

	if options.describeStudy {
		metadata, err := dicommeta.ReadFile(inputPath)
		if err != nil {
			return err
		}

		return writeJSON(stdout, legacyStudyDescription{
			MeasurementScale: metadata.MeasurementScale(),
		})
	}

	if options.analyzeTooth {
		return analyzeLegacyStudy(inputPath, strings.TrimSpace(options.previewOutput), stdout)
	}

	if isPlainPreviewRequest(options) {
		return renderLegacyPreview(inputPath, strings.TrimSpace(options.previewOutput), stdout)
	}

	outputPath := strings.TrimSpace(options.output)
	previewOutput := strings.TrimSpace(options.previewOutput)
	if outputPath == "" && previewOutput == "" {
		outputPath = defaultLegacyOutputPath(inputPath)
	}

	return processLegacyStudy(inputPath, outputPath, previewOutput, options, stdout)
}

// validateLegacyModeSelection enforces "pick at most one backend mode":
// describe-presets, describe-study, and analyze-tooth are mutually
// exclusive on a single invocation. Zero is fine — we fall through to
// the processing path.
func validateLegacyModeSelection(options legacyCLIOptions) error {
	modeCount := 0
	for _, enabled := range []bool{
		options.describePresets,
		options.describeStudy,
		options.analyzeTooth,
	} {
		if enabled {
			modeCount++
		}
	}

	if modeCount > 1 {
		return fmt.Errorf(
			"choose only one backend mode: --describe-presets, --describe-study, or --analyze-tooth",
		)
	}

	return nil
}

func requiredInputPath(options legacyCLIOptions) (string, error) {
	inputPath := strings.TrimSpace(options.input)
	if inputPath == "" {
		return "", fmt.Errorf("--input is required")
	}

	if err := validateLegacyInputPath(inputPath); err != nil {
		return "", err
	}

	return inputPath, nil
}

func validateLegacyInputPath(inputPath string) error {
	info, err := os.Stat(inputPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("input file does not exist: %s", inputPath)
		}

		return fmt.Errorf("inspect input file %s: %w", inputPath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("input path must be a file: %s", inputPath)
	}

	return nil
}

func analyzeLegacyStudy(inputPath, previewOutput string, stdout io.Writer) error {
	study, err := decodeLegacyStudy(inputPath)
	if err != nil {
		return err
	}

	preview := render.RenderSourceImage(study.Image, render.DefaultRenderPlan())
	if previewOutput != "" {
		if err := render.SavePreviewPNG(previewOutput, preview); err != nil {
			return err
		}
	}

	toothAnalysis, err := analysis.AnalyzePreview(preview, study.MeasurementScale)
	if err != nil {
		return err
	}

	return writeJSON(stdout, normalizeLegacyToothAnalysis(toothAnalysis))
}

func renderLegacyPreview(inputPath, previewOutput string, stdout io.Writer) error {
	study, err := decodeLegacyStudy(inputPath)
	if err != nil {
		return err
	}

	preview := render.RenderSourceImage(study.Image, render.DefaultRenderPlan())
	if err := render.SavePreviewPNG(previewOutput, preview); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "loaded dicom image: %dx%d\n", study.Image.Width, study.Image.Height)
	fmt.Fprintf(stdout, "saved grayscale preview image: %s\n", previewOutput)
	return nil
}

func processLegacyStudy(
	inputPath string,
	outputPath string,
	previewOutput string,
	options legacyCLIOptions,
	stdout io.Writer,
) error {
	study, err := decodeLegacyStudy(inputPath)
	if err != nil {
		return err
	}

	command := contracts.ProcessStudyCommand{
		PresetID: options.preset,
		Invert:   options.invert,
		Equalize: options.equalize,
		Compare:  options.compare,
	}
	if options.brightness.set {
		command.Brightness = &options.brightness.value
	}
	if options.contrast.set {
		command.Contrast = &options.contrast.value
	}
	if palette := strings.TrimSpace(options.palette); palette != "" {
		paletteName := contracts.PaletteName(palette)
		command.Palette = &paletteName
	}

	resolved, err := processing.ResolveProcessStudyCommand(command)
	if err != nil {
		return err
	}

	processed, err := processing.ProcessSourceImage(
		study.Image,
		render.DefaultRenderPlan(),
		resolved.Controls,
		resolved.Palette,
		resolved.Compare,
	)
	if err != nil {
		return err
	}

	if previewOutput != "" {
		if err := render.SavePreviewPNG(previewOutput, processed.Preview); err != nil {
			return err
		}
	}

	if outputPath != "" {
		writer, err := dicomexport.NewWriterFromEnvironment()
		if err != nil {
			return err
		}

		if err := writer.WriteSecondaryCapture(
			context.Background(),
			outputPath,
			processed.Preview,
			study.Metadata,
		); err != nil {
			return err
		}
	}

	fmt.Fprintf(stdout, "loaded dicom image: %dx%d\n", study.Image.Width, study.Image.Height)
	if previewOutput != "" {
		fmt.Fprintf(stdout, "saved %s preview image: %s\n", processed.Mode, previewOutput)
	}
	if outputPath != "" {
		fmt.Fprintf(stdout, "saved %s dicom image: %s\n", processed.Mode, outputPath)
	}

	return nil
}

func decodeLegacyStudy(inputPath string) (dicommeta.SourceStudy, error) {
	return dicommeta.DecodeFile(inputPath)
}

func defaultLegacyOutputPath(inputPath string) string {
	stem := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	if stem == "" {
		stem = filepath.Base(inputPath)
	}

	return filepath.Join(filepath.Dir(inputPath), stem+"_processed.dcm")
}

// isPlainPreviewRequest is true when the caller asked for a preview and
// didn't touch anything that would pull the call into the processing
// pipeline. That lets us short-circuit to a straight render and skip
// the resolve/process/export dance.
func isPlainPreviewRequest(options legacyCLIOptions) bool {
	return strings.TrimSpace(options.previewOutput) != "" &&
		strings.TrimSpace(options.output) == "" &&
		strings.EqualFold(options.preset, contracts.DefaultProcessingPresetID) &&
		!options.invert &&
		!options.brightness.set &&
		!options.contrast.set &&
		!options.equalize &&
		!options.compare &&
		strings.TrimSpace(options.palette) == ""
}

func normalizeLegacyToothAnalysis(analysis contracts.ToothAnalysis) legacyToothAnalysis {
	teeth := analysis.Teeth
	if teeth == nil {
		teeth = []contracts.ToothCandidate{}
	}

	warnings := analysis.Warnings
	if warnings == nil {
		warnings = []string{}
	}

	return legacyToothAnalysis{
		Image:       analysis.Image,
		Calibration: analysis.Calibration,
		Tooth:       analysis.Tooth,
		Teeth:       teeth,
		Warnings:    warnings,
	}
}

func writeJSON(stdout io.Writer, payload any) error {
	encoder := json.NewEncoder(stdout)
	return encoder.Encode(payload)
}

func printLegacyUsage(stream io.Writer) {
	fmt.Fprintln(stream, "usage: xrayview-cli [workflow flags]")
	fmt.Fprintln(stream, "")
	fmt.Fprintln(stream, "input / output:")
	fmt.Fprintln(stream, "  --input <study.dcm>           path to the source DICOM study")
	fmt.Fprintln(stream, "  --output <study.dcm>          output DICOM path")
	fmt.Fprintln(stream, "  --preview-output <image.png>  PNG preview output path")
	fmt.Fprintln(stream, "")
	fmt.Fprintln(stream, "metadata / analysis:")
	fmt.Fprintln(stream, "  --describe-presets            print processing preset metadata as JSON")
	fmt.Fprintln(stream, "  --describe-study              print study measurement metadata as JSON")
	fmt.Fprintln(stream, "  --analyze-tooth               print automatic tooth analysis as JSON")
	fmt.Fprintln(stream, "")
	fmt.Fprintln(stream, "processing:")
	fmt.Fprintln(stream, "  --preset <id>                 default, xray, or high-contrast")
	fmt.Fprintln(stream, "  --invert                      invert grayscale")
	fmt.Fprintln(stream, "  --brightness <int>            brightness adjustment (-256 to 256)")
	fmt.Fprintln(stream, "  --contrast <float>            contrast multiplier (>= 0.0)")
	fmt.Fprintln(stream, "  --equalize                    apply histogram equalization")
	fmt.Fprintln(stream, "  --compare                     show before/after comparison")
	fmt.Fprintln(stream, "  --palette <name>              none, hot, or bone")
}
