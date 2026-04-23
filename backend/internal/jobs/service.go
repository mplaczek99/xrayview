// Package jobs runs the long-running render and process study commands behind
// a small worker pool. Per job:
//
//	StartXJob fingerprints the request, consults the in-memory result cache,
//	and on a miss reserves an entry in the registry. The execute closure is
//	handed to one of two queues: renderQueue (drained first) or jobQueue
//	(process). A worker walks the executeXJob stage machine,
//	polling the registry for cancellation between stages, and completeXJob
//	stores the result and fires callbacks.
//
// Dedupe is by fingerprint — a second StartJob for the same inputs returns
// the in-flight snapshot rather than launching a duplicate. Cancellation is a
// context.CancelFunc attached to the registry entry so Cancel() can unblock
// decode and write calls immediately.
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
	"sync"

	"xrayview/backend/internal/cache"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/dicommeta"
	dicomexport "xrayview/backend/internal/export"
	"xrayview/backend/internal/imaging"
	"xrayview/backend/internal/processing"
	"xrayview/backend/internal/render"
	"xrayview/backend/internal/studies"
)

type studyDecoder interface {
	DecodeStudy(context.Context, string) (dicommeta.SourceStudy, error)
}

type decodeHelperFactory func() (studyDecoder, error)
type idGenerator func() (string, error)
type renderSourcePreviewFunc func(imaging.SourceImage, render.RenderPlan) imaging.PreviewImage
type JobCompletionCallback func(snapshot contracts.JobSnapshot)

type Service struct {
	supportedKinds         []contracts.JobKind
	cache                  *cache.Store
	studies                *studies.Registry
	logger                 *slog.Logger
	newDecoder             decodeHelperFactory
	secondaryCaptureWriter dicomexport.Writer
	memoryCache            *cache.Memory // finished job payloads keyed by fingerprint; separate from decodeCache (source-study decode)
	registry               *Registry
	decodeCache            *studies.DecodeCache // source-study decode dedupe; separate from memoryCache (result payloads)
	renderQueue            chan func()          // high-priority: render jobs
	jobQueue               chan func()          // normal-priority: process jobs
	workerStop             chan struct{}
	workerOnce             sync.Once // guards against a double close of workerStop from repeated Stop() calls
	renderSourcePreview    renderSourcePreviewFunc
	callbackMu             sync.RWMutex
	onJobCompletion        JobCompletionCallback
	onJobUpdate            JobCompletionCallback
}

const decodeBenchmarkEnvKey = "XRAYVIEW_BENCH_LOG_DECODES"

// maxConcurrentJobs sizes the worker pool. Three CPU-bound workers fit a
// desktop workload — render and process both contend on the same
// decoder cache, and more than a handful just starves the UI thread.
const maxConcurrentJobs = 3

// maxArtifactBytes is a soft upper bound on total bytes held in the on-disk
// artifact cache. Sized for a session's worth of previews and exported
// DICOMs; the eviction pass runs after each successful completion.
const maxArtifactBytes int64 = 256 * 1024 * 1024 // 256 MB

var decodeBenchmarkCounts = struct {
	mu     sync.Mutex
	counts map[string]int
}{
	counts: make(map[string]int),
}

func New(cacheStore *cache.Store, studyRegistry *studies.Registry, logger *slog.Logger) *Service {
	return newService(
		cacheStore,
		studyRegistry,
		logger,
		dicomexport.GoWriter{},
		func() (studyDecoder, error) {
			return dicommeta.NewDecoder(), nil
		},
		generateJobID,
	)
}

func NewFromEnvironment(
	cacheStore *cache.Store,
	studyRegistry *studies.Registry,
	logger *slog.Logger,
) (*Service, error) {
	writer, err := dicomexport.NewWriterFromEnvironment()
	if err != nil {
		return nil, err
	}

	return newService(
		cacheStore,
		studyRegistry,
		logger,
		writer,
		func() (studyDecoder, error) {
			return dicommeta.NewDecoder(), nil
		},
		generateJobID,
	), nil
}

func (service *Service) SupportedKinds() []string {
	kinds := make([]string, 0, len(service.supportedKinds))
	for _, kind := range service.supportedKinds {
		kinds = append(kinds, string(kind))
	}

	return kinds
}

func (service *Service) OnJobCompletion(callback JobCompletionCallback) {
	service.callbackMu.Lock()
	defer service.callbackMu.Unlock()

	service.onJobCompletion = callback
}

func (service *Service) OnJobUpdate(callback JobCompletionCallback) {
	service.callbackMu.Lock()
	defer service.callbackMu.Unlock()

	service.onJobUpdate = callback
}

// StartRenderJob hands a render request to the worker pool. All three
// StartXJob entry points follow the same shape: validate inputs, fingerprint
// the request, short-circuit on a memory cache hit (returning a snapshot with
// FromCache=true), otherwise reserve a registry entry — if the same
// fingerprint is already in-flight, the existing snapshot comes back with
// Created=false — attach a cancel func, and launch the execute closure.
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

	service.launchJob(contracts.JobKindRenderStudy, func() {
		service.executeRenderJob(ctx, outcome.Snapshot.JobID, study, fingerprint)
	})

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

	service.launchJob(contracts.JobKindProcessStudy, func() {
		service.executeProcessJob(
			ctx,
			outcome.Snapshot.JobID,
			study,
			resolved,
			fingerprint,
			previewPath,
			dicomPath,
		)
	})

	return contracts.StartedJob{JobID: outcome.Snapshot.JobID}, nil
}

func (service *Service) GetJob(command contracts.JobCommand) (contracts.JobSnapshot, error) {
	jobID := strings.TrimSpace(command.JobID)
	if jobID == "" {
		return contracts.JobSnapshot{}, contracts.InvalidInput("jobId is required")
	}

	return service.registry.Get(jobID)
}

func (service *Service) GetJobs(command contracts.GetJobsCommand) ([]contracts.JobSnapshot, error) {
	snapshots := make([]contracts.JobSnapshot, 0, len(command.JobIDs))
	for _, jobID := range command.JobIDs {
		jobID = strings.TrimSpace(jobID)
		if jobID == "" {
			continue
		}
		snapshot, err := service.registry.Get(jobID)
		if err != nil {
			continue // silently skip missing/unknown jobs
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
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
	secondaryCaptureWriter dicomexport.Writer,
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
			return dicommeta.NewDecoder(), nil
		}
	}
	if secondaryCaptureWriter == nil {
		secondaryCaptureWriter = dicomexport.GoWriter{}
	}
	if jobIDFactory == nil {
		jobIDFactory = generateJobID
	}

	svc := &Service{
		supportedKinds: []contracts.JobKind{
			contracts.JobKindRenderStudy,
			contracts.JobKindProcessStudy,
		},
		cache:                  cacheStore,
		studies:                studyRegistry,
		logger:                 logger,
		newDecoder:             decoderFactory,
		secondaryCaptureWriter: secondaryCaptureWriter,
		memoryCache:            cache.NewMemory(logger),
		registry:               NewRegistry(jobIDFactory),
		decodeCache:            studies.NewDecodeCache(0),
		renderQueue:            make(chan func(), 16),
		jobQueue:               make(chan func(), 16),
		workerStop:             make(chan struct{}),
		renderSourcePreview:    render.RenderSourceImage,
	}
	for i := 0; i < maxConcurrentJobs; i++ {
		go svc.runWorker()
	}
	return svc
}

func (service *Service) logDecodeStudyCall(jobKind contracts.JobKind, study contracts.StudyRecord) {
	if strings.TrimSpace(os.Getenv(decodeBenchmarkEnvKey)) == "" {
		return
	}

	decodeBenchmarkCounts.mu.Lock()
	decodeBenchmarkCounts.counts[study.InputPath]++
	count := decodeBenchmarkCounts.counts[study.InputPath]
	decodeBenchmarkCounts.mu.Unlock()

	service.logger.Info(
		"[bench] DecodeStudy invocation",
		slog.String("job_kind", string(jobKind)),
		slog.String("study_id", study.StudyID),
		slog.String("input_path", study.InputPath),
		slog.Int("workflow_decode_count", count),
	)
}

// Stop shuts down the worker pool. Unstarted jobs in the queue are abandoned.
func (service *Service) Stop() {
	service.workerOnce.Do(func() {
		close(service.workerStop)
	})
}

func (service *Service) runWorker() {
	for {
		// Non-blocking pull from renderQueue first; the default: branch below
		// is the whole priority mechanism — a ready render is taken before
		// the blocking select competes both queues against workerStop.
		select {
		case run := <-service.renderQueue:
			run()
			continue
		default:
		}
		select {
		case run := <-service.renderQueue:
			run()
		case run := <-service.jobQueue:
			run()
		case <-service.workerStop:
			return
		}
	}
}

func (service *Service) launchJob(kind contracts.JobKind, run func()) {
	if kind == contracts.JobKindRenderStudy {
		service.renderQueue <- run
	} else {
		service.jobQueue <- run
	}
}

// cachedRenderSnapshot synthesizes a completed snapshot from a memory cache
// hit. A fresh job ID is minted for every hit rather than replaying an
// earlier one — the prior job's registry entry has typically been evicted,
// and the UI wants a clean handle to subscribe to and dispose of.
// cachedProcessSnapshot follows the same contract.
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

// executeRenderJob walks the render pipeline to completion or cancellation.
//
// The stage names ("validating", "loadingStudy", "renderingPreview",
// "writingPreview", and in the peer "processingPixels" / "writingDicom")
// and the percent values below are part of the frontend
// contract — the UI labels progress from them. Renaming a stage or nudging a
// percent breaks the UI's progress display, so update the contracts schema
// and the frontend in lockstep. Shared convention with executeProcessJob.
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

	sourceStudy, err := service.loadSourceStudy(ctx, contracts.JobKindRenderStudy, study)
	if err != nil {
		if service.finishCancelledIfRequested(ctx, jobID, "loadingStudy", "") {
			return
		}
		service.failJob(jobID, err)
		return
	}
	service.memoryCache.StoreMeasurementScale(study.InputPath, sourceStudy.MeasurementScale)
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

	preview := service.loadOrRenderSourcePreview(study.InputPath, sourceStudy.Image)
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
		cleanupPaths(previewPath)
		service.failJob(jobID, contracts.Internal(fmt.Sprintf("write preview PNG: %v", err)))
		return
	}
	if info, err := os.Stat(previewPath); err == nil {
		service.cache.AddArtifactBytes(info.Size())
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

	sourceStudy, err := service.loadSourceStudy(ctx, contracts.JobKindProcessStudy, study)
	if err != nil {
		if service.finishCancelledIfRequested(ctx, jobID, "loadingStudy", previewPath) {
			return
		}
		service.failJob(jobID, err)
		return
	}
	service.memoryCache.StoreMeasurementScale(study.InputPath, sourceStudy.MeasurementScale)
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

	output, err := processing.ProcessRenderedPreview(
		service.loadOrRenderSourcePreview(study.InputPath, sourceStudy.Image),
		resolved.Controls,
		resolved.Palette,
		resolved.Compare,
	)
	if err != nil {
		service.failJob(jobID, contracts.Internal(fmt.Sprintf("process source preview: %v", err)))
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
		cleanupPaths(previewPath, dicomPath)
		service.failJob(jobID, contracts.Internal(fmt.Sprintf("write preview PNG: %v", err)))
		return
	}
	if info, err := os.Stat(previewPath); err == nil {
		service.cache.AddArtifactBytes(info.Size())
	}
	if service.finishCancelledIfRequested(ctx, jobID, "writingPreview", previewPath) {
		return
	}

	if err := service.transitionJob(
		jobID,
		contracts.JobStateRunning,
		95,
		"writingDicom",
		"Writing processed DICOM",
	); err != nil {
		service.failJob(jobID, err)
		return
	}
	if err := service.secondaryCaptureWriter.WriteSecondaryCapture(
		ctx,
		dicomPath,
		output.Preview,
		sourceStudy.Metadata,
	); err != nil {
		if service.finishCancelledIfRequested(ctx, jobID, "writingDicom", previewPath) {
			cleanupPaths(dicomPath)
			return
		}
		cleanupPaths(previewPath, dicomPath)
		service.failJob(jobID, contracts.Internal(fmt.Sprintf("write processed DICOM: %v", err)))
		return
	}
	if service.finishCancelledIfRequested(ctx, jobID, "writingDicom", previewPath) {
		cleanupPaths(dicomPath)
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

func (service *Service) transitionJob(
	jobID string,
	state contracts.JobState,
	percent int,
	stage string,
	message string,
) error {
	snapshot, err := service.registry.UpdateProgress(jobID, state, percent, stage, message)
	if err != nil {
		return err
	}
	service.notifyJobUpdate(snapshot)
	return nil
}

// finishCancelledIfRequested is both a check and a cleanup. If the registry
// latch or the job context says we have been asked to cancel, it removes any
// partially written preview, drives the registry to JobStateCancelled, fires
// the completion callback, and returns true. Callers MUST return immediately
// on true — continuing would clobber the Cancelled terminal state with a
// later Complete or Fail, and the preview cleanup would race whatever the
// next stage tries to write. The name reads like a predicate; treat it as
// "bail out here if we are done".
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
	snapshot, err := service.registry.MarkCancelled(jobID, stage, "Cancelled by user")
	if err == nil {
		service.notifyJobCompletion(snapshot)
	}
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
		service.notifyJobCompletion(snapshot)
		return
	}
	service.memoryCache.StoreRender(fingerprint, result)
	service.notifyJobCompletion(snapshot)
	service.evictArtifactsIfNeeded()
}

// completeProcessJob mirrors completeRenderJob but also deletes the DICOM
// artifact on the cancellation race — process is the only variant that
// writes a second output file.
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
		cleanupPaths(result.PreviewPath, result.DicomPath)
		service.notifyJobCompletion(snapshot)
		return
	}
	service.memoryCache.StoreProcess(fingerprint, result)
	service.notifyJobCompletion(snapshot)
	service.evictArtifactsIfNeeded()
}

func (service *Service) evictArtifactsIfNeeded() {
	removed, err := service.cache.EvictArtifactsOverLimit(maxArtifactBytes)
	if err != nil && service.logger != nil {
		service.logger.Warn("artifact eviction failed", "error", err)
	}
	if removed > 0 && service.logger != nil {
		service.logger.Info("evicted old artifacts", "removed", removed)
	}
}

func (service *Service) failJob(jobID string, err error) {
	backendErr, ok := err.(contracts.BackendError)
	if !ok {
		backendErr = contracts.Internal(err.Error())
	}

	snapshot, err := service.registry.Fail(jobID, backendErr)
	if err == nil {
		service.notifyJobCompletion(snapshot)
	}
}

func (service *Service) loadSourceStudy(
	ctx context.Context,
	jobKind contracts.JobKind,
	study contracts.StudyRecord,
) (dicommeta.SourceStudy, error) {
	decoder, err := service.newDecoder()
	if err != nil {
		return dicommeta.SourceStudy{}, contracts.Internal(
			fmt.Sprintf("configure DICOM decoder: %v", err),
		)
	}

	sourceStudy, err := service.decodeCache.GetOrDecode(
		ctx,
		study.InputPath,
		benchmarkingDecoder{
			delegate: decoder,
			beforeDecode: func() {
				service.logDecodeStudyCall(jobKind, study)
			},
		},
	)
	if err != nil {
		return dicommeta.SourceStudy{}, contracts.Internal(fmt.Sprintf("load source study: %v", err))
	}

	return sourceStudy, nil
}

func (service *Service) loadOrRenderSourcePreview(
	inputPath string,
	source imaging.SourceImage,
) imaging.PreviewImage {
	if preview, ok := service.memoryCache.LoadSourcePreview(inputPath); ok {
		return preview
	}

	preview := service.renderSourcePreview(source, render.DefaultRenderPlan())
	service.memoryCache.StoreSourcePreview(inputPath, preview)
	return preview
}

func (service *Service) notifyJobUpdate(snapshot contracts.JobSnapshot) {
	service.callbackMu.RLock()
	callback := service.onJobUpdate
	service.callbackMu.RUnlock()

	if callback != nil {
		callback(snapshot)
	}
}

func (service *Service) notifyJobCompletion(snapshot contracts.JobSnapshot) {
	service.notifyJobUpdate(snapshot)

	service.callbackMu.RLock()
	callback := service.onJobCompletion
	service.callbackMu.RUnlock()

	if callback != nil {
		callback(snapshot)
	}
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

// The Namespace string embedded in every fingerprint ("render-study-v1",
// "process-study-v3") is a deliberate cache-bust key.
// Bump the version suffix whenever the shape of what should invalidate a
// cached result changes — without a bump, a stale entry keyed on the old
// payload shape will be served for the new request.
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
		PresetID   string                 `json:"presetId"`
		Invert     bool                   `json:"invert"`
		Brightness *int                   `json:"brightness"`
		Contrast   *float64               `json:"contrast"`
		Equalize   bool                   `json:"equalize"`
		Compare    bool                   `json:"compare"`
		Palette    *contracts.PaletteName `json:"palette"`
	}{
		Namespace:  "process-study-v3",
		InputPath:  study.InputPath,
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

type benchmarkingDecoder struct {
	delegate     studyDecoder
	beforeDecode func()
}

func (decoder benchmarkingDecoder) DecodeStudy(
	ctx context.Context,
	path string,
) (dicommeta.SourceStudy, error) {
	if decoder.beforeDecode != nil {
		decoder.beforeDecode()
	}

	return decoder.delegate.DecodeStudy(ctx, path)
}

// resolveProcessOutputPath picks the DICOM output path for a process job.
// A user-supplied path wins (after parent-directory sanity checks); when nil
// we fall back to a deterministic cache-managed artifact so repeated runs
// against the same fingerprint overwrite in place instead of piling up.
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

// generateJobID returns a dash-separated 16-byte hex string shaped like a
// UUID v4. We skip the variant/version bit twiddling because crypto/rand
// already yields uniform bytes and nothing in the system parses these as
// canonical UUIDs — contrast with dicommeta.generateSourceStudyUID, which
// does need to produce a spec-compliant OID.
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

func cleanupPaths(paths ...string) {
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		_ = os.Remove(path)
	}
}
