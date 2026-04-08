package export

import (
	"context"
	"fmt"
	"os"
	"strings"

	"xrayview/go-backend/internal/imaging"
	"xrayview/go-backend/internal/rustdecode"
	"xrayview/go-backend/internal/rustexport"
)

const ExporterModeEnv = "XRAYVIEW_SECONDARY_CAPTURE_EXPORTER"

const (
	ExporterModeGo         = "go"
	ExporterModeRustHelper = "rust-helper"
)

type Writer interface {
	WriteSecondaryCapture(
		ctx context.Context,
		path string,
		preview imaging.PreviewImage,
		sourceMeta rustdecode.SourceMetadata,
	) error
}

type GoWriter struct{}

func NewWriterFromEnvironment() (Writer, error) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv(ExporterModeEnv)))
	switch mode {
	case "", ExporterModeGo:
		return GoWriter{}, nil
	case ExporterModeRustHelper:
		return rustexport.NewFromEnvironment()
	default:
		return nil, fmt.Errorf(
			"%s must be one of %q or %q, got %q",
			ExporterModeEnv,
			ExporterModeGo,
			ExporterModeRustHelper,
			mode,
		)
	}
}

func (GoWriter) WriteSecondaryCapture(
	ctx context.Context,
	path string,
	preview imaging.PreviewImage,
	sourceMeta rustdecode.SourceMetadata,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return WriteSecondaryCapture(path, preview, sourceMeta)
}
