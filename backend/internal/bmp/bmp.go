package bmp

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

const (
	minBMPHeaderBytes       = 54
	maxDecodedPreviewPixels = 128 * 1024 * 1024
)

// Metadata exposes BMP header information needed by the rest of the backend.
// BMP files do not carry pixel spacing, so measurement scale is intentionally
// absent in this first Go model.
type Metadata struct {
	Width             uint32
	Height            uint32
	ColorChannelCount uint16
	BitsPerChannel    uint16
	ColorModel        string
}

// DecodedSourcePreview stores raw decoded Gray8 source pixels before display
// stretching.
type DecodedSourcePreview struct {
	Width  uint32
	Height uint32
	Pixels []byte
}

// RenderedPreview stores Gray8 pixels ready for display after optional range
// stretching.
type RenderedPreview struct {
	Width  uint32
	Height uint32
	Pixels []byte
}

// ReadFile reads only the BMP header prefix and returns metadata.
func ReadFile(path string) (Metadata, error) {
	if !supportsBMPPath(path) {
		return Metadata{}, fmt.Errorf("unsupported source image extension for %s; expected .bmp", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return Metadata{}, fmt.Errorf("open source file: %w", err)
	}
	defer file.Close()
	metadata, err := readHeaderPrefix(file)
	if err != nil {
		return Metadata{}, fmt.Errorf("read BMP metadata from %s: %w", path, err)
	}
	return metadata, nil
}

func readHeaderPrefix(reader io.Reader) (Metadata, error) {
	limited := io.LimitReader(reader, minBMPHeaderBytes)
	bytes, err := io.ReadAll(limited)
	if err != nil {
		return Metadata{}, fmt.Errorf("read BMP header: %w", err)
	}
	return ReadHeader(bytes)
}

// ReadHeader parses BMP metadata from an in-memory header prefix.
func ReadHeader(bytes []byte) (Metadata, error) {
	header, err := parseBMPHeader(bytes)
	if err != nil {
		return Metadata{}, err
	}
	return Metadata{
		Width:             uint32(header.width),
		Height:            uint32(header.height),
		ColorChannelCount: header.colorChannelCount(),
		BitsPerChannel:    8,
		ColorModel:        header.colorModel(),
	}, nil
}

// RenderGrayscalePreviewFile reads, decodes, and linearly stretches a BMP.
func RenderGrayscalePreviewFile(path string) (RenderedPreview, error) {
	return renderGrayscalePreviewFileInner(path, false)
}

// RenderGrayscalePreviewFileForToothAnalysis preserves the decoded 8-bit
// range for analysis paths that rely on absolute pixel values.
func RenderGrayscalePreviewFileForToothAnalysis(path string) (RenderedPreview, error) {
	return renderGrayscalePreviewFileInner(path, true)
}

// DecodeSourcePreviewFile reads and decodes a BMP without display stretching.
func DecodeSourcePreviewFile(path string) (DecodedSourcePreview, error) {
	if !supportsBMPPath(path) {
		return DecodedSourcePreview{}, fmt.Errorf("unsupported source image extension for %s; expected .bmp", path)
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return DecodedSourcePreview{}, fmt.Errorf("open source file: %w", err)
	}
	preview, err := DecodeSourcePreview(bytes)
	if err != nil {
		return DecodedSourcePreview{}, fmt.Errorf("decode BMP image from %s: %w", path, err)
	}
	return preview, nil
}

func renderGrayscalePreviewFileInner(path string, preserveEightBitRange bool) (RenderedPreview, error) {
	if !supportsBMPPath(path) {
		return RenderedPreview{}, fmt.Errorf("unsupported source image extension for %s; expected .bmp", path)
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return RenderedPreview{}, fmt.Errorf("open source file: %w", err)
	}
	preview, err := renderGrayscalePreviewWithOptions(bytes, preserveEightBitRange)
	if err != nil {
		return RenderedPreview{}, fmt.Errorf("decode BMP image from %s: %w", path, err)
	}
	return preview, nil
}

// RenderGrayscalePreview decodes a BMP and stretches its values to 0..255.
func RenderGrayscalePreview(bytes []byte) (RenderedPreview, error) {
	return renderGrayscalePreviewWithOptions(bytes, false)
}

func renderGrayscalePreviewWithOptions(bytes []byte, preserveEightBitRange bool) (RenderedPreview, error) {
	image, err := decodeBMP(bytes)
	if err != nil {
		return RenderedPreview{}, err
	}
	return renderDecodedBMPWithOptions(image, preserveEightBitRange), nil
}

// DecodeSourcePreview decodes a BMP into raw top-down Gray8 pixels.
func DecodeSourcePreview(bytes []byte) (DecodedSourcePreview, error) {
	image, err := decodeBMP(bytes)
	if err != nil {
		return DecodedSourcePreview{}, err
	}
	return DecodedSourcePreview{Width: image.width, Height: image.height, Pixels: image.pixels}, nil
}

// RenderGrayscalePreviewFromSource stretches a previously decoded source.
func RenderGrayscalePreviewFromSource(source DecodedSourcePreview) RenderedPreview {
	return renderGrayscalePreviewFromSourceWithOptions(source, false)
}

// RenderGrayscalePreviewFromSourceForToothAnalysis preserves the decoded
// source values without allocating a transformed buffer.
func RenderGrayscalePreviewFromSourceForToothAnalysis(source DecodedSourcePreview) RenderedPreview {
	return renderGrayscalePreviewFromSourceWithOptions(source, true)
}

func renderGrayscalePreviewFromSourceWithOptions(source DecodedSourcePreview, preserveEightBitRange bool) RenderedPreview {
	if preserveEightBitRange {
		return RenderedPreview{Width: source.Width, Height: source.Height, Pixels: source.Pixels}
	}
	return RenderedPreview{Width: source.Width, Height: source.Height, Pixels: stretchGray(source.Pixels)}
}

func renderDecodedBMPWithOptions(image decodedBMP, preserveEightBitRange bool) RenderedPreview {
	if preserveEightBitRange {
		return RenderedPreview{Width: image.width, Height: image.height, Pixels: image.pixels}
	}
	return RenderedPreview{Width: image.width, Height: image.height, Pixels: stretchGray(image.pixels)}
}

func stretchGray(pixels []byte) []byte {
	minValue := pixels[0]
	maxValue := pixels[0]
	for _, value := range pixels[1:] {
		if value < minValue {
			minValue = value
		}
		if value > maxValue {
			maxValue = value
		}
	}

	var lut [256]byte
	for value := range lut {
		lut[value] = mapLinear(float32(value), float32(minValue), float32(maxValue))
	}
	stretched := make([]byte, len(pixels))
	for index, value := range pixels {
		stretched[index] = lut[value]
	}
	return stretched
}

func supportsBMPPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".bmp")
}

type decodedBMP struct {
	width  uint32
	height uint32
	pixels []byte
}

type bmpHeader struct {
	pixelOffset  int
	dibSize      uint32
	width        int
	height       int
	topDown      bool
	bitsPerPixel uint16
}

func (header bmpHeader) colorChannelCount() uint16 {
	if header.bitsPerPixel == 8 {
		return 1
	}
	return 3
}

func (header bmpHeader) colorModel() string {
	if header.colorChannelCount() == 1 {
		return "grayscale"
	}
	return "rgb"
}

func (header bmpHeader) bytesPerPixel() int {
	return int(header.bitsPerPixel / 8)
}

func (header bmpHeader) rowStride() (int, error) {
	rowBytes, ok := checkedMul(header.width, header.bytesPerPixel())
	if !ok {
		return 0, fmt.Errorf("BMP row size overflow")
	}
	padding := (4 - rowBytes%4) % 4
	rowStride, ok := checkedAdd(rowBytes, padding)
	if !ok {
		return 0, fmt.Errorf("BMP row size overflow")
	}
	return rowStride, nil
}

type bmpPixelFormat int

const (
	pixelGray8 bmpPixelFormat = iota
	pixelPalette8
	pixelBGR24
	pixelBGRA32
)

type pixelFormatInfo struct {
	format     bmpPixelFormat
	paletteLUT [256]byte
}

func parseBMPHeader(bytes []byte) (bmpHeader, error) {
	if len(bytes) < minBMPHeaderBytes || string(bytes[:2]) != "BM" {
		return bmpHeader{}, fmt.Errorf("unsupported BMP header")
	}

	pixelOffset := int(binary.LittleEndian.Uint32(bytes[10:14]))
	dibSize := binary.LittleEndian.Uint32(bytes[14:18])
	if dibSize < 40 {
		return bmpHeader{}, fmt.Errorf("unsupported BMP DIB header size: %d", dibSize)
	}
	if dibSize > math.MaxInt32 {
		return bmpHeader{}, fmt.Errorf("BMP DIB header size overflow")
	}
	dibEnd, ok := checkedAdd(14, int(dibSize))
	if !ok {
		return bmpHeader{}, fmt.Errorf("BMP DIB header size overflow")
	}
	if pixelOffset < dibEnd {
		return bmpHeader{}, fmt.Errorf("invalid BMP pixel data offset: %d, want at least %d", pixelOffset, dibEnd)
	}

	width := int(int32(binary.LittleEndian.Uint32(bytes[18:22])))
	rawHeight := int(int32(binary.LittleEndian.Uint32(bytes[22:26])))
	if width <= 0 || rawHeight == 0 {
		return bmpHeader{}, fmt.Errorf("invalid BMP dimensions: %dx%d", width, rawHeight)
	}
	height := rawHeight
	if height < 0 {
		height = -height
	}
	if width > math.MaxUint16 || height > math.MaxUint16 {
		return bmpHeader{}, fmt.Errorf("BMP dimensions exceed supported range: %dx%d", width, rawHeight)
	}
	pixelCount, ok := checkedMul(width, height)
	if !ok {
		return bmpHeader{}, fmt.Errorf("BMP pixel count overflow")
	}
	if pixelCount > maxDecodedPreviewPixels {
		return bmpHeader{}, fmt.Errorf("BMP pixel count %d exceeds supported limit %d", pixelCount, maxDecodedPreviewPixels)
	}

	planes := binary.LittleEndian.Uint16(bytes[26:28])
	if planes != 1 {
		return bmpHeader{}, fmt.Errorf("unsupported BMP plane count: %d", planes)
	}
	bitsPerPixel := binary.LittleEndian.Uint16(bytes[28:30])
	compression := binary.LittleEndian.Uint32(bytes[30:34])
	if compression != 0 {
		return bmpHeader{}, fmt.Errorf("unsupported BMP compression: %d", compression)
	}
	if bitsPerPixel != 8 && bitsPerPixel != 24 && bitsPerPixel != 32 {
		return bmpHeader{}, fmt.Errorf("unsupported BMP bit depth: %d", bitsPerPixel)
	}

	return bmpHeader{
		pixelOffset:  pixelOffset,
		dibSize:      dibSize,
		width:        width,
		height:       height,
		topDown:      rawHeight < 0,
		bitsPerPixel: bitsPerPixel,
	}, nil
}

func decodeBMP(bytes []byte) (decodedBMP, error) {
	header, err := parseBMPHeader(bytes)
	if err != nil {
		return decodedBMP{}, err
	}
	rowStride, err := header.rowStride()
	if err != nil {
		return decodedBMP{}, err
	}
	pixelBytes, ok := checkedMul(rowStride, header.height)
	if !ok {
		return decodedBMP{}, fmt.Errorf("BMP pixel data size overflow")
	}
	required, ok := checkedAdd(header.pixelOffset, pixelBytes)
	if !ok {
		return decodedBMP{}, fmt.Errorf("BMP pixel data size overflow")
	}
	if len(bytes) < required {
		return decodedBMP{}, fmt.Errorf("BMP pixel data length = %d, want at least %d", len(bytes), required)
	}

	format, err := bmpPixelFormatFor(bytes, header, rowStride)
	if err != nil {
		return decodedBMP{}, err
	}
	pixels := make([]byte, header.width*header.height)
	switch format.format {
	case pixelGray8:
		decodeGray8Rows(bytes, header, rowStride, pixels)
	case pixelPalette8:
		decodePalette8Rows(bytes, header, rowStride, format.paletteLUT, pixels)
	case pixelBGR24:
		decodeBGR24Rows(bytes, header, rowStride, pixels)
	case pixelBGRA32:
		decodeBGRA32Rows(bytes, header, rowStride, pixels)
	}
	return decodedBMP{width: uint32(header.width), height: uint32(header.height), pixels: pixels}, nil
}

func bmpPixelFormatFor(bytes []byte, header bmpHeader, rowStride int) (pixelFormatInfo, error) {
	switch header.bitsPerPixel {
	case 8:
		return bmp8BitPixelFormat(bytes, header, rowStride)
	case 24:
		return pixelFormatInfo{format: pixelBGR24}, nil
	case 32:
		return pixelFormatInfo{format: pixelBGRA32}, nil
	default:
		panic("parseBMPHeader allowed unsupported bit depth")
	}
}

func bmp8BitPixelFormat(bytes []byte, header bmpHeader, rowStride int) (pixelFormatInfo, error) {
	paletteStart := 14 + int(header.dibSize)
	colorCount := binary.LittleEndian.Uint32(bytes[46:50])
	paletteEntries := int(colorCount)
	if colorCount == 0 {
		paletteEntries = 256
	}
	if paletteEntries == 0 || paletteEntries > 256 {
		return pixelFormatInfo{}, fmt.Errorf("unsupported BMP palette entry count: %d", paletteEntries)
	}
	paletteBytes, ok := checkedMul(paletteEntries, 4)
	if !ok {
		return pixelFormatInfo{}, fmt.Errorf("BMP palette size overflow")
	}
	if header.pixelOffset < paletteStart+paletteBytes || len(bytes) < paletteStart+paletteBytes {
		return pixelFormatInfo{}, fmt.Errorf("truncated BMP palette")
	}

	palette := bytes[paletteStart : paletteStart+paletteBytes]
	var paletteLUT [256]byte
	identityGray := paletteEntries == 256
	for index := range paletteEntries {
		offset := index * 4
		gray := grayFromRGB8(palette[offset+2], palette[offset+1], palette[offset])
		paletteLUT[index] = gray
		identityGray = identityGray && gray == byte(index)
	}
	if paletteEntries < 256 {
		if err := validate8BitPaletteIndexes(bytes, header, rowStride, paletteEntries); err != nil {
			return pixelFormatInfo{}, err
		}
	}
	if identityGray {
		return pixelFormatInfo{format: pixelGray8}, nil
	}
	return pixelFormatInfo{format: pixelPalette8, paletteLUT: paletteLUT}, nil
}

func validate8BitPaletteIndexes(bytes []byte, header bmpHeader, rowStride, paletteEntries int) error {
	for outputY := range header.height {
		sourceY := outputY
		if !header.topDown {
			sourceY = header.height - 1 - outputY
		}
		rowStart := header.pixelOffset + sourceY*rowStride
		row := bytes[rowStart : rowStart+header.width]
		for _, index := range row {
			if int(index) >= paletteEntries {
				return fmt.Errorf("BMP palette index %d exceeds %d entries", index, paletteEntries)
			}
		}
	}
	return nil
}

func decodeGray8Rows(bytes []byte, header bmpHeader, rowStride int, pixels []byte) {
	for outputY := range header.height {
		sourceY := outputY
		if !header.topDown {
			sourceY = header.height - 1 - outputY
		}
		rowStart := header.pixelOffset + sourceY*rowStride
		outputStart := outputY * header.width
		copy(pixels[outputStart:outputStart+header.width], bytes[rowStart:rowStart+header.width])
	}
}

func decodePalette8Rows(bytes []byte, header bmpHeader, rowStride int, paletteLUT [256]byte, pixels []byte) {
	for outputY := range header.height {
		sourceY := outputY
		if !header.topDown {
			sourceY = header.height - 1 - outputY
		}
		rowStart := header.pixelOffset + sourceY*rowStride
		row := bytes[rowStart : rowStart+header.width]
		outputStart := outputY * header.width
		for x, value := range row {
			pixels[outputStart+x] = paletteLUT[value]
		}
	}
}

func decodeBGR24Rows(bytes []byte, header bmpHeader, rowStride int, pixels []byte) {
	for outputY := range header.height {
		sourceY := outputY
		if !header.topDown {
			sourceY = header.height - 1 - outputY
		}
		rowStart := header.pixelOffset + sourceY*rowStride
		outputStart := outputY * header.width
		for x := range header.width {
			offset := rowStart + x*3
			pixels[outputStart+x] = grayFromRGB8(bytes[offset+2], bytes[offset+1], bytes[offset])
		}
	}
}

func decodeBGRA32Rows(bytes []byte, header bmpHeader, rowStride int, pixels []byte) {
	for outputY := range header.height {
		sourceY := outputY
		if !header.topDown {
			sourceY = header.height - 1 - outputY
		}
		rowStart := header.pixelOffset + sourceY*rowStride
		outputStart := outputY * header.width
		for x := range header.width {
			offset := rowStart + x*4
			pixels[outputStart+x] = grayFromRGB8(bytes[offset+2], bytes[offset+1], bytes[offset])
		}
	}
}

func grayFromRGB8(red, green, blue byte) byte {
	red16 := uint32(red) | (uint32(red) << 8)
	green16 := uint32(green) | (uint32(green) << 8)
	blue16 := uint32(blue) | (uint32(blue) << 8)
	return byte((19595*red16 + 38470*green16 + 7471*blue16 + (1 << 15)) >> 24)
}

func mapLinear(value, minValue, maxValue float32) byte {
	if maxValue <= minValue {
		return 0
	}
	return clampToByte((value - minValue) * (255.0 / (maxValue - minValue)))
}

func clampToByte(value float32) byte {
	if value <= 0 {
		return 0
	}
	if value >= 255 {
		return 255
	}
	return byte(value + 0.5)
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
	if right > 0 && left > maxInt-right {
		return 0, false
	}
	return left + right, true
}

const maxInt = int(^uint(0) >> 1)
