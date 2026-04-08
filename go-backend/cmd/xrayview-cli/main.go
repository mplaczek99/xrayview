package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"xrayview/go-backend/internal/app"
	"xrayview/go-backend/internal/config"
	"xrayview/go-backend/internal/contracts"
	"xrayview/go-backend/internal/dicommeta"
	"xrayview/go-backend/internal/imaging"
	"xrayview/go-backend/internal/render"
	"xrayview/go-backend/internal/rustdecode"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage(os.Stderr)
		return fmt.Errorf("expected a subcommand")
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
	case "list-commands":
		for _, command := range contracts.SupportedCommandStrings() {
			fmt.Println(command)
		}
		return nil
	case "version":
		fmt.Printf("%s contract-v%d\n", contracts.ServiceName, contracts.BackendContractVersion)
		return nil
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return nil
	default:
		printUsage(os.Stderr)
		return fmt.Errorf("unknown subcommand: %s", args[0])
	}
}

func serve() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
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

	helper, err := rustdecode.NewFromEnvironment()
	if err != nil {
		return err
	}

	study, err := helper.DecodeStudy(context.Background(), args[0])
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

	helper, err := rustdecode.NewFromEnvironment()
	if err != nil {
		return err
	}

	study, err := helper.DecodeStudy(context.Background(), inputPath)
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

func printUsage(stream *os.File) {
	fmt.Fprintln(stream, "usage: xrayview-cli <subcommand>")
	fmt.Fprintln(stream, "")
	fmt.Fprintln(stream, "subcommands:")
	fmt.Fprintln(stream, "  serve         run the phase 7 local HTTP backend")
	fmt.Fprintln(stream, "  print-config  print resolved backend configuration as JSON")
	fmt.Fprintln(stream, "  inspect-decode inspect decode-relevant DICOM metadata as JSON")
	fmt.Fprintln(stream, "  decode-source decode source pixels through the phase 13 Rust helper")
	fmt.Fprintln(stream, "  render-preview render a grayscale PNG preview through the phase 16 Go pipeline")
	fmt.Fprintln(stream, "  list-commands print supported command names")
	fmt.Fprintln(stream, "  version       print service and contract version")
}
