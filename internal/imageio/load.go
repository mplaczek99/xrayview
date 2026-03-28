package imageio

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
)

func Load(path string) (image.Image, string, error) {
	// The blank imports register decoders up front so image.Decode can choose the
	// format automatically without the caller having to branch on file type.
	file, err := os.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("open input image: %w", err)
	}
	defer file.Close()

	img, format, err := image.Decode(file)
	if err != nil {
		return nil, "", fmt.Errorf("decode input image: %w", err)
	}

	return img, format, nil
}
