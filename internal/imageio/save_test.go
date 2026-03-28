package imageio

import (
	"image"
	"path/filepath"
	"testing"
)

func TestSavePNG(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output.png")
	img := image.NewGray(image.Rect(0, 0, 4, 5))

	if err := SavePNG(path, img); err != nil {
		t.Fatalf("save png: %v", err)
	}

	loaded, format, err := Load(path)
	if err != nil {
		t.Fatalf("load saved png: %v", err)
	}

	if format != "png" {
		t.Fatalf("format = %q, want %q", format, "png")
	}

	bounds := loaded.Bounds()
	if bounds.Dx() != 4 || bounds.Dy() != 5 {
		t.Fatalf("bounds = %dx%d, want 4x5", bounds.Dx(), bounds.Dy())
	}
}
