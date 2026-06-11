package render

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestEncodeGrayBMPWritesBMPSignature(t *testing.T) {
	pixels := []byte{0, 128, 192, 255}
	bmp, err := EncodeGrayBMP(2, 2, pixels)
	if err != nil {
		t.Fatal(err)
	}
	if string(bmp[:2]) != "BM" {
		t.Fatalf("BMP signature = %q, want BM", bmp[:2])
	}
}

func TestEncodeGrayBMPRejectsWrongPixelCount(t *testing.T) {
	_, err := EncodeGrayBMP(2, 2, []byte{0, 1, 2})
	if err == nil || !strings.Contains(err.Error(), "preview pixel length") {
		t.Fatalf("error = %v, want preview pixel length", err)
	}
}

func TestEncodeGrayBMPRejectsZeroDimensions(t *testing.T) {
	_, err := EncodeGrayBMP(0, 2, nil)
	if err == nil || !strings.Contains(err.Error(), "non-zero") {
		t.Fatalf("error = %v, want non-zero", err)
	}
}

func TestEncodeGrayBMPRejectsDimensionsOutsideSignedHeaderRange(t *testing.T) {
	_, err := EncodeGrayBMP(uint32(maxInt32)+1, 1, nil)
	if err == nil || !strings.Contains(err.Error(), "signed 32-bit header limits") {
		t.Fatalf("error = %v, want signed header range", err)
	}
}

func TestEncodeGrayBMPPadsRowsToFourByteBoundary(t *testing.T) {
	pixels := []byte{10, 20, 30, 40, 50, 60}
	bmp, err := EncodeGrayBMP(3, 2, pixels)
	if err != nil {
		t.Fatal(err)
	}

	pixelOffset := int(binary.LittleEndian.Uint32(bmp[10:14]))
	lastRowStart := pixelOffset
	if got := bmp[lastRowStart : lastRowStart+3]; string(got) != string([]byte{40, 50, 60}) {
		t.Fatalf("last row = %v, want [40 50 60]", got)
	}
	if bmp[lastRowStart+3] != 0 {
		t.Fatalf("padding byte = %d, want 0", bmp[lastRowStart+3])
	}

	firstRowStart := pixelOffset + 4
	if got := bmp[firstRowStart : firstRowStart+3]; string(got) != string([]byte{10, 20, 30}) {
		t.Fatalf("first row = %v, want [10 20 30]", got)
	}
}

func TestEncodeRGBAPreviewWrites24BitBMP(t *testing.T) {
	pixels := []byte{
		255, 0, 0, 255, 0, 255, 0, 255,
		0, 0, 255, 255, 255, 255, 255, 255,
	}
	bmp, err := EncodePreviewBMP(RGBA(2, 2, pixels))
	if err != nil {
		t.Fatal(err)
	}
	if string(bmp[:2]) != "BM" {
		t.Fatalf("BMP signature = %q, want BM", bmp[:2])
	}
	if bits := binary.LittleEndian.Uint16(bmp[28:30]); bits != 24 {
		t.Fatalf("bits per pixel = %d, want 24", bits)
	}
	pixelOffset := int(binary.LittleEndian.Uint32(bmp[10:14]))
	if pixelOffset != 14+40 {
		t.Fatalf("pixel offset = %d, want %d", pixelOffset, 14+40)
	}

	lastRow := bmp[pixelOffset : pixelOffset+8]
	if got := lastRow[0:3]; string(got) != string([]byte{255, 0, 0}) {
		t.Fatalf("blue pixel = %v, want BGR blue [255 0 0]", got)
	}
	if got := lastRow[3:6]; string(got) != string([]byte{255, 255, 255}) {
		t.Fatalf("white pixel = %v, want [255 255 255]", got)
	}
}
