package imageio

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/suyashkumar/dicom"
	"github.com/suyashkumar/dicom/pkg/tag"
)

func TestSavePreviewPNG(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preview.png")
	img := image.NewGray(image.Rect(0, 0, 4, 5))

	if err := SavePreviewPNG(path, img); err != nil {
		t.Fatalf("save preview png: %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open preview png: %v", err)
	}
	defer file.Close()

	decoded, err := png.Decode(file)
	if err != nil {
		t.Fatalf("decode preview png: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 4 || bounds.Dy() != 5 {
		t.Fatalf("bounds = %dx%d, want 4x5", bounds.Dx(), bounds.Dy())
	}
}

func TestSaveDICOM(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.dcm")
	if err := writeTestDICOM(sourcePath, []uint16{0, 1024, 2048, 4095}, 2, 2, "MONOCHROME2"); err != nil {
		t.Fatalf("write source dicom: %v", err)
	}

	source, err := Load(sourcePath)
	if err != nil {
		t.Fatalf("load source dicom: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "processed.dcm")
	img := image.NewGray(image.Rect(0, 0, 2, 2))
	img.Pix = []uint8{5, 50, 100, 200}

	if err := SaveDICOM(outputPath, img, source); err != nil {
		t.Fatalf("save dicom: %v", err)
	}

	parsed, err := dicom.ParseFile(outputPath, nil)
	if err != nil {
		t.Fatalf("parse saved dicom: %v", err)
	}

	patientID, err := parsed.FindElementByTag(tag.PatientID)
	if err != nil {
		t.Fatalf("find PatientID: %v", err)
	}
	if got := dicom.MustGetStrings(patientID.Value)[0]; got != "PID-123" {
		t.Fatalf("PatientID = %q, want %q", got, "PID-123")
	}

	photometric, err := parsed.FindElementByTag(tag.PhotometricInterpretation)
	if err != nil {
		t.Fatalf("find PhotometricInterpretation: %v", err)
	}
	if got := dicom.MustGetStrings(photometric.Value)[0]; got != "MONOCHROME2" {
		t.Fatalf("PhotometricInterpretation = %q, want %q", got, "MONOCHROME2")
	}
}
