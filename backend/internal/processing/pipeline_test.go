package processing

import (
	"testing"

	"xrayview/backend/internal/dicommeta"
	"xrayview/backend/internal/imaging"
	"xrayview/backend/internal/render"
)

func TestProcessRenderedPreviewCompareOutputIsRGBAAndDoubleWidth(t *testing.T) {
	preview := imaging.GrayPreview(2, 1, []uint8{0, 255})

	output, err := ProcessRenderedPreview(
		preview,
		GrayscaleControls{Contrast: 1.0},
		PaletteBone,
		true,
	)
	if err != nil {
		t.Fatalf("ProcessRenderedPreview returned error: %v", err)
	}

	if got, want := output.Mode, "comparison of grayscale and grayscale with bone palette"; got != want {
		t.Fatalf("Mode = %q, want %q", got, want)
	}
	if output.Preview.Format != imaging.FormatRGBA8 {
		t.Fatalf("Format = %q, want %q", output.Preview.Format, imaging.FormatRGBA8)
	}
	if output.Preview.Width != 4 || output.Preview.Height != 1 {
		t.Fatalf("size = %dx%d, want 4x1", output.Preview.Width, output.Preview.Height)
	}
}

func TestProcessSourceImageMatchesRustPaletteFixture(t *testing.T) {
	study, err := dicommeta.DecodeFile(sampleDicomPath(t))
	if err != nil {
		t.Fatalf("DecodeFile returned error: %v", err)
	}

	output, err := ProcessSourceImage(
		study.Image,
		render.DefaultRenderPlan(),
		GrayscaleControls{
			Brightness: 10,
			Contrast:   1.4,
			Equalize:   true,
		},
		PaletteBone,
		false,
	)
	if err != nil {
		t.Fatalf("ProcessSourceImage returned error: %v", err)
	}

	if got, want := output.Mode, "grayscale with brightness +10 with contrast 1.4 with histogram equalization with bone palette"; got != want {
		t.Fatalf("Mode = %q, want %q", got, want)
	}

	fixture := decodeRGBAPNG(t, sampleProcessedPaletteFixturePath(t))
	if got, want := output.Preview.Width, uint32(fixture.Bounds().Dx()); got != want {
		t.Fatalf("preview width = %d, want %d", got, want)
	}
	if got, want := output.Preview.Height, uint32(fixture.Bounds().Dy()); got != want {
		t.Fatalf("preview height = %d, want %d", got, want)
	}
	if got, want := output.Preview.Format, imaging.FormatRGBA8; got != want {
		t.Fatalf("preview format = %q, want %q", got, want)
	}
	if got, want := output.Preview.Pixels, rgbaPixels(fixture); !equalBytes(got, want) {
		t.Fatalf("processed preview does not match the Rust palette fixture")
	}
}

func TestProcessSourceImageMatchesRustCompareFixture(t *testing.T) {
	study, err := dicommeta.DecodeFile(sampleDicomPath(t))
	if err != nil {
		t.Fatalf("DecodeFile returned error: %v", err)
	}

	output, err := ProcessSourceImage(
		study.Image,
		render.DefaultRenderPlan(),
		GrayscaleControls{
			Brightness: 10,
			Contrast:   1.4,
			Equalize:   true,
		},
		PaletteBone,
		true,
	)
	if err != nil {
		t.Fatalf("ProcessSourceImage returned error: %v", err)
	}

	if got, want := output.Mode, "comparison of grayscale and grayscale with brightness +10 with contrast 1.4 with histogram equalization with bone palette"; got != want {
		t.Fatalf("Mode = %q, want %q", got, want)
	}

	fixture := decodeRGBAPNG(t, sampleCompareFixturePath(t))
	if got, want := output.Preview.Width, uint32(fixture.Bounds().Dx()); got != want {
		t.Fatalf("preview width = %d, want %d", got, want)
	}
	if got, want := output.Preview.Height, uint32(fixture.Bounds().Dy()); got != want {
		t.Fatalf("preview height = %d, want %d", got, want)
	}
	if got, want := output.Preview.Format, imaging.FormatRGBA8; got != want {
		t.Fatalf("preview format = %q, want %q", got, want)
	}
	if got, want := output.Preview.Pixels, rgbaPixels(fixture); !equalBytes(got, want) {
		t.Fatalf("processed compare preview does not match the Rust compare fixture")
	}
}
