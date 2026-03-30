package imageio

import (
	"fmt"
	"image"
	"image/png"
	"os"
)

// SavePreviewPNG writes img to path as an internal PNG preview file.
func SavePreviewPNG(path string, img image.Image) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create output image: %w", err)
	}

	if err := png.Encode(file, img); err != nil {
		file.Close()
		return fmt.Errorf("encode output image: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close output image: %w", err)
	}

	return nil
}

// SaveDICOM writes img to path as a derived DICOM image.
func SaveDICOM(path string, img image.Image, source LoadedImage) error {
	return saveDICOM(path, img, source)
}
