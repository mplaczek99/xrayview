package render

import (
	"bufio"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"

	"xrayview/backend/internal/imaging"
)

func SavePreviewPNG(path string, preview imaging.PreviewImage) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create preview PNG %s: %w", path, err)
	}

	bw := bufio.NewWriterSize(file, 64*1024)

	if err := EncodePreviewPNG(bw, preview); err != nil {
		_ = file.Close()
		return err
	}

	if err := bw.Flush(); err != nil {
		_ = file.Close()
		return fmt.Errorf("flush preview PNG %s: %w", path, err)
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

	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(writer, imageValue); err != nil {
		return fmt.Errorf("encode preview PNG: %w", err)
	}

	return nil
}

func previewImage(preview imaging.PreviewImage) (image.Image, error) {
	rect := image.Rect(0, 0, int(preview.Width), int(preview.Height))

	switch preview.Format {
	case imaging.FormatGray8:
		return &image.Gray{
			Pix:    preview.Pixels,
			Stride: int(preview.Width),
			Rect:   rect,
		}, nil
	case imaging.FormatRGBA8:
		return &image.RGBA{
			Pix:    preview.Pixels,
			Stride: int(preview.Width) * 4,
			Rect:   rect,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported preview image format %q", preview.Format)
	}
}
