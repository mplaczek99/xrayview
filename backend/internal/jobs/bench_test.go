package jobs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xrayview/backend/internal/analysis"
	"xrayview/backend/internal/cache"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/dicommeta"
	dicomexport "xrayview/backend/internal/export"
	"xrayview/backend/internal/imaging"
	"xrayview/backend/internal/processing"
	"xrayview/backend/internal/render"
	"xrayview/backend/internal/studies"
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

func BenchmarkAnalyzeJob(b *testing.B) {
	study := loadBenchmarkStudy(b)
	plan := render.DefaultRenderPlan()

	b.Run("WithDecode", func(b *testing.B) {
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			decoded, err := dicommeta.NewDecoder().DecodeStudy(ctx, benchmarkDicomPath)
			if err != nil {
				b.Fatal(err)
			}
			preview := render.RenderSourceImage(decoded.Image, plan)
			result, err := analysis.AnalyzePreview(preview, decoded.MeasurementScale)
			if err != nil {
				b.Fatal(err)
			}
			benchmarkToothAnalysis = result
		}
	})

	b.Run("CacheHit", func(b *testing.B) {
		preview := render.RenderSourceImage(study.Image, plan)

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			result, err := analysis.AnalyzePreview(preview, study.MeasurementScale)
			if err != nil {
				b.Fatal(err)
			}
			benchmarkToothAnalysis = result
		}
	})
}

func BenchmarkFullWorkflow(b *testing.B) {
	if _, err := os.Stat(benchmarkDicomPath); err != nil {
		b.Skip("benchmark DICOM not available")
	}

	absPath, err := filepath.Abs(benchmarkDicomPath)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		tempDir := b.TempDir()
		cacheStore := cache.New(filepath.Join(tempDir, "cache"))
		studyRegistry := studies.New()
		jobSeq := 0
		service := newService(
			cacheStore,
			studyRegistry,
			nil,
			dicomexport.GoWriter{},
			nil,
			func() (string, error) {
				jobSeq++
				return fmt.Sprintf("bench-job-%d", jobSeq), nil
			},
		)

		study, err := studyRegistry.Register(absPath, nil)
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		// Render
		renderJob, err := service.StartRenderJob(contracts.RenderStudyCommand{StudyID: study.StudyID})
		if err != nil {
			b.Fatal(err)
		}
		waitBenchJob(b, service, renderJob.JobID)

		// Process
		processJob, err := service.StartProcessJob(contracts.ProcessStudyCommand{
			StudyID:  study.StudyID,
			PresetID: "default",
		})
		if err != nil {
			b.Fatal(err)
		}
		waitBenchJob(b, service, processJob.JobID)

		// Analyze
		analyzeJob, err := service.StartAnalyzeJob(contracts.AnalyzeStudyCommand{StudyID: study.StudyID})
		if err != nil {
			b.Fatal(err)
		}
		waitBenchJob(b, service, analyzeJob.JobID)
	}
}

func waitBenchJob(b *testing.B, service *Service, jobID string) {
	b.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := service.GetJob(contracts.JobCommand{JobID: jobID})
		if err != nil {
			b.Fatal(err)
		}
		if isTerminalState(snapshot.State) {
			if snapshot.State != contracts.JobStateCompleted {
				b.Fatalf("job %s ended in state %s", jobID, snapshot.State)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	b.Fatalf("job %s did not complete before timeout", jobID)
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
