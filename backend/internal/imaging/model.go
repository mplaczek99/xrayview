package imaging

import (
	"encoding/json"
	"fmt"
)

type ImageFormat string

const (
	FormatGrayFloat32 ImageFormat = "gray-f32"
	FormatGray8       ImageFormat = "gray8"
	FormatRGBA8       ImageFormat = "rgba8"
)

type WindowLevel struct {
	Center float32 `json:"center"`
	Width  float32 `json:"width"`
}

// SourceImage is the decode-side type: float32 modality values
// (post-rescale, pre-window) as produced by dicommeta. One sample per
// pixel — color source images aren't modeled here. This is the input
// the render pipeline consumes.
type SourceImage struct {
	Width         uint32       `json:"width"`
	Height        uint32       `json:"height"`
	Format        ImageFormat  `json:"format,omitempty"`
	Pixels        []float32    `json:"pixels"`
	MinValue      float32      `json:"minValue"`
	MaxValue      float32      `json:"maxValue"`
	DefaultWindow *WindowLevel `json:"defaultWindow,omitempty"`
	Invert        bool         `json:"invert"`
}

// PreviewImage is the display-side type: uint8 bytes ready for PNG/JPEG
// encoding or secondary-capture export. Emitted by render and processing;
// Format tells grayscale (1 byte/px) apart from RGBA (4).
type PreviewImage struct {
	Width  uint32      `json:"width"`
	Height uint32      `json:"height"`
	Format ImageFormat `json:"format"`
	Pixels []uint8     `json:"pixels"`
}

func (image *SourceImage) UnmarshalJSON(data []byte) error {
	type sourceImageAlias SourceImage

	var payload sourceImageAlias
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}

	*image = SourceImage(payload)
	image.Normalize()
	return nil
}

func (image *SourceImage) Normalize() {
	if image.Format == "" {
		image.Format = FormatGrayFloat32
	}
}

func (image SourceImage) ByteSize() uint64 {
	return uint64(len(image.Pixels)) * 4 // float32 = 4 bytes
}

func (image SourceImage) ExpectedPixelCount() uint64 {
	return uint64(image.Width) * uint64(image.Height)
}

func (image SourceImage) Validate() error {
	normalized := image
	normalized.Normalize()

	if normalized.Width == 0 || normalized.Height == 0 {
		return fmt.Errorf("source image size must be non-zero, got %dx%d", normalized.Width, normalized.Height)
	}

	if normalized.Format != FormatGrayFloat32 {
		return fmt.Errorf(
			"source image format must be %q, got %q",
			FormatGrayFloat32,
			normalized.Format,
		)
	}

	if got, want := uint64(len(normalized.Pixels)), normalized.ExpectedPixelCount(); got != want {
		return fmt.Errorf(
			"source image pixel count %d does not match image size %dx%d",
			got,
			normalized.Width,
			normalized.Height,
		)
	}

	if len(normalized.Pixels) > 0 && normalized.MaxValue < normalized.MinValue {
		return fmt.Errorf(
			"source image max value %v must be greater than or equal to min value %v",
			normalized.MaxValue,
			normalized.MinValue,
		)
	}

	if normalized.DefaultWindow != nil && normalized.DefaultWindow.Width <= 1.0 {
		return fmt.Errorf(
			"source image default window width must be greater than 1, got %v",
			normalized.DefaultWindow.Width,
		)
	}

	return nil
}

func (image PreviewImage) ByteSize() uint64 {
	return uint64(len(image.Pixels))
}

func (image PreviewImage) ExpectedByteCount() uint64 {
	return uint64(image.Width) * uint64(image.Height) * uint64(image.Format.channelsPerPixel())
}

func (image PreviewImage) Validate() error {
	if image.Width == 0 || image.Height == 0 {
		return fmt.Errorf("preview image size must be non-zero, got %dx%d", image.Width, image.Height)
	}

	switch image.Format {
	case FormatGray8, FormatRGBA8:
	default:
		return fmt.Errorf(
			"preview image format must be one of %q or %q, got %q",
			FormatGray8,
			FormatRGBA8,
			image.Format,
		)
	}

	if got, want := uint64(len(image.Pixels)), image.ExpectedByteCount(); got != want {
		return fmt.Errorf(
			"preview image byte count %d does not match image size %dx%d with format %q",
			got,
			image.Width,
			image.Height,
			image.Format,
		)
	}

	return nil
}

func GrayPreview(width, height uint32, pixels []uint8) PreviewImage {
	return PreviewImage{
		Width:  width,
		Height: height,
		Format: FormatGray8,
		Pixels: pixels,
	}
}

func RGBAPreview(width, height uint32, pixels []uint8) PreviewImage {
	return PreviewImage{
		Width:  width,
		Height: height,
		Format: FormatRGBA8,
		Pixels: pixels,
	}
}

func (format ImageFormat) channelsPerPixel() int {
	switch format {
	case FormatGrayFloat32, FormatGray8:
		return 1
	case FormatRGBA8:
		return 4
	default:
		return 0
	}
}
