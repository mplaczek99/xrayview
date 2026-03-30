package imageio

import (
	"fmt"
	"image"

	"github.com/suyashkumar/dicom"
)

// MeasurementScale describes the physical size of a pixel in millimeters.
type MeasurementScale struct {
	RowSpacingMM    float64 `json:"rowSpacingMm"`
	ColumnSpacingMM float64 `json:"columnSpacingMm"`
	Source          string  `json:"source"`
}

// LoadedImage contains the decoded image together with its source format.
type LoadedImage struct {
	Image            image.Image
	Format           string
	DICOM            *dicom.Dataset
	MeasurementScale *MeasurementScale
}

// Load opens and decodes a DICOM file.
func Load(path string) (LoadedImage, error) {
	loaded, err := loadDICOM(path)
	if err != nil {
		return LoadedImage{}, fmt.Errorf("decode input image: %w", err)
	}

	return loaded, nil
}
