package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/mplaczek99/xrayview/internal/filters"
)

const updateGoldenEnv = "XRAYVIEW_UPDATE_GOLDEN"

func TestGoldenOutputs(t *testing.T) {
	testCases := []struct {
		name string
		cfg  config
	}{
		{
			name: "default",
			cfg:  config{},
		},
		{
			name: "pipeline",
			cfg: config{
				invert:     true,
				brightness: 18,
				contrast:   1.35,
				pipeline:   "contrast,invert,brightness",
			},
		},
		{
			name: "equalize",
			cfg: config{
				equalize: true,
			},
		},
		{
			name: "hot",
			cfg: config{
				palette: "hot",
			},
		},
		{
			name: "bone",
			cfg: config{
				palette: "bone",
			},
		},
		{
			name: "xray_compare",
			cfg: config{
				brightness: 10,
				contrast:   1.4,
				equalize:   true,
				palette:    "bone",
				compare:    true,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderGoldenOutput(t, tc.cfg)
			assertGoldenImage(t, filepath.Join("testdata", "golden", tc.name+".png"), got)
		})
	}
}

func renderGoldenOutput(t *testing.T, cfg config) image.Image {
	t.Helper()

	src := goldenSourceImage()
	originalGray := filters.Grayscale(src)
	output, _ := processGrayImage(originalGray, cfg)
	if cfg.compare {
		output = combineComparison(originalGray, output)
	}

	return output
}

func goldenSourceImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			r := uint8((x*37 + y*29 + 11) % 256)
			g := uint8((x*19 + y*61 + 47) % 256)
			b := uint8((x*71 + y*23 + 89) % 256)
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}

	img.SetRGBA(0, 0, color.RGBA{A: 255})
	img.SetRGBA(7, 0, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	img.SetRGBA(0, 5, color.RGBA{R: 255, A: 255})
	img.SetRGBA(7, 5, color.RGBA{B: 255, A: 255})

	return img
}

func assertGoldenImage(t *testing.T, goldenPath string, got image.Image) {
	t.Helper()

	if os.Getenv(updateGoldenEnv) == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("create golden directory: %v", err)
		}

		file, err := os.Create(goldenPath)
		if err != nil {
			t.Fatalf("create golden image: %v", err)
		}
		defer file.Close()

		if err := png.Encode(file, got); err != nil {
			t.Fatalf("encode golden image: %v", err)
		}
		return
	}

	file, err := os.Open(goldenPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("missing golden image %s; regenerate with %s=1 go test ./cmd/xrayview", goldenPath, updateGoldenEnv)
		}
		t.Fatalf("open golden image: %v", err)
	}
	defer file.Close()

	want, err := png.Decode(file)
	if err != nil {
		t.Fatalf("decode golden image: %v", err)
	}

	assertImagesEqual(t, want, got)
}

func assertImagesEqual(t *testing.T, want, got image.Image) {
	t.Helper()

	if !want.Bounds().Eq(got.Bounds()) {
		t.Fatalf("bounds = %v, want %v", got.Bounds(), want.Bounds())
	}

	bounds := want.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			wantColor := color.RGBA64Model.Convert(want.At(x, y)).(color.RGBA64)
			gotColor := color.RGBA64Model.Convert(got.At(x, y)).(color.RGBA64)
			if gotColor != wantColor {
				t.Fatalf("pixel (%d,%d) = %s, want %s", x, y, formatRGBA64(gotColor), formatRGBA64(wantColor))
			}
		}
	}
}

func formatRGBA64(c color.RGBA64) string {
	return fmt.Sprintf("rgba64(%d,%d,%d,%d)", c.R, c.G, c.B, c.A)
}
