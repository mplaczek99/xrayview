package jobs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xrayview/backend/internal/cache"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/dicommeta"
	dicomexport "xrayview/backend/internal/export"
	"xrayview/backend/internal/imaging"
	"xrayview/backend/internal/render"
	"xrayview/backend/internal/studies"
)

type staticDecoder struct {
	study dicommeta.SourceStudy
}

type failingDecoder struct {
	err error
}

func (decoder staticDecoder) DecodeStudy(
	_ context.Context,
	_ string,
) (dicommeta.SourceStudy, error) {
	return decoder.study, nil
}

func (decoder failingDecoder) DecodeStudy(
	_ context.Context,
	_ string,
) (dicommeta.SourceStudy, error) {
	return dicommeta.SourceStudy{}, decoder.err
}

type blockingDecoder struct {
	study   dicommeta.SourceStudy
	started chan struct{}
}

func (decoder *blockingDecoder) DecodeStudy(
	ctx context.Context,
	_ string,
) (dicommeta.SourceStudy, error) {
	select {
	case decoder.started <- struct{}{}:
	default:
	}

	<-ctx.Done()
	return dicommeta.SourceStudy{}, ctx.Err()
}

type countingServiceDecoder struct {
	mu    sync.Mutex
	study dicommeta.SourceStudy
	calls int
}

func (decoder *countingServiceDecoder) DecodeStudy(
	ctx context.Context,
	_ string,
) (dicommeta.SourceStudy, error) {
	if err := ctx.Err(); err != nil {
		return dicommeta.SourceStudy{}, err
	}

	decoder.mu.Lock()
	decoder.calls++
	decoder.mu.Unlock()

	return decoder.study, nil
}

func (decoder *countingServiceDecoder) CallCount() int {
	decoder.mu.Lock()
	defer decoder.mu.Unlock()

	return decoder.calls
}

type concurrencyTrackingDecoder struct {
	mu        sync.Mutex
	study     dicommeta.SourceStudy
	release   <-chan struct{}
	started   chan struct{}
	active    int
	maxActive int
}

func (decoder *concurrencyTrackingDecoder) DecodeStudy(
	ctx context.Context,
	_ string,
) (dicommeta.SourceStudy, error) {
	if err := ctx.Err(); err != nil {
		return dicommeta.SourceStudy{}, err
	}

	decoder.mu.Lock()
	decoder.active++
	if decoder.active > decoder.maxActive {
		decoder.maxActive = decoder.active
	}
	decoder.mu.Unlock()

	select {
	case decoder.started <- struct{}{}:
	default:
	}

	defer func() {
		decoder.mu.Lock()
		decoder.active--
		decoder.mu.Unlock()
	}()

	if decoder.release != nil {
		select {
		case <-ctx.Done():
			return dicommeta.SourceStudy{}, ctx.Err()
		case <-decoder.release:
		}
	}

	return decoder.study, nil
}

func (decoder *concurrencyTrackingDecoder) MaxActive() int {
	decoder.mu.Lock()
	defer decoder.mu.Unlock()

	return decoder.maxActive
}

type partialFailingSecondaryCaptureWriter struct {
	err error
}

func (writer partialFailingSecondaryCaptureWriter) WriteSecondaryCapture(
	ctx context.Context,
	path string,
	_ imaging.PreviewImage,
	_ dicommeta.SourceMetadata,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte("partial-dicom"), 0o644); err != nil {
		return err
	}

	return writer.err
}

type blockingSecondaryCaptureWriter struct {
	started chan struct{}
}

func (writer *blockingSecondaryCaptureWriter) WriteSecondaryCapture(
	ctx context.Context,
	path string,
	_ imaging.PreviewImage,
	_ dicommeta.SourceMetadata,
) error {
	if err := os.WriteFile(path, []byte("partial-dicom"), 0o644); err != nil {
		return err
	}

	select {
	case writer.started <- struct{}{}:
	default:
	}

	<-ctx.Done()
	return ctx.Err()
}

type successfulSecondaryCaptureWriter struct{}

func (successfulSecondaryCaptureWriter) WriteSecondaryCapture(
	ctx context.Context,
	path string,
	_ imaging.PreviewImage,
	_ dicommeta.SourceMetadata,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return os.WriteFile(path, []byte("complete-dicom"), 0o644)
}

func TestStartRenderJobWritesPreviewAndServesCachedSnapshot(t *testing.T) {
	studyRegistry, study := registerTestStudy(t)
	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	sourceStudy := dicommeta.SourceStudy{
		Image: imaging.SourceImage{
			Width:    2,
			Height:   2,
			Format:   imaging.FormatGrayFloat32,
			Pixels:   []float32{0, 32, 128, 255},
			MinValue: 0,
			MaxValue: 255,
			DefaultWindow: &imaging.WindowLevel{
				Center: 127.5,
				Width:  255,
			},
		},
		MeasurementScale: &contracts.MeasurementScale{
			RowSpacingMM:    0.25,
			ColumnSpacingMM: 0.40,
			Source:          "PixelSpacing",
		},
		Metadata: dicommeta.SourceMetadata{
			StudyInstanceUID: "1.2.3.4.5",
		},
	}
	service := newService(
		cacheStore,
		studyRegistry,
		nil,
		dicomexport.GoWriter{},
		func() (studyDecoder, error) { return staticDecoder{study: sourceStudy}, nil },
		sequenceJobIDs("job-1", "job-2"),
	)

	started, err := service.StartRenderJob(contracts.RenderStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatalf("StartRenderJob returned error: %v", err)
	}

	snapshot := waitForTerminalJob(t, service, started.JobID)
	if got, want := snapshot.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}
	if snapshot.FromCache {
		t.Fatal("FromCache = true, want false for first render")
	}
	if snapshot.Result == nil {
		t.Fatal("Result = nil, want completed render payload")
	}
	if got, want := snapshot.Result.Kind, contracts.JobKindRenderStudy; got != want {
		t.Fatalf("Result.Kind = %q, want %q", got, want)
	}

	result, ok := snapshot.Result.Payload.(contracts.RenderStudyCommandResult)
	if !ok {
		t.Fatalf("Result.Payload type = %T, want contracts.RenderStudyCommandResult", snapshot.Result.Payload)
	}
	if got, want := result.StudyID, study.StudyID; got != want {
		t.Fatalf("Result.StudyID = %q, want %q", got, want)
	}
	if got, want := result.LoadedWidth, uint32(2); got != want {
		t.Fatalf("LoadedWidth = %d, want %d", got, want)
	}
	if got, want := result.LoadedHeight, uint32(2); got != want {
		t.Fatalf("LoadedHeight = %d, want %d", got, want)
	}
	if result.MeasurementScale == nil {
		t.Fatal("MeasurementScale = nil, want decoded scale")
	}
	if !stringsHasPathPrefix(result.PreviewPath, filepath.Join(cacheStore.RootDir(), "artifacts", "render")) {
		t.Fatalf("PreviewPath = %q, want cache/artifacts/render prefix", result.PreviewPath)
	}
	if info, err := os.Stat(result.PreviewPath); err != nil || info.IsDir() {
		t.Fatalf("preview artifact missing or invalid: %v", err)
	}

	cachedStarted, err := service.StartRenderJob(contracts.RenderStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatalf("cached StartRenderJob returned error: %v", err)
	}
	if got, want := cachedStarted.JobID, "job-2"; got != want {
		t.Fatalf("cached JobID = %q, want %q", got, want)
	}

	cachedSnapshot, err := service.GetJob(contracts.JobCommand{JobID: cachedStarted.JobID})
	if err != nil {
		t.Fatalf("GetJob returned error: %v", err)
	}
	if got, want := cachedSnapshot.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("cached State = %q, want %q", got, want)
	}
	if !cachedSnapshot.FromCache {
		t.Fatal("cached FromCache = false, want true")
	}

	cachedResult, ok := cachedSnapshot.Result.Payload.(contracts.RenderStudyCommandResult)
	if !ok {
		t.Fatalf("cached Result.Payload type = %T, want contracts.RenderStudyCommandResult", cachedSnapshot.Result.Payload)
	}
	if got, want := cachedResult.PreviewPath, result.PreviewPath; got != want {
		t.Fatalf("cached PreviewPath = %q, want %q", got, want)
	}
}

func TestStartRenderJobReusesCachedResultAcrossStudyReopen(t *testing.T) {
	inputPath := filepath.Join(t.TempDir(), "study.dcm")
	if err := os.WriteFile(inputPath, []byte("dicom"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	studyRegistry := studies.New()
	firstStudy, err := studyRegistry.Register(inputPath, nil)
	if err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}
	secondStudy, err := studyRegistry.Register(inputPath, nil)
	if err != nil {
		t.Fatalf("second Register returned error: %v", err)
	}
	if firstStudy.StudyID == secondStudy.StudyID {
		t.Fatalf("StudyID = %q for both studies, want distinct registrations", firstStudy.StudyID)
	}

	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	sourceStudy := dicommeta.SourceStudy{
		Image: imaging.SourceImage{
			Width:    2,
			Height:   2,
			Format:   imaging.FormatGrayFloat32,
			Pixels:   []float32{0, 32, 128, 255},
			MinValue: 0,
			MaxValue: 255,
			DefaultWindow: &imaging.WindowLevel{
				Center: 127.5,
				Width:  255,
			},
		},
	}
	service := newService(
		cacheStore,
		studyRegistry,
		nil,
		dicomexport.GoWriter{},
		func() (studyDecoder, error) { return staticDecoder{study: sourceStudy}, nil },
		sequenceJobIDs("job-1", "job-2"),
	)

	firstStarted, err := service.StartRenderJob(contracts.RenderStudyCommand{StudyID: firstStudy.StudyID})
	if err != nil {
		t.Fatalf("first StartRenderJob returned error: %v", err)
	}
	firstSnapshot := waitForTerminalJob(t, service, firstStarted.JobID)
	firstResult, ok := firstSnapshot.Result.Payload.(contracts.RenderStudyCommandResult)
	if !ok {
		t.Fatalf("first Result.Payload type = %T, want contracts.RenderStudyCommandResult", firstSnapshot.Result.Payload)
	}

	secondStarted, err := service.StartRenderJob(contracts.RenderStudyCommand{StudyID: secondStudy.StudyID})
	if err != nil {
		t.Fatalf("second StartRenderJob returned error: %v", err)
	}
	if got, want := secondStarted.JobID, "job-2"; got != want {
		t.Fatalf("second JobID = %q, want %q", got, want)
	}

	secondSnapshot, err := service.GetJob(contracts.JobCommand{JobID: secondStarted.JobID})
	if err != nil {
		t.Fatalf("GetJob returned error: %v", err)
	}
	if !secondSnapshot.FromCache {
		t.Fatal("second FromCache = false, want true")
	}
	if secondSnapshot.StudyID == nil {
		t.Fatal("second StudyID = nil, want top-level study id")
	}
	if got, want := *secondSnapshot.StudyID, secondStudy.StudyID; got != want {
		t.Fatalf("second top-level StudyID = %q, want %q", got, want)
	}

	secondResult, ok := secondSnapshot.Result.Payload.(contracts.RenderStudyCommandResult)
	if !ok {
		t.Fatalf("second Result.Payload type = %T, want contracts.RenderStudyCommandResult", secondSnapshot.Result.Payload)
	}
	if got, want := secondResult.PreviewPath, firstResult.PreviewPath; got != want {
		t.Fatalf("second PreviewPath = %q, want reused %q", got, want)
	}
	if got, want := secondResult.StudyID, firstStudy.StudyID; got != want {
		t.Fatalf("second payload StudyID = %q, want cached %q", got, want)
	}
	if secondResult.StudyID == *secondSnapshot.StudyID {
		t.Fatal("second payload StudyID unexpectedly matched top-level cached study id")
	}
}

func TestOnJobCompletionReceivesCompletedSnapshot(t *testing.T) {
	studyRegistry, study := registerTestStudy(t)
	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	sourceStudy := dicommeta.SourceStudy{
		Image: imaging.SourceImage{
			Width:  2,
			Height: 2,
			Format: imaging.FormatGrayFloat32,
			Pixels: []float32{0, 32, 128, 255},
		},
	}
	service := newService(
		cacheStore,
		studyRegistry,
		nil,
		dicomexport.GoWriter{},
		func() (studyDecoder, error) { return staticDecoder{study: sourceStudy}, nil },
		sequenceJobIDs("job-1"),
	)
	completions := make(chan contracts.JobSnapshot, 1)
	service.OnJobCompletion(func(snapshot contracts.JobSnapshot) {
		completions <- snapshot
	})

	started, err := service.StartRenderJob(contracts.RenderStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatalf("StartRenderJob returned error: %v", err)
	}

	waitForTerminalJob(t, service, started.JobID)
	snapshot := waitForCompletionCallback(t, completions)
	if got, want := snapshot.JobID, started.JobID; got != want {
		t.Fatalf("callback JobID = %q, want %q", got, want)
	}
	if got, want := snapshot.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("callback State = %q, want %q", got, want)
	}
}

func TestOnJobCompletionReceivesFailedSnapshot(t *testing.T) {
	studyRegistry, study := registerTestStudy(t)
	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	service := newService(
		cacheStore,
		studyRegistry,
		nil,
		dicomexport.GoWriter{},
		func() (studyDecoder, error) { return failingDecoder{err: fmt.Errorf("boom")}, nil },
		sequenceJobIDs("job-1"),
	)
	completions := make(chan contracts.JobSnapshot, 1)
	service.OnJobCompletion(func(snapshot contracts.JobSnapshot) {
		completions <- snapshot
	})

	started, err := service.StartRenderJob(contracts.RenderStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatalf("StartRenderJob returned error: %v", err)
	}

	waitForTerminalJob(t, service, started.JobID)
	snapshot := waitForCompletionCallback(t, completions)
	if got, want := snapshot.State, contracts.JobStateFailed; got != want {
		t.Fatalf("callback State = %q, want %q", got, want)
	}
	if snapshot.Error == nil {
		t.Fatal("callback Error = nil, want backend error payload")
	}
}

func TestOnJobCompletionReceivesCancelledSnapshot(t *testing.T) {
	studyRegistry, study := registerTestStudy(t)
	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	decoder := &blockingDecoder{
		started: make(chan struct{}, 1),
	}
	service := newService(
		cacheStore,
		studyRegistry,
		nil,
		dicomexport.GoWriter{},
		func() (studyDecoder, error) { return decoder, nil },
		sequenceJobIDs("job-1"),
	)
	completions := make(chan contracts.JobSnapshot, 1)
	service.OnJobCompletion(func(snapshot contracts.JobSnapshot) {
		completions <- snapshot
	})

	started, err := service.StartRenderJob(contracts.RenderStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatalf("StartRenderJob returned error: %v", err)
	}

	<-decoder.started

	if _, err := service.CancelJob(contracts.JobCommand{JobID: started.JobID}); err != nil {
		t.Fatalf("CancelJob returned error: %v", err)
	}

	waitForTerminalJob(t, service, started.JobID)
	snapshot := waitForCompletionCallback(t, completions)
	if got, want := snapshot.State, contracts.JobStateCancelled; got != want {
		t.Fatalf("callback State = %q, want %q", got, want)
	}
}

func TestStartRenderJobDeduplicatesActiveStudyRender(t *testing.T) {
	studyRegistry, study := registerTestStudy(t)
	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	decoder := &blockingDecoder{
		started: make(chan struct{}, 1),
	}
	service := newService(
		cacheStore,
		studyRegistry,
		nil,
		dicomexport.GoWriter{},
		func() (studyDecoder, error) { return decoder, nil },
		sequenceJobIDs("job-1"),
	)

	first, err := service.StartRenderJob(contracts.RenderStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatalf("StartRenderJob returned error: %v", err)
	}
	second, err := service.StartRenderJob(contracts.RenderStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatalf("second StartRenderJob returned error: %v", err)
	}

	if got, want := second.JobID, first.JobID; got != want {
		t.Fatalf("second JobID = %q, want deduped %q", got, want)
	}

	<-decoder.started

	cancelled, err := service.CancelJob(contracts.JobCommand{JobID: first.JobID})
	if err != nil {
		t.Fatalf("CancelJob returned error: %v", err)
	}
	if got, want := cancelled.State, contracts.JobStateCancelling; got != want {
		t.Fatalf("CancelJob state = %q, want %q", got, want)
	}

	snapshot := waitForTerminalJob(t, service, first.JobID)
	if got, want := snapshot.State, contracts.JobStateCancelled; got != want {
		t.Fatalf("terminal State = %q, want %q", got, want)
	}
	if snapshot.Result != nil {
		t.Fatalf("Result = %#v, want nil for cancelled job", snapshot.Result)
	}
}

func TestStartProcessJobWritesPreviewAndServesCachedSnapshot(t *testing.T) {
	studyRegistry, study := registerTestStudy(t)
	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	sourceStudy := dicommeta.SourceStudy{
		Image: imaging.SourceImage{
			Width:    2,
			Height:   2,
			Format:   imaging.FormatGrayFloat32,
			Pixels:   []float32{0, 32, 128, 255},
			MinValue: 0,
			MaxValue: 255,
			DefaultWindow: &imaging.WindowLevel{
				Center: 127.5,
				Width:  255,
			},
		},
		MeasurementScale: &contracts.MeasurementScale{
			RowSpacingMM:    0.25,
			ColumnSpacingMM: 0.40,
			Source:          "PixelSpacing",
		},
	}
	service := newService(
		cacheStore,
		studyRegistry,
		nil,
		dicomexport.GoWriter{},
		func() (studyDecoder, error) { return staticDecoder{study: sourceStudy}, nil },
		sequenceJobIDs("job-1", "job-2"),
	)

	outputPath := filepath.Join(t.TempDir(), "processed-output.dcm")
	started, err := service.StartProcessJob(contracts.ProcessStudyCommand{
		StudyID:  study.StudyID,
		PresetID: "xray",
		Compare:  false,
		OutputPath: func() *string {
			value := outputPath
			return &value
		}(),
	})
	if err != nil {
		t.Fatalf("StartProcessJob returned error: %v", err)
	}

	snapshot := waitForTerminalJob(t, service, started.JobID)
	if got, want := snapshot.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}
	if snapshot.FromCache {
		t.Fatal("FromCache = true, want false for first process job")
	}
	if snapshot.Result == nil {
		t.Fatal("Result = nil, want completed process payload")
	}
	if got, want := snapshot.Result.Kind, contracts.JobKindProcessStudy; got != want {
		t.Fatalf("Result.Kind = %q, want %q", got, want)
	}

	result, ok := snapshot.Result.Payload.(contracts.ProcessStudyCommandResult)
	if !ok {
		t.Fatalf("Result.Payload type = %T, want contracts.ProcessStudyCommandResult", snapshot.Result.Payload)
	}
	if got, want := result.StudyID, study.StudyID; got != want {
		t.Fatalf("Result.StudyID = %q, want %q", got, want)
	}
	if got, want := result.LoadedWidth, uint32(2); got != want {
		t.Fatalf("LoadedWidth = %d, want %d", got, want)
	}
	if got, want := result.LoadedHeight, uint32(2); got != want {
		t.Fatalf("LoadedHeight = %d, want %d", got, want)
	}
	if got, want := result.DicomPath, outputPath; got != want {
		t.Fatalf("DicomPath = %q, want %q", got, want)
	}
	if result.MeasurementScale == nil {
		t.Fatal("MeasurementScale = nil, want decoded scale")
	}
	if !stringsHasPathPrefix(result.PreviewPath, filepath.Join(cacheStore.RootDir(), "artifacts", "process")) {
		t.Fatalf("PreviewPath = %q, want cache/artifacts/process prefix", result.PreviewPath)
	}
	if info, err := os.Stat(result.PreviewPath); err != nil || info.IsDir() {
		t.Fatalf("preview artifact missing or invalid: %v", err)
	}
	if info, err := os.Stat(result.DicomPath); err != nil || info.IsDir() {
		t.Fatalf("dicom artifact missing or invalid: %v", err)
	}

	metadata, err := dicommeta.ReadFile(result.DicomPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if got, want := metadata.TransferSyntaxUID, "1.2.840.10008.1.2.1"; got != want {
		t.Fatalf("TransferSyntaxUID = %q, want %q", got, want)
	}
	if got, want := metadata.PhotometricInterpretation, "RGB"; got != want {
		t.Fatalf("PhotometricInterpretation = %q, want %q", got, want)
	}
	if got, want := metadata.SamplesPerPixel, uint16(3); got != want {
		t.Fatalf("SamplesPerPixel = %d, want %d", got, want)
	}

	cachedStarted, err := service.StartProcessJob(contracts.ProcessStudyCommand{
		StudyID:  study.StudyID,
		PresetID: "xray",
		Compare:  false,
		OutputPath: func() *string {
			value := outputPath
			return &value
		}(),
	})
	if err != nil {
		t.Fatalf("cached StartProcessJob returned error: %v", err)
	}
	if got, want := cachedStarted.JobID, "job-2"; got != want {
		t.Fatalf("cached JobID = %q, want %q", got, want)
	}

	cachedSnapshot, err := service.GetJob(contracts.JobCommand{JobID: cachedStarted.JobID})
	if err != nil {
		t.Fatalf("GetJob returned error: %v", err)
	}
	if got, want := cachedSnapshot.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("cached State = %q, want %q", got, want)
	}
	if !cachedSnapshot.FromCache {
		t.Fatal("cached FromCache = false, want true")
	}

	cachedResult, ok := cachedSnapshot.Result.Payload.(contracts.ProcessStudyCommandResult)
	if !ok {
		t.Fatalf("cached Result.Payload type = %T, want contracts.ProcessStudyCommandResult", cachedSnapshot.Result.Payload)
	}
	if got, want := cachedResult.PreviewPath, result.PreviewPath; got != want {
		t.Fatalf("cached PreviewPath = %q, want %q", got, want)
	}
	if got, want := cachedResult.DicomPath, result.DicomPath; got != want {
		t.Fatalf("cached DicomPath = %q, want %q", got, want)
	}
}

func TestStartProcessJobUsesCacheAcrossOutputPaths(t *testing.T) {
	studyRegistry, study := registerTestStudy(t)
	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	sourceStudy := syntheticAnalyzeSourceStudy()
	sourceStudy.Metadata = dicommeta.SourceMetadata{StudyInstanceUID: "1.2.3.4.5"}
	service := newService(
		cacheStore,
		studyRegistry,
		nil,
		dicomexport.GoWriter{},
		func() (studyDecoder, error) { return staticDecoder{study: sourceStudy}, nil },
		sequenceJobIDs("job-1", "job-2"),
	)

	firstOutputPath := filepath.Join(t.TempDir(), "first-output.dcm")
	firstStarted, err := service.StartProcessJob(contracts.ProcessStudyCommand{
		StudyID:  study.StudyID,
		PresetID: "xray",
		OutputPath: func() *string {
			value := firstOutputPath
			return &value
		}(),
	})
	if err != nil {
		t.Fatalf("first StartProcessJob returned error: %v", err)
	}

	firstSnapshot := waitForTerminalJob(t, service, firstStarted.JobID)
	if got, want := firstSnapshot.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("first State = %q, want %q", got, want)
	}

	firstResult, ok := firstSnapshot.Result.Payload.(contracts.ProcessStudyCommandResult)
	if !ok {
		t.Fatalf("first Result.Payload type = %T, want contracts.ProcessStudyCommandResult", firstSnapshot.Result.Payload)
	}

	secondOutputPath := filepath.Join(t.TempDir(), "second-output.dcm")
	secondStarted, err := service.StartProcessJob(contracts.ProcessStudyCommand{
		StudyID:  study.StudyID,
		PresetID: "xray",
		OutputPath: func() *string {
			value := secondOutputPath
			return &value
		}(),
	})
	if err != nil {
		t.Fatalf("second StartProcessJob returned error: %v", err)
	}

	secondSnapshot, err := service.GetJob(contracts.JobCommand{JobID: secondStarted.JobID})
	if err != nil {
		t.Fatalf("GetJob returned error: %v", err)
	}
	if got, want := secondSnapshot.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("second State = %q, want %q", got, want)
	}
	if !secondSnapshot.FromCache {
		t.Fatal("second FromCache = false, want true")
	}

	secondResult, ok := secondSnapshot.Result.Payload.(contracts.ProcessStudyCommandResult)
	if !ok {
		t.Fatalf("second Result.Payload type = %T, want contracts.ProcessStudyCommandResult", secondSnapshot.Result.Payload)
	}
	if got, want := secondResult.DicomPath, firstResult.DicomPath; got != want {
		t.Fatalf("second DicomPath = %q, want reused %q", got, want)
	}
	if _, err := os.Stat(secondOutputPath); !os.IsNotExist(err) {
		t.Fatalf("second output path unexpectedly exists, err = %v", err)
	}
}

func TestStartProcessJobDeduplicatesActiveStudyProcessing(t *testing.T) {
	studyRegistry, study := registerTestStudy(t)
	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	decoder := &blockingDecoder{
		started: make(chan struct{}, 1),
	}
	service := newService(
		cacheStore,
		studyRegistry,
		nil,
		dicomexport.GoWriter{},
		func() (studyDecoder, error) { return decoder, nil },
		sequenceJobIDs("job-1"),
	)

	first, err := service.StartProcessJob(contracts.ProcessStudyCommand{
		StudyID:  study.StudyID,
		PresetID: "default",
	})
	if err != nil {
		t.Fatalf("StartProcessJob returned error: %v", err)
	}
	second, err := service.StartProcessJob(contracts.ProcessStudyCommand{
		StudyID:  study.StudyID,
		PresetID: "default",
	})
	if err != nil {
		t.Fatalf("second StartProcessJob returned error: %v", err)
	}

	if got, want := second.JobID, first.JobID; got != want {
		t.Fatalf("second JobID = %q, want deduped %q", got, want)
	}

	<-decoder.started

	cancelled, err := service.CancelJob(contracts.JobCommand{JobID: first.JobID})
	if err != nil {
		t.Fatalf("CancelJob returned error: %v", err)
	}
	if got, want := cancelled.State, contracts.JobStateCancelling; got != want {
		t.Fatalf("CancelJob state = %q, want %q", got, want)
	}

	snapshot := waitForTerminalJob(t, service, first.JobID)
	if got, want := snapshot.State, contracts.JobStateCancelled; got != want {
		t.Fatalf("terminal State = %q, want %q", got, want)
	}
	if snapshot.Result != nil {
		t.Fatalf("Result = %#v, want nil for cancelled job", snapshot.Result)
	}
}

func TestStartAnalyzeJobWritesPreviewAndServesCachedSnapshot(t *testing.T) {
	studyRegistry, study := registerTestStudy(t)
	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	sourceStudy := syntheticAnalyzeSourceStudy()
	service := newService(
		cacheStore,
		studyRegistry,
		nil,
		dicomexport.GoWriter{},
		func() (studyDecoder, error) { return staticDecoder{study: sourceStudy}, nil },
		sequenceJobIDs("job-1", "job-2"),
	)

	started, err := service.StartAnalyzeJob(contracts.AnalyzeStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatalf("StartAnalyzeJob returned error: %v", err)
	}

	snapshot := waitForTerminalJob(t, service, started.JobID)
	if got, want := snapshot.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}
	if snapshot.FromCache {
		t.Fatal("FromCache = true, want false for first analyze job")
	}
	if snapshot.Result == nil {
		t.Fatal("Result = nil, want completed analyze payload")
	}
	if got, want := snapshot.Result.Kind, contracts.JobKindAnalyzeStudy; got != want {
		t.Fatalf("Result.Kind = %q, want %q", got, want)
	}

	result, ok := snapshot.Result.Payload.(contracts.AnalyzeStudyCommandResult)
	if !ok {
		t.Fatalf("Result.Payload type = %T, want contracts.AnalyzeStudyCommandResult", snapshot.Result.Payload)
	}
	if got, want := result.StudyID, study.StudyID; got != want {
		t.Fatalf("Result.StudyID = %q, want %q", got, want)
	}
	if !stringsHasPathPrefix(result.PreviewPath, filepath.Join(cacheStore.RootDir(), "artifacts", "analyze")) {
		t.Fatalf("PreviewPath = %q, want cache/artifacts/analyze prefix", result.PreviewPath)
	}
	if info, err := os.Stat(result.PreviewPath); err != nil || info.IsDir() {
		t.Fatalf("preview artifact missing or invalid: %v", err)
	}
	if result.Analysis.Tooth == nil {
		t.Fatal("Analysis.Tooth = nil, want detected synthetic candidate")
	}
	if len(result.Analysis.Teeth) == 0 {
		t.Fatal("len(Analysis.Teeth) = 0, want detected teeth")
	}
	if result.Analysis.Calibration.MeasurementScale == nil {
		t.Fatal("Analysis.Calibration.MeasurementScale = nil, want decoded scale")
	}
	if !result.Analysis.Calibration.RealWorldMeasurementsAvailable {
		t.Fatal("Analysis.Calibration.RealWorldMeasurementsAvailable = false, want true")
	}
	if got, want := len(result.SuggestedAnnotations.Lines), len(result.Analysis.Teeth)*2; got != want {
		t.Fatalf("len(SuggestedAnnotations.Lines) = %d, want %d", got, want)
	}
	if got, want := len(result.SuggestedAnnotations.Rectangles), len(result.Analysis.Teeth); got != want {
		t.Fatalf("len(SuggestedAnnotations.Rectangles) = %d, want %d", got, want)
	}

	cachedStarted, err := service.StartAnalyzeJob(contracts.AnalyzeStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatalf("cached StartAnalyzeJob returned error: %v", err)
	}
	if got, want := cachedStarted.JobID, "job-2"; got != want {
		t.Fatalf("cached JobID = %q, want %q", got, want)
	}

	cachedSnapshot, err := service.GetJob(contracts.JobCommand{JobID: cachedStarted.JobID})
	if err != nil {
		t.Fatalf("GetJob returned error: %v", err)
	}
	if got, want := cachedSnapshot.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("cached State = %q, want %q", got, want)
	}
	if !cachedSnapshot.FromCache {
		t.Fatal("cached FromCache = false, want true")
	}

	cachedResult, ok := cachedSnapshot.Result.Payload.(contracts.AnalyzeStudyCommandResult)
	if !ok {
		t.Fatalf("cached Result.Payload type = %T, want contracts.AnalyzeStudyCommandResult", cachedSnapshot.Result.Payload)
	}
	if got, want := cachedResult.PreviewPath, result.PreviewPath; got != want {
		t.Fatalf("cached PreviewPath = %q, want %q", got, want)
	}
	if got, want := len(cachedResult.SuggestedAnnotations.Lines), len(result.SuggestedAnnotations.Lines); got != want {
		t.Fatalf("cached len(SuggestedAnnotations.Lines) = %d, want %d", got, want)
	}
}

func TestStartAnalyzeJobDeduplicatesActiveStudyAnalysis(t *testing.T) {
	studyRegistry, study := registerTestStudy(t)
	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	decoder := &blockingDecoder{
		started: make(chan struct{}, 1),
	}
	service := newService(
		cacheStore,
		studyRegistry,
		nil,
		dicomexport.GoWriter{},
		func() (studyDecoder, error) { return decoder, nil },
		sequenceJobIDs("job-1"),
	)

	first, err := service.StartAnalyzeJob(contracts.AnalyzeStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatalf("StartAnalyzeJob returned error: %v", err)
	}
	second, err := service.StartAnalyzeJob(contracts.AnalyzeStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatalf("second StartAnalyzeJob returned error: %v", err)
	}

	if got, want := second.JobID, first.JobID; got != want {
		t.Fatalf("second JobID = %q, want deduped %q", got, want)
	}

	<-decoder.started

	cancelled, err := service.CancelJob(contracts.JobCommand{JobID: first.JobID})
	if err != nil {
		t.Fatalf("CancelJob returned error: %v", err)
	}
	if got, want := cancelled.State, contracts.JobStateCancelling; got != want {
		t.Fatalf("CancelJob state = %q, want %q", got, want)
	}

	snapshot := waitForTerminalJob(t, service, first.JobID)
	if got, want := snapshot.State, contracts.JobStateCancelled; got != want {
		t.Fatalf("terminal State = %q, want %q", got, want)
	}
	if snapshot.Result != nil {
		t.Fatalf("Result = %#v, want nil for cancelled job", snapshot.Result)
	}
}

func TestAnalyzeJobSkipsDecodeWhenSourcePreviewAndScaleCached(t *testing.T) {
	studyRegistry, study := registerTestStudy(t)
	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	sourceStudy := syntheticAnalyzeSourceStudy()
	decoder := &countingServiceDecoder{study: sourceStudy}
	service := newService(
		cacheStore,
		studyRegistry,
		nil,
		dicomexport.GoWriter{},
		func() (studyDecoder, error) { return decoder, nil },
		sequenceJobIDs("job-1"),
	)

	preview := render.RenderSourceImage(sourceStudy.Image, render.DefaultRenderPlan())
	service.memoryCache.StoreSourcePreview(study.InputPath, preview)
	service.memoryCache.StoreMeasurementScale(study.InputPath, sourceStudy.MeasurementScale)

	started, err := service.StartAnalyzeJob(contracts.AnalyzeStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatalf("StartAnalyzeJob returned error: %v", err)
	}

	snapshot := waitForTerminalJob(t, service, started.JobID)
	if got, want := snapshot.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}

	if got := decoder.CallCount(); got != 0 {
		t.Fatalf("DecodeStudy calls = %d, want 0 (should skip decode on cache hit)", got)
	}

	result, ok := snapshot.Result.Payload.(contracts.AnalyzeStudyCommandResult)
	if !ok {
		t.Fatalf("Result.Payload type = %T, want contracts.AnalyzeStudyCommandResult", snapshot.Result.Payload)
	}
	if result.Analysis.Tooth == nil {
		t.Fatal("Analysis.Tooth = nil, want detected synthetic candidate")
	}
	if result.Analysis.Calibration.MeasurementScale == nil {
		t.Fatal("Analysis.Calibration.MeasurementScale = nil, want cached scale")
	}
	if !result.Analysis.Calibration.RealWorldMeasurementsAvailable {
		t.Fatal("Analysis.Calibration.RealWorldMeasurementsAvailable = false, want true")
	}
}

func TestRenderThenAnalyzeSkipsDecodeOnEvictedDecodeCache(t *testing.T) {
	studyRegistry := studies.New()
	studyDir := t.TempDir()
	inputPaths := make([]string, 6)
	studyRecords := make([]contracts.StudyRecord, 6)
	for i := range inputPaths {
		inputPaths[i] = filepath.Join(studyDir, fmt.Sprintf("study-%d.dcm", i))
		if err := os.WriteFile(inputPaths[i], []byte("dicom"), 0o644); err != nil {
			t.Fatalf("WriteFile returned error: %v", err)
		}
		study, err := studyRegistry.Register(inputPaths[i], nil)
		if err != nil {
			t.Fatalf("Register returned error: %v", err)
		}
		studyRecords[i] = study
	}

	sourceStudy := syntheticAnalyzeSourceStudy()
	decoder := &countingServiceDecoder{study: sourceStudy}
	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	jobSeq := 0
	service := newService(
		cacheStore,
		studyRegistry,
		nil,
		dicomexport.GoWriter{},
		func() (studyDecoder, error) { return decoder, nil },
		func() (string, error) {
			jobSeq++
			return fmt.Sprintf("job-%d", jobSeq), nil
		},
	)

	renderStarted, err := service.StartRenderJob(contracts.RenderStudyCommand{StudyID: studyRecords[0].StudyID})
	if err != nil {
		t.Fatalf("StartRenderJob returned error: %v", err)
	}
	renderSnapshot := waitForTerminalJob(t, service, renderStarted.JobID)
	if got, want := renderSnapshot.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("render State = %q, want %q", got, want)
	}
	decodesAfterRender := decoder.CallCount()

	for i := 1; i <= 5; i++ {
		started, err := service.StartRenderJob(contracts.RenderStudyCommand{StudyID: studyRecords[i].StudyID})
		if err != nil {
			t.Fatalf("StartRenderJob[%d] error: %v", i, err)
		}
		snap := waitForTerminalJob(t, service, started.JobID)
		if snap.State != contracts.JobStateCompleted {
			t.Fatalf("render[%d] State = %q", i, snap.State)
		}
	}

	analyzeStarted, err := service.StartAnalyzeJob(contracts.AnalyzeStudyCommand{StudyID: studyRecords[0].StudyID})
	if err != nil {
		t.Fatalf("StartAnalyzeJob returned error: %v", err)
	}
	analyzeSnapshot := waitForTerminalJob(t, service, analyzeStarted.JobID)
	if got, want := analyzeSnapshot.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("analyze State = %q, want %q", got, want)
	}

	if got := decoder.CallCount(); got != decodesAfterRender+5 {
		t.Fatalf("DecodeStudy calls = %d, want %d (analyze should skip decode, 5 more renders fill decode cache)",
			got, decodesAfterRender+5)
	}

	result, ok := analyzeSnapshot.Result.Payload.(contracts.AnalyzeStudyCommandResult)
	if !ok {
		t.Fatalf("Result.Payload type = %T, want contracts.AnalyzeStudyCommandResult", analyzeSnapshot.Result.Payload)
	}
	if result.Analysis.Tooth == nil {
		t.Fatal("Analysis.Tooth = nil, want detected candidate")
	}
}

func TestWorkflowReusesDecodeAndSourcePreviewAcrossJobs(t *testing.T) {
	studyRegistry, study := registerTestStudy(t)
	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	sourceStudy := syntheticAnalyzeSourceStudy()
	sourceStudy.Metadata = dicommeta.SourceMetadata{StudyInstanceUID: "1.2.3.4.5"}
	decoder := &countingServiceDecoder{study: sourceStudy}
	service := newService(
		cacheStore,
		studyRegistry,
		nil,
		dicomexport.GoWriter{},
		func() (studyDecoder, error) { return decoder, nil },
		sequenceJobIDs("job-1", "job-2", "job-3"),
	)

	var renderMu sync.Mutex
	renderCalls := 0
	service.renderSourcePreview = func(
		source imaging.SourceImage,
		plan render.RenderPlan,
	) imaging.PreviewImage {
		renderMu.Lock()
		renderCalls++
		renderMu.Unlock()
		return render.RenderSourceImage(source, plan)
	}

	renderStarted, err := service.StartRenderJob(contracts.RenderStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatalf("StartRenderJob returned error: %v", err)
	}
	renderSnapshot := waitForTerminalJob(t, service, renderStarted.JobID)
	if got, want := renderSnapshot.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("render State = %q, want %q", got, want)
	}

	processStarted, err := service.StartProcessJob(contracts.ProcessStudyCommand{
		StudyID:  study.StudyID,
		PresetID: "xray",
	})
	if err != nil {
		t.Fatalf("StartProcessJob returned error: %v", err)
	}
	processSnapshot := waitForTerminalJob(t, service, processStarted.JobID)
	if got, want := processSnapshot.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("process State = %q, want %q", got, want)
	}

	analyzeStarted, err := service.StartAnalyzeJob(contracts.AnalyzeStudyCommand{StudyID: study.StudyID})
	if err != nil {
		t.Fatalf("StartAnalyzeJob returned error: %v", err)
	}
	analyzeSnapshot := waitForTerminalJob(t, service, analyzeStarted.JobID)
	if got, want := analyzeSnapshot.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("analyze State = %q, want %q", got, want)
	}

	if got, want := decoder.CallCount(), 1; got != want {
		t.Fatalf("DecodeStudy calls = %d, want %d", got, want)
	}

	renderMu.Lock()
	defer renderMu.Unlock()
	if got, want := renderCalls, 1; got != want {
		t.Fatalf("RenderSourceImage calls = %d, want %d", got, want)
	}
}

func TestStartRenderJobBoundsConcurrentExecutions(t *testing.T) {
	studyRegistry := studies.New()
	studyDir := t.TempDir()
	studiesUnderTest := make([]contracts.StudyRecord, 0, maxConcurrentJobs+2)
	for index := 0; index < maxConcurrentJobs+2; index++ {
		inputPath := filepath.Join(studyDir, fmt.Sprintf("study-%d.dcm", index))
		if err := os.WriteFile(inputPath, []byte("dicom"), 0o644); err != nil {
			t.Fatalf("WriteFile returned error: %v", err)
		}

		study, err := studyRegistry.Register(inputPath, nil)
		if err != nil {
			t.Fatalf("Register returned error: %v", err)
		}
		studiesUnderTest = append(studiesUnderTest, study)
	}

	release := make(chan struct{})
	decoder := &concurrencyTrackingDecoder{
		study:   syntheticAnalyzeSourceStudy(),
		release: release,
		started: make(chan struct{}, len(studiesUnderTest)),
	}
	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	service := newService(
		cacheStore,
		studyRegistry,
		nil,
		dicomexport.GoWriter{},
		func() (studyDecoder, error) { return decoder, nil },
		sequenceJobIDs("job-1", "job-2", "job-3", "job-4", "job-5"),
	)

	jobIDs := make([]string, 0, len(studiesUnderTest))
	for _, study := range studiesUnderTest {
		started, err := service.StartRenderJob(contracts.RenderStudyCommand{StudyID: study.StudyID})
		if err != nil {
			t.Fatalf("StartRenderJob returned error: %v", err)
		}
		jobIDs = append(jobIDs, started.JobID)
	}

	for index := 0; index < maxConcurrentJobs; index++ {
		select {
		case <-decoder.started:
		case <-time.After(2 * time.Second):
			t.Fatalf("expected %d running decodes before timeout", maxConcurrentJobs)
		}
	}

	select {
	case <-decoder.started:
		t.Fatal("observed more than max concurrent decodes before releasing the semaphore")
	case <-time.After(150 * time.Millisecond):
	}

	close(release)

	for _, jobID := range jobIDs {
		snapshot := waitForTerminalJob(t, service, jobID)
		if got, want := snapshot.State, contracts.JobStateCompleted; got != want {
			t.Fatalf("job %s State = %q, want %q", jobID, got, want)
		}
	}

	if got, want := decoder.MaxActive(), maxConcurrentJobs; got != want {
		t.Fatalf("max active decodes = %d, want %d", got, want)
	}
}

func TestServiceRejectsBlankIdentifiers(t *testing.T) {
	service := newService(
		cache.New(filepath.Join(t.TempDir(), "cache")),
		studies.New(),
		nil,
		dicomexport.GoWriter{},
		nil,
		sequenceJobIDs("job-1"),
	)

	tests := []struct {
		name        string
		run         func(*Service) error
		wantMessage string
	}{
		{
			name: "render study id",
			run: func(service *Service) error {
				_, err := service.StartRenderJob(contracts.RenderStudyCommand{StudyID: " \t"})
				return err
			},
			wantMessage: "studyId is required",
		},
		{
			name: "process study id",
			run: func(service *Service) error {
				_, err := service.StartProcessJob(contracts.ProcessStudyCommand{
					StudyID:  " ",
					PresetID: "default",
				})
				return err
			},
			wantMessage: "studyId is required",
		},
		{
			name: "analyze study id",
			run: func(service *Service) error {
				_, err := service.StartAnalyzeJob(contracts.AnalyzeStudyCommand{StudyID: "\n"})
				return err
			},
			wantMessage: "studyId is required",
		},
		{
			name: "get job id",
			run: func(service *Service) error {
				_, err := service.GetJob(contracts.JobCommand{JobID: "  "})
				return err
			},
			wantMessage: "jobId is required",
		},
		{
			name: "cancel job id",
			run: func(service *Service) error {
				_, err := service.CancelJob(contracts.JobCommand{JobID: "\t"})
				return err
			},
			wantMessage: "jobId is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.run(service)
			if err == nil {
				t.Fatal("returned nil error, want invalid input error")
			}

			backendErr, ok := err.(contracts.BackendError)
			if !ok {
				t.Fatalf("error type = %T, want contracts.BackendError", err)
			}
			if got, want := backendErr.Code, contracts.BackendErrorCodeInvalidInput; got != want {
				t.Fatalf("error code = %q, want %q", got, want)
			}
			if got, want := backendErr.Message, test.wantMessage; got != want {
				t.Fatalf("error message = %q, want %q", got, want)
			}
		})
	}
}

func TestStartProcessJobRejectsInvalidOutputPath(t *testing.T) {
	tests := []struct {
		name        string
		outputPath  *string
		wantCode    contracts.BackendErrorCode
		wantMessage string
	}{
		{
			name:        "blank path",
			outputPath:  stringPointer("   "),
			wantCode:    contracts.BackendErrorCodeInvalidInput,
			wantMessage: "outputPath is required when provided",
		},
		{
			name:        "dot path",
			outputPath:  stringPointer("."),
			wantCode:    contracts.BackendErrorCodeInvalidInput,
			wantMessage: "outputPath is required when provided",
		},
		{
			name:        "existing directory",
			outputPath:  stringPointer(t.TempDir()),
			wantCode:    contracts.BackendErrorCodeInvalidInput,
			wantMessage: "output path must be a file",
		},
		{
			name:        "missing parent directory",
			outputPath:  stringPointer(filepath.Join(t.TempDir(), "missing", "output.dcm")),
			wantCode:    contracts.BackendErrorCodeNotFound,
			wantMessage: "output directory does not exist",
		},
		{
			name: "parent is file",
			outputPath: func() *string {
				parentFile := filepath.Join(t.TempDir(), "not-a-directory")
				return stringPointer(filepath.Join(parentFile, "output.dcm"))
			}(),
			wantCode:    contracts.BackendErrorCodeInvalidInput,
			wantMessage: "output directory must be a directory",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			studyRegistry, study := registerTestStudy(t)
			cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
			service := newService(
				cacheStore,
				studyRegistry,
				nil,
				dicomexport.GoWriter{},
				func() (studyDecoder, error) { return staticDecoder{study: syntheticAnalyzeSourceStudy()}, nil },
				sequenceJobIDs("job-1"),
			)

			if test.name == "parent is file" {
				parent := filepath.Dir(*test.outputPath)
				if err := os.WriteFile(parent, []byte("file"), 0o644); err != nil {
					t.Fatalf("WriteFile returned error: %v", err)
				}
			}

			_, err := service.StartProcessJob(contracts.ProcessStudyCommand{
				StudyID:    study.StudyID,
				OutputPath: test.outputPath,
				PresetID:   "default",
			})
			if err == nil {
				t.Fatal("StartProcessJob returned nil error, want path validation error")
			}

			backendErr, ok := err.(contracts.BackendError)
			if !ok {
				t.Fatalf("error type = %T, want contracts.BackendError", err)
			}
			if got, want := backendErr.Code, test.wantCode; got != want {
				t.Fatalf("error code = %q, want %q", got, want)
			}
			if got := backendErr.Message; !strings.HasPrefix(got, test.wantMessage) {
				t.Fatalf("error message = %q, want prefix %q", backendErr.Message, test.wantMessage)
			}
		})
	}
}

func TestStartJobsFailWhenRegisteredInputFileDisappears(t *testing.T) {
	tests := []struct {
		name  string
		start func(*Service, string) (contracts.StartedJob, error)
	}{
		{
			name: "render",
			start: func(service *Service, studyID string) (contracts.StartedJob, error) {
				return service.StartRenderJob(contracts.RenderStudyCommand{StudyID: studyID})
			},
		},
		{
			name: "process",
			start: func(service *Service, studyID string) (contracts.StartedJob, error) {
				return service.StartProcessJob(contracts.ProcessStudyCommand{
					StudyID:  studyID,
					PresetID: "default",
				})
			},
		},
		{
			name: "analyze",
			start: func(service *Service, studyID string) (contracts.StartedJob, error) {
				return service.StartAnalyzeJob(contracts.AnalyzeStudyCommand{StudyID: studyID})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			studyRegistry, study := registerTestStudy(t)
			if err := os.Remove(study.InputPath); err != nil {
				t.Fatalf("Remove returned error: %v", err)
			}

			service := newService(
				cache.New(filepath.Join(t.TempDir(), "cache")),
				studyRegistry,
				nil,
				dicomexport.GoWriter{},
				func() (studyDecoder, error) { return staticDecoder{study: syntheticAnalyzeSourceStudy()}, nil },
				sequenceJobIDs("job-1"),
			)

			started, err := test.start(service, study.StudyID)
			if err != nil {
				t.Fatalf("start returned error: %v", err)
			}

			snapshot := waitForTerminalJob(t, service, started.JobID)
			if got, want := snapshot.State, contracts.JobStateFailed; got != want {
				t.Fatalf("State = %q, want %q", got, want)
			}
			if snapshot.Result != nil {
				t.Fatalf("Result = %#v, want nil", snapshot.Result)
			}
			if snapshot.Error == nil {
				t.Fatal("Error = nil, want backend error payload")
			}
			if got, want := snapshot.Error.Code, contracts.BackendErrorCodeNotFound; got != want {
				t.Fatalf("Error.Code = %q, want %q", got, want)
			}
			if got, want := snapshot.Error.Message, fmt.Sprintf("input file does not exist: %s", study.InputPath); got != want {
				t.Fatalf("Error.Message = %q, want %q", got, want)
			}
		})
	}
}

func TestRenderAndAnalyzeJobsRemovePreviewArtifactWhenPreviewWriteFails(t *testing.T) {
	tests := []struct {
		name        string
		namespace   string
		start       func(*Service, string) (contracts.StartedJob, error)
		fingerprint func(contracts.StudyRecord) (string, error)
		wantMessage string
	}{
		{
			name:      "render",
			namespace: "render",
			start: func(service *Service, studyID string) (contracts.StartedJob, error) {
				return service.StartRenderJob(contracts.RenderStudyCommand{StudyID: studyID})
			},
			fingerprint: renderFingerprint,
			wantMessage: "write preview PNG",
		},
		{
			name:      "analyze",
			namespace: "analyze",
			start: func(service *Service, studyID string) (contracts.StartedJob, error) {
				return service.StartAnalyzeJob(contracts.AnalyzeStudyCommand{StudyID: studyID})
			},
			fingerprint: analyzeFingerprint,
			wantMessage: "write analysis preview PNG",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			studyRegistry, study := registerTestStudy(t)
			cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
			service := newService(
				cacheStore,
				studyRegistry,
				nil,
				dicomexport.GoWriter{},
				func() (studyDecoder, error) { return staticDecoder{study: syntheticAnalyzeSourceStudy()}, nil },
				sequenceJobIDs("job-1"),
			)
			service.renderSourcePreview = func(
				imaging.SourceImage,
				render.RenderPlan,
			) imaging.PreviewImage {
				return imaging.PreviewImage{
					Width:  1,
					Height: 1,
					Format: imaging.FormatGray8,
				}
			}

			fingerprint, err := test.fingerprint(study)
			if err != nil {
				t.Fatalf("fingerprint returned error: %v", err)
			}
			previewPath, err := cacheStore.ArtifactPath(test.namespace, fingerprint, "png")
			if err != nil {
				t.Fatalf("ArtifactPath returned error: %v", err)
			}

			started, err := test.start(service, study.StudyID)
			if err != nil {
				t.Fatalf("start returned error: %v", err)
			}

			snapshot := waitForTerminalJob(t, service, started.JobID)
			if got, want := snapshot.State, contracts.JobStateFailed; got != want {
				t.Fatalf("State = %q, want %q", got, want)
			}
			if snapshot.Result != nil {
				t.Fatalf("Result = %#v, want nil on preview write failure", snapshot.Result)
			}
			if snapshot.Error == nil {
				t.Fatal("Error = nil, want backend error payload")
			}
			if got, want := snapshot.Error.Code, contracts.BackendErrorCodeInternal; got != want {
				t.Fatalf("Error.Code = %q, want %q", got, want)
			}
			if !strings.Contains(snapshot.Error.Message, test.wantMessage) {
				t.Fatalf("Error.Message = %q, want substring %q", snapshot.Error.Message, test.wantMessage)
			}
			if _, err := os.Stat(previewPath); !os.IsNotExist(err) {
				t.Fatalf("preview artifact unexpectedly remained, err = %v", err)
			}
		})
	}
}

func TestStartProcessJobWriterFailureCleansUpArtifactsAndDoesNotCacheFailure(t *testing.T) {
	studyRegistry, study := registerTestStudy(t)
	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	service := newService(
		cacheStore,
		studyRegistry,
		nil,
		partialFailingSecondaryCaptureWriter{err: fmt.Errorf("disk full")},
		func() (studyDecoder, error) { return staticDecoder{study: syntheticAnalyzeSourceStudy()}, nil },
		sequenceJobIDs("job-1", "job-2"),
	)

	outputPath := filepath.Join(t.TempDir(), "processed-output.dcm")
	command := contracts.ProcessStudyCommand{
		StudyID:    study.StudyID,
		PresetID:   "default",
		OutputPath: stringPointer(outputPath),
	}

	fingerprint, err := processFingerprint(study, command)
	if err != nil {
		t.Fatalf("processFingerprint returned error: %v", err)
	}
	previewPath, err := cacheStore.ArtifactPath("process", fingerprint, "png")
	if err != nil {
		t.Fatalf("ArtifactPath returned error: %v", err)
	}

	started, err := service.StartProcessJob(command)
	if err != nil {
		t.Fatalf("StartProcessJob returned error: %v", err)
	}

	failed := waitForTerminalJob(t, service, started.JobID)
	if got, want := failed.State, contracts.JobStateFailed; got != want {
		t.Fatalf("failed State = %q, want %q", got, want)
	}
	if failed.Result != nil {
		t.Fatalf("failed Result = %#v, want nil", failed.Result)
	}
	if failed.Error == nil {
		t.Fatal("failed Error = nil, want backend error payload")
	}
	if got, want := failed.Error.Code, contracts.BackendErrorCodeInternal; got != want {
		t.Fatalf("failed Error.Code = %q, want %q", got, want)
	}
	if !strings.Contains(failed.Error.Message, "write processed DICOM: disk full") {
		t.Fatalf("failed Error.Message = %q, want wrapped writer error", failed.Error.Message)
	}
	for _, path := range []string{previewPath, outputPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("artifact %q unexpectedly remained after failure, err = %v", path, err)
		}
	}

	service.secondaryCaptureWriter = successfulSecondaryCaptureWriter{}

	restarted, err := service.StartProcessJob(command)
	if err != nil {
		t.Fatalf("second StartProcessJob returned error: %v", err)
	}
	if got, want := restarted.JobID, "job-2"; got != want {
		t.Fatalf("second JobID = %q, want %q", got, want)
	}

	completed := waitForTerminalJob(t, service, restarted.JobID)
	if got, want := completed.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("completed State = %q, want %q", got, want)
	}
	if completed.FromCache {
		t.Fatal("completed FromCache = true, want false after prior failure")
	}
	if info, err := os.Stat(outputPath); err != nil || info.IsDir() {
		t.Fatalf("output artifact missing after successful retry: %v", err)
	}
}

func TestStartProcessJobCancellationDuringSecondaryCaptureWriteRemovesArtifacts(t *testing.T) {
	studyRegistry, study := registerTestStudy(t)
	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	writer := &blockingSecondaryCaptureWriter{started: make(chan struct{}, 1)}
	service := newService(
		cacheStore,
		studyRegistry,
		nil,
		writer,
		func() (studyDecoder, error) { return staticDecoder{study: syntheticAnalyzeSourceStudy()}, nil },
		sequenceJobIDs("job-1"),
	)

	outputPath := filepath.Join(t.TempDir(), "processed-output.dcm")
	command := contracts.ProcessStudyCommand{
		StudyID:    study.StudyID,
		PresetID:   "default",
		OutputPath: stringPointer(outputPath),
	}

	fingerprint, err := processFingerprint(study, command)
	if err != nil {
		t.Fatalf("processFingerprint returned error: %v", err)
	}
	previewPath, err := cacheStore.ArtifactPath("process", fingerprint, "png")
	if err != nil {
		t.Fatalf("ArtifactPath returned error: %v", err)
	}

	started, err := service.StartProcessJob(command)
	if err != nil {
		t.Fatalf("StartProcessJob returned error: %v", err)
	}

	select {
	case <-writer.started:
	case <-time.After(2 * time.Second):
		t.Fatal("secondary capture writer did not start before timeout")
	}

	for _, path := range []string{previewPath, outputPath} {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			t.Fatalf("artifact %q missing before cancellation cleanup, err = %v", path, err)
		}
	}

	cancelled, err := service.CancelJob(contracts.JobCommand{JobID: started.JobID})
	if err != nil {
		t.Fatalf("CancelJob returned error: %v", err)
	}
	if got, want := cancelled.State, contracts.JobStateCancelling; got != want {
		t.Fatalf("CancelJob State = %q, want %q", got, want)
	}

	snapshot := waitForTerminalJob(t, service, started.JobID)
	if got, want := snapshot.State, contracts.JobStateCancelled; got != want {
		t.Fatalf("terminal State = %q, want %q", got, want)
	}
	if snapshot.Result != nil {
		t.Fatalf("Result = %#v, want nil for cancelled job", snapshot.Result)
	}
	if snapshot.Error != nil {
		t.Fatalf("Error = %#v, want nil for cancelled job", snapshot.Error)
	}
	for _, path := range []string{previewPath, outputPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("artifact %q unexpectedly remained after cancellation, err = %v", path, err)
		}
	}
}

func registerTestStudy(t *testing.T) (*studies.Registry, contracts.StudyRecord) {
	t.Helper()

	inputPath := filepath.Join(t.TempDir(), "study.dcm")
	if err := os.WriteFile(inputPath, []byte("dicom"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	registry := studies.New()
	study, err := registry.Register(inputPath, nil)
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	return registry, study
}

func sequenceJobIDs(ids ...string) idGenerator {
	index := 0
	return func() (string, error) {
		if index >= len(ids) {
			return "", fmt.Errorf("no test job id available")
		}

		value := ids[index]
		index += 1
		return value, nil
	}
}

func waitForTerminalJob(t *testing.T, service *Service, jobID string) contracts.JobSnapshot {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := service.GetJob(contracts.JobCommand{JobID: jobID})
		if err != nil {
			t.Fatalf("GetJob returned error: %v", err)
		}
		if isTerminalState(snapshot.State) {
			return snapshot
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("job %s did not reach a terminal state before timeout", jobID)
	return contracts.JobSnapshot{}
}

func waitForCompletionCallback(
	t *testing.T,
	completions <-chan contracts.JobSnapshot,
) contracts.JobSnapshot {
	t.Helper()

	select {
	case snapshot := <-completions:
		return snapshot
	case <-time.After(5 * time.Second):
		t.Fatal("job completion callback did not fire before timeout")
		return contracts.JobSnapshot{}
	}
}

func stringsHasPathPrefix(path string, prefix string) bool {
	relative, err := filepath.Rel(prefix, path)
	return err == nil && relative != ".." && relative != "." && relative != ""
}

func syntheticAnalyzeSourceStudy() dicommeta.SourceStudy {
	const width = 240
	const height = 160

	pixels := make([]float32, width*height)
	for index := range pixels {
		pixels[index] = 24
	}

	fillSourceRect(pixels, width, 14, 24, 212, 106, 54)
	fillSourceRect(pixels, width, 38, 54, 34, 34, 174)
	fillSourceTriangleRoot(pixels, width, 38, 88, 62, 32, 174)

	fillSourceRect(pixels, width, 100, 42, 42, 38, 236)
	fillSourceTriangleRoot(pixels, width, 100, 80, 92, 54, 236)

	fillSourceRect(pixels, width, 172, 56, 28, 32, 160)
	fillSourceTriangleRoot(pixels, width, 172, 88, 50, 30, 160)

	return dicommeta.SourceStudy{
		Image: imaging.SourceImage{
			Width:    width,
			Height:   height,
			Format:   imaging.FormatGrayFloat32,
			Pixels:   pixels,
			MinValue: 0,
			MaxValue: 255,
		},
		MeasurementScale: &contracts.MeasurementScale{
			RowSpacingMM:    0.2,
			ColumnSpacingMM: 0.3,
			Source:          "PixelSpacing",
		},
	}
}

func fillSourceRect(
	pixels []float32,
	width uint32,
	x, y uint32,
	rectWidth, rectHeight uint32,
	value float32,
) {
	for yy := y; yy < y+rectHeight; yy++ {
		for xx := x; xx < x+rectWidth; xx++ {
			pixels[yy*width+xx] = value
		}
	}
}

func fillSourceTriangleRoot(
	pixels []float32,
	width uint32,
	x, y uint32,
	rootWidth, rootHeight uint32,
	value float32,
) {
	centerX := x + rootWidth/2
	for offset := uint32(0); offset < rootHeight; offset++ {
		rowY := y + offset
		span := rootWidth - (offset*rootWidth)/rootHeight
		halfSpan := span / 2
		startX := centerX - halfSpan
		endX := centerX + halfSpan
		for xx := startX; xx <= endX; xx++ {
			pixels[rowY*width+xx] = value
		}
	}
}

func stringPointer(value string) *string {
	return &value
}
