package dicommeta

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
)

func supportsStandaloneImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".bmp", ".tif", ".tiff":
		return true
	default:
		return false
	}
}

func tryReadImageMetadata(source readerAtSeeker) (Metadata, error) {
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return Metadata{}, fmt.Errorf("seek image input: %w", err)
	}

	config, _, err := image.DecodeConfig(source)
	if err != nil {
		return Metadata{}, err
	}

	return metadataFromImageConfig(config)
}

func tryDecodeImageStudy(source readerAtSeeker) (SourceStudy, error) {
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return SourceStudy{}, fmt.Errorf("seek image input: %w", err)
	}

	decoded, _, err := image.Decode(source)
	if err != nil {
		return SourceStudy{}, err
	}

	imageValue, err := sourceImageFromImage(decoded, nil, false)
	if err != nil {
		return SourceStudy{}, err
	}

	return SourceStudy{
		Image:            imageValue,
		Metadata:         SourceMetadata{},
		MeasurementScale: nil,
	}, nil
}

func metadataFromImageConfig(config image.Config) (Metadata, error) {
	if config.Width <= 0 || config.Height <= 0 {
		return Metadata{}, fmt.Errorf("invalid image size %dx%d", config.Width, config.Height)
	}
	if config.Width > math.MaxUint16 || config.Height > math.MaxUint16 {
		return Metadata{}, fmt.Errorf(
			"image dimensions exceed supported range: %dx%d",
			config.Width,
			config.Height,
		)
	}

	sample := config.ColorModel.Convert(color.Black)

	metadata := Metadata{
		Rows:                      uint16(config.Height),
		Columns:                   uint16(config.Width),
		SamplesPerPixel:           3,
		BitsAllocated:             8,
		BitsStored:                8,
		PixelRepresentation:       0,
		NumberOfFrames:            1,
		PixelDataEncoding:         PixelDataEncodingNative,
		PhotometricInterpretation: "RGB",
	}

	switch sample.(type) {
	case color.Gray, color.Alpha:
		metadata.SamplesPerPixel = 1
		metadata.PhotometricInterpretation = "MONOCHROME2"
	case color.Gray16, color.Alpha16:
		metadata.SamplesPerPixel = 1
		metadata.BitsAllocated = 16
		metadata.BitsStored = 16
		metadata.PhotometricInterpretation = "MONOCHROME2"
	case color.RGBA64, color.NRGBA64:
		metadata.BitsAllocated = 16
		metadata.BitsStored = 16
	}

	metadata.applyDecodeDefaults()
	return metadata, nil
}
