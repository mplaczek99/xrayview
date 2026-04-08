package processing

import (
	"fmt"

	"xrayview/go-backend/internal/imaging"
)

func CombineComparison(
	left imaging.PreviewImage,
	right imaging.PreviewImage,
) (imaging.PreviewImage, error) {
	if err := left.Validate(); err != nil {
		return imaging.PreviewImage{}, fmt.Errorf("validate left preview image: %w", err)
	}
	if err := right.Validate(); err != nil {
		return imaging.PreviewImage{}, fmt.Errorf("validate right preview image: %w", err)
	}
	if left.Format != imaging.FormatGray8 {
		return imaging.PreviewImage{}, fmt.Errorf(
			"compare preview requires %q source on the left side",
			imaging.FormatGray8,
		)
	}
	if left.Width != right.Width || left.Height != right.Height {
		return imaging.PreviewImage{}, fmt.Errorf("compare preview requires matching image dimensions")
	}
	if left.Width > ^uint32(0)/2 {
		return imaging.PreviewImage{}, fmt.Errorf("compare preview width overflow")
	}

	width := int(left.Width)
	height := int(left.Height)
	combinedWidth := left.Width * 2
	pixels := make([]uint8, int(uint64(combinedWidth)*uint64(left.Height)*4))

	for row := 0; row < height; row++ {
		dstRowStart := row * int(combinedWidth) * 4
		dstRow := pixels[dstRowStart : dstRowStart+int(combinedWidth)*4]

		leftRow := left.Pixels[row*width : (row+1)*width]
		for column, value := range leftRow {
			base := column * 4
			dstRow[base] = value
			dstRow[base+1] = value
			dstRow[base+2] = value
			dstRow[base+3] = 255
		}

		switch right.Format {
		case imaging.FormatGray8:
			rightRow := right.Pixels[row*width : (row+1)*width]
			base := width * 4
			for column, value := range rightRow {
				offset := base + column*4
				dstRow[offset] = value
				dstRow[offset+1] = value
				dstRow[offset+2] = value
				dstRow[offset+3] = 255
			}
		case imaging.FormatRGBA8:
			srcRowStart := row * width * 4
			srcRow := right.Pixels[srcRowStart : srcRowStart+width*4]
			copy(dstRow[width*4:width*8], srcRow)
		default:
			return imaging.PreviewImage{}, fmt.Errorf("unsupported preview image format %q", right.Format)
		}
	}

	return imaging.RGBAPreview(combinedWidth, left.Height, pixels), nil
}
