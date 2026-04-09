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
	if got, want := len(study.Metadata.PreservedElements), 0; got != want {
		t.Fatalf("len(Metadata.PreservedElements) = %d, want %d", got, want)
	}
	if study.MeasurementScale != nil {
		t.Fatalf("MeasurementScale = %+v, want nil", study.MeasurementScale)
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
