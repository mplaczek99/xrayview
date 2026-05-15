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
	"runtime"
	"strconv"
	"strings"
	"sync"

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
	workerCount            int
	renderSourcePreview    renderSourcePreviewFunc
	callbackMu             sync.RWMutex
	onJobCompletion        JobCompletionCallback
	onJobUpdate            JobCompletionCallback
}

const decodeBenchmarkEnvKey = "XRAYVIEW_BENCH_LOG_DECODES"

const analyzeFingerprintNamespace = "analyze-study"

const workerCountEnvKey = "XRAYVIEW_BACKEND_WORKERS"

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

func (service *Service) StartRenderJob(
	command contracts.RenderStudyCommand,
) (contracts.StartedJob, error) {
	return startJob(service, command, jobSpec[
		contracts.RenderStudyCommand,
		struct{},
		contracts.RenderStudyCommandResult,
	]{
		name: "render",
		kind: contracts.JobKindRenderStudy,
		studyID: func(command contracts.RenderStudyCommand) string {
			return command.StudyID
		},
		fingerprint: func(study contracts.StudyRecord, _ contracts.RenderStudyCommand) (string, error) {
			return renderFingerprint(study)
		},
		cacheLoad: service.memoryCache.LoadRender,
		prepare: func(
			contracts.StudyRecord,
			contracts.RenderStudyCommand,
			string,
		) (struct{}, error) {
			return struct{}{}, nil
		},
		execute: func(
			ctx context.Context,
			jobID string,
			study contracts.StudyRecord,
			_ struct{},
			fingerprint string,
		) {
			service.executeRenderJob(ctx, jobID, study, fingerprint)
		},
	})
}

func (service *Service) StartAnalyzeJob(
	command contracts.AnalyzeStudyCommand,
) (contracts.StartedJob, error) {
	return startJob(service, command, jobSpec[
		contracts.AnalyzeStudyCommand,
		analyzeJobInput,
		contracts.AnalyzeStudyCommandResult,
	]{
		name: "analyze",
		kind: contracts.JobKindAnalyzeStudy,
		studyID: func(command contracts.AnalyzeStudyCommand) string {
			return command.StudyID
		},
		fingerprint: func(study contracts.StudyRecord, _ contracts.AnalyzeStudyCommand) (string, error) {
			return analyzeFingerprint(study)
		},
		cacheLoad: service.memoryCache.LoadAnalyze,
		prepare: func(
			_ contracts.StudyRecord,
			_ contracts.AnalyzeStudyCommand,
			fingerprint string,
		) (analyzeJobInput, error) {
			previewPath, err := service.cache.ArtifactPath("analyze", fingerprint, "png")
			if err != nil {
				return analyzeJobInput{}, err
			}
			filledPreviewPath, err := service.cache.ArtifactPath("analyze-filled", fingerprint, "png")
			if err != nil {
				return analyzeJobInput{}, err
			}
			return analyzeJobInput{
				previewPath:       previewPath,
				filledPreviewPath: filledPreviewPath,
			}, nil
		},
		execute: func(
			ctx context.Context,
			jobID string,
			study contracts.StudyRecord,
			input analyzeJobInput,
			fingerprint string,
		) {
			service.executeAnalyzeJob(ctx, jobID, study, fingerprint, input)
		},
	})
}

func (service *Service) StartProcessJob(
	command contracts.ProcessStudyCommand,
) (contracts.StartedJob, error) {
	return startJob(service, command, jobSpec[
		contracts.ProcessStudyCommand,
		processJobInput,
		contracts.ProcessStudyCommandResult,
	]{
		name: "process",
		kind: contracts.JobKindProcessStudy,
		studyID: func(command contracts.ProcessStudyCommand) string {
			return command.StudyID
		},
		fingerprint: processFingerprint,
		cacheLoad:   service.memoryCache.LoadProcess,
		prepare: func(
			_ contracts.StudyRecord,
			command contracts.ProcessStudyCommand,
			fingerprint string,
		) (processJobInput, error) {
			resolved, err := processing.ResolveProcessStudyCommand(command)
			if err != nil {
				return processJobInput{}, err
			}

			previewPath, err := service.cache.ArtifactPath("process", fingerprint, "png")
			if err != nil {
				return processJobInput{}, err
			}

			dicomPath, err := service.resolveProcessOutputPath(command.OutputPath, fingerprint)
			if err != nil {
				return processJobInput{}, err
			}

			return processJobInput{
				resolved:    resolved,
				previewPath: previewPath,
				dicomPath:   dicomPath,
			}, nil
		},
		execute: func(
			ctx context.Context,
			jobID string,
			study contracts.StudyRecord,
			input processJobInput,
			fingerprint string,
		) {
			service.executeProcessJob(ctx, jobID, study, input, fingerprint)
		},
	})
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

	workerCount := resolveWorkerCount(logger)
	svc := &Service{
		supportedKinds: []contracts.JobKind{
			contracts.JobKindRenderStudy,
			contracts.JobKindAnalyzeStudy,
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
		workerCount:            workerCount,
		renderSourcePreview:    render.RenderSourceImage,
	}
	for i := 0; i < workerCount; i++ {
		go svc.runWorker()
	}
	return svc
}

func resolveWorkerCount(logger *slog.Logger) int {
	if value := strings.TrimSpace(os.Getenv(workerCountEnvKey)); value != "" {
		workers, err := strconv.Atoi(value)
		if err == nil && workers > 0 {
			return workers
		}

		logger.Warn(
			"invalid backend worker count; using default",
			slog.String("env_key", workerCountEnvKey),
			slog.String("value", value),
			slog.Int("default_workers", defaultWorkerCount()),
		)
	}

	return defaultWorkerCount()
}

func defaultWorkerCount() int {
	cpus := runtime.NumCPU()
	if cpus <= 1 {
		return 1
	}
	if cpus-1 < 4 {
		return cpus - 1
	}
	return 4
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

type jobSpec[Cmd any, Prepared any, Result any] struct {
	name        string
	kind        contracts.JobKind
	studyID     func(Cmd) string
	fingerprint func(contracts.StudyRecord, Cmd) (string, error)
	cacheLoad   func(string) (Result, bool)
	prepare     func(contracts.StudyRecord, Cmd, string) (Prepared, error)
	execute     func(context.Context, string, contracts.StudyRecord, Prepared, string)
}

type processJobInput struct {
	resolved    processing.ResolvedProcessStudy
	previewPath string
	dicomPath   string
}

type analyzeJobInput struct {
	previewPath       string
	filledPreviewPath string
}

type inputFileIdentity struct {
	State           string `json:"state"`
	Size            int64  `json:"size,omitempty"`
	Mode            uint32 `json:"mode,omitempty"`
	ModTimeUnixNano int64  `json:"modTimeUnixNano,omitempty"`
}

// startJob is the common StartXJob lifecycle: validate inputs, fingerprint the
// request, short-circuit on a memory cache hit, reserve or dedupe a registry
// entry, attach cancellation, and hand execution to the correct worker queue.
func startJob[Cmd any, Prepared any, Result any](
	service *Service,
	command Cmd,
	spec jobSpec[Cmd, Prepared, Result],
) (contracts.StartedJob, error) {
	studyID := strings.TrimSpace(spec.studyID(command))
	if studyID == "" {
		return contracts.StartedJob{}, contracts.InvalidInput("studyId is required")
	}

	study, ok := service.studies.Get(studyID)
	if !ok {
		return contracts.StartedJob{}, contracts.NotFound(fmt.Sprintf("study not found: %s", studyID))
	}

	fingerprint, err := spec.fingerprint(study, command)
	if err != nil {
		return contracts.StartedJob{}, contracts.Internal(
			fmt.Sprintf("serialize %s job fingerprint: %v", spec.name, err),
		)
	}

	if snapshot, ok, err := cachedJobSnapshot(
		service,
		spec.kind,
		fingerprint,
		study.StudyID,
		spec.cacheLoad,
	); err != nil {
		return contracts.StartedJob{}, err
	} else if ok {
		return contracts.StartedJob{JobID: snapshot.JobID}, nil
	}

	prepared, err := spec.prepare(study, command, fingerprint)
	if err != nil {
		return contracts.StartedJob{}, err
	}

	outcome, err := service.registry.StartJob(spec.kind, study.StudyID, fingerprint)
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

	service.launchJob(spec.kind, func() {
		spec.execute(ctx, outcome.Snapshot.JobID, study, prepared, fingerprint)
	})

	return contracts.StartedJob{JobID: outcome.Snapshot.JobID}, nil
}

// cachedJobSnapshot synthesizes a completed snapshot from a memory cache hit.
// A fresh job ID is minted for every hit rather than replaying an earlier one —
// the prior job's registry entry has typically been evicted, and the UI wants a
// clean handle to subscribe to and dispose of.
func cachedJobSnapshot[Result any](
	service *Service,
	kind contracts.JobKind,
	fingerprint string,
	studyID string,
	load func(string) (Result, bool),
) (contracts.JobSnapshot, bool, error) {
	cached, ok := load(fingerprint)
	if !ok {
		return contracts.JobSnapshot{}, false, nil
	}

	snapshot, err := service.registry.CreateCachedJob(
		kind,
		studyID,
		contracts.JobResult{Kind: kind, Payload: cached},
	)
	if err != nil {
		return contracts.JobSnapshot{}, false, err
	}

	return snapshot, true, nil
}

type jobStage struct {
	percent       int
	stage         string
	message       string
	work          func() error
	cleanupPath   func() string
	cancelOnError bool
	onError       func()
	onCancel      func()
}

func (service *Service) runJobStage(
	ctx context.Context,
	jobID string,
	stage jobStage,
) bool {
	if err := service.transitionJob(
		jobID,
		contracts.JobStateRunning,
		stage.percent,
		stage.stage,
		stage.message,
	); err != nil {
		service.failJob(jobID, err)
		return false
	}

	if err := stage.work(); err != nil {
		if stage.cancelOnError && service.finishCancelledIfRequested(
			ctx,
			jobID,
			stage.stage,
			stage.currentCleanupPath(),
		) {
			stage.cleanupAfterCancel()
			return false
		}
		if stage.onError != nil {
			stage.onError()
		}
		service.failJob(jobID, err)
		return false
	}

	if service.finishCancelledIfRequested(ctx, jobID, stage.stage, stage.currentCleanupPath()) {
		stage.cleanupAfterCancel()
		return false
	}

	return true
}

func (stage jobStage) currentCleanupPath() string {
	if stage.cleanupPath == nil {
		return ""
	}
	return stage.cleanupPath()
}

func (stage jobStage) cleanupAfterCancel() {
	if stage.onCancel != nil {
		stage.onCancel()
	}
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

	if !service.runJobStage(ctx, jobID, jobStage{
		percent: 10,
		stage:   "validating",
		message: "Validating source study",
		work: func() error {
			return validateInputFile(study.InputPath)
		},
	}) {
		return
	}

	var sourceStudy dicommeta.SourceStudy
	if !service.runJobStage(ctx, jobID, jobStage{
		percent:       35,
		stage:         "loadingStudy",
		message:       "Loading source study",
		cancelOnError: true,
		work: func() error {
			loaded, err := service.loadSourceStudy(ctx, contracts.JobKindRenderStudy, study)
			if err != nil {
				return err
			}
			sourceStudy = loaded
			service.memoryCache.StoreMeasurementScale(study.InputPath, sourceStudy.MeasurementScale)
			return nil
		},
	}) {
		return
	}

	var preview imaging.PreviewImage
	if !service.runJobStage(ctx, jobID, jobStage{
		percent: 75,
		stage:   "renderingPreview",
		message: "Rendering preview",
		work: func() error {
			preview = service.loadOrRenderSourcePreview(study.InputPath, sourceStudy.Image)
			return nil
		},
	}) {
		return
	}
	defer preview.Release()

	var previewPath string
	if !service.runJobStage(ctx, jobID, jobStage{
		percent: 90,
		stage:   "writingPreview",
		message: "Writing preview",
		cleanupPath: func() string {
			return previewPath
		},
		onError: func() {
			cleanupPaths(previewPath)
		},
		work: func() error {
			path, err := service.cache.ArtifactPath("render", fingerprint, "png")
			if err != nil {
				return err
			}
			previewPath = path
			if err := render.SavePreviewPNG(previewPath, preview); err != nil {
				return contracts.Internal(fmt.Sprintf("write preview PNG: %v", err))
			}
			if info, err := os.Stat(previewPath); err == nil {
				service.cache.AddArtifactBytes(info.Size())
			}
			return nil
		},
	}) {
		return
	}

	completeJob(
		service,
		contracts.JobKindRenderStudy,
		jobID,
		fingerprint,
		contracts.RenderStudyCommandResult{
			StudyID:          study.StudyID,
			PreviewPath:      previewPath,
			LoadedWidth:      sourceStudy.Image.Width,
			LoadedHeight:     sourceStudy.Image.Height,
			MeasurementScale: cloneMeasurementScale(sourceStudy.MeasurementScale),
		},
		service.memoryCache.StoreRender,
		func(result contracts.RenderStudyCommandResult) {
			cleanupPaths(result.PreviewPath)
		},
	)
}

func (service *Service) executeAnalyzeJob(
	ctx context.Context,
	jobID string,
	study contracts.StudyRecord,
	fingerprint string,
	input analyzeJobInput,
) {
	previewPath := input.previewPath
	filledPreviewPath := input.filledPreviewPath
	cleanupAnalyzePaths := func() {
		cleanupPaths(previewPath, filledPreviewPath)
	}

	if service.finishCancelledIfRequested(ctx, jobID, "queued", previewPath) {
		cleanupPaths(filledPreviewPath)
		return
	}

	if !service.runJobStage(ctx, jobID, jobStage{
		percent: 10,
		stage:   "validating",
		message: "Validating source study",
		cleanupPath: func() string {
			return previewPath
		},
		onCancel: cleanupAnalyzePaths,
		work: func() error {
			return validateInputFile(study.InputPath)
		},
	}) {
		return
	}

	var sourceStudy dicommeta.SourceStudy
	if !service.runJobStage(ctx, jobID, jobStage{
		percent:       30,
		stage:         "loadingStudy",
		message:       "Loading source pixels",
		cancelOnError: true,
		cleanupPath: func() string {
			return previewPath
		},
		onCancel: cleanupAnalyzePaths,
		work: func() error {
			loaded, err := service.loadSourceStudy(ctx, contracts.JobKindAnalyzeStudy, study)
			if err != nil {
				return err
			}
			sourceStudy = loaded
			service.memoryCache.StoreMeasurementScale(study.InputPath, sourceStudy.MeasurementScale)
			return nil
		},
	}) {
		return
	}

	var analysisResult analysis.ToothOverlayResult
	if !service.runJobStage(ctx, jobID, jobStage{
		percent: 68,
		stage:   "analyzingDentalStructures",
		message: "Detecting tooth regions and bone level",
		cleanupPath: func() string {
			return previewPath
		},
		onCancel: cleanupAnalyzePaths,
		work: func() error {
			sourcePreview := service.loadOrRenderSourcePreview(study.InputPath, sourceStudy.Image)
			defer sourcePreview.Release()
			result, err := analysis.GenerateToothOverlay(
				sourcePreview,
			)
			if err != nil {
				return contracts.Internal(fmt.Sprintf("analyze source preview: %v", err))
			}
			analysisResult = result
			return nil
		},
	}) {
		return
	}

	if !service.runJobStage(ctx, jobID, jobStage{
		percent: 90,
		stage:   "writingPreview",
		message: "Writing tooth and bone overlay preview",
		cleanupPath: func() string {
			return previewPath
		},
		onError: func() {
			cleanupAnalyzePaths()
		},
		onCancel: cleanupAnalyzePaths,
		work: func() error {
			if err := render.SavePreviewPNG(previewPath, analysisResult.Preview); err != nil {
				return contracts.Internal(fmt.Sprintf("write analyze preview PNG: %v", err))
			}
			if err := render.SavePreviewPNG(filledPreviewPath, analysisResult.FilledPreview); err != nil {
				return contracts.Internal(fmt.Sprintf("write filled analyze preview PNG: %v", err))
			}
			if info, err := os.Stat(previewPath); err == nil {
				service.cache.AddArtifactBytes(info.Size())
			}
			if info, err := os.Stat(filledPreviewPath); err == nil {
				service.cache.AddArtifactBytes(info.Size())
			}
			return nil
		},
	}) {
		return
	}

	completeJob(
		service,
		contracts.JobKindAnalyzeStudy,
		jobID,
		fingerprint,
		contracts.AnalyzeStudyCommandResult{
			StudyID:           study.StudyID,
			PreviewPath:       previewPath,
			FilledPreviewPath: filledPreviewPath,
			LoadedWidth:       sourceStudy.Image.Width,
			LoadedHeight:      sourceStudy.Image.Height,
			Mode:              analysisResult.Mode,
			MeasurementScale:  cloneMeasurementScale(sourceStudy.MeasurementScale),
		},
		service.memoryCache.StoreAnalyze,
		func(result contracts.AnalyzeStudyCommandResult) {
			cleanupPaths(result.PreviewPath, result.FilledPreviewPath)
		},
	)
}

func (service *Service) executeProcessJob(
	ctx context.Context,
	jobID string,
	study contracts.StudyRecord,
	input processJobInput,
	fingerprint string,
) {
	resolved := input.resolved
	previewPath := input.previewPath
	dicomPath := input.dicomPath

	if service.finishCancelledIfRequested(ctx, jobID, "queued", previewPath) {
		return
	}

	if !service.runJobStage(ctx, jobID, jobStage{
		percent: 10,
		stage:   "validating",
		message: "Validating processing request",
		cleanupPath: func() string {
			return previewPath
		},
		work: func() error {
			return validateInputFile(study.InputPath)
		},
	}) {
		return
	}

	var sourceStudy dicommeta.SourceStudy
	if !service.runJobStage(ctx, jobID, jobStage{
		percent:       30,
		stage:         "loadingStudy",
		message:       "Loading source pixels",
		cancelOnError: true,
		cleanupPath: func() string {
			return previewPath
		},
		work: func() error {
			loaded, err := service.loadSourceStudy(ctx, contracts.JobKindProcessStudy, study)
			if err != nil {
				return err
			}
			sourceStudy = loaded
			service.memoryCache.StoreMeasurementScale(study.InputPath, sourceStudy.MeasurementScale)
			return nil
		},
	}) {
		return
	}

	var output processing.PipelineOutput
	if !service.runJobStage(ctx, jobID, jobStage{
		percent: 65,
		stage:   "processingPixels",
		message: "Applying processing pipeline",
		cleanupPath: func() string {
			return previewPath
		},
		work: func() error {
			sourcePreview := service.loadOrRenderSourcePreview(study.InputPath, sourceStudy.Image)
			defer sourcePreview.Release()
			processed, err := processing.ProcessRenderedPreview(
				sourcePreview,
				resolved.Controls,
				resolved.Palette,
				resolved.Compare,
			)
			if err != nil {
				return contracts.Internal(fmt.Sprintf("process source preview: %v", err))
			}
			output = processed
			return nil
		},
	}) {
		return
	}
	defer output.Preview.Release()

	if !service.runJobStage(ctx, jobID, jobStage{
		percent: 84,
		stage:   "writingPreview",
		message: "Writing processed preview",
		cleanupPath: func() string {
			return previewPath
		},
		onError: func() {
			cleanupPaths(previewPath, dicomPath)
		},
		work: func() error {
			if err := render.SavePreviewPNG(previewPath, output.Preview); err != nil {
				return contracts.Internal(fmt.Sprintf("write preview PNG: %v", err))
			}
			if info, err := os.Stat(previewPath); err == nil {
				service.cache.AddArtifactBytes(info.Size())
			}
			return nil
		},
	}) {
		return
	}

	if !service.runJobStage(ctx, jobID, jobStage{
		percent:       95,
		stage:         "writingDicom",
		message:       "Writing processed DICOM",
		cancelOnError: true,
		cleanupPath: func() string {
			return previewPath
		},
		onError: func() {
			cleanupPaths(previewPath, dicomPath)
		},
		onCancel: func() {
			cleanupPaths(dicomPath)
		},
		work: func() error {
			if err := service.secondaryCaptureWriter.WriteSecondaryCapture(
				ctx,
				dicomPath,
				output.Preview,
				sourceStudy.Metadata,
			); err != nil {
				return contracts.Internal(fmt.Sprintf("write processed DICOM: %v", err))
			}
			return nil
		},
	}) {
		return
	}

	completeJob(
		service,
		contracts.JobKindProcessStudy,
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
		service.memoryCache.StoreProcess,
		func(result contracts.ProcessStudyCommandResult) {
			cleanupPaths(result.PreviewPath, result.DicomPath)
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

func completeJob[Result any](
	service *Service,
	kind contracts.JobKind,
	jobID string,
	fingerprint string,
	result Result,
	store func(string, Result),
	cleanupCancelled func(Result),
) {
	snapshot, err := service.registry.Complete(jobID, contracts.JobResult{
		Kind:    kind,
		Payload: result,
	})
	if err != nil {
		return
	}
	if snapshot.State == contracts.JobStateCancelled {
		if cleanupCancelled != nil {
			cleanupCancelled(result)
		}
		service.notifyJobCompletion(snapshot)
		return
	}
	store(fingerprint, result)
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

	sourceStudy, err := service.decodeCache.GetOrDecodeWithKey(
		ctx,
		inputCacheKey(study.InputPath),
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
	cacheKey := inputCacheKey(inputPath)
	if preview, ok := service.memoryCache.LoadSourcePreview(cacheKey); ok {
		return preview
	}

	preview := service.renderSourcePreview(source, render.DefaultRenderPlan())
	service.memoryCache.StoreSourcePreview(cacheKey, preview)
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

// The Namespace string embedded in every fingerprint ("render-study-v2",
// "process-study-v4") is a deliberate cache-bust key. Analyze uses a stable
// namespace plus analysis.AnalyzeAlgorithmVersion so algorithm changes are
// explicit.
func renderFingerprint(study contracts.StudyRecord) (string, error) {
	return fingerprintJSON(struct {
		Namespace     string            `json:"namespace"`
		InputPath     string            `json:"inputPath"`
		InputIdentity inputFileIdentity `json:"inputIdentity"`
	}{
		Namespace:     "render-study-v2",
		InputPath:     study.InputPath,
		InputIdentity: currentInputFileIdentity(study.InputPath),
	})
}

func analyzeFingerprint(study contracts.StudyRecord) (string, error) {
	return fingerprintJSON(struct {
		Namespace        string            `json:"namespace"`
		AlgorithmVersion string            `json:"algorithmVersion"`
		InputPath        string            `json:"inputPath"`
		InputIdentity    inputFileIdentity `json:"inputIdentity"`
	}{
		Namespace:        analyzeFingerprintNamespace,
		AlgorithmVersion: analysis.AnalyzeAlgorithmVersion,
		InputPath:        study.InputPath,
		InputIdentity:    currentInputFileIdentity(study.InputPath),
	})
}

func processFingerprint(
	study contracts.StudyRecord,
	command contracts.ProcessStudyCommand,
) (string, error) {
	return fingerprintJSON(struct {
		Namespace     string                 `json:"namespace"`
		InputPath     string                 `json:"inputPath"`
		InputIdentity inputFileIdentity      `json:"inputIdentity"`
		PresetID      string                 `json:"presetId"`
		Invert        bool                   `json:"invert"`
		Brightness    *int                   `json:"brightness"`
		Contrast      *float64               `json:"contrast"`
		Equalize      bool                   `json:"equalize"`
		Compare       bool                   `json:"compare"`
		Palette       *contracts.PaletteName `json:"palette"`
	}{
		Namespace:     "process-study-v4",
		InputPath:     study.InputPath,
		InputIdentity: currentInputFileIdentity(study.InputPath),
		PresetID:      command.PresetID,
		Invert:        command.Invert,
		Brightness:    command.Brightness,
		Contrast:      command.Contrast,
		Equalize:      command.Equalize,
		Compare:       command.Compare,
		Palette:       command.Palette,
	})
}

func fingerprintJSON(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func currentInputFileIdentity(inputPath string) inputFileIdentity {
	info, err := os.Stat(inputPath)
	if err != nil {
		return inputFileIdentity{State: "unavailable"}
	}

	state := "file"
	if info.IsDir() {
		state = "directory"
	}

	return inputFileIdentity{
		State:           state,
		Size:            info.Size(),
		Mode:            uint32(info.Mode()),
		ModTimeUnixNano: info.ModTime().UnixNano(),
	}
}

func inputCacheKey(inputPath string) string {
	identity := currentInputFileIdentity(inputPath)
	return fmt.Sprintf(
		"%s\x00%s\x00%d\x00%d\x00%d",
		inputPath,
		identity.State,
		identity.Size,
		identity.Mode,
		identity.ModTimeUnixNano,
	)
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
