package dicommeta

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"

	"xrayview/backend/internal/imaging"
)

func TestReadFileSupportsStandaloneBMPInput(t *testing.T) {
	inputPath := filepath.Join(t.TempDir(), "standalone.bmp")
	writeBMPFixture(t, inputPath)

	metadata, err := ReadFile(inputPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if got, want := metadata.Rows, uint16(1); got != want {
		t.Fatalf("Rows = %d, want %d", got, want)
	}
	if got, want := metadata.Columns, uint16(2); got != want {
		t.Fatalf("Columns = %d, want %d", got, want)
	}
	if metadata.SamplesPerPixel == 0 {
		t.Fatal("SamplesPerPixel = 0, want non-zero")
	}
	if got, want := metadata.BitsAllocated, uint16(8); got != want {
		t.Fatalf("BitsAllocated = %d, want %d", got, want)
	}
	if got, want := metadata.BitsStored, uint16(8); got != want {
		t.Fatalf("BitsStored = %d, want %d", got, want)
	}
	if got, want := metadata.PixelDataEncoding, PixelDataEncodingNative; got != want {
		t.Fatalf("PixelDataEncoding = %q, want %q", got, want)
	}
	if metadata.PhotometricInterpretation == "" {
		t.Fatal("PhotometricInterpretation = empty, want populated value")
	}
	if metadata.MeasurementScale() != nil {
		t.Fatalf("MeasurementScale = %+v, want nil", metadata.MeasurementScale())
	}
}

func TestDecodeFileSupportsStandaloneTIFFInput(t *testing.T) {
	inputPath := filepath.Join(t.TempDir(), "standalone.tif")
	writeTIFFFixture(t, inputPath)

	study, err := DecodeFile(inputPath)
	if err != nil {
		t.Fatalf("DecodeFile returned error: %v", err)
	}

	if got, want := study.Image.Width, uint32(2); got != want {
		t.Fatalf("Image.Width = %d, want %d", got, want)
	}
	if got, want := study.Image.Height, uint32(1); got != want {
		t.Fatalf("Image.Height = %d, want %d", got, want)
	}
	if got, want := study.Image.Format, imaging.FormatGrayFloat32; got != want {
		t.Fatalf("Image.Format = %q, want %q", got, want)
	}
	if got, want := len(study.Image.Pixels), 2; got != want {
		t.Fatalf("len(Image.Pixels) = %d, want %d", got, want)
	}
	if got, want := study.Image.Pixels[0], float32(0); got != want {
		t.Fatalf("Image.Pixels[0] = %v, want %v", got, want)
	}
	if got, want := study.Image.Pixels[1], float32(0xffff); got != want {
		t.Fatalf("Image.Pixels[1] = %v, want %v", got, want)
	}
	if got, want := study.Image.MinValue, float32(0); got != want {
		t.Fatalf("Image.MinValue = %v, want %v", got, want)
	}
	if got, want := study.Image.MaxValue, float32(0xffff); got != want {
		t.Fatalf("Image.MaxValue = %v, want %v", got, want)
	}
	if !study.Image.FitsUint16 {
		t.Fatal("Image.FitsUint16 = false, want true for standalone 16-bit image range")
	}
	if got, want := len(study.Metadata.PreservedElements), 0; got != want {
		t.Fatalf("len(Metadata.PreservedElements) = %d, want %d", got, want)
	}
	if study.MeasurementScale != nil {
		t.Fatalf("MeasurementScale = %+v, want nil", study.MeasurementScale)
	}
}

func TestSourceImageFromImageCommonFormats(t *testing.T) {
	defaultWindow := &imaging.WindowLevel{Center: 128, Width: 256}
	testCases := []struct {
		name       string
		image      image.Image
		window     *imaging.WindowLevel
		invert     bool
		wantPixels []float32
		wantMin    float32
		wantMax    float32
	}{
		{
			name:       "gray16",
			image:      testGray16SubImage(),
			window:     defaultWindow,
			invert:     true,
			wantPixels: []float32{0x1234, 0xabcd, 0x00ff, 0xffff},
			wantMin:    0x00ff,
			wantMax:    0xffff,
		},
		{
			name:       "rgba",
			image:      testRGBASubImage(),
			wantPixels: colorImagePixelsFromAt(testRGBASubImage()),
			wantMin:    minFloat32(colorImagePixelsFromAt(testRGBASubImage())),
			wantMax:    maxFloat32(colorImagePixelsFromAt(testRGBASubImage())),
		},
		{
			name:       "nrgba",
			image:      testNRGBASubImage(),
			wantPixels: colorImagePixelsFromAt(testNRGBASubImage()),
			wantMin:    minFloat32(colorImagePixelsFromAt(testNRGBASubImage())),
			wantMax:    maxFloat32(colorImagePixelsFromAt(testNRGBASubImage())),
		},
		{
			name:       "ycbcr",
			image:      testYCbCrSubImage(),
			wantPixels: colorImagePixelsFromAt(testYCbCrSubImage()),
			wantMin:    minFloat32(colorImagePixelsFromAt(testYCbCrSubImage())),
			wantMax:    maxFloat32(colorImagePixelsFromAt(testYCbCrSubImage())),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sourceImageFromImage(tc.image, tc.window, tc.invert)
			if err != nil {
				t.Fatalf("sourceImageFromImage returned error: %v", err)
			}
			if got.Width != 2 || got.Height != 2 {
				t.Fatalf("size = %dx%d, want 2x2", got.Width, got.Height)
			}
			if got.Format != imaging.FormatGrayFloat32 {
				t.Fatalf("Format = %q, want %q", got.Format, imaging.FormatGrayFloat32)
			}
			if !float32SlicesEqual(got.Pixels, tc.wantPixels) {
				t.Fatalf("Pixels = %v, want %v", got.Pixels, tc.wantPixels)
			}
			if got.MinValue != tc.wantMin || got.MaxValue != tc.wantMax {
				t.Fatalf("range = [%v, %v], want [%v, %v]", got.MinValue, got.MaxValue, tc.wantMin, tc.wantMax)
			}
			if got.DefaultWindow != tc.window {
				t.Fatalf("DefaultWindow = %+v, want %+v", got.DefaultWindow, tc.window)
			}
			if got.Invert != tc.invert {
				t.Fatalf("Invert = %v, want %v", got.Invert, tc.invert)
			}
		})
	}
}

func TestReadFileRejectsStandalonePNGAndJPEGInput(t *testing.T) {
	testCases := []struct {
		name   string
		path   string
		encode func(t *testing.T, path string)
	}{
		{
			name: "png",
			path: filepath.Join(t.TempDir(), "standalone.png"),
			encode: func(t *testing.T, path string) {
				t.Helper()
				img := image.NewGray(image.Rect(0, 0, 2, 1))
				img.SetGray(0, 0, color.Gray{Y: 0})
				img.SetGray(1, 0, color.Gray{Y: 255})
				var payload bytes.Buffer
				if err := png.Encode(&payload, img); err != nil {
					t.Fatalf("png.Encode returned error: %v", err)
				}
				if err := os.WriteFile(path, payload.Bytes(), 0o644); err != nil {
					t.Fatalf("WriteFile returned error: %v", err)
				}
			},
		},
		{
			name: "jpeg",
			path: filepath.Join(t.TempDir(), "standalone.jpg"),
			encode: func(t *testing.T, path string) {
				t.Helper()
				img := image.NewGray(image.Rect(0, 0, 2, 1))
				img.SetGray(0, 0, color.Gray{Y: 0})
				img.SetGray(1, 0, color.Gray{Y: 255})
				var payload bytes.Buffer
				if err := jpeg.Encode(&payload, img, nil); err != nil {
					t.Fatalf("jpeg.Encode returned error: %v", err)
				}
				if err := os.WriteFile(path, payload.Bytes(), 0o644); err != nil {
					t.Fatalf("WriteFile returned error: %v", err)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.encode(t, tc.path)
			if _, err := ReadFile(tc.path); err == nil {
				t.Fatalf("ReadFile(%q) returned nil error, want rejection", tc.path)
			}
		})
	}
}

func BenchmarkSourceImageFromImage(b *testing.B) {
	const width, height = 2048, 1536
	benchmarks := []struct {
		name  string
		image image.Image
		bytes int64
	}{
		{name: "Gray16", image: benchmarkGray16Image(width, height), bytes: width * height * 2},
		{name: "RGBA", image: benchmarkRGBAImage(width, height), bytes: width * height * 4},
		{name: "NRGBA", image: benchmarkNRGBAImage(width, height), bytes: width * height * 4},
		{name: "YCbCr", image: benchmarkYCbCrImage(width, height), bytes: width * height * 3},
	}

	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			b.SetBytes(benchmark.bytes)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sourceImage, err := sourceImageFromImage(benchmark.image, nil, false)
				if err != nil {
					b.Fatal(err)
				}
				if len(sourceImage.Pixels) != width*height {
					b.Fatalf("len(Pixels) = %d, want %d", len(sourceImage.Pixels), width*height)
				}
			}
		})
	}
}

func writeBMPFixture(t *testing.T, path string) {
	t.Helper()

	img := image.NewGray(image.Rect(0, 0, 2, 1))
	img.SetGray(0, 0, color.Gray{Y: 0})
	img.SetGray(1, 0, color.Gray{Y: 255})

	var payload bytes.Buffer
	if err := bmp.Encode(&payload, img); err != nil {
		t.Fatalf("bmp.Encode returned error: %v", err)
	}
	if err := os.WriteFile(path, payload.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func testGray16SubImage() image.Image {
	img := image.NewGray16(image.Rect(0, 0, 4, 4))
	values := []uint16{0x1234, 0xabcd, 0x00ff, 0xffff}
	index := 0
	for y := 1; y < 3; y++ {
		for x := 1; x < 3; x++ {
			img.SetGray16(x, y, color.Gray16{Y: values[index]})
			index++
		}
	}
	return img.SubImage(image.Rect(1, 1, 3, 3))
}

func testRGBASubImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	colors := []color.RGBA{
		{R: 10, G: 20, B: 30, A: 255},
		{R: 240, G: 20, B: 10, A: 255},
		{R: 20, G: 230, B: 40, A: 255},
		{R: 30, G: 40, B: 220, A: 255},
	}
	index := 0
	for y := 1; y < 3; y++ {
		for x := 1; x < 3; x++ {
			img.SetRGBA(x, y, colors[index])
			index++
		}
	}
	return img.SubImage(image.Rect(1, 1, 3, 3))
}

func testNRGBASubImage() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	colors := []color.NRGBA{
		{R: 12, G: 34, B: 56, A: 128},
		{R: 210, G: 45, B: 60, A: 255},
		{R: 50, G: 200, B: 80, A: 255},
		{R: 70, G: 90, B: 210, A: 255},
	}
	index := 0
	for y := 1; y < 3; y++ {
		for x := 1; x < 3; x++ {
			img.SetNRGBA(x, y, colors[index])
			index++
		}
	}
	return img.SubImage(image.Rect(1, 1, 3, 3))
}

func testYCbCrSubImage() image.Image {
	img := image.NewYCbCr(image.Rect(0, 0, 4, 4), image.YCbCrSubsampleRatio444)
	values := []color.YCbCr{
		{Y: 0x7f, Cb: 0x7f, Cr: 0x7f},
		{Y: 96, Cb: 80, Cr: 170},
		{Y: 160, Cb: 190, Cr: 70},
		{Y: 220, Cb: 128, Cr: 128},
	}
	index := 0
	for y := 1; y < 3; y++ {
		for x := 1; x < 3; x++ {
			yOffset := img.YOffset(x, y)
			cOffset := img.COffset(x, y)
			img.Y[yOffset] = values[index].Y
			img.Cb[cOffset] = values[index].Cb
			img.Cr[cOffset] = values[index].Cr
			index++
		}
	}
	return img.SubImage(image.Rect(1, 1, 3, 3))
}

func colorImagePixelsFromAt(img image.Image) []float32 {
	bounds := img.Bounds()
	pixels := make([]float32, 0, bounds.Dx()*bounds.Dy())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			red, green, blue, _ := img.At(x, y).RGBA()
			pixels = append(pixels, float32(grayFromRGB8(uint8(red>>8), uint8(green>>8), uint8(blue>>8))))
		}
	}
	return pixels
}

func minFloat32(values []float32) float32 {
	minVal := values[0]
	for _, value := range values[1:] {
		if value < minVal {
			minVal = value
		}
	}
	return minVal
}

func maxFloat32(values []float32) float32 {
	maxVal := values[0]
	for _, value := range values[1:] {
		if value > maxVal {
			maxVal = value
		}
	}
	return maxVal
}

func float32SlicesEqual(left []float32, right []float32) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func benchmarkGray16Image(width int, height int) *image.Gray16 {
	img := image.NewGray16(image.Rect(0, 0, width, height))
	for i := 0; i+1 < len(img.Pix); i += 2 {
		value := uint16(i*13 + i/7)
		img.Pix[i] = byte(value >> 8)
		img.Pix[i+1] = byte(value)
	}
	return img
}

func benchmarkRGBAImage(width int, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i+3 < len(img.Pix); i += 4 {
		img.Pix[i] = byte(i * 3)
		img.Pix[i+1] = byte(i*5 + 17)
		img.Pix[i+2] = byte(i*7 + 29)
		img.Pix[i+3] = 0xff
	}
	return img
}

func benchmarkNRGBAImage(width int, height int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i := 0; i+3 < len(img.Pix); i += 4 {
		img.Pix[i] = byte(i * 11)
		img.Pix[i+1] = byte(i*13 + 19)
		img.Pix[i+2] = byte(i*17 + 31)
		img.Pix[i+3] = 0xff
	}
	return img
}

func benchmarkYCbCrImage(width int, height int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio444)
	for i := range img.Y {
		img.Y[i] = byte(i*3 + 11)
	}
	for i := range img.Cb {
		img.Cb[i] = byte(i*5 + 127)
		img.Cr[i] = byte(i*7 + 83)
	}
	return img
}

func writeTIFFFixture(t *testing.T, path string) {
	t.Helper()

	img := image.NewGray16(image.Rect(0, 0, 2, 1))
	img.SetGray16(0, 0, color.Gray16{Y: 0})
	img.SetGray16(1, 0, color.Gray16{Y: 0xffff})

	var payload bytes.Buffer
	if err := tiff.Encode(&payload, img, nil); err != nil {
		t.Fatalf("tiff.Encode returned error: %v", err)
	}
	if err := os.WriteFile(path, payload.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
