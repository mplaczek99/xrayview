package imageio

import (
	"fmt"
	"image"
	"image/png"
	"os"
)

// SavePNG writes img to path as a PNG file.
func SavePNG(path string, img image.Image) error {
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
