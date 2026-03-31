package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mplaczek99/xrayview/internal/imageio"
)

var (
	benchmarkWorkflowImageResult    = bytes.Buffer{}
	benchmarkWorkflowMetadataResult imageio.StudyMetadata
)

func BenchmarkDescribeStudyWorkflowSample(b *testing.B) {
	path := workflowSampleDICOMPath(b)
	info, err := os.Stat(path)
	if err != nil {
		b.Fatalf("stat sample dicom: %v", err)
	}

	b.ReportAllocs()
	b.SetBytes(info.Size())
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		metadata, err := imageio.LoadStudyMetadata(path)
		if err != nil {
			b.Fatalf("load study metadata: %v", err)
		}
		benchmarkWorkflowMetadataResult = metadata

		benchmarkWorkflowImageResult.Reset()
		if err := writeStudyDescription(&benchmarkWorkflowImageResult, metadata); err != nil {
			b.Fatalf("write study description: %v", err)
		}
	}
}

func BenchmarkPreviewWorkflowSample(b *testing.B) {
	path := workflowSampleDICOMPath(b)
	info, err := os.Stat(path)
	if err != nil {
		b.Fatalf("stat sample dicom: %v", err)
	}
	previewPath := filepath.Join(b.TempDir(), "preview.png")

	b.ReportAllocs()
	b.SetBytes(info.Size())
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		loaded, err := imageio.Load(path)
		if err != nil {
			b.Fatalf("load dicom: %v", err)
		}
		output, _ := processGrayImage(asGrayImage(loaded.Image), config{})
		if err := imageio.SavePreviewPNG(previewPath, output); err != nil {
			b.Fatalf("save preview png: %v", err)
		}
	}
}

func BenchmarkProcessDICOMWorkflowSampleXray(b *testing.B) {
	path := workflowSampleDICOMPath(b)
	info, err := os.Stat(path)
	if err != nil {
		b.Fatalf("stat sample dicom: %v", err)
	}
	outputPath := filepath.Join(b.TempDir(), "processed.dcm")
	cfg := config{
		brightness: 10,
		contrast:   1.4,
		equalize:   true,
		palette:    "bone",
	}

	b.ReportAllocs()
	b.SetBytes(info.Size())
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		loaded, err := imageio.Load(path)
		if err != nil {
			b.Fatalf("load dicom: %v", err)
		}
		output, _ := processGrayImage(asGrayImage(loaded.Image), cfg)
		if err := imageio.SaveDICOM(outputPath, output, loaded); err != nil {
			b.Fatalf("save dicom: %v", err)
		}
	}
}

func workflowSampleDICOMPath(tb testing.TB) string {
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
