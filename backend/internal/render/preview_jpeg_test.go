package render

import (
	"fmt"
	"io"
	"path/filepath"
	"testing"

	"xrayview/backend/internal/imaging"
)

func BenchmarkEncodePreviewJPEG(b *testing.B) {
	const width, height = 2048, 1536
	grayPix := make([]uint8, width*height)
	for i := range grayPix {
		grayPix[i] = uint8(i % 256)
	}
	rgbaPix := make([]uint8, width*height*4)
	for i := 0; i < width*height; i++ {
		v := uint8(i % 256)
		rgbaPix[i*4] = v
		rgbaPix[i*4+1] = v
		rgbaPix[i*4+2] = v
		rgbaPix[i*4+3] = 255
	}

	b.Run("Gray8", func(b *testing.B) {
		preview := imaging.GrayPreview(width, height, grayPix)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := EncodePreviewJPEG(io.Discard, preview); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("RGBA8", func(b *testing.B) {
		preview := imaging.RGBAPreview(width, height, rgbaPix)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := EncodePreviewJPEG(io.Discard, preview); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkSavePreviewJPEG(b *testing.B) {
	const width, height = 2048, 1536
	grayPix := make([]uint8, width*height)
	for i := range grayPix {
		grayPix[i] = uint8(i % 256)
	}

	b.Run("Gray8", func(b *testing.B) {
		preview := imaging.GrayPreview(width, height, grayPix)
		dir := b.TempDir()
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			path := filepath.Join(dir, fmt.Sprintf("preview-%d.jpeg", i))
			if err := SavePreviewJPEG(path, preview); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkSavePreview(b *testing.B) {
	const width, height = 2048, 1536
	grayPix := make([]uint8, width*height)
	for i := range grayPix {
		grayPix[i] = uint8(i % 256)
	}
	rgbaPix := make([]uint8, width*height*4)
	for i := 0; i < width*height; i++ {
		v := uint8(i % 256)
		rgbaPix[i*4] = v
		rgbaPix[i*4+1] = v
		rgbaPix[i*4+2] = v
		rgbaPix[i*4+3] = 255
	}

	b.Run("Gray8_JPEG", func(b *testing.B) {
		preview := imaging.GrayPreview(width, height, grayPix)
		dir := b.TempDir()
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			path := filepath.Join(dir, fmt.Sprintf("preview-%d.jpeg", i))
			if err := SavePreview(path, preview); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("RGBA8_PNG", func(b *testing.B) {
		preview := imaging.RGBAPreview(width, height, rgbaPix)
		dir := b.TempDir()
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			path := filepath.Join(dir, fmt.Sprintf("preview-%d.png", i))
			if err := SavePreview(path, preview); err != nil {
				b.Fatal(err)
			}
		}
	})
}
