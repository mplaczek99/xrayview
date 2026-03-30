package imageio

import (
	"image"
	"image/color"
	"testing"
)

func TestConvertToGrayMatchesGenericConversion(t *testing.T) {
	tests := []struct {
		name string
		img  image.Image
	}{
		{
			name: "gray",
			img: func() image.Image {
				img := image.NewGray(image.Rect(0, 0, 2, 2))
				img.Pix = []uint8{0, 64, 128, 255}
				return img
			}(),
		},
		{
			name: "gray16",
			img: func() image.Image {
				img := image.NewGray16(image.Rect(0, 0, 2, 1))
				img.SetGray16(0, 0, color.Gray16{Y: 0x1234})
				img.SetGray16(1, 0, color.Gray16{Y: 0xFEDC})
				return img
			}(),
		},
		{
			name: "rgba",
			img: func() image.Image {
				img := image.NewRGBA(image.Rect(0, 0, 2, 1))
				img.SetRGBA(0, 0, color.RGBA{R: 10, G: 80, B: 200, A: 255})
				img.SetRGBA(1, 0, color.RGBA{R: 250, G: 120, B: 20, A: 255})
				return img
			}(),
		},
		{
			name: "nrgba",
			img: func() image.Image {
				img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
				img.SetNRGBA(0, 0, color.NRGBA{R: 180, G: 40, B: 90, A: 128})
				img.SetNRGBA(1, 0, color.NRGBA{R: 60, G: 220, B: 30, A: 200})
				return img
			}(),
		},
		{
			name: "ycbcr",
			img: func() image.Image {
				img := image.NewYCbCr(image.Rect(0, 0, 2, 1), image.YCbCrSubsampleRatio444)
				img.Y[0] = 90
				img.Cb[0] = 110
				img.Cr[0] = 170
				img.Y[1] = 200
				img.Cb[1] = 90
				img.Cr[1] = 40
				return img
			}(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := convertToGray(tc.img)
			want := genericGrayConversion(tc.img)

			if got.Bounds() != want.Bounds() {
				t.Fatalf("bounds = %v, want %v", got.Bounds(), want.Bounds())
			}
			if len(got.Pix) != len(want.Pix) {
				t.Fatalf("len(Pix) = %d, want %d", len(got.Pix), len(want.Pix))
			}
			for i := range want.Pix {
				if got.Pix[i] != want.Pix[i] {
					t.Fatalf("Pix[%d] = %d, want %d", i, got.Pix[i], want.Pix[i])
				}
			}
		})
	}
}

func TestGrayPixelsUsesVisibleBounds(t *testing.T) {
	base := image.NewGray(image.Rect(0, 0, 4, 1))
	base.Pix = []uint8{10, 20, 30, 40}
	sub := base.SubImage(image.Rect(1, 0, 3, 1)).(*image.Gray)

	raw := grayPixels(sub, sub.Bounds(), sub.Bounds().Dx(), sub.Bounds().Dy())
	want := []uint8{20, 30}

	if len(raw) != len(want) {
		t.Fatalf("len(raw) = %d, want %d", len(raw), len(want))
	}
	for i := range want {
		if raw[i] != want[i] {
			t.Fatalf("raw[%d] = %d, want %d", i, raw[i], want[i])
		}
	}
}

func genericGrayConversion(img image.Image) *image.Gray {
	bounds := img.Bounds()
	gray := image.NewGray(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gray.SetGray(x, y, color.GrayModel.Convert(img.At(x, y)).(color.Gray))
		}
	}
	return gray
}
