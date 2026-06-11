package bmp

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileReadsBMPMetadata(t *testing.T) {
	path := writeTempBMP(t, "metadata.bmp", buildBMP32(2, 2, []rgb{
		{0, 0, 0}, {255, 0, 0}, {0, 255, 0}, {255, 255, 255},
	}))

	metadata, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if metadata != (Metadata{Width: 2, Height: 2, ColorChannelCount: 3, BitsPerChannel: 8, ColorModel: "rgb"}) {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestReadFileReadsMetadataWithoutPixelData(t *testing.T) {
	pixels := make([]rgb, 854*1200)
	bmp := buildBMP32(854, 1200, pixels)
	bmp = bmp[:54]
	path := writeTempBMP(t, "metadata-header-only.bmp", bmp)

	metadata, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Width != 854 || metadata.Height != 1200 || metadata.ColorModel != "rgb" {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestReadHeaderPrefixStopsBeforePixelData(t *testing.T) {
	reader := &headerOnlyReader{bytes: buildBMP32(2, 2, []rgb{
		{0, 0, 0}, {255, 0, 0}, {0, 255, 0}, {255, 255, 255},
	})}

	metadata, err := readHeaderPrefix(reader)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Width != 2 || metadata.Height != 2 {
		t.Fatalf("metadata = %+v", metadata)
	}
	if reader.position != minBMPHeaderBytes {
		t.Fatalf("reader position = %d, want %d", reader.position, minBMPHeaderBytes)
	}
}

func TestRenderStillRejectsMissingPixelData(t *testing.T) {
	bmp := buildBMP32(2, 2, []rgb{{0, 0, 0}, {255, 0, 0}, {0, 255, 0}, {255, 255, 255}})
	bmp = bmp[:54]

	_, err := RenderGrayscalePreview(bmp)
	if err == nil || !strings.Contains(err.Error(), "BMP pixel data length") {
		t.Fatalf("error = %v, want pixel data length", err)
	}
}

func TestRenderRejectsPixelOffsetInsideHeader(t *testing.T) {
	bmp := buildBMP32(1, 1, []rgb{{255, 255, 255}})
	binary.LittleEndian.PutUint32(bmp[10:14], 14)

	_, err := RenderGrayscalePreview(bmp)
	if err == nil || !strings.Contains(err.Error(), "invalid BMP pixel data offset") {
		t.Fatalf("error = %v, want invalid pixel data offset", err)
	}
}

func TestRenderRejectsAbsurdDIBHeaderSizeWithoutPanicking(t *testing.T) {
	bmp := buildBMP32(1, 1, []rgb{{255, 255, 255}})
	binary.LittleEndian.PutUint32(bmp[14:18], ^uint32(0))

	_, err := RenderGrayscalePreview(bmp)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "BMP DIB header size overflow") &&
		!strings.Contains(err.Error(), "truncated BMP DIB header") &&
		!strings.Contains(err.Error(), "invalid BMP pixel data offset") {
		t.Fatalf("error = %v", err)
	}
}

func TestRenderRejectsAbsurdPixelCountBeforeAllocation(t *testing.T) {
	bmp := buildBMP32Header(65_535, 65_535)

	_, err := RenderGrayscalePreview(bmp)
	if err == nil || !strings.Contains(err.Error(), "BMP pixel count") || !strings.Contains(err.Error(), "exceeds supported limit") {
		t.Fatalf("error = %v, want pixel count supported limit", err)
	}
	_, err = ReadHeader(bmp[:54])
	if err == nil || !strings.Contains(err.Error(), "BMP pixel count") {
		t.Fatalf("ReadHeader error = %v, want pixel count", err)
	}
}

func TestRowStrideRejectsPaddingOverflow(t *testing.T) {
	header := bmpHeader{
		pixelOffset:  54,
		dibSize:      40,
		width:        maxInt,
		height:       1,
		bitsPerPixel: 8,
	}

	_, err := header.rowStride()
	if err == nil || !strings.Contains(err.Error(), "BMP row size overflow") {
		t.Fatalf("error = %v, want row size overflow", err)
	}
}

func TestRenderGrayscalePreviewFileReadsBMPPixels(t *testing.T) {
	input := []rgb{{0, 0, 0}, {255, 0, 0}, {0, 255, 0}, {255, 255, 255}}
	path := writeTempBMP(t, "render.bmp", buildBMP32(2, 2, input))

	preview, err := RenderGrayscalePreviewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := fullRangeMapped([]byte{
		grayFromRGB8(0, 0, 0),
		grayFromRGB8(255, 0, 0),
		grayFromRGB8(0, 255, 0),
		grayFromRGB8(255, 255, 255),
	})
	if preview.Width != 2 || preview.Height != 2 || string(preview.Pixels) != string(want) {
		t.Fatalf("preview = %dx%d %v, want %v", preview.Width, preview.Height, preview.Pixels, want)
	}
}

func TestRenderToothAnalysisPreviewPreservesBMP8BitRange(t *testing.T) {
	path := writeTempBMP(t, "analysis-render.bmp", buildBMP8Palette(
		4, 1,
		[]rgb{{10, 10, 10}, {20, 20, 20}, {30, 30, 30}, {40, 40, 40}},
		[]byte{0, 1, 2, 3},
	))

	defaultPreview, err := RenderGrayscalePreviewFile(path)
	if err != nil {
		t.Fatal(err)
	}
	analysisPreview, err := RenderGrayscalePreviewFileForToothAnalysis(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(defaultPreview.Pixels) != string([]byte{0, 85, 170, 255}) {
		t.Fatalf("default pixels = %v", defaultPreview.Pixels)
	}
	if string(analysisPreview.Pixels) != string([]byte{10, 20, 30, 40}) {
		t.Fatalf("analysis pixels = %v", analysisPreview.Pixels)
	}
}

func TestDecodedSourcePreviewDerivesMatchingRenderVariants(t *testing.T) {
	bmp := buildBMP32(2, 2, []rgb{{10, 10, 10}, {40, 40, 40}, {80, 80, 80}, {160, 160, 160}})
	source, err := DecodeSourcePreview(bmp)
	if err != nil {
		t.Fatal(err)
	}
	defaultPreview, err := RenderGrayscalePreview(bmp)
	if err != nil {
		t.Fatal(err)
	}
	analysisPreview, err := renderGrayscalePreviewWithOptions(bmp, true)
	if err != nil {
		t.Fatal(err)
	}

	if got := RenderGrayscalePreviewFromSource(source); got.Width != defaultPreview.Width || got.Height != defaultPreview.Height || string(got.Pixels) != string(defaultPreview.Pixels) {
		t.Fatalf("default from source = %+v, want %+v", got, defaultPreview)
	}
	if got := RenderGrayscalePreviewFromSourceForToothAnalysis(source); got.Width != analysisPreview.Width || got.Height != analysisPreview.Height || string(got.Pixels) != string(analysisPreview.Pixels) {
		t.Fatalf("analysis from source = %+v, want %+v", got, analysisPreview)
	}
}

func TestRenderBMPSupportsPalettePixels(t *testing.T) {
	bmp := buildBMP8Palette(2, 1, []rgb{{0, 0, 0}, {255, 255, 255}}, []byte{0, 1})
	preview, err := RenderGrayscalePreview(bmp)
	if err != nil {
		t.Fatal(err)
	}
	if string(preview.Pixels) != string([]byte{0, 255}) {
		t.Fatalf("pixels = %v, want [0 255]", preview.Pixels)
	}
}

func TestRenderBMPSupportsTopDownRows(t *testing.T) {
	bmp := buildBMP32TopDown(2, 2, []rgb{{0, 0, 0}, {32, 32, 32}, {160, 160, 160}, {255, 255, 255}})
	preview, err := renderGrayscalePreviewWithOptions(bmp, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(preview.Pixels) != string([]byte{0, 32, 160, 255}) {
		t.Fatalf("pixels = %v, want [0 32 160 255]", preview.Pixels)
	}
}

func TestRenderBMPRejectsPartialPaletteIndexOutOfRange(t *testing.T) {
	bmp := buildBMP8Palette(2, 1, []rgb{{0, 0, 0}, {255, 255, 255}}, []byte{0, 2})

	_, err := RenderGrayscalePreview(bmp)
	if err == nil || !strings.Contains(err.Error(), "BMP palette index 2 exceeds 2 entries") {
		t.Fatalf("error = %v", err)
	}
}

func TestRejectsNonBMPExtension(t *testing.T) {
	path := writeTempFile(t, "metadata.png", buildBMP32(1, 1, []rgb{{0, 0, 0}}))

	_, readErr := ReadFile(path)
	_, decodeErr := DecodeSourcePreviewFile(path)
	_, renderErr := RenderGrayscalePreviewFile(path)
	for _, err := range []error{readErr, decodeErr, renderErr} {
		if err == nil || !strings.Contains(err.Error(), "expected .bmp") {
			t.Fatalf("error = %v, want expected .bmp", err)
		}
	}
}

type headerOnlyReader struct {
	bytes    []byte
	position int
}

func (reader *headerOnlyReader) Read(output []byte) (int, error) {
	if reader.position >= minBMPHeaderBytes {
		return 0, errors.New("pixel data was read")
	}
	remainingHeader := minBMPHeaderBytes - reader.position
	count := min(len(output), remainingHeader)
	copy(output[:count], reader.bytes[reader.position:reader.position+count])
	reader.position += count
	if count == 0 {
		return 0, io.EOF
	}
	return count, nil
}

type rgb struct {
	red   byte
	green byte
	blue  byte
}

func writeTempBMP(t *testing.T, name string, bytes []byte) string {
	t.Helper()
	return writeTempFile(t, name, bytes)
}

func writeTempFile(t *testing.T, name string, bytes []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, bytes, 0o666); err != nil {
		t.Fatal(err)
	}
	return path
}

func buildBMP32(width, height uint32, rgbTopDown []rgb) []byte {
	if len(rgbTopDown) != int(width)*int(height) {
		panic("rgb length does not match dimensions")
	}
	rowStride := int(width) * 4
	pixelBytes := rowStride * int(height)
	fileSize := 54 + pixelBytes
	bmp := buildBMP32Header(width, height)
	for outputY := int(height) - 1; outputY >= 0; outputY-- {
		row := rgbTopDown[outputY*int(width) : (outputY+1)*int(width)]
		for _, pixel := range row {
			bmp = append(bmp, pixel.blue, pixel.green, pixel.red, 255)
		}
	}
	if len(bmp) != fileSize {
		panic("BMP size mismatch")
	}
	return bmp
}

func buildBMP32TopDown(width, height uint32, rgbTopDown []rgb) []byte {
	if len(rgbTopDown) != int(width)*int(height) {
		panic("rgb length does not match dimensions")
	}
	bmp := buildBMP32Header(width, height)
	binary.LittleEndian.PutUint32(bmp[22:26], uint32(-int32(height)))
	for _, pixel := range rgbTopDown {
		bmp = append(bmp, pixel.blue, pixel.green, pixel.red, 255)
	}
	return bmp
}

func buildBMP32Header(width, height uint32) []byte {
	rowStride := int(width) * 4
	pixelBytes := rowStride * int(height)
	fileSize := 54 + pixelBytes
	bmp := make([]byte, 0, 54)
	bmp = append(bmp, 'B', 'M')
	bmp = binary.LittleEndian.AppendUint32(bmp, uint32(fileSize))
	bmp = append(bmp, 0, 0, 0, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, 54)
	bmp = binary.LittleEndian.AppendUint32(bmp, 40)
	bmp = binary.LittleEndian.AppendUint32(bmp, uint32(int32(width)))
	bmp = binary.LittleEndian.AppendUint32(bmp, uint32(int32(height)))
	bmp = binary.LittleEndian.AppendUint16(bmp, 1)
	bmp = binary.LittleEndian.AppendUint16(bmp, 32)
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, uint32(pixelBytes))
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	return bmp
}

func buildBMP8Palette(width, height uint32, palette []rgb, indexesTopDown []byte) []byte {
	if len(indexesTopDown) != int(width)*int(height) {
		panic("index length does not match dimensions")
	}
	rowStride := (int(width) + 3) / 4 * 4
	paletteBytes := len(palette) * 4
	pixelOffset := 54 + paletteBytes
	pixelBytes := rowStride * int(height)
	fileSize := pixelOffset + pixelBytes
	bmp := make([]byte, 0, fileSize)
	bmp = append(bmp, 'B', 'M')
	bmp = binary.LittleEndian.AppendUint32(bmp, uint32(fileSize))
	bmp = append(bmp, 0, 0, 0, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, uint32(pixelOffset))
	bmp = binary.LittleEndian.AppendUint32(bmp, 40)
	bmp = binary.LittleEndian.AppendUint32(bmp, uint32(int32(width)))
	bmp = binary.LittleEndian.AppendUint32(bmp, uint32(int32(height)))
	bmp = binary.LittleEndian.AppendUint16(bmp, 1)
	bmp = binary.LittleEndian.AppendUint16(bmp, 8)
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, uint32(pixelBytes))
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, uint32(len(palette)))
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	for _, color := range palette {
		bmp = append(bmp, color.blue, color.green, color.red, 0)
	}
	for outputY := int(height) - 1; outputY >= 0; outputY-- {
		row := indexesTopDown[outputY*int(width) : (outputY+1)*int(width)]
		bmp = append(bmp, row...)
		for range rowStride - int(width) {
			bmp = append(bmp, 0)
		}
	}
	return bmp
}

func fullRangeMapped(values []byte) []byte {
	minValue := values[0]
	maxValue := values[0]
	for _, value := range values[1:] {
		if value < minValue {
			minValue = value
		}
		if value > maxValue {
			maxValue = value
		}
	}
	mapped := make([]byte, len(values))
	for index, value := range values {
		mapped[index] = mapLinear(float32(value), float32(minValue), float32(maxValue))
	}
	return mapped
}
