package testfixtures

import (
	"path/filepath"
	"testing"

	"xrayview/backend/internal/dicommeta"
	dicomexport "xrayview/backend/internal/export"
	"xrayview/backend/internal/imaging"
)

const (
	SampleDicomName          = "sample-dental-radiograph.dcm"
	ProcessedSampleDicomName = "sample-dental-radiograph_processed.dcm"
	sampleWidth              = 4
	sampleHeight             = 2
)

var (
	sampleSourcePixels = []uint8{
		0, 64, 128, 255,
		10, 80, 160, 240,
	}
	sampleRenderedPixels = []uint8{
		0, 64, 129, 255,
		10, 80, 161, 241,
	}
	sampleProcessedPixels = []uint8{
		0, 43, 128, 255,
		0, 85, 170, 255,
	}
	samplePalettePixels = []uint8{
		0, 0, 0, 255,
		37, 39, 43, 255,
		112, 120, 128, 255,
		255, 255, 255, 255,
		0, 0, 0, 255,
		74, 79, 85, 255,
		190, 200, 191, 255,
		255, 255, 255, 255,
	}
	sampleComparePixels = []uint8{
		0, 0, 0, 255,
		64, 64, 64, 255,
		129, 129, 129, 255,
		255, 255, 255, 255,
		0, 0, 0, 255,
		37, 39, 43, 255,
		112, 120, 128, 255,
		255, 255, 255, 255,
		10, 10, 10, 255,
		80, 80, 80, 255,
		161, 161, 161, 255,
		241, 241, 241, 255,
		0, 0, 0, 255,
		74, 79, 85, 255,
		190, 200, 191, 255,
		255, 255, 255, 255,
	}
)

func WriteSampleDicom(t testing.TB) string {
	t.Helper()

	return writeSecondaryCapture(t, SampleDicomName, SampleSourcePreview())
}

func WriteProcessedSampleDicom(t testing.TB) string {
	t.Helper()

	return writeSecondaryCapture(t, ProcessedSampleDicomName, SampleProcessedPreview())
}

func SampleSourcePreview() imaging.PreviewImage {
	return imaging.GrayPreview(sampleWidth, sampleHeight, cloneBytes(sampleSourcePixels))
}

func SampleRenderedPreview() imaging.PreviewImage {
	return imaging.GrayPreview(sampleWidth, sampleHeight, cloneBytes(sampleRenderedPixels))
}

func SampleProcessedPreview() imaging.PreviewImage {
	return imaging.GrayPreview(sampleWidth, sampleHeight, cloneBytes(sampleProcessedPixels))
}

func SamplePalettePreview() imaging.PreviewImage {
	return imaging.RGBAPreview(sampleWidth, sampleHeight, cloneBytes(samplePalettePixels))
}

func SampleComparePreview() imaging.PreviewImage {
	return imaging.RGBAPreview(sampleWidth*2, sampleHeight, cloneBytes(sampleComparePixels))
}

func writeSecondaryCapture(t testing.TB, name string, preview imaging.PreviewImage) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := dicomexport.WriteSecondaryCapture(
		path,
		preview,
		dicommeta.SourceMetadata{StudyInstanceUID: "1.2.3.4.5"},
	); err != nil {
		t.Fatalf("WriteSecondaryCapture returned error: %v", err)
	}

	return path
}

func cloneBytes(values []uint8) []uint8 {
	return append([]uint8(nil), values...)
}
