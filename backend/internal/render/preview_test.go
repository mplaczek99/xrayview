package render

import (
	"bytes"
	"image/jpeg"
	"image/png"
	"testing"

	"xrayview/backend/internal/imaging"
)

func TestSavePreviewGray8UsesJPEG(t *testing.T) {
	preview := imaging.GrayPreview(4, 4, make([]uint8, 16))
	var buf bytes.Buffer
	if err := EncodePreviewJPEG(&buf, preview); err != nil {
		t.Fatalf("EncodePreviewJPEG error: %v", err)
	}

	_, err := jpeg.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("output is not valid JPEG: %v", err)
	}
}

func TestSavePreviewRGBA8UsesPNG(t *testing.T) {
	preview := imaging.RGBAPreview(4, 4, make([]uint8, 64))
	var buf bytes.Buffer
	if err := EncodePreviewPNG(&buf, preview); err != nil {
		t.Fatalf("EncodePreviewPNG error: %v", err)
	}

	_, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("output is not valid PNG: %v", err)
	}
}

func TestPreviewExtension(t *testing.T) {
	if got := PreviewExtension(imaging.FormatGray8); got != "jpeg" {
		t.Fatalf("Gray8 extension = %q, want jpeg", got)
	}
	if got := PreviewExtension(imaging.FormatRGBA8); got != "png" {
		t.Fatalf("RGBA8 extension = %q, want png", got)
	}
}

func TestJPEGPreviewQualityPreservesContent(t *testing.T) {
	const width, height = 64, 64
	pixels := make([]uint8, width*height)
	for i := range pixels {
		pixels[i] = uint8(i % 256)
	}
	preview := imaging.GrayPreview(width, height, pixels)

	var buf bytes.Buffer
	if err := EncodePreviewJPEG(&buf, preview); err != nil {
		t.Fatalf("encode error: %v", err)
	}

	decoded, err := jpeg.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != width || bounds.Dy() != height {
		t.Fatalf("decoded size = %dx%d, want %dx%d", bounds.Dx(), bounds.Dy(), width, height)
	}

	// Quality 92 should keep max pixel error small for grayscale.
	maxDiff := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, _, _, _ := decoded.At(x, y).RGBA()
			got := int(r >> 8)
			want := int(pixels[y*width+x])
			diff := got - want
			if diff < 0 {
				diff = -diff
			}
			if diff > maxDiff {
				maxDiff = diff
			}
		}
	}

	if maxDiff > 5 {
		t.Fatalf("max pixel difference = %d, want <= 5 for quality %d", maxDiff, jpegPreviewQuality)
	}
}
