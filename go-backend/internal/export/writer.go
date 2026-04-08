package export

import (
	"context"

	"xrayview/go-backend/internal/dicommeta"
	"xrayview/go-backend/internal/imaging"
)

type Writer interface {
	WriteSecondaryCapture(
		ctx context.Context,
		path string,
		preview imaging.PreviewImage,
		sourceMeta dicommeta.SourceMetadata,
	) error
}

type GoWriter struct{}

func NewWriterFromEnvironment() (Writer, error) {
	return GoWriter{}, nil
}

func (GoWriter) WriteSecondaryCapture(
	ctx context.Context,
	path string,
	preview imaging.PreviewImage,
	sourceMeta dicommeta.SourceMetadata,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return WriteSecondaryCapture(path, preview, sourceMeta)
}
