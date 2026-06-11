package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"xrayview/backend/internal/analysis"
	"xrayview/backend/internal/app"
	"xrayview/backend/internal/bmp"
	"xrayview/backend/internal/config"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/ipc"
	"xrayview/backend/internal/processing"
	"xrayview/backend/internal/render"
)

const programName = "xrayview-backend"

// Run executes the headless inspection/batch CLI.
func Run(args []string, stdout, stderr io.Writer) error {
	start := 0
	for start < len(args) && args[start] == "--" {
		start++
	}
	args = args[start:]

	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		writeUsage(stdout)
		return nil
	}

	command := args[0]
	rest := args[1:]
	switch command {
	case "print-config":
		return printConfig(stdout)
	case "decode-source":
		return decodeSource(rest, stdout)
	case "processing-manifest":
		return writeJSON(stdout, contracts.DefaultProcessingManifest())
	case "describe-study":
		return describeStudy(rest, stdout)
	case "render-preview":
		return renderPreview(rest, stdout)
	case "process-preview":
		return processPreview(rest, stdout)
	case "analyze-preview":
		return analyzePreview(rest, stdout)
	case "serve-ipc":
		return serveIPC(rest, stdout)
	case "list-commands":
		return listCommands(stdout)
	case "version":
		_, err := fmt.Fprintf(stdout, "%s contract-v%d\n", contracts.ServiceName, contracts.BackendContractVersion)
		return err
	default:
		message := fmt.Sprintf("error: unrecognized subcommand '%s'\n\n", command)
		_, _ = io.WriteString(stderr, message)
		writeUsage(stderr)
		return fmt.Errorf("%sunrecognized subcommand '%s'", message, command)
	}
}

func printConfig(stdout io.Writer) error {
	loaded, err := config.Load()
	if err != nil {
		return err
	}
	return writeJSON(stdout, configSummary{
		ServiceName: loaded.ServiceName,
		Logging:     loaded.Logging,
		Paths:       loaded.Paths,
	})
}

func decodeSource(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("decode-source expects <path>")
	}
	metadata, err := bmp.ReadFile(args[0])
	if err != nil {
		return err
	}
	source, err := bmp.DecodeSourcePreviewFile(args[0])
	if err != nil {
		return err
	}
	return writeJSON(stdout, decodeSourceSummaryFrom(source, metadata))
}

func describeStudy(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("describe-study expects <path>")
	}
	metadata, err := bmp.ReadFile(args[0])
	if err != nil {
		return err
	}
	return writeJSON(stdout, studyDescription{
		Width:             metadata.Width,
		Height:            metadata.Height,
		ColorChannelCount: metadata.ColorChannelCount,
		BitsPerChannel:    metadata.BitsPerChannel,
		ColorModel:        metadata.ColorModel,
	})
}

func renderPreview(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("render-preview", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	fullRange := flags.Bool("full-range", false, "")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("render-preview expects [--full-range] <input-path> <output-path>")
	}

	inputPath := flags.Arg(0)
	outputPath := flags.Arg(1)
	rendered, err := renderCLISourcePreview(inputPath, *fullRange)
	if err != nil {
		return err
	}
	if err := render.SaveGrayBMP(outputPath, rendered.Width, rendered.Height, rendered.Pixels); err != nil {
		return err
	}

	return writeJSON(stdout, renderPreviewSummary{
		PreviewOutput:     outputPath,
		LoadedWidth:       rendered.Width,
		LoadedHeight:      rendered.Height,
		WindowMode:        windowMode(*fullRange),
		RenderedByteCount: len(rendered.Pixels),
	})
}

func processPreview(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("process-preview", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	fullRange := flags.Bool("full-range", false, "")
	invert := flags.Bool("invert", false, "")
	brightness := flags.Int("brightness", 0, "")
	contrast := flags.Float64("contrast", 1.0, "")
	equalize := flags.Bool("equalize", false, "")
	paletteName := flags.String("palette", "none", "")
	compare := flags.Bool("compare", false, "")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("process-preview expects flags <input-path> <output-path>")
	}
	if *brightness < -256 || *brightness > 256 {
		return fmt.Errorf("brightness must be between -256 and 256, got %d", *brightness)
	}
	if math.IsNaN(*contrast) || math.IsInf(*contrast, 0) || *contrast < processing.MinContrast {
		return fmt.Errorf("contrast must be >= %g, got %v", processing.MinContrast, *contrast)
	}

	palette, err := processing.NormalizePaletteName(*paletteName)
	if err != nil {
		return err
	}
	inputPath := flags.Arg(0)
	outputPath := flags.Arg(1)
	rendered, err := renderCLISourcePreview(inputPath, *fullRange)
	if err != nil {
		return err
	}
	source := render.Gray(rendered.Width, rendered.Height, rendered.Pixels)
	processed, err := processing.ProcessRenderedPreview(source, processing.GrayscaleControls{
		Invert:     *invert,
		Brightness: *brightness,
		Contrast:   *contrast,
		Equalize:   *equalize,
	}, palette, *compare)
	if err != nil {
		return err
	}
	if err := render.SavePreviewBMP(outputPath, processed.Preview); err != nil {
		return err
	}

	return writeJSON(stdout, processPreviewSummary{
		PreviewOutput:     outputPath,
		LoadedWidth:       rendered.Width,
		LoadedHeight:      rendered.Height,
		WindowMode:        windowMode(*fullRange),
		Mode:              processed.Mode,
		Palette:           palette.Label(),
		Compare:           *compare,
		RenderedByteCount: len(processed.Preview.Pixels),
	})
}

func analyzePreview(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("analyze-preview", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	filled := flags.Bool("filled", false, "")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("analyze-preview expects [--filled] <input-path> <output-path>")
	}
	inputPath := flags.Arg(0)
	outputPath := flags.Arg(1)
	rendered, err := bmp.RenderGrayscalePreviewFileForToothAnalysis(inputPath)
	if err != nil {
		return err
	}
	result, err := analysis.GenerateToothOverlay(render.Gray(rendered.Width, rendered.Height, rendered.Pixels))
	if err != nil {
		return err
	}
	preview := result.Preview
	if *filled {
		preview = result.FilledPreview
	}
	if err := render.SavePreviewBMP(outputPath, preview); err != nil {
		return err
	}
	return writeJSON(stdout, analyzePreviewSummary{
		PreviewOutput:  outputPath,
		LoadedWidth:    rendered.Width,
		LoadedHeight:   rendered.Height,
		Filled:         *filled,
		Mode:           result.Mode,
		ToothPixels:    result.ToothPixels,
		BonePixels:     result.BonePixels,
		Coverage:       result.Coverage,
		CandidateCount: result.CandidateCount,
	})
}

func listCommands(stdout io.Writer) error {
	for _, command := range contracts.SupportedCommands {
		if _, err := fmt.Fprintln(stdout, command); err != nil {
			return err
		}
	}
	return nil
}

func serveIPC(args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return fmt.Errorf("serve-ipc expects no arguments")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	backend, err := app.New(cfg)
	if err != nil {
		return err
	}
	if err := backend.Prepare(); err != nil {
		return err
	}
	return ipc.NewServer(backend).Serve(os.Stdin, stdout)
}

func renderCLISourcePreview(path string, fullRange bool) (bmp.RenderedPreview, error) {
	if fullRange {
		return bmp.RenderGrayscalePreviewFileForToothAnalysis(path)
	}
	return bmp.RenderGrayscalePreviewFile(path)
}

func writeJSON(stdout io.Writer, value any) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeUsage(output io.Writer) {
	_, _ = fmt.Fprintf(output, `X-ray BMP preview / processing / analysis CLI

Usage: %s <COMMAND>

Commands:
  print-config
  decode-source <path>
  processing-manifest
  describe-study <path>
  render-preview [--full-range] <input-path> <output-path>
  process-preview [--full-range] [--invert] [--brightness N] [--contrast N] [--equalize] [--palette NAME] [--compare] <input-path> <output-path>
  analyze-preview [--filled] <input-path> <output-path>
  serve-ipc
  list-commands
  version
`, programName)
}

func windowMode(fullRange bool) string {
	if fullRange {
		return "full-range"
	}
	return "default"
}

type configSummary struct {
	ServiceName string               `json:"serviceName"`
	Logging     config.LoggingConfig `json:"logging"`
	Paths       config.PathsConfig   `json:"paths"`
}

type decodeSourceSummary struct {
	Width       uint32                      `json:"width"`
	Height      uint32                      `json:"height"`
	Format      string                      `json:"format"`
	PixelCount  int                         `json:"pixelCount"`
	MinValue    byte                        `json:"minValue"`
	MaxValue    byte                        `json:"maxValue"`
	Invert      bool                        `json:"invert"`
	Measurement *contracts.MeasurementScale `json:"measurementScale,omitempty"`
}

func decodeSourceSummaryFrom(source bmp.DecodedSourcePreview, _ bmp.Metadata) decodeSourceSummary {
	minValue := byte(0)
	maxValue := byte(0)
	if len(source.Pixels) > 0 {
		minValue = source.Pixels[0]
		maxValue = source.Pixels[0]
		for _, value := range source.Pixels[1:] {
			if value < minValue {
				minValue = value
			}
			if value > maxValue {
				maxValue = value
			}
		}
	}
	return decodeSourceSummary{
		Width:      source.Width,
		Height:     source.Height,
		Format:     "gray8",
		PixelCount: len(source.Pixels),
		MinValue:   minValue,
		MaxValue:   maxValue,
		Invert:     false,
	}
}

type studyDescription struct {
	Width             uint32                      `json:"width"`
	Height            uint32                      `json:"height"`
	ColorChannelCount uint16                      `json:"colorChannelCount"`
	BitsPerChannel    uint16                      `json:"bitsPerChannel"`
	ColorModel        string                      `json:"colorModel"`
	Measurement       *contracts.MeasurementScale `json:"measurementScale,omitempty"`
}

type renderPreviewSummary struct {
	PreviewOutput     string                      `json:"previewOutput"`
	LoadedWidth       uint32                      `json:"loadedWidth"`
	LoadedHeight      uint32                      `json:"loadedHeight"`
	WindowMode        string                      `json:"windowMode"`
	Measurement       *contracts.MeasurementScale `json:"measurementScale,omitempty"`
	RenderedByteCount int                         `json:"renderedByteCount"`
}

type processPreviewSummary struct {
	PreviewOutput     string                      `json:"previewOutput"`
	LoadedWidth       uint32                      `json:"loadedWidth"`
	LoadedHeight      uint32                      `json:"loadedHeight"`
	WindowMode        string                      `json:"windowMode"`
	Mode              string                      `json:"mode"`
	Palette           string                      `json:"palette"`
	Compare           bool                        `json:"compare"`
	Measurement       *contracts.MeasurementScale `json:"measurementScale,omitempty"`
	RenderedByteCount int                         `json:"renderedByteCount"`
}

type analyzePreviewSummary struct {
	PreviewOutput  string  `json:"previewOutput"`
	LoadedWidth    uint32  `json:"loadedWidth"`
	LoadedHeight   uint32  `json:"loadedHeight"`
	Filled         bool    `json:"filled"`
	Mode           string  `json:"mode"`
	ToothPixels    int     `json:"toothPixels"`
	BonePixels     int     `json:"bonePixels"`
	Coverage       float64 `json:"coverage"`
	CandidateCount int     `json:"candidateCount"`
}

func DisplayPath(path string) string {
	return filepath.Clean(path)
}

func Main() {
	if err := Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
