package jobs

import (
	"context"
	"os"
	"testing"

	"xrayview/backend/internal/analysis"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/dicommeta"
	"xrayview/backend/internal/imaging"
	"xrayview/backend/internal/processing"
	"xrayview/backend/internal/render"
)

const benchmarkDicomPath = "../../../images/sample-dental-radiograph.dcm"

var (
	benchmarkDecodedStudy  dicommeta.SourceStudy
	benchmarkPreview       imaging.PreviewImage
	benchmarkPipeline      processing.PipelineOutput
	benchmarkToothAnalysis contracts.ToothAnalysis
)

func BenchmarkDecodeStudy(b *testing.B) {
	if _, err := os.Stat(benchmarkDicomPath); err != nil {
		b.Skip("benchmark DICOM not available")
	}

	decoder := dicommeta.NewDecoder()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		study, err := decoder.DecodeStudy(ctx, benchmarkDicomPath)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkDecodedStudy = study
	}
}

func BenchmarkRenderSourceImage(b *testing.B) {
	study := loadBenchmarkStudy(b)
	plan := render.DefaultRenderPlan()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkPreview = render.RenderSourceImage(study.Image, plan)
	}
}

func BenchmarkProcessSourceImage(b *testing.B) {
	study := loadBenchmarkStudy(b)
	plan := render.DefaultRenderPlan()
	controls := processing.GrayscaleControls{Contrast: 1.0}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		output, err := processing.ProcessSourceImage(
			study.Image,
			plan,
			controls,
			"none",
			false,
		)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkPipeline = output
	}
}

func BenchmarkAnalyzePreview(b *testing.B) {
	study := loadBenchmarkStudy(b)
	preview := render.RenderSourceImage(study.Image, render.DefaultRenderPlan())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := analysis.AnalyzePreview(preview, study.MeasurementScale)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkToothAnalysis = result
	}
}

func loadBenchmarkStudy(b *testing.B) dicommeta.SourceStudy {
	b.Helper()

	if _, err := os.Stat(benchmarkDicomPath); err != nil {
		b.Skip("benchmark DICOM not available")
	}

	study, err := dicommeta.NewDecoder().DecodeStudy(context.Background(), benchmarkDicomPath)
	if err != nil {
		b.Fatal(err)
	}

	return study
}
