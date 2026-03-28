package imageio

import (
	"fmt"
	"image"
	"image/png"
	"os"
)

func SavePNG(path string, img image.Image) error {
	// Every output is normalized to PNG so callers can save grayscale and RGBA
	// results through one path without negotiating multiple file formats.
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
