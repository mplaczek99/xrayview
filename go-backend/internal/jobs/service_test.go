package jobs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xrayview/go-backend/internal/cache"
	"xrayview/go-backend/internal/contracts"
	"xrayview/go-backend/internal/imaging"
	"xrayview/go-backend/internal/rustdecode"
	"xrayview/go-backend/internal/studies"
)

type staticDecoder struct {
	study rustdecode.SourceStudy
}

func (decoder staticDecoder) DecodeStudy(
	_ context.Context,
	_ string,
) (rustdecode.SourceStudy, error) {
	return decoder.study, nil
}

type blockingDecoder struct {
	study   rustdecode.SourceStudy
	started chan struct{}
}

func (decoder *blockingDecoder) DecodeStudy(
	ctx context.Context,
	_ string,
) (rustdecode.SourceStudy, error) {
	select {
	case decoder.started <- struct{}{}:
	default:
	}

	<-ctx.Done()
	return rustdecode.SourceStudy{}, ctx.Err()
}

func TestStartRenderJobWritesPreviewAndServesCachedSnapshot(t *testing.T) {
	studyRegistry, study := registerTestStudy(t)
	cacheStore := cache.New(filepath.Join(t.TempDir(), "cache"))
	sourceStudy := rustdecode.SourceStudy{
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
	sourceStudy := rustdecode.SourceStudy{
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

func stringsHasPathPrefix(path string, prefix string) bool {
	relative, err := filepath.Rel(prefix, path)
	return err == nil && relative != ".." && relative != "." && relative != ""
}
