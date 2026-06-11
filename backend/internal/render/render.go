package render

import (
	"encoding/binary"
	"fmt"
	"os"
)

// PreviewFormat names the two preview buffer layouts the backend writes as
// BMP files. Gray8 is one byte per pixel; RGBA8 is four bytes per pixel.
type PreviewFormat int

const (
	Gray8 PreviewFormat = iota
	RGBA8
)

// PreviewImage is the in-memory preview passed between decode, processing,
// cache, and encode paths.
type PreviewImage struct {
	Width  uint32
	Height uint32
	Format PreviewFormat
	Pixels []byte
}

// Gray constructs a Gray8 preview. The caller keeps ownership of pixels.
func Gray(width, height uint32, pixels []byte) PreviewImage {
	return PreviewImage{Width: width, Height: height, Format: Gray8, Pixels: pixels}
}

// RGBA constructs an RGBA8 preview. The caller keeps ownership of pixels.
func RGBA(width, height uint32, pixels []byte) PreviewImage {
	return PreviewImage{Width: width, Height: height, Format: RGBA8, Pixels: pixels}
}

// SavePreviewBMP encodes a preview and writes it to path.
func SavePreviewBMP(path string, preview PreviewImage) error {
	encoded, err := EncodePreviewBMP(preview)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, encoded, 0o666); err != nil {
		return fmt.Errorf("write preview BMP %s: %w", path, err)
	}
	return nil
}

// SaveGrayBMP writes raw Gray8 pixels as an indexed grayscale BMP.
func SaveGrayBMP(path string, width, height uint32, pixels []byte) error {
	encoded, err := EncodeGrayBMP(width, height, pixels)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, encoded, 0o666); err != nil {
		return fmt.Errorf("write preview BMP %s: %w", path, err)
	}
	return nil
}

// EncodeGrayBMP encodes raw Gray8 pixels as an 8-bit indexed BMP with an
// identity grayscale palette.
func EncodeGrayBMP(width, height uint32, pixels []byte) ([]byte, error) {
	if err := validatePreviewPixels(width, height, Gray8, len(pixels)); err != nil {
		return nil, err
	}
	return encodeGray8BMP(width, height, pixels)
}

// EncodePreviewBMP encodes Gray8 as indexed BMP and RGBA8 as portable 24-bit
// BGR BMP, preserving the existing backend file format.
func EncodePreviewBMP(preview PreviewImage) ([]byte, error) {
	if err := validatePreviewPixels(preview.Width, preview.Height, preview.Format, len(preview.Pixels)); err != nil {
		return nil, err
	}
	switch preview.Format {
	case Gray8:
		return encodeGray8BMP(preview.Width, preview.Height, preview.Pixels)
	case RGBA8:
		return encodeRGBA8AsBGR24BMP(preview.Width, preview.Height, preview.Pixels)
	default:
		return nil, fmt.Errorf("unsupported preview format")
	}
}

func validatePreviewPixels(width, height uint32, format PreviewFormat, pixelLen int) error {
	if width == 0 || height == 0 {
		return fmt.Errorf("preview dimensions must be non-zero")
	}
	if width > uint32(maxInt32) || height > uint32(maxInt32) {
		return fmt.Errorf("BMP dimensions exceed signed 32-bit header limits")
	}

	channels := uint64(1)
	switch format {
	case Gray8:
		channels = 1
	case RGBA8:
		channels = 4
	default:
		return fmt.Errorf("unsupported preview format")
	}
	expected := uint64(width) * uint64(height) * channels
	if expected > uint64(maxInt) {
		return fmt.Errorf("preview dimensions overflow")
	}
	if pixelLen != int(expected) {
		return fmt.Errorf("preview pixel length = %d, want %d", pixelLen, expected)
	}
	return nil
}

func encodeGray8BMP(width, height uint32, pixels []byte) ([]byte, error) {
	widthN := int(width)
	heightN := int(height)
	rowStride := roundUpToFour(widthN)
	paletteBytes := 256 * 4
	pixelOffset := 14 + 40 + paletteBytes
	pixelBytes, ok := checkedMul(rowStride, heightN)
	if !ok {
		return nil, fmt.Errorf("BMP pixel data overflow")
	}
	fileSize, ok := checkedAdd(pixelOffset, pixelBytes)
	if !ok {
		return nil, fmt.Errorf("BMP file size overflow")
	}
	if fileSize > maxUint32 {
		return nil, fmt.Errorf("BMP file exceeds 4 GB")
	}

	bmp := make([]byte, 0, fileSize)
	bmp = appendFileHeader(bmp, uint32(fileSize), uint32(pixelOffset))
	bmp = appendInfoHeader(bmp, width, height, 8, uint32(pixelBytes), 256)
	for index := 0; index <= 255; index++ {
		value := byte(index)
		bmp = append(bmp, value, value, value, 0)
	}

	padding := rowStride - widthN
	for sourceY := heightN - 1; sourceY >= 0; sourceY-- {
		rowStart := sourceY * widthN
		bmp = append(bmp, pixels[rowStart:rowStart+widthN]...)
		bmp = appendZeroes(bmp, padding)
	}
	return bmp, nil
}

func encodeRGBA8AsBGR24BMP(width, height uint32, pixels []byte) ([]byte, error) {
	widthN := int(width)
	heightN := int(height)
	rowBytes, ok := checkedMul(widthN, 3)
	if !ok {
		return nil, fmt.Errorf("BMP row size overflow")
	}
	rowStride := roundUpToFour(rowBytes)
	pixelOffset := 14 + 40
	pixelBytes, ok := checkedMul(rowStride, heightN)
	if !ok {
		return nil, fmt.Errorf("BMP pixel data overflow")
	}
	fileSize, ok := checkedAdd(pixelOffset, pixelBytes)
	if !ok {
		return nil, fmt.Errorf("BMP file size overflow")
	}
	if fileSize > maxUint32 {
		return nil, fmt.Errorf("BMP file exceeds 4 GB")
	}

	bmp := make([]byte, 0, fileSize)
	bmp = appendFileHeader(bmp, uint32(fileSize), uint32(pixelOffset))
	bmp = appendInfoHeader(bmp, width, height, 24, uint32(pixelBytes), 0)

	padding := rowStride - rowBytes
	for sourceY := heightN - 1; sourceY >= 0; sourceY-- {
		rowStart := sourceY * widthN * 4
		for x := 0; x < widthN; x++ {
			offset := rowStart + x*4
			bmp = append(bmp, pixels[offset+2], pixels[offset+1], pixels[offset])
		}
		bmp = appendZeroes(bmp, padding)
	}
	return bmp, nil
}

func appendFileHeader(output []byte, fileSize, pixelOffset uint32) []byte {
	output = append(output, 'B', 'M')
	output = binary.LittleEndian.AppendUint32(output, fileSize)
	output = append(output, 0, 0, 0, 0)
	output = binary.LittleEndian.AppendUint32(output, pixelOffset)
	return output
}

func appendInfoHeader(output []byte, width, height uint32, bitsPerPixel uint16, imageSize, colorsUsed uint32) []byte {
	output = binary.LittleEndian.AppendUint32(output, 40)
	output = binary.LittleEndian.AppendUint32(output, uint32(int32(width)))
	output = binary.LittleEndian.AppendUint32(output, uint32(int32(height)))
	output = binary.LittleEndian.AppendUint16(output, 1)
	output = binary.LittleEndian.AppendUint16(output, bitsPerPixel)
	output = binary.LittleEndian.AppendUint32(output, 0)
	output = binary.LittleEndian.AppendUint32(output, imageSize)
	output = binary.LittleEndian.AppendUint32(output, 0)
	output = binary.LittleEndian.AppendUint32(output, 0)
	output = binary.LittleEndian.AppendUint32(output, colorsUsed)
	output = binary.LittleEndian.AppendUint32(output, 0)
	return output
}

func appendZeroes(output []byte, count int) []byte {
	for range count {
		output = append(output, 0)
	}
	return output
}

func roundUpToFour(value int) int {
	return (value + 3) / 4 * 4
}

func checkedMul(left, right int) (int, bool) {
	if left < 0 || right < 0 {
		return 0, false
	}
	if left != 0 && right > maxInt/left {
		return 0, false
	}
	return left * right, true
}

func checkedAdd(left, right int) (int, bool) {
	if left > maxInt-right {
		return 0, false
	}
	return left + right, true
}

const (
	maxInt32  = int32(^uint32(0) >> 1)
	maxUint32 = int(^uint32(0))
	maxInt    = int(^uint(0) >> 1)
)
