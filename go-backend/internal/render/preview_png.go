package render

import (
	"fmt"
	"image"
	"image/png"
	"io"
	"os"

	"xrayview/go-backend/internal/imaging"
)

func SavePreviewPNG(path string, preview imaging.PreviewImage) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create preview PNG %s: %w", path, err)
	}

	if err := EncodePreviewPNG(file, preview); err != nil {
		_ = file.Close()
		return err
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close preview PNG %s: %w", path, err)
	}

	return nil
}

func EncodePreviewPNG(writer io.Writer, preview imaging.PreviewImage) error {
	if err := preview.Validate(); err != nil {
		return fmt.Errorf("validate preview image: %w", err)
	}

	imageValue, err := previewImage(preview)
	if err != nil {
		return err
	}

	if err := png.Encode(writer, imageValue); err != nil {
		return fmt.Errorf("encode preview PNG: %w", err)
	}

	return nil
}

func previewImage(preview imaging.PreviewImage) (image.Image, error) {
	rect := image.Rect(0, 0, int(preview.Width), int(preview.Height))

	switch preview.Format {
	case imaging.FormatGray8:
		return &image.Gray{
			Pix:    append([]uint8(nil), preview.Pixels...),
			Stride: int(preview.Width),
			Rect:   rect,
		}, nil
	case imaging.FormatRGBA8:
		return &image.RGBA{
			Pix:    append([]uint8(nil), preview.Pixels...),
			Stride: int(preview.Width) * 4,
			Rect:   rect,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported preview image format %q", preview.Format)
	}
}
