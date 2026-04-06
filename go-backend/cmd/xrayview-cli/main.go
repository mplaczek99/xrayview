package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"xrayview/go-backend/internal/app"
	"xrayview/go-backend/internal/config"
	"xrayview/go-backend/internal/contracts"
	"xrayview/go-backend/internal/dicommeta"
	"xrayview/go-backend/internal/imaging"
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

func printUsage(stream *os.File) {
	fmt.Fprintln(stream, "usage: xrayview-cli <subcommand>")
	fmt.Fprintln(stream, "")
	fmt.Fprintln(stream, "subcommands:")
	fmt.Fprintln(stream, "  serve         run the phase 7 local HTTP backend")
	fmt.Fprintln(stream, "  print-config  print resolved backend configuration as JSON")
	fmt.Fprintln(stream, "  inspect-decode inspect decode-relevant DICOM metadata as JSON")
	fmt.Fprintln(stream, "  decode-source decode source pixels through the phase 13 Rust helper")
	fmt.Fprintln(stream, "  list-commands print supported command names")
	fmt.Fprintln(stream, "  version       print service and contract version")
}
