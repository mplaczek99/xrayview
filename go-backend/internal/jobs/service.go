package jobs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"xrayview/go-backend/internal/analysis"
	"xrayview/go-backend/internal/annotations"
	"xrayview/go-backend/internal/cache"
	"xrayview/go-backend/internal/contracts"
	"xrayview/go-backend/internal/processing"
	"xrayview/go-backend/internal/render"
	"xrayview/go-backend/internal/rustdecode"
	"xrayview/go-backend/internal/studies"
)

type studyDecoder interface {
	DecodeStudy(context.Context, string) (rustdecode.SourceStudy, error)
}

type decodeHelperFactory func() (studyDecoder, error)
type idGenerator func() (string, error)

type Service struct {
	supportedKinds []contracts.JobKind
	cache          *cache.Store
	studies        *studies.Registry
	newDecoder     decodeHelperFactory
	memoryCache    *cache.Memory
	registry       *Registry
}

func New(cacheStore *cache.Store, studyRegistry *studies.Registry, logger *slog.Logger) *Service {
	return newService(
		cacheStore,
		studyRegistry,
		logger,
		func() (studyDecoder, error) {
			return rustdecode.NewFromEnvironment()
		},
		generateJobID,
	)
}

func (service *Service) SupportedKinds() []string {
	kinds := make([]string, 0, len(service.supportedKinds))
	for _, kind := range service.supportedKinds {
		kinds = append(kinds, string(kind))
	}

	return kinds
}

func (service *Service) StartRenderJob(
	command contracts.RenderStudyCommand,
) (contracts.StartedJob, error) {
	studyID := strings.TrimSpace(command.StudyID)
	if studyID == "" {
		return contracts.StartedJob{}, contracts.InvalidInput("studyId is required")
	}

	study, ok := service.studies.Get(studyID)
	if !ok {
		return contracts.StartedJob{}, contracts.NotFound(fmt.Sprintf("study not found: %s", studyID))
	}

	fingerprint, err := renderFingerprint(study)
	if err != nil {
		return contracts.StartedJob{}, contracts.Internal(
			fmt.Sprintf("serialize render job fingerprint: %v", err),
		)
	}

	if snapshot, ok, err := service.cachedRenderSnapshot(fingerprint, study.StudyID); err != nil {
		return contracts.StartedJob{}, err
	} else if ok {
		return contracts.StartedJob{JobID: snapshot.JobID}, nil
	}

	outcome, err := service.registry.StartJob(
		contracts.JobKindRenderStudy,
		study.StudyID,
		fingerprint,
	)
	if err != nil {
		return contracts.StartedJob{}, err
	}
	if !outcome.Created {
		return contracts.StartedJob{JobID: outcome.Snapshot.JobID}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := service.registry.AttachCancel(outcome.Snapshot.JobID, cancel); err != nil {
		cancel()
		return contracts.StartedJob{}, err
	}

	go service.executeRenderJob(ctx, outcome.Snapshot.JobID, study, fingerprint)

	return contracts.StartedJob{JobID: outcome.Snapshot.JobID}, nil
}

func (service *Service) StartProcessJob(
	command contracts.ProcessStudyCommand,
) (contracts.StartedJob, error) {
	studyID := strings.TrimSpace(command.StudyID)
	if studyID == "" {
		return contracts.StartedJob{}, contracts.InvalidInput("studyId is required")
	}

	study, ok := service.studies.Get(studyID)
	if !ok {
		return contracts.StartedJob{}, contracts.NotFound(fmt.Sprintf("study not found: %s", studyID))
	}

	fingerprint, err := processFingerprint(study, command)
	if err != nil {
		return contracts.StartedJob{}, contracts.Internal(
			fmt.Sprintf("serialize process job fingerprint: %v", err),
		)
	}

	if snapshot, ok, err := service.cachedProcessSnapshot(fingerprint, study.StudyID); err != nil {
		return contracts.StartedJob{}, err
	} else if ok {
		return contracts.StartedJob{JobID: snapshot.JobID}, nil
	}

	resolved, err := processing.ResolveProcessStudyCommand(command)
	if err != nil {
		return contracts.StartedJob{}, err
	}

	previewPath, err := service.cache.ArtifactPath("process", fingerprint, "png")
	if err != nil {
		return contracts.StartedJob{}, err
	}

	dicomPath, err := service.resolveProcessOutputPath(command.OutputPath, fingerprint)
	if err != nil {
		return contracts.StartedJob{}, err
	}

	outcome, err := service.registry.StartJob(
		contracts.JobKindProcessStudy,
		study.StudyID,
		fingerprint,
	)
	if err != nil {
		return contracts.StartedJob{}, err
	}
	if !outcome.Created {
		return contracts.StartedJob{JobID: outcome.Snapshot.JobID}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := service.registry.AttachCancel(outcome.Snapshot.JobID, cancel); err != nil {
		cancel()
		return contracts.StartedJob{}, err
	}

	go service.executeProcessJob(
		ctx,
		outcome.Snapshot.JobID,
		study,
		resolved,
		fingerprint,
		previewPath,
		dicomPath,
	)

	return contracts.StartedJob{JobID: outcome.Snapshot.JobID}, nil
}

func (service *Service) StartAnalyzeJob(
	command contracts.AnalyzeStudyCommand,
) (contracts.StartedJob, error) {
	studyID := strings.TrimSpace(command.StudyID)
	if studyID == "" {
		return contracts.StartedJob{}, contracts.InvalidInput("studyId is required")
	}

	study, ok := service.studies.Get(studyID)
	if !ok {
		return contracts.StartedJob{}, contracts.NotFound(fmt.Sprintf("study not found: %s", studyID))
	}

	fingerprint, err := analyzeFingerprint(study)
	if err != nil {
		return contracts.StartedJob{}, contracts.Internal(
			fmt.Sprintf("serialize analyze job fingerprint: %v", err),
		)
	}

	if snapshot, ok, err := service.cachedAnalyzeSnapshot(fingerprint, study.StudyID); err != nil {
		return contracts.StartedJob{}, err
	} else if ok {
		return contracts.StartedJob{JobID: snapshot.JobID}, nil
	}

	previewPath, err := service.cache.ArtifactPath("analyze", fingerprint, "png")
	if err != nil {
		return contracts.StartedJob{}, err
	}

	outcome, err := service.registry.StartJob(
		contracts.JobKindAnalyzeStudy,
		study.StudyID,
		fingerprint,
	)
	if err != nil {
		return contracts.StartedJob{}, err
	}
	if !outcome.Created {
		return contracts.StartedJob{JobID: outcome.Snapshot.JobID}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := service.registry.AttachCancel(outcome.Snapshot.JobID, cancel); err != nil {
		cancel()
		return contracts.StartedJob{}, err
	}

	go service.executeAnalyzeJob(ctx, outcome.Snapshot.JobID, study, fingerprint, previewPath)

	return contracts.StartedJob{JobID: outcome.Snapshot.JobID}, nil
}

func (service *Service) GetJob(command contracts.JobCommand) (contracts.JobSnapshot, error) {
	jobID := strings.TrimSpace(command.JobID)
	if jobID == "" {
		return contracts.JobSnapshot{}, contracts.InvalidInput("jobId is required")
	}

	return service.registry.Get(jobID)
}

func (service *Service) CancelJob(command contracts.JobCommand) (contracts.JobSnapshot, error) {
	jobID := strings.TrimSpace(command.JobID)
	if jobID == "" {
		return contracts.JobSnapshot{}, contracts.InvalidInput("jobId is required")
	}

	return service.registry.Cancel(jobID)
}

func newService(
	cacheStore *cache.Store,
	studyRegistry *studies.Registry,
	logger *slog.Logger,
	decoderFactory decodeHelperFactory,
	jobIDFactory idGenerator,
) *Service {
	if cacheStore == nil {
		cacheStore = cache.NewWithRoot(cache.DefaultRootDir())
	}
	if studyRegistry == nil {
		studyRegistry = studies.New()
	}
	if logger == nil {
		logger = slog.Default()
	}
	if decoderFactory == nil {
		decoderFactory = func() (studyDecoder, error) {
			return rustdecode.NewFromEnvironment()
		}
	}
	if jobIDFactory == nil {
		jobIDFactory = generateJobID
	}

	return &Service{
		supportedKinds: []contracts.JobKind{
			contracts.JobKindRenderStudy,
			contracts.JobKindProcessStudy,
			contracts.JobKindAnalyzeStudy,
		},
		cache:       cacheStore,
		studies:     studyRegistry,
		newDecoder:  decoderFactory,
		memoryCache: cache.NewMemory(logger),
		registry:    NewRegistry(jobIDFactory),
	}
}

func (service *Service) cachedRenderSnapshot(
	fingerprint string,
	studyID string,
) (contracts.JobSnapshot, bool, error) {
	cached, ok := service.memoryCache.LoadRender(fingerprint)
	if !ok {
		return contracts.JobSnapshot{}, false, nil
	}

	snapshot, err := service.registry.CreateCachedJob(
		contracts.JobKindRenderStudy,
		studyID,
		contracts.JobResult{
			Kind:    contracts.JobKindRenderStudy,
			Payload: cached,
		},
	)
	if err != nil {
		return contracts.JobSnapshot{}, false, err
	}

	return snapshot, true, nil
}

func (service *Service) cachedProcessSnapshot(
	fingerprint string,
	studyID string,
) (contracts.JobSnapshot, bool, error) {
	cached, ok := service.memoryCache.LoadProcess(fingerprint)
	if !ok {
		return contracts.JobSnapshot{}, false, nil
	}

	snapshot, err := service.registry.CreateCachedJob(
		contracts.JobKindProcessStudy,
		studyID,
		contracts.JobResult{
			Kind:    contracts.JobKindProcessStudy,
			Payload: cached,
		},
	)
	if err != nil {
		return contracts.JobSnapshot{}, false, err
	}

	return snapshot, true, nil
}

func (service *Service) cachedAnalyzeSnapshot(
	fingerprint string,
	studyID string,
) (contracts.JobSnapshot, bool, error) {
	cached, ok := service.memoryCache.LoadAnalyze(fingerprint)
	if !ok {
		return contracts.JobSnapshot{}, false, nil
	}

	snapshot, err := service.registry.CreateCachedJob(
		contracts.JobKindAnalyzeStudy,
		studyID,
		contracts.JobResult{
			Kind:    contracts.JobKindAnalyzeStudy,
			Payload: cached,
		},
	)
	if err != nil {
		return contracts.JobSnapshot{}, false, err
	}

	return snapshot, true, nil
}

func (service *Service) executeRenderJob(
	ctx context.Context,
	jobID string,
	study contracts.StudyRecord,
	fingerprint string,
) {
	if service.finishCancelledIfRequested(ctx, jobID, "queued", "") {
		return
	}

	if err := service.transitionJob(
		jobID,
		contracts.JobStateRunning,
		10,
		"validating",
		"Validating source study",
	); err != nil {
		service.failJob(jobID, err)
		return
	}

	if err := validateInputFile(study.InputPath); err != nil {
		service.failJob(jobID, err)
		return
	}
	if service.finishCancelledIfRequested(ctx, jobID, "validating", "") {
		return
	}

	if err := service.transitionJob(
		jobID,
		contracts.JobStateRunning,
		35,
		"loadingStudy",
		"Loading source study",
	); err != nil {
		service.failJob(jobID, err)
		return
	}

	decoder, err := service.newDecoder()
	if err != nil {
		service.failJob(
			jobID,
			contracts.Internal(fmt.Sprintf("configure rust decode helper: %v", err)),
		)
		return
	}

	sourceStudy, err := decoder.DecodeStudy(ctx, study.InputPath)
	if err != nil {
		if service.finishCancelledIfRequested(ctx, jobID, "loadingStudy", "") {
			return
		}
		service.failJob(jobID, contracts.Internal(fmt.Sprintf("load source study: %v", err)))
		return
	}
	if service.finishCancelledIfRequested(ctx, jobID, "loadingStudy", "") {
		return
	}

	if err := service.transitionJob(
		jobID,
		contracts.JobStateRunning,
		75,
		"renderingPreview",
		"Rendering preview",
	); err != nil {
		service.failJob(jobID, err)
		return
	}

	preview := render.RenderSourceImage(sourceStudy.Image, render.DefaultRenderPlan())
	if service.finishCancelledIfRequested(ctx, jobID, "renderingPreview", "") {
		return
	}

	if err := service.transitionJob(
		jobID,
		contracts.JobStateRunning,
		90,
		"writingPreview",
		"Writing preview",
	); err != nil {
		service.failJob(jobID, err)
		return
	}

	previewPath, err := service.cache.ArtifactPath("render", fingerprint, "png")
	if err != nil {
		service.failJob(jobID, err)
		return
	}
	if err := render.SavePreviewPNG(previewPath, preview); err != nil {
		service.failJob(jobID, contracts.Internal(fmt.Sprintf("write preview PNG: %v", err)))
		return
	}
	if service.finishCancelledIfRequested(ctx, jobID, "writingPreview", previewPath) {
		return
	}

	service.completeRenderJob(
		jobID,
		fingerprint,
		contracts.RenderStudyCommandResult{
			StudyID:          study.StudyID,
			PreviewPath:      previewPath,
			LoadedWidth:      sourceStudy.Image.Width,
			LoadedHeight:     sourceStudy.Image.Height,
			MeasurementScale: cloneMeasurementScale(sourceStudy.MeasurementScale),
		},
	)
}

func (service *Service) executeProcessJob(
	ctx context.Context,
	jobID string,
	study contracts.StudyRecord,
	resolved processing.ResolvedProcessStudy,
	fingerprint string,
	previewPath string,
	dicomPath string,
) {
	if service.finishCancelledIfRequested(ctx, jobID, "queued", previewPath) {
		return
	}

	if err := service.transitionJob(
		jobID,
		contracts.JobStateRunning,
		10,
		"validating",
		"Validating processing request",
	); err != nil {
		service.failJob(jobID, err)
		return
	}

	if err := validateInputFile(study.InputPath); err != nil {
		service.failJob(jobID, err)
		return
	}
	if service.finishCancelledIfRequested(ctx, jobID, "validating", previewPath) {
		return
	}

	if err := service.transitionJob(
		jobID,
		contracts.JobStateRunning,
		30,
		"loadingStudy",
		"Loading source pixels",
	); err != nil {
		service.failJob(jobID, err)
		return
	}

	decoder, err := service.newDecoder()
	if err != nil {
		service.failJob(
			jobID,
			contracts.Internal(fmt.Sprintf("configure rust decode helper: %v", err)),
		)
		return
	}

	sourceStudy, err := decoder.DecodeStudy(ctx, study.InputPath)
	if err != nil {
		if service.finishCancelledIfRequested(ctx, jobID, "loadingStudy", previewPath) {
			return
		}
		service.failJob(jobID, contracts.Internal(fmt.Sprintf("load source study: %v", err)))
		return
	}
	if service.finishCancelledIfRequested(ctx, jobID, "loadingStudy", previewPath) {
		return
	}

	if err := service.transitionJob(
		jobID,
		contracts.JobStateRunning,
		65,
		"processingPixels",
		"Applying processing pipeline",
	); err != nil {
		service.failJob(jobID, err)
		return
	}

	output, err := processing.ProcessSourceImage(
		sourceStudy.Image,
		render.DefaultRenderPlan(),
		resolved.Controls,
		resolved.Palette,
		resolved.Compare,
	)
	if err != nil {
		service.failJob(jobID, contracts.Internal(fmt.Sprintf("process source study: %v", err)))
		return
	}
	if service.finishCancelledIfRequested(ctx, jobID, "processingPixels", previewPath) {
		return
	}

	if err := service.transitionJob(
		jobID,
		contracts.JobStateRunning,
		84,
		"writingPreview",
		"Writing processed preview",
	); err != nil {
		service.failJob(jobID, err)
		return
	}

	if err := render.SavePreviewPNG(previewPath, output.Preview); err != nil {
		service.failJob(jobID, contracts.Internal(fmt.Sprintf("write preview PNG: %v", err)))
		return
	}
	if service.finishCancelledIfRequested(ctx, jobID, "writingPreview", previewPath) {
		return
	}

	if err := service.transitionJob(
		jobID,
		contracts.JobStateRunning,
		95,
		"resolvingOutputPath",
		"Reserving processed DICOM path",
	); err != nil {
		service.failJob(jobID, err)
		return
	}
	if service.finishCancelledIfRequested(ctx, jobID, "resolvingOutputPath", previewPath) {
		return
	}

	service.completeProcessJob(
		jobID,
		fingerprint,
		contracts.ProcessStudyCommandResult{
			StudyID:          study.StudyID,
			PreviewPath:      previewPath,
			DicomPath:        dicomPath,
			LoadedWidth:      sourceStudy.Image.Width,
			LoadedHeight:     sourceStudy.Image.Height,
			Mode:             output.Mode,
			MeasurementScale: cloneMeasurementScale(sourceStudy.MeasurementScale),
		},
	)
}

func (service *Service) executeAnalyzeJob(
	ctx context.Context,
	jobID string,
	study contracts.StudyRecord,
	fingerprint string,
	previewPath string,
) {
	if service.finishCancelledIfRequested(ctx, jobID, "queued", previewPath) {
		return
	}

	if err := service.transitionJob(
		jobID,
		contracts.JobStateRunning,
		10,
		"validating",
		"Validating analysis request",
	); err != nil {
		service.failJob(jobID, err)
		return
	}

	if err := validateInputFile(study.InputPath); err != nil {
		service.failJob(jobID, err)
		return
	}
	if service.finishCancelledIfRequested(ctx, jobID, "validating", previewPath) {
		return
	}

	if err := service.transitionJob(
		jobID,
		contracts.JobStateRunning,
		35,
		"loadingStudy",
		"Loading source study",
	); err != nil {
		service.failJob(jobID, err)
		return
	}

	decoder, err := service.newDecoder()
	if err != nil {
		service.failJob(
			jobID,
			contracts.Internal(fmt.Sprintf("configure rust decode helper: %v", err)),
		)
		return
	}

	sourceStudy, err := decoder.DecodeStudy(ctx, study.InputPath)
	if err != nil {
		if service.finishCancelledIfRequested(ctx, jobID, "loadingStudy", previewPath) {
			return
		}
		service.failJob(jobID, contracts.Internal(fmt.Sprintf("load source study: %v", err)))
		return
	}
	if service.finishCancelledIfRequested(ctx, jobID, "loadingStudy", previewPath) {
		return
	}

	if err := service.transitionJob(
		jobID,
		contracts.JobStateRunning,
		65,
		"renderingPreview",
		"Rendering analysis preview",
	); err != nil {
		service.failJob(jobID, err)
		return
	}

	preview := render.RenderSourceImage(sourceStudy.Image, render.DefaultRenderPlan())
	if err := render.SavePreviewPNG(previewPath, preview); err != nil {
		service.failJob(jobID, contracts.Internal(fmt.Sprintf("write analysis preview PNG: %v", err)))
		return
	}
	if service.finishCancelledIfRequested(ctx, jobID, "renderingPreview", previewPath) {
		return
	}

	if err := service.transitionJob(
		jobID,
		contracts.JobStateRunning,
		88,
		"measuringTooth",
		"Measuring tooth candidate",
	); err != nil {
		service.failJob(jobID, err)
		return
	}

	toothAnalysis, err := analysis.AnalyzePreview(preview, cloneMeasurementScale(sourceStudy.MeasurementScale))
	if err != nil {
		service.failJob(jobID, contracts.Internal(fmt.Sprintf("analyze tooth candidate: %v", err)))
		return
	}

	service.completeAnalyzeJob(
		jobID,
		fingerprint,
		contracts.AnalyzeStudyCommandResult{
			StudyID:              study.StudyID,
			PreviewPath:          previewPath,
			Analysis:             toothAnalysis,
			SuggestedAnnotations: annotations.SuggestedAnnotations(&toothAnalysis),
		},
	)
}

func (service *Service) transitionJob(
	jobID string,
	state contracts.JobState,
	percent int,
	stage string,
	message string,
) error {
	_, err := service.registry.UpdateProgress(jobID, state, percent, stage, message)
	return err
}

func (service *Service) finishCancelledIfRequested(
	ctx context.Context,
	jobID string,
	stage string,
	previewPath string,
) bool {
	cancelled, err := service.registry.IsCancellationRequested(jobID)
	if err != nil {
		return false
	}
	if !cancelled && ctx.Err() == nil {
		return false
	}

	if previewPath != "" {
		_ = os.Remove(previewPath)
	}
	_, _ = service.registry.MarkCancelled(jobID, stage, "Cancelled by user")
	return true
}

func (service *Service) completeRenderJob(
	jobID string,
	fingerprint string,
	result contracts.RenderStudyCommandResult,
) {
	snapshot, err := service.registry.Complete(jobID, contracts.JobResult{
		Kind:    contracts.JobKindRenderStudy,
		Payload: result,
	})
	if err != nil {
		return
	}
	if snapshot.State == contracts.JobStateCancelled {
		_ = os.Remove(result.PreviewPath)
		return
	}
	service.memoryCache.StoreRender(fingerprint, result)
}

func (service *Service) completeProcessJob(
	jobID string,
	fingerprint string,
	result contracts.ProcessStudyCommandResult,
) {
	snapshot, err := service.registry.Complete(jobID, contracts.JobResult{
		Kind:    contracts.JobKindProcessStudy,
		Payload: result,
	})
	if err != nil {
		return
	}
	if snapshot.State == contracts.JobStateCancelled {
		_ = os.Remove(result.PreviewPath)
		return
	}
	service.memoryCache.StoreProcess(fingerprint, result)
}

func (service *Service) completeAnalyzeJob(
	jobID string,
	fingerprint string,
	result contracts.AnalyzeStudyCommandResult,
) {
	snapshot, err := service.registry.Complete(jobID, contracts.JobResult{
		Kind:    contracts.JobKindAnalyzeStudy,
		Payload: result,
	})
	if err != nil {
		return
	}
	if snapshot.State == contracts.JobStateCancelled {
		_ = os.Remove(result.PreviewPath)
		return
	}
	service.memoryCache.StoreAnalyze(fingerprint, result)
}

func (service *Service) failJob(jobID string, err error) {
	backendErr, ok := err.(contracts.BackendError)
	if !ok {
		backendErr = contracts.Internal(err.Error())
	}

	_, _ = service.registry.Fail(jobID, backendErr)
}

func queuedJobSnapshot(
	jobID string,
	jobKind contracts.JobKind,
	studyID string,
) contracts.JobSnapshot {
	return contracts.JobSnapshot{
		JobID:     jobID,
		JobKind:   jobKind,
		StudyID:   studyIDPointer(studyID),
		State:     contracts.JobStateQueued,
		Progress:  contracts.JobProgress{Percent: 0, Stage: "queued", Message: "Queued"},
		FromCache: false,
	}
}

func completedJobSnapshot(
	jobID string,
	jobKind contracts.JobKind,
	studyID string,
	fromCache bool,
	result contracts.JobResult,
) contracts.JobSnapshot {
	return contracts.JobSnapshot{
		JobID:     jobID,
		JobKind:   jobKind,
		StudyID:   studyIDPointer(studyID),
		State:     contracts.JobStateCompleted,
		Progress:  contracts.JobProgress{Percent: 100, Stage: "cacheHit", Message: "Loaded from cache"},
		FromCache: fromCache,
		Result:    &result,
	}
}

func validateInputFile(inputPath string) error {
	info, err := os.Stat(inputPath)
	if err != nil {
		if os.IsNotExist(err) {
			return contracts.NotFound(fmt.Sprintf("input file does not exist: %s", inputPath))
		}

		return contracts.Internal(fmt.Sprintf("failed to inspect input file %s: %v", inputPath, err))
	}
	if info.IsDir() {
		return contracts.InvalidInput(fmt.Sprintf("input path must be a file: %s", inputPath))
	}

	return nil
}

func renderFingerprint(study contracts.StudyRecord) (string, error) {
	payload, err := json.Marshal(struct {
		Namespace string `json:"namespace"`
		InputPath string `json:"inputPath"`
	}{
		Namespace: "render-study-v1",
		InputPath: study.InputPath,
	})
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func processFingerprint(
	study contracts.StudyRecord,
	command contracts.ProcessStudyCommand,
) (string, error) {
	payload, err := json.Marshal(struct {
		Namespace  string                 `json:"namespace"`
		InputPath  string                 `json:"inputPath"`
		OutputPath *string                `json:"outputPath"`
		PresetID   string                 `json:"presetId"`
		Invert     bool                   `json:"invert"`
		Brightness *int                   `json:"brightness"`
		Contrast   *float64               `json:"contrast"`
		Equalize   bool                   `json:"equalize"`
		Compare    bool                   `json:"compare"`
		Palette    *contracts.PaletteName `json:"palette"`
	}{
		Namespace:  "process-study-v2",
		InputPath:  study.InputPath,
		OutputPath: command.OutputPath,
		PresetID:   command.PresetID,
		Invert:     command.Invert,
		Brightness: command.Brightness,
		Contrast:   command.Contrast,
		Equalize:   command.Equalize,
		Compare:    command.Compare,
		Palette:    command.Palette,
	})
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func analyzeFingerprint(study contracts.StudyRecord) (string, error) {
	payload, err := json.Marshal(struct {
		Namespace string `json:"namespace"`
		InputPath string `json:"inputPath"`
	}{
		Namespace: "analyze-study-v1",
		InputPath: study.InputPath,
	})
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func (service *Service) resolveProcessOutputPath(
	outputPath *string,
	fingerprint string,
) (string, error) {
	if outputPath == nil {
		return service.cache.ArtifactPath("process", fingerprint, "dcm")
	}

	resolved := filepath.Clean(strings.TrimSpace(*outputPath))
	if resolved == "" || resolved == "." {
		return "", contracts.InvalidInput("outputPath is required when provided")
	}

	if info, err := os.Stat(resolved); err == nil && info.IsDir() {
		return "", contracts.InvalidInput(fmt.Sprintf("output path must be a file: %s", resolved))
	}

	parent := filepath.Dir(resolved)
	info, err := os.Stat(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return "", contracts.NotFound(fmt.Sprintf("output directory does not exist: %s", parent))
		}
		return "", contracts.Internal(fmt.Sprintf("inspect output directory %s: %v", parent, err))
	}
	if !info.IsDir() {
		return "", contracts.InvalidInput(fmt.Sprintf("output directory must be a directory: %s", parent))
	}

	return resolved, nil
}

func generateJobID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}

	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], raw[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], raw[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], raw[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], raw[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], raw[10:16])

	return string(encoded), nil
}

func isTerminalState(state contracts.JobState) bool {
	return state == contracts.JobStateCompleted ||
		state == contracts.JobStateFailed ||
		state == contracts.JobStateCancelled
}

func studyIDPointer(studyID string) *string {
	value := studyID
	return &value
}

func cloneMeasurementScale(
	scale *contracts.MeasurementScale,
) *contracts.MeasurementScale {
	if scale == nil {
		return nil
	}

	value := *scale
	return &value
}
