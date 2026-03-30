package imageio

import (
	"fmt"
	"image"

	"github.com/suyashkumar/dicom"
)

// LoadedImage contains the decoded image together with its source format.
type LoadedImage struct {
	Image  image.Image
	Format string
	DICOM  *dicom.Dataset
}

// Load opens and decodes a DICOM file.
func Load(path string) (LoadedImage, error) {
	loaded, err := loadDICOM(path)
	if err != nil {
		return LoadedImage{}, fmt.Errorf("decode input image: %w", err)
	}

	return loaded, nil
}
