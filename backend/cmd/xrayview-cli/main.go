package main

// xrayview-cli has two entry shapes, dispatched on args[0]:
//
//   - subcommand form — "xrayview-cli serve", "print-config",
//     "render-preview", etc. Handled by the switch in runWithIO;
//     adding a new one means adding a case there.
//   - legacy workflow-flags form — "xrayview-cli --input … --preset …".
//     A "-"-prefixed first arg routes everything to runLegacyCLI in
//     legacy_cli.go, where the old flag-based interface still lives.
//     Adding a new workflow flag means editing parseLegacyCLIArgs.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"xrayview/backend/internal/app"
	"xrayview/backend/internal/config"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/dicommeta"
	dicomexport "xrayview/backend/internal/export"
	"xrayview/backend/internal/imaging"
	"xrayview/backend/internal/processing"
	"xrayview/backend/internal/render"
	"xrayview/backend/internal/shutdown"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	return runWithIO(args, os.Stdout, os.Stderr)
}

func runWithIO(args []string, stdout, stderr io.Writer) error {
	for len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}

	if len(args) == 0 {
		printUsage(stderr)
		return fmt.Errorf("expected workflow flags or a subcommand")
	}

	if strings.HasPrefix(args[0], "-") {
		return runLegacyCLI(args, stdout, stderr)
	}

	switch args[0] {
	case "serve":
		return serve()
	case "print-config":
		return printConfig()
	case "inspect-decode":
		return inspectDecode(args[1:])
	case "decode-source":
		return decodeSource(args[1:])
	case "render-preview":
		return renderPreview(args[1:])
	case "process-preview":
		return processPreview(args[1:])
	case "export-secondary-capture":
		return exportSecondaryCapture(args[1:])
	case "list-commands":
		for _, command := range contracts.SupportedCommandStrings() {
			fmt.Println(command)
		}
		return nil
	case "version":
		fmt.Fprintf(stdout, "%s contract-v%d\n", contracts.ServiceName, contracts.BackendContractVersion)
		return nil
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown subcommand: %s", args[0])
	}
}

func serve() error {
	ctx, stop := signal.NotifyContext(context.Background(), shutdown.Signals()...)
	defer stop()

	application, err := app.NewFromEnvironment()
	if err != nil {
		return err
	}

	return application.Run(ctx)
}

func printConfig() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(cfg)
}

func inspectDecode(paths []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("inspect-decode requires at least one DICOM path")
	}

	report, err := dicommeta.InspectFiles(paths)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func decodeSource(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("decode-source requires exactly one DICOM path")
	}

	study, err := dicommeta.DecodeFile(args[0])
	if err != nil {
		return err
	}

	summary := struct {
		Width                 uint32                      `json:"width"`
		Height                uint32                      `json:"height"`
		Format                imaging.ImageFormat         `json:"format"`
		PixelCount            int                         `json:"pixelCount"`
		MinValue              float32                     `json:"minValue"`
		MaxValue              float32                     `json:"maxValue"`
		DefaultWindow         *imaging.WindowLevel        `json:"defaultWindow,omitempty"`
		Invert                bool                        `json:"invert"`
		MeasurementScale      *contracts.MeasurementScale `json:"measurementScale,omitempty"`
		StudyInstanceUID      string                      `json:"studyInstanceUid"`
		PreservedElementCount int                         `json:"preservedElementCount"`
	}{
		Width:                 study.Image.Width,
		Height:                study.Image.Height,
		Format:                study.Image.Format,
		PixelCount:            len(study.Image.Pixels),
		MinValue:              study.Image.MinValue,
		MaxValue:              study.Image.MaxValue,
		DefaultWindow:         study.Image.DefaultWindow,
		Invert:                study.Image.Invert,
		MeasurementScale:      study.MeasurementScale,
		StudyInstanceUID:      study.Metadata.StudyInstanceUID,
		PreservedElementCount: len(study.Metadata.PreservedElements),
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(summary)
}

func renderPreview(args []string) error {
	plan, inputPath, outputPath, err := parseRenderPreviewArgs(args)
	if err != nil {
		return err
	}

	study, err := dicommeta.DecodeFile(inputPath)
	if err != nil {
		return err
	}

	preview := render.RenderSourceImage(study.Image, plan)
	if err := render.SavePreviewPNG(outputPath, preview); err != nil {
		return err
	}

	summary := struct {
		PreviewOutput     string                      `json:"previewOutput"`
		LoadedWidth       uint32                      `json:"loadedWidth"`
		LoadedHeight      uint32                      `json:"loadedHeight"`
		WindowMode        string                      `json:"windowMode"`
		MeasurementScale  *contracts.MeasurementScale `json:"measurementScale,omitempty"`
		RenderedByteCount int                         `json:"renderedByteCount"`
	}{
		PreviewOutput:     outputPath,
		LoadedWidth:       study.Image.Width,
		LoadedHeight:      study.Image.Height,
		WindowMode:        windowModeLabel(plan.Window),
		MeasurementScale:  study.MeasurementScale,
		RenderedByteCount: len(preview.Pixels),
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(summary)
}

func processPreview(args []string) error {
	plan, controls, palette, compare, inputPath, outputPath, err := parseProcessPreviewArgs(args)
	if err != nil {
		return err
	}

	study, err := dicommeta.DecodeFile(inputPath)
	if err != nil {
		return err
	}

	processed, err := processing.ProcessSourceImage(study.Image, plan, controls, palette, compare)
	if err != nil {
		return err
	}
	if err := render.SavePreviewPNG(outputPath, processed.Preview); err != nil {
		return err
	}

	summary := struct {
		PreviewOutput     string                      `json:"previewOutput"`
		LoadedWidth       uint32                      `json:"loadedWidth"`
		LoadedHeight      uint32                      `json:"loadedHeight"`
		WindowMode        string                      `json:"windowMode"`
		Mode              string                      `json:"mode"`
		Palette           string                      `json:"palette"`
		Compare           bool                        `json:"compare"`
		MeasurementScale  *contracts.MeasurementScale `json:"measurementScale,omitempty"`
		RenderedByteCount int                         `json:"renderedByteCount"`
	}{
		PreviewOutput:     outputPath,
		LoadedWidth:       study.Image.Width,
		LoadedHeight:      study.Image.Height,
		WindowMode:        windowModeLabel(plan.Window),
		Mode:              processed.Mode,
		Palette:           palette,
		Compare:           compare,
		MeasurementScale:  study.MeasurementScale,
		RenderedByteCount: len(processed.Preview.Pixels),
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(summary)
}

func exportSecondaryCapture(args []string) error {
	plan, controls, palette, compare, inputPath, outputPath, err := parseProcessPreviewArgs(args)
	if err != nil {
		return err
	}

	study, err := dicommeta.DecodeFile(inputPath)
	if err != nil {
		return err
	}

	processed, err := processing.ProcessSourceImage(study.Image, plan, controls, palette, compare)
	if err != nil {
		return err
	}
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

	summary := struct {
		DicomOutput       string                      `json:"dicomOutput"`
		LoadedWidth       uint32                      `json:"loadedWidth"`
		LoadedHeight      uint32                      `json:"loadedHeight"`
		WindowMode        string                      `json:"windowMode"`
		Mode              string                      `json:"mode"`
		Palette           string                      `json:"palette"`
		Compare           bool                        `json:"compare"`
		MeasurementScale  *contracts.MeasurementScale `json:"measurementScale,omitempty"`
		RenderedByteCount int                         `json:"renderedByteCount"`
	}{
		DicomOutput:       outputPath,
		LoadedWidth:       study.Image.Width,
		LoadedHeight:      study.Image.Height,
		WindowMode:        windowModeLabel(plan.Window),
		Mode:              processed.Mode,
		Palette:           palette,
		Compare:           compare,
		MeasurementScale:  study.MeasurementScale,
		RenderedByteCount: len(processed.Preview.Pixels),
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(summary)
}

func parseRenderPreviewArgs(args []string) (render.RenderPlan, string, string, error) {
	plan := render.DefaultRenderPlan()
	positional := make([]string, 0, 2)

	for _, arg := range args {
		switch arg {
		case "--full-range":
			plan.Window = render.FullRangeWindowMode()
		default:
			if strings.HasPrefix(arg, "-") {
				return render.RenderPlan{}, "", "", fmt.Errorf("unknown render-preview flag: %s", arg)
			}
			positional = append(positional, arg)
		}
	}

	if len(positional) != 2 {
		return render.RenderPlan{}, "", "", fmt.Errorf(
			"render-preview requires INPUT_DCM OUTPUT_PNG and accepts optional --full-range",
		)
	}

	return plan, positional[0], positional[1], nil
}

func parseProcessPreviewArgs(
	args []string,
) (render.RenderPlan, processing.GrayscaleControls, string, bool, string, string, error) {
	plan := render.DefaultRenderPlan()
	controls := processing.GrayscaleControls{Contrast: 1.0}
	palette := processing.PaletteNone
	compare := false
	positional := make([]string, 0, 2)

	for index := 0; index < len(args); index++ {
		arg := args[index]

		switch arg {
		case "--full-range":
			plan.Window = render.FullRangeWindowMode()
		case "--invert":
			controls.Invert = true
		case "--equalize":
			controls.Equalize = true
		case "--compare":
			compare = true
		case "--brightness":
			if index+1 >= len(args) {
				return render.RenderPlan{}, processing.GrayscaleControls{}, "", false, "", "", fmt.Errorf(
					"process-preview flag %s requires a value",
					arg,
				)
			}

			index += 1
			value, err := strconv.Atoi(args[index])
			if err != nil {
				return render.RenderPlan{}, processing.GrayscaleControls{}, "", false, "", "", fmt.Errorf(
					"parse process-preview brightness %q: %w",
					args[index],
					err,
				)
			}
			controls.Brightness = value
		case "--contrast":
			if index+1 >= len(args) {
				return render.RenderPlan{}, processing.GrayscaleControls{}, "", false, "", "", fmt.Errorf(
					"process-preview flag %s requires a value",
					arg,
				)
			}

			index += 1
			value, err := strconv.ParseFloat(args[index], 64)
			if err != nil {
				return render.RenderPlan{}, processing.GrayscaleControls{}, "", false, "", "", fmt.Errorf(
					"parse process-preview contrast %q: %w",
					args[index],
					err,
				)
			}
			controls.Contrast = value
		case "--palette":
			if index+1 >= len(args) {
				return render.RenderPlan{}, processing.GrayscaleControls{}, "", false, "", "", fmt.Errorf(
					"process-preview flag %s requires a value",
					arg,
				)
			}

			index += 1
			normalized, err := processing.NormalizePaletteName(args[index])
			if err != nil {
				return render.RenderPlan{}, processing.GrayscaleControls{}, "", false, "", "", err
			}
			palette = normalized
		default:
			if strings.HasPrefix(arg, "-") {
				return render.RenderPlan{}, processing.GrayscaleControls{}, "", false, "", "", fmt.Errorf(
					"unknown process-preview flag: %s",
					arg,
				)
			}
			positional = append(positional, arg)
		}
	}

	if len(positional) != 2 {
		return render.RenderPlan{}, processing.GrayscaleControls{}, "", false, "", "", fmt.Errorf(
			"process-preview requires INPUT_DCM OUTPUT_PNG and accepts optional --full-range, --invert, --brightness, --contrast, --equalize, --palette, and --compare",
		)
	}

	return plan, controls, palette, compare, positional[0], positional[1], nil
}

func windowModeLabel(mode render.WindowMode) string {
	switch mode.Kind {
	case render.WindowModeDefault:
		return "default"
	case render.WindowModeFullRange:
		return "full-range"
	case render.WindowModeManual:
		return "manual"
	default:
		return "unknown"
	}
}

func printUsage(stream io.Writer) {
	fmt.Fprintln(stream, "usage: xrayview-cli [workflow flags]")
	fmt.Fprintln(stream, "       xrayview-cli <subcommand>")
	fmt.Fprintln(stream, "")
	fmt.Fprintln(stream, "workflow flags:")
	fmt.Fprintln(stream, "  --describe-presets                          print processing preset metadata as JSON")
	fmt.Fprintln(stream, "  --input <study.dcm> --describe-study       print study metadata as JSON")
	fmt.Fprintln(stream, "  --input <study.dcm> --analyze-tooth        print automatic tooth analysis as JSON")
	fmt.Fprintln(stream, "  --input <study.dcm> --preview-output <png> render a grayscale preview PNG")
	fmt.Fprintln(stream, "  --input <study.dcm> [processing flags]     write processed preview/DICOM output")
	fmt.Fprintln(stream, "")
	fmt.Fprintln(stream, "utility subcommands:")
	fmt.Fprintln(stream, "  serve         run the phase 7 local HTTP backend")
	fmt.Fprintln(stream, "  print-config  print resolved backend configuration as JSON")
	fmt.Fprintln(stream, "  inspect-decode inspect decode-relevant DICOM metadata as JSON")
	fmt.Fprintln(stream, "  decode-source decode source pixels directly in Go")
	fmt.Fprintln(stream, "  render-preview render a grayscale PNG preview through the phase 16 Go pipeline")
	fmt.Fprintln(stream, "  process-preview render then run the phase 19 preview processing pipeline")
	fmt.Fprintln(stream, "  export-secondary-capture render, process, and write a phase 29 Go DICOM export")
	fmt.Fprintln(stream, "  list-commands print supported command names")
	fmt.Fprintln(stream, "  version       print service and contract version")
}
