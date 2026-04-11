package render

import (
	"bufio"
	"fmt"
	"image/jpeg"
	"io"
	"os"

	"xrayview/backend/internal/imaging"
)

const jpegPreviewQuality = 92

func SavePreviewJPEG(path string, preview imaging.PreviewImage) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create preview JPEG %s: %w", path, err)
	}

	bw := bufio.NewWriterSize(file, 64*1024)

	if err := EncodePreviewJPEG(bw, preview); err != nil {
		_ = file.Close()
		return err
	}

	if err := bw.Flush(); err != nil {
		_ = file.Close()
		return fmt.Errorf("flush preview JPEG %s: %w", path, err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close preview JPEG %s: %w", path, err)
	}

	return nil
}

func EncodePreviewJPEG(writer io.Writer, preview imaging.PreviewImage) error {
	if err := preview.Validate(); err != nil {
		return fmt.Errorf("validate preview image: %w", err)
	}

	imageValue, err := previewImage(preview)
	if err != nil {
		return err
	}

	if err := jpeg.Encode(writer, imageValue, &jpeg.Options{Quality: jpegPreviewQuality}); err != nil {
		return fmt.Errorf("encode preview JPEG: %w", err)
	}

	return nil
}
