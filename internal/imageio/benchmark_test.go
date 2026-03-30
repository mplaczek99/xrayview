package imageio

import (
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/suyashkumar/dicom"
	"github.com/suyashkumar/dicom/pkg/frame"
	"github.com/suyashkumar/dicom/pkg/tag"
)

var (
	benchmarkLoadedResult    LoadedImage
	benchmarkGrayResult      *image.Gray
	benchmarkPixelDataResult dicom.PixelDataInfo
	benchmarkElementResult   []*dicom.Element
)

var syntheticBenchmarkSizes = []struct {
	name   string
	width  int
	height int
}{
	{name: "2048x2048", width: 2048, height: 2048},
	{name: "4096x4096", width: 4096, height: 4096},
}

func BenchmarkLoadSampleDICOM(b *testing.B) {
	path := sampleBenchmarkDICOMPath(b)
	info, err := os.Stat(path)
	if err != nil {
		b.Fatalf("stat sample dicom: %v", err)
	}

	b.ReportAllocs()
	b.SetBytes(info.Size())
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		loaded, err := loadDICOM(path)
		if err != nil {
			b.Fatalf("load dicom: %v", err)
		}
		benchmarkLoadedResult = loaded
	}
}

func BenchmarkRenderNativeFrameSample(b *testing.B) {
	nativeFrame, ds := benchmarkNativeFrame(b)
	bounds := image.Rect(0, 0, nativeFrame.Cols(), nativeFrame.Rows())

	b.ReportAllocs()
	b.SetBytes(int64(bounds.Dx() * bounds.Dy()))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		gray, err := renderNativeFrame(nativeFrame, ds)
		if err != nil {
			b.Fatalf("render native frame: %v", err)
		}
		benchmarkGrayResult = gray
	}
}

func BenchmarkConvertToGrayRGBASample(b *testing.B) {
	rgba := benchmarkRGBAImage(b)
	bounds := rgba.Bounds()

	b.ReportAllocs()
	b.SetBytes(int64(bounds.Dx() * bounds.Dy()))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		benchmarkGrayResult = convertToGray(rgba)
	}
}

func BenchmarkDICOMPixelDataFromGraySample(b *testing.B) {
	gray := benchmarkGrayImageSample(b)
	bounds := gray.Bounds()

	b.ReportAllocs()
	b.SetBytes(int64(bounds.Dx() * bounds.Dy()))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pixelData, elements, err := dicomPixelDataFromImage(gray)
		if err != nil {
			b.Fatalf("build grayscale pixel data: %v", err)
		}
		benchmarkPixelDataResult = pixelData
		benchmarkElementResult = elements
	}
}

func BenchmarkDICOMPixelDataFromRGBASample(b *testing.B) {
	rgba := benchmarkRGBAImage(b)
	bounds := rgba.Bounds()

	b.ReportAllocs()
	b.SetBytes(int64(bounds.Dx() * bounds.Dy() * 3))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pixelData, elements, err := dicomPixelDataFromImage(rgba)
		if err != nil {
			b.Fatalf("build rgba pixel data: %v", err)
		}
		benchmarkPixelDataResult = pixelData
		benchmarkElementResult = elements
	}
}

func BenchmarkRenderNativeFrameSynthetic(b *testing.B) {
	for _, size := range syntheticBenchmarkSizes {
		b.Run(size.name, func(b *testing.B) {
			nativeFrame, ds := syntheticNativeFrameDataset(size.width, size.height)

			b.ReportAllocs()
			b.SetBytes(int64(size.width * size.height))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				gray, err := renderNativeFrame(nativeFrame, ds)
				if err != nil {
					b.Fatalf("render synthetic native frame: %v", err)
				}
				benchmarkGrayResult = gray
			}
		})
	}
}

func BenchmarkConvertToGrayRGBASynthetic(b *testing.B) {
	for _, size := range syntheticBenchmarkSizes {
		b.Run(size.name, func(b *testing.B) {
			rgba := syntheticRGBAImage(size.width, size.height)

			b.ReportAllocs()
			b.SetBytes(int64(size.width * size.height))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				benchmarkGrayResult = convertToGray(rgba)
			}
		})
	}
}

func BenchmarkDICOMPixelDataFromGraySynthetic(b *testing.B) {
	for _, size := range syntheticBenchmarkSizes {
		b.Run(size.name, func(b *testing.B) {
			gray := syntheticGrayImage(size.width, size.height)

			b.ReportAllocs()
			b.SetBytes(int64(size.width * size.height))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				pixelData, elements, err := dicomPixelDataFromImage(gray)
				if err != nil {
					b.Fatalf("build synthetic grayscale pixel data: %v", err)
				}
				benchmarkPixelDataResult = pixelData
				benchmarkElementResult = elements
			}
		})
	}
}

func BenchmarkDICOMPixelDataFromRGBASynthetic(b *testing.B) {
	for _, size := range syntheticBenchmarkSizes {
		b.Run(size.name, func(b *testing.B) {
			rgba := syntheticRGBAImage(size.width, size.height)

			b.ReportAllocs()
			b.SetBytes(int64(size.width * size.height * 3))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				pixelData, elements, err := dicomPixelDataFromImage(rgba)
				if err != nil {
					b.Fatalf("build synthetic rgba pixel data: %v", err)
				}
				benchmarkPixelDataResult = pixelData
				benchmarkElementResult = elements
			}
		})
	}
}

func sampleBenchmarkDICOMPath(tb testing.TB) string {
	tb.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("resolve benchmark file path")
	}

	path := filepath.Join(filepath.Dir(file), "..", "..", "images", "sample-dental-radiograph.dcm")
	if _, err := os.Stat(path); err != nil {
		tb.Fatalf("stat sample dicom: %v", err)
	}

	return path
}

func benchmarkNativeFrame(tb testing.TB) (frame.INativeFrame, *dicom.Dataset) {
	tb.Helper()

	ds, err := dicom.ParseFile(
		sampleBenchmarkDICOMPath(tb),
		nil,
		dicom.AllowMissingMetaElementGroupLength(),
		dicom.AllowUnknownSpecificCharacterSet(),
	)
	if err != nil {
		tb.Fatalf("parse sample dicom: %v", err)
	}

	pixelDataElement, err := ds.FindElementByTag(tag.PixelData)
	if err != nil {
		tb.Fatalf("find pixel data: %v", err)
	}

	pixelData := dicom.MustGetPixelDataInfo(pixelDataElement.Value)
	if len(pixelData.Frames) == 0 {
		tb.Fatal("sample dicom contains no frames")
	}

	nativeFrame, err := pixelData.Frames[0].GetNativeFrame()
	if err != nil {
		tb.Fatalf("get native frame: %v", err)
	}

	return nativeFrame, &ds
}

func benchmarkGrayImageSample(tb testing.TB) *image.Gray {
	tb.Helper()

	loaded, err := loadDICOM(sampleBenchmarkDICOMPath(tb))
	if err != nil {
		tb.Fatalf("load sample dicom: %v", err)
	}

	gray, ok := loaded.Image.(*image.Gray)
	if !ok {
		gray = convertToGray(loaded.Image)
	}

	return gray
}

func benchmarkRGBAImage(tb testing.TB) *image.RGBA {
	tb.Helper()

	gray := benchmarkGrayImageSample(tb)
	rgba := image.NewRGBA(gray.Bounds())
	draw.Draw(rgba, gray.Bounds(), gray, gray.Bounds().Min, draw.Src)
	return rgba
}

func syntheticNativeFrameDataset(width, height int) (frame.INativeFrame, *dicom.Dataset) {
	raw := make([]uint16, width*height)
	for y := 0; y < height; y++ {
		row := raw[y*width : (y+1)*width]
		for x := 0; x < width; x++ {
			row[x] = uint16((x*31 + y*17) & 0x0fff)
		}
	}

	return &frame.NativeFrame[uint16]{
			InternalBitsPerSample:   16,
			InternalRows:            height,
			InternalCols:            width,
			InternalSamplesPerPixel: 1,
			RawData:                 raw,
		}, &dicom.Dataset{Elements: []*dicom.Element{
			mustNewElement(nil, tag.BitsStored, []int{12}),
			mustNewElement(nil, tag.PixelRepresentation, []int{0}),
			mustNewElement(nil, tag.PhotometricInterpretation, []string{"MONOCHROME2"}),
		}}
}

func syntheticGrayImage(width, height int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		row := img.Pix[y*img.Stride : y*img.Stride+width]
		for x := 0; x < width; x++ {
			row[x] = uint8((x*19 + y*23) & 0xff)
		}
	}
	return img
}

func syntheticRGBAImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		row := img.Pix[y*img.Stride : y*img.Stride+width*4]
		for x := 0; x < width; x++ {
			offset := x * 4
			row[offset] = uint8((x*11 + y*7) & 0xff)
			row[offset+1] = uint8((x*5 + y*29) & 0xff)
			row[offset+2] = uint8((x*17 + y*13) & 0xff)
			row[offset+3] = 255
		}
	}
	return img
}
