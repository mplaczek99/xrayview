package render

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"xrayview/go-backend/internal/imaging"
	"xrayview/go-backend/internal/rustdecode"
)

func TestSavePreviewPNGEncodesGrayPreview(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "preview.png")
	preview := imaging.GrayPreview(2, 2, []uint8{
		0, 64,
		128, 255,
	})

	if err := SavePreviewPNG(outputPath, preview); err != nil {
		t.Fatalf("SavePreviewPNG returned error: %v", err)
	}

	decoded := decodeGrayPNG(t, outputPath)
	if got, want := decoded.Bounds().Dx(), 2; got != want {
		t.Fatalf("decoded width = %d, want %d", got, want)
	}
	if got, want := decoded.Bounds().Dy(), 2; got != want {
		t.Fatalf("decoded height = %d, want %d", got, want)
	}

	wantPixels := []uint8{0, 64, 128, 255}
	gotPixels := grayPixels(decoded)
	if !equalBytes(gotPixels, wantPixels) {
		t.Fatalf("decoded pixels = %v, want %v", gotPixels, wantPixels)
	}
}

func TestRenderSourceImageMatchesRustPreviewFixture(t *testing.T) {
	if rustdecode.DefaultDevCommand()[0] == "cargo" {
		if _, err := exec.LookPath("cargo"); err != nil {
			t.Skip("cargo is not available and no prebuilt decode helper binary was found")
		}
	}

	helper, err := rustdecode.New(rustdecode.DefaultDevCommand())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	study, err := helper.DecodeStudy(ctx, sampleDicomPath(t))
	if err != nil {
		t.Fatalf("DecodeStudy returned error: %v", err)
	}

	preview := RenderSourceImage(study.Image, DefaultRenderPlan())
	if err := preview.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	fixture := decodeGrayPNG(t, samplePreviewFixturePath(t))
	if got, want := preview.Width, uint32(fixture.Bounds().Dx()); got != want {
		t.Fatalf("preview width = %d, want %d", got, want)
	}
	if got, want := preview.Height, uint32(fixture.Bounds().Dy()); got != want {
		t.Fatalf("preview height = %d, want %d", got, want)
	}

	if got, want := preview.Pixels, grayPixels(fixture); !equalBytes(got, want) {
		t.Fatalf("rendered preview does not match the Rust fixture")
	}
}

func sampleDicomPath(t *testing.T) string {
	t.Helper()

	return repoPathFromHere(t, "images", "sample-dental-radiograph.dcm")
}

func samplePreviewFixturePath(t *testing.T) string {
	t.Helper()

	return repoPathFromHere(t, "backend", "tests", "fixtures", "parity", "sample-dental-radiograph", "render-preview.png")
}

func repoPathFromHere(t *testing.T, pathParts ...string) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller returned no file path")
	}

	parts := []string{filepath.Dir(currentFile), "..", "..", ".."}
	parts = append(parts, pathParts...)
	return filepath.Clean(filepath.Join(parts...))
}

func decodeGrayPNG(t *testing.T, path string) *image.Gray {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer file.Close()

	decoded, err := png.Decode(file)
	if err != nil {
		t.Fatalf("png.Decode returned error: %v", err)
	}

	if gray, ok := decoded.(*image.Gray); ok {
		return gray
	}

	bounds := decoded.Bounds()
	gray := image.NewGray(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gray.Set(x, y, color.GrayModel.Convert(decoded.At(x, y)))
		}
	}

	return gray
}

func grayPixels(imageValue *image.Gray) []uint8 {
	bounds := imageValue.Bounds()
	pixels := make([]uint8, 0, bounds.Dx()*bounds.Dy())

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		rowStart := imageValue.PixOffset(bounds.Min.X, y)
		rowEnd := rowStart + bounds.Dx()
		pixels = append(pixels, imageValue.Pix[rowStart:rowEnd]...)
	}

	return pixels
}
