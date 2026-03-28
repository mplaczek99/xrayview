package imageio

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPNG(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "sample.png")

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create temp image: %v", err)
	}

	img := image.NewGray(image.Rect(0, 0, 2, 3))
	if err := png.Encode(file, img); err != nil {
		file.Close()
		t.Fatalf("encode png: %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("close temp image: %v", err)
	}

	loaded, format, err := Load(path)
	if err != nil {
		t.Fatalf("load image: %v", err)
	}

	if format != "png" {
		t.Fatalf("format = %q, want %q", format, "png")
	}

	bounds := loaded.Bounds()
	if bounds.Dx() != 2 || bounds.Dy() != 3 {
		t.Fatalf("bounds = %dx%d, want 2x3", bounds.Dx(), bounds.Dy())
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, _, err := Load("does-not-exist.png")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
