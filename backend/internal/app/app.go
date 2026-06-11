package app

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"xrayview/backend/internal/analysis"
	"xrayview/backend/internal/annotations"
	"xrayview/backend/internal/bmp"
	"xrayview/backend/internal/cache"
	"xrayview/backend/internal/config"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/persistence"
	"xrayview/backend/internal/processing"
	"xrayview/backend/internal/render"
)

const (
	MaxArtifactBytes      = 256 * 1024 * 1024
	MaxTerminalJobs       = 64
	MaxResultCacheEntries = 64
	JobUpdateBufferSize   = 16
)

// App is the backend orchestrator used by command handlers. It exercises the
// file, cache, persistence, processing, and contract surfaces the desktop shell
// and CLI call into.
type App struct {
	config         config.Config
	startedAt      time.Time
	cacheSessionID string

	cache              *cache.Store
	persistence        *persistence.Catalog
	sourcePreviewCache *cache.SourcePreviewCache

	studiesMu sync.Mutex
	studies   map[string]contracts.StudyRecord

	jobsMu sync.Mutex
	jobs   jobRegistry

	activeMu           sync.Mutex
	activeFingerprints map[string]string

	resultCacheMu sync.Mutex
	resultCache   resultCache

	subscribersMu sync.Mutex
	subscribers   map[uint64]chan contracts.JobSnapshot

	nextStudyNumber atomic.Uint64
	nextJobNumber   atomic.Uint64
	nextSubscriber  atomic.Uint64
}

// JobUpdateSubscription owns a buffered job-update channel. Dropping the
// receiver is enough to stop receiving updates; Unsubscribe removes it eagerly.
type JobUpdateSubscription struct {
	ID       uint64
	Receiver <-chan contracts.JobSnapshot
}

type jobRegistry struct {
	snapshots     map[string]contracts.JobSnapshot
	terminalOrder []string
}

type resultCache struct {
	entries map[string]contracts.JobResult
	order   []string
}

func New(cfg config.Config) (*App, error) {
	startedAt := time.Now().UTC()
	app := &App{
		config:             cfg,
		startedAt:          startedAt,
		cacheSessionID:     fmt.Sprintf("%d-%d", os.Getpid(), startedAt.UnixNano()),
		cache:              cache.NewStoreWithPaths(cfg.Paths.CacheDir, cfg.Paths.PersistenceDir),
		persistence:        persistence.NewCatalog(cfg.Paths.PersistenceDir),
		sourcePreviewCache: cache.DefaultSessionSourcePreviewCache(),
		studies:            make(map[string]contracts.StudyRecord),
		jobs: jobRegistry{
			snapshots: make(map[string]contracts.JobSnapshot),
		},
		activeFingerprints: make(map[string]string),
		resultCache: resultCache{
			entries: make(map[string]contracts.JobResult),
		},
		subscribers: make(map[uint64]chan contracts.JobSnapshot),
	}
	app.nextStudyNumber.Store(1)
	app.nextJobNumber.Store(1)
	app.nextSubscriber.Store(1)
	return app, nil
}

func (app *App) Config() config.Config {
	return app.config
}

func (app *App) StartedAt() time.Time {
	return app.startedAt
}

func (app *App) Prepare() error {
	if err := app.cache.Ensure(); err != nil {
		return err
	}
	return app.persistence.Ensure()
}

func (app *App) StudyCount() int {
	app.studiesMu.Lock()
	defer app.studiesMu.Unlock()
	return len(app.studies)
}

func (app *App) SupportedJobKinds() [3]string {
	return [3]string{string(contracts.JobKindRenderStudy), string(contracts.JobKindAnalyzeStudy), string(contracts.JobKindProcessStudy)}
}

func (app *App) GetProcessingManifest() contracts.ProcessingManifest {
	return contracts.DefaultProcessingManifest()
}

func (app *App) SubscribeJobUpdates() JobUpdateSubscription {
	id := app.nextSubscriber.Add(1) - 1
	receiver := make(chan contracts.JobSnapshot, JobUpdateBufferSize)
	app.subscribersMu.Lock()
	app.subscribers[id] = receiver
	app.subscribersMu.Unlock()
	return JobUpdateSubscription{ID: id, Receiver: receiver}
}

func (app *App) UnsubscribeJobUpdates(subscriptionID uint64) {
	app.subscribersMu.Lock()
	if receiver, ok := app.subscribers[subscriptionID]; ok {
		delete(app.subscribers, subscriptionID)
		close(receiver)
	}
	app.subscribersMu.Unlock()
}

func (app *App) OpenStudy(command contracts.OpenStudyCommand) (contracts.OpenStudyCommandResult, error) {
	if stringsTrimmedEmpty(command.InputPath) {
		return contracts.OpenStudyCommandResult{}, contracts.InvalidInputError("inputPath is required")
	}
	if err := validateInputFile(command.InputPath); err != nil {
		return contracts.OpenStudyCommandResult{}, err
	}
	if _, err := bmp.ReadFile(command.InputPath); err != nil {
		return contracts.OpenStudyCommandResult{}, contracts.InvalidInputError(fmt.Sprintf("failed to read study metadata: %v", err))
	}

	study, err := app.RegisterStudy(command.InputPath, nil)
	if err != nil {
		return contracts.OpenStudyCommandResult{}, err
	}
	_ = app.persistence.RecordOpenedStudy(study)
	return contracts.OpenStudyCommandResult{Study: study}, nil
}

func (app *App) RegisterStudy(inputPath string, scale *contracts.MeasurementScale) (contracts.StudyRecord, error) {
	inputName := filepath.Base(inputPath)
	if inputName == "." || inputName == string(filepath.Separator) || inputName == "" {
		inputName = inputPath
	}
	studyID := fmt.Sprintf("study-%d", app.nextStudyNumber.Add(1)-1)
	study := contracts.StudyRecord{
		StudyID:          studyID,
		InputPath:        inputPath,
		InputName:        inputName,
		MeasurementScale: cloneMeasurementScale(scale),
	}

	app.studiesMu.Lock()
	app.studies[studyID] = study
	app.studiesMu.Unlock()
	return study, nil
}

func (app *App) MeasureLineAnnotation(command contracts.MeasureLineAnnotationCommand) (contracts.MeasureLineAnnotationCommandResult, error) {
	studyID := stringsTrim(command.StudyID)
	if studyID == "" {
		return contracts.MeasureLineAnnotationCommandResult{}, contracts.InvalidInputError("studyId is required")
	}
	study, err := app.study(studyID)
	if err != nil {
		return contracts.MeasureLineAnnotationCommandResult{}, err
	}
	if err := validateLineAnnotationPoints(command.Annotation); err != nil {
		return contracts.MeasureLineAnnotationCommandResult{}, err
	}
	return contracts.MeasureLineAnnotationCommandResult{
		StudyID:    study.StudyID,
		Annotation: annotations.MeasureLineAnnotation(command.Annotation, study.MeasurementScale),
	}, nil
}

// SetStudyCalibration sets or clears a study's measurement calibration. With a
// reference, the segment's pixel length is recomputed via annotations.MeasureLine
// (so all measurement math stays in one place) and divided into the known mm
// length to get an isotropic mm-per-pixel scale. A single in-plane segment can
// only constrain one ratio, so row and column spacing are set equal — anisotropic
// spacing would need a second reference along the other axis. A nil reference
// clears any existing calibration.
func (app *App) SetStudyCalibration(command contracts.SetStudyCalibrationCommand) (contracts.SetStudyCalibrationCommandResult, error) {
	studyID := stringsTrim(command.StudyID)
	if studyID == "" {
		return contracts.SetStudyCalibrationCommandResult{}, contracts.InvalidInputError("studyId is required")
	}

	var scale *contracts.MeasurementScale
	if command.Reference != nil {
		derived, err := scaleFromCalibrationReference(*command.Reference)
		if err != nil {
			return contracts.SetStudyCalibrationCommandResult{}, err
		}
		scale = &derived
	}

	app.studiesMu.Lock()
	existing, ok := app.studies[studyID]
	if !ok {
		app.studiesMu.Unlock()
		return contracts.SetStudyCalibrationCommandResult{}, contracts.NotFoundError(fmt.Sprintf("study not found: %s", studyID))
	}
	existing.MeasurementScale = cloneMeasurementScale(scale)
	app.studies[studyID] = existing
	updated := cloneStudyRecord(existing)
	app.studiesMu.Unlock()

	// Persist so calibration survives a reopen. Best-effort, like OpenStudy.
	_ = app.persistence.RecordOpenedStudy(updated)
	return contracts.SetStudyCalibrationCommandResult{Study: updated}, nil
}

// scaleFromCalibrationReference derives an isotropic mm-per-pixel
// MeasurementScale from a reference segment of known physical length.
func scaleFromCalibrationReference(reference contracts.CalibrationReference) (contracts.MeasurementScale, error) {
	if !finite(reference.Start.X) || !finite(reference.Start.Y) ||
		!finite(reference.End.X) || !finite(reference.End.Y) {
		return contracts.MeasurementScale{}, contracts.InvalidInputError("calibration reference points must be finite numbers")
	}
	if !finite(reference.KnownLengthMM) || reference.KnownLengthMM <= 0 {
		return contracts.MeasurementScale{}, contracts.InvalidInputError("calibration length must be a positive number of millimetres")
	}

	// Reuse the canonical pixel-length math instead of re-implementing hypot.
	pixelLength := annotations.MeasureLine(reference.Start, reference.End, nil).PixelLength
	if pixelLength <= 0 {
		return contracts.MeasurementScale{}, contracts.InvalidInputError("calibration reference line must have a non-zero pixel length")
	}

	mmPerPixel := reference.KnownLengthMM / pixelLength
	return contracts.MeasurementScale{
		RowSpacingMM:    mmPerPixel,
		ColumnSpacingMM: mmPerPixel,
		Source:          "manualCalibration",
	}, nil
}

func (app *App) StartRenderJob(command contracts.RenderStudyCommand) (contracts.StartedJob, error) {
	studyID := stringsTrim(command.StudyID)
	if studyID == "" {
		return contracts.StartedJob{}, contracts.InvalidInputError("studyId is required")
	}
	study, err := app.study(studyID)
	if err != nil {
		return contracts.StartedJob{}, err
	}
	fingerprint, err := app.renderFingerprint(study)
	if err != nil {
		return contracts.StartedJob{}, err
	}
	if started, ok, err := app.startCachedJob(fingerprint, contracts.JobKindRenderStudy, study.StudyID); err != nil || ok {
		return started, err
	}

	jobID := app.nextJobID()
	snapshot, err := app.renderStudyJob(jobID, fingerprint, study)
	if err != nil {
		snapshot = failedJobSnapshot(jobID, contracts.JobKindRenderStudy, &study.StudyID, toBackendError(err))
	}
	app.storeCompletedResult(fingerprint, snapshot)
	app.storeJobSnapshot(snapshot)
	app.publishJobUpdate(snapshot)
	app.evictArtifactsIfNeeded()
	return contracts.StartedJob{JobID: jobID}, nil
}

func (app *App) StartRenderJobAsync(command contracts.RenderStudyCommand) (contracts.StartedJob, error) {
	studyID := stringsTrim(command.StudyID)
	if studyID == "" {
		return contracts.StartedJob{}, contracts.InvalidInputError("studyId is required")
	}
	study, err := app.study(studyID)
	if err != nil {
		return contracts.StartedJob{}, err
	}
	fingerprint, err := app.renderFingerprint(study)
	if err != nil {
		return contracts.StartedJob{}, err
	}
	if started, ok, err := app.startCachedJob(fingerprint, contracts.JobKindRenderStudy, study.StudyID); err != nil || ok {
		return started, err
	}
	jobID, created := app.reserveAsyncJob(fingerprint, contracts.JobKindRenderStudy, study.StudyID)
	if !created {
		return contracts.StartedJob{JobID: jobID}, nil
	}
	go app.runRenderJobAsync(jobID, fingerprint, study)
	return contracts.StartedJob{JobID: jobID}, nil
}

func (app *App) StartProcessJob(command contracts.ProcessStudyCommand) (contracts.StartedJob, error) {
	studyID := stringsTrim(command.StudyID)
	if studyID == "" {
		return contracts.StartedJob{}, contracts.InvalidInputError("studyId is required")
	}
	study, err := app.study(studyID)
	if err != nil {
		return contracts.StartedJob{}, err
	}
	resolved, backendErr := processing.ResolveProcessStudyCommand(command)
	if backendErr != nil {
		return contracts.StartedJob{}, *backendErr
	}
	fingerprint, err := app.processFingerprint(study, resolved)
	if err != nil {
		return contracts.StartedJob{}, err
	}
	if started, ok, err := app.startCachedJob(fingerprint, contracts.JobKindProcessStudy, study.StudyID); err != nil || ok {
		return started, err
	}

	jobID := app.nextJobID()
	snapshot, err := app.processStudyJob(jobID, fingerprint, study, resolved)
	if err != nil {
		snapshot = failedJobSnapshot(jobID, contracts.JobKindProcessStudy, &study.StudyID, toBackendError(err))
	}
	app.storeCompletedResult(fingerprint, snapshot)
	app.storeJobSnapshot(snapshot)
	app.publishJobUpdate(snapshot)
	app.evictArtifactsIfNeeded()
	return contracts.StartedJob{JobID: jobID}, nil
}

func (app *App) StartProcessJobAsync(command contracts.ProcessStudyCommand) (contracts.StartedJob, error) {
	studyID := stringsTrim(command.StudyID)
	if studyID == "" {
		return contracts.StartedJob{}, contracts.InvalidInputError("studyId is required")
	}
	study, err := app.study(studyID)
	if err != nil {
		return contracts.StartedJob{}, err
	}
	resolved, backendErr := processing.ResolveProcessStudyCommand(command)
	if backendErr != nil {
		return contracts.StartedJob{}, *backendErr
	}
	fingerprint, err := app.processFingerprint(study, resolved)
	if err != nil {
		return contracts.StartedJob{}, err
	}
	if started, ok, err := app.startCachedJob(fingerprint, contracts.JobKindProcessStudy, study.StudyID); err != nil || ok {
		return started, err
	}
	jobID, created := app.reserveAsyncJob(fingerprint, contracts.JobKindProcessStudy, study.StudyID)
	if !created {
		return contracts.StartedJob{JobID: jobID}, nil
	}
	go app.runProcessJobAsync(jobID, fingerprint, study, resolved)
	return contracts.StartedJob{JobID: jobID}, nil
}

func (app *App) StartAnalyzeJob(command contracts.AnalyzeStudyCommand) (contracts.StartedJob, error) {
	studyID := stringsTrim(command.StudyID)
	if studyID == "" {
		return contracts.StartedJob{}, contracts.InvalidInputError("studyId is required")
	}
	study, err := app.study(studyID)
	if err != nil {
		return contracts.StartedJob{}, err
	}
	fingerprint, err := app.analyzeFingerprint(study)
	if err != nil {
		return contracts.StartedJob{}, err
	}
	if started, ok, err := app.startCachedJob(fingerprint, contracts.JobKindAnalyzeStudy, study.StudyID); err != nil || ok {
		return started, err
	}

	jobID := app.nextJobID()
	snapshot, err := app.analyzeStudyJob(jobID, fingerprint, study)
	if err != nil {
		snapshot = failedJobSnapshot(jobID, contracts.JobKindAnalyzeStudy, &study.StudyID, toBackendError(err))
	}
	app.storeCompletedResult(fingerprint, snapshot)
	app.storeJobSnapshot(snapshot)
	app.publishJobUpdate(snapshot)
	app.evictArtifactsIfNeeded()
	return contracts.StartedJob{JobID: jobID}, nil
}

func (app *App) StartAnalyzeJobAsync(command contracts.AnalyzeStudyCommand) (contracts.StartedJob, error) {
	studyID := stringsTrim(command.StudyID)
	if studyID == "" {
		return contracts.StartedJob{}, contracts.InvalidInputError("studyId is required")
	}
	study, err := app.study(studyID)
	if err != nil {
		return contracts.StartedJob{}, err
	}
	fingerprint, err := app.analyzeFingerprint(study)
	if err != nil {
		return contracts.StartedJob{}, err
	}
	if started, ok, err := app.startCachedJob(fingerprint, contracts.JobKindAnalyzeStudy, study.StudyID); err != nil || ok {
		return started, err
	}
	jobID, created := app.reserveAsyncJob(fingerprint, contracts.JobKindAnalyzeStudy, study.StudyID)
	if !created {
		return contracts.StartedJob{JobID: jobID}, nil
	}
	go app.runAnalyzeJobAsync(jobID, fingerprint, study)
	return contracts.StartedJob{JobID: jobID}, nil
}

func (app *App) GetJob(command contracts.JobCommand) (contracts.JobSnapshot, error) {
	jobID := stringsTrim(command.JobID)
	if jobID == "" {
		return contracts.JobSnapshot{}, contracts.InvalidInputError("jobId is required")
	}
	app.jobsMu.Lock()
	defer app.jobsMu.Unlock()
	snapshot, ok := app.jobs.snapshots[jobID]
	if !ok {
		return contracts.JobSnapshot{}, contracts.NotFoundError(fmt.Sprintf("job not found: %s", jobID))
	}
	return cloneJobSnapshot(snapshot), nil
}

func (app *App) GetJobs(command contracts.GetJobsCommand) ([]contracts.JobSnapshot, error) {
	app.jobsMu.Lock()
	defer app.jobsMu.Unlock()
	snapshots := make([]contracts.JobSnapshot, 0, len(command.JobIDs))
	for _, jobID := range command.JobIDs {
		jobID = stringsTrim(jobID)
		if jobID == "" {
			continue
		}
		if snapshot, ok := app.jobs.snapshots[jobID]; ok {
			snapshots = append(snapshots, cloneJobSnapshot(snapshot))
		}
	}
	return snapshots, nil
}

func (app *App) CancelJob(command contracts.JobCommand) (contracts.JobSnapshot, error) {
	jobID := stringsTrim(command.JobID)
	if jobID == "" {
		return contracts.JobSnapshot{}, contracts.InvalidInputError("jobId is required")
	}
	app.jobsMu.Lock()
	defer app.jobsMu.Unlock()
	snapshot, ok := app.jobs.snapshots[jobID]
	if !ok {
		return contracts.JobSnapshot{}, contracts.NotFoundError(fmt.Sprintf("job not found: %s", jobID))
	}
	switch snapshot.State {
	case contracts.JobStateQueued:
		snapshot.State = contracts.JobStateCancelled
		snapshot.Progress.Message = "Cancelled before start"
		snapshot.Result = nil
		snapshot.Error = nil
		app.removeActiveJob(jobID)
		app.jobs.insert(snapshot)
	case contracts.JobStateRunning, contracts.JobStateCancelling:
		snapshot.State = contracts.JobStateCancelling
		snapshot.Progress.Message = "Cancellation requested"
		snapshot.Result = nil
		snapshot.Error = nil
		app.removeActiveJob(jobID)
		app.jobs.insert(snapshot)
	}
	app.publishJobUpdate(snapshot)
	return cloneJobSnapshot(snapshot), nil
}

func (app *App) study(studyID string) (contracts.StudyRecord, error) {
	app.studiesMu.Lock()
	defer app.studiesMu.Unlock()
	study, ok := app.studies[studyID]
	if !ok {
		return contracts.StudyRecord{}, contracts.NotFoundError(fmt.Sprintf("study not found: %s", studyID))
	}
	return cloneStudyRecord(study), nil
}

func (app *App) renderStudyJob(jobID, fingerprint string, study contracts.StudyRecord) (contracts.JobSnapshot, error) {
	preview, err := app.loadSourcePreview(study)
	if err != nil {
		return contracts.JobSnapshot{}, err
	}
	previewPath, err := app.cache.ArtifactPath("render", fingerprint, "bmp")
	if err != nil {
		return contracts.JobSnapshot{}, err
	}
	if err := render.SaveGrayBMP(previewPath, preview.Width, preview.Height, preview.Pixels); err != nil {
		return contracts.JobSnapshot{}, contracts.InternalError(err.Error())
	}
	app.trackArtifactBytes(previewPath)

	result := contracts.RenderStudyCommandResult{
		StudyID:          study.StudyID,
		PreviewPath:      previewPath,
		LoadedWidth:      preview.Width,
		LoadedHeight:     preview.Height,
		MeasurementScale: cloneMeasurementScale(study.MeasurementScale),
	}
	return completedJobSnapshot(jobID, contracts.JobKindRenderStudy, &study.StudyID, contracts.JobResult{
		Kind:    contracts.JobKindRenderStudy,
		Payload: result,
	}), nil
}

func (app *App) processStudyJob(jobID, fingerprint string, study contracts.StudyRecord, resolved processing.ResolvedProcessStudy) (contracts.JobSnapshot, error) {
	source, err := app.cachedSourcePreview(study)
	if err != nil {
		return contracts.JobSnapshot{}, err
	}
	sourcePreview := render.Gray(source.Width, source.Height, source.Pixels)
	output, err := processing.ProcessRenderedPreview(sourcePreview, resolved.Controls, resolved.Palette, resolved.Compare)
	if err != nil {
		return contracts.JobSnapshot{}, contracts.InternalError(fmt.Sprintf("process source preview: %v", err))
	}
	previewPath, err := app.cache.ArtifactPath("process", fingerprint, "bmp")
	if err != nil {
		return contracts.JobSnapshot{}, err
	}
	if err := render.SavePreviewBMP(previewPath, output.Preview); err != nil {
		return contracts.JobSnapshot{}, contracts.InternalError(err.Error())
	}
	app.trackArtifactBytes(previewPath)

	result := contracts.ProcessStudyCommandResult{
		StudyID:          study.StudyID,
		PreviewPath:      previewPath,
		LoadedWidth:      source.Width,
		LoadedHeight:     source.Height,
		Mode:             output.Mode,
		MeasurementScale: cloneMeasurementScale(study.MeasurementScale),
	}
	return completedJobSnapshot(jobID, contracts.JobKindProcessStudy, &study.StudyID, contracts.JobResult{
		Kind:    contracts.JobKindProcessStudy,
		Payload: result,
	}), nil
}

func (app *App) analyzeStudyJob(jobID, fingerprint string, study contracts.StudyRecord) (contracts.JobSnapshot, error) {
	source, err := app.cachedSourcePreview(study)
	if err != nil {
		return contracts.JobSnapshot{}, err
	}
	output, err := analysis.GenerateToothOverlay(render.Gray(source.Width, source.Height, source.Pixels))
	if err != nil {
		return contracts.JobSnapshot{}, contracts.InternalError(fmt.Sprintf("analyze source preview: %v", err))
	}
	previewPath, err := app.cache.ArtifactPath("analyze", fingerprint, "bmp")
	if err != nil {
		return contracts.JobSnapshot{}, err
	}
	filledPreviewPath, err := app.cache.ArtifactPath("analyze-filled", fingerprint, "bmp")
	if err != nil {
		return contracts.JobSnapshot{}, err
	}
	if err := render.SavePreviewBMP(previewPath, output.Preview); err != nil {
		return contracts.JobSnapshot{}, contracts.InternalError(err.Error())
	}
	app.trackArtifactBytes(previewPath)
	if err := render.SavePreviewBMP(filledPreviewPath, output.FilledPreview); err != nil {
		return contracts.JobSnapshot{}, contracts.InternalError(err.Error())
	}
	app.trackArtifactBytes(filledPreviewPath)

	result := contracts.AnalyzeStudyCommandResult{
		StudyID:           study.StudyID,
		PreviewPath:       previewPath,
		FilledPreviewPath: filledPreviewPath,
		LoadedWidth:       source.Width,
		LoadedHeight:      source.Height,
		Mode:              output.Mode,
		MeasurementScale:  cloneMeasurementScale(study.MeasurementScale),
	}
	return completedJobSnapshot(jobID, contracts.JobKindAnalyzeStudy, &study.StudyID, contracts.JobResult{
		Kind:    contracts.JobKindAnalyzeStudy,
		Payload: result,
	}), nil
}

func (app *App) runRenderJobAsync(jobID, fingerprint string, study contracts.StudyRecord) {
	if result, ok := app.runJobStage(jobID, fingerprint, 10, "validating", "Validating source study", nil, func() error {
		return validateInputFile(study.InputPath)
	}); !ok || result != nil {
		if result != nil {
			app.finishAsyncJob(jobID, fingerprint, failedJobSnapshot(jobID, contracts.JobKindRenderStudy, &study.StudyID, toBackendError(result)))
		}
		return
	}

	source, err, ok := runJobStageValue(app, jobID, fingerprint, 35, "loadingStudy", "Loading source study", nil, func() (bmp.RenderedPreview, error) {
		return app.loadSourcePreview(study)
	})
	if !ok {
		return
	}
	if err != nil {
		app.finishAsyncJob(jobID, fingerprint, failedJobSnapshot(jobID, contracts.JobKindRenderStudy, &study.StudyID, toBackendError(err)))
		return
	}

	if result, ok := app.runJobStage(jobID, fingerprint, 75, "renderingPreview", "Rendering preview", nil, func() error {
		return nil
	}); !ok || result != nil {
		if result != nil {
			app.finishAsyncJob(jobID, fingerprint, failedJobSnapshot(jobID, contracts.JobKindRenderStudy, &study.StudyID, toBackendError(result)))
		}
		return
	}

	var previewPath string
	if result, ok := app.runJobStage(jobID, fingerprint, 90, "writingPreview", "Writing preview", nil, func() error {
		path, err := app.cache.ArtifactPath("render", fingerprint, "bmp")
		if err != nil {
			return err
		}
		previewPath = path
		if err := render.SaveGrayBMP(path, source.Width, source.Height, source.Pixels); err != nil {
			return contracts.InternalError(err.Error())
		}
		app.trackArtifactBytes(path)
		return nil
	}); !ok || result != nil {
		if previewPath != "" {
			_ = os.Remove(previewPath)
		}
		if result != nil {
			app.finishAsyncJob(jobID, fingerprint, failedJobSnapshot(jobID, contracts.JobKindRenderStudy, &study.StudyID, toBackendError(result)))
		}
		return
	}

	result := contracts.RenderStudyCommandResult{
		StudyID:          study.StudyID,
		PreviewPath:      previewPath,
		LoadedWidth:      source.Width,
		LoadedHeight:     source.Height,
		MeasurementScale: cloneMeasurementScale(study.MeasurementScale),
	}
	app.finishAsyncJob(jobID, fingerprint, completedJobSnapshot(jobID, contracts.JobKindRenderStudy, &study.StudyID, contracts.JobResult{
		Kind:    contracts.JobKindRenderStudy,
		Payload: result,
	}))
}

func (app *App) runProcessJobAsync(jobID, fingerprint string, study contracts.StudyRecord, resolved processing.ResolvedProcessStudy) {
	var previewPath string
	if result, ok := app.runJobStage(jobID, fingerprint, 10, "validating", "Validating processing request", nil, func() error {
		if err := validateInputFile(study.InputPath); err != nil {
			return err
		}
		path, err := app.cache.ArtifactPath("process", fingerprint, "bmp")
		if err != nil {
			return err
		}
		previewPath = path
		return nil
	}); !ok || result != nil {
		if result != nil {
			app.finishAsyncJob(jobID, fingerprint, failedJobSnapshot(jobID, contracts.JobKindProcessStudy, &study.StudyID, toBackendError(result)))
		}
		return
	}

	source, err, ok := runJobStageValue(app, jobID, fingerprint, 30, "loadingStudy", "Loading source pixels", []string{previewPath}, func() (bmp.RenderedPreview, error) {
		return app.loadSourcePreview(study)
	})
	if !ok {
		return
	}
	if err != nil {
		_ = os.Remove(previewPath)
		app.finishAsyncJob(jobID, fingerprint, failedJobSnapshot(jobID, contracts.JobKindProcessStudy, &study.StudyID, toBackendError(err)))
		return
	}

	output, err, ok := runJobStageValue(app, jobID, fingerprint, 65, "processingPixels", "Applying processing pipeline", []string{previewPath}, func() (processing.PipelineOutput, error) {
		sourcePreview := render.Gray(source.Width, source.Height, source.Pixels)
		return processing.ProcessRenderedPreview(sourcePreview, resolved.Controls, resolved.Palette, resolved.Compare)
	})
	if !ok {
		return
	}
	if err != nil {
		_ = os.Remove(previewPath)
		app.finishAsyncJob(jobID, fingerprint, failedJobSnapshot(jobID, contracts.JobKindProcessStudy, &study.StudyID, toBackendError(err)))
		return
	}

	if result, ok := app.runJobStage(jobID, fingerprint, 84, "writingPreview", "Writing processed preview", []string{previewPath}, func() error {
		if err := render.SavePreviewBMP(previewPath, output.Preview); err != nil {
			return contracts.InternalError(err.Error())
		}
		app.trackArtifactBytes(previewPath)
		return nil
	}); !ok || result != nil {
		_ = os.Remove(previewPath)
		if result != nil {
			app.finishAsyncJob(jobID, fingerprint, failedJobSnapshot(jobID, contracts.JobKindProcessStudy, &study.StudyID, toBackendError(result)))
		}
		return
	}

	result := contracts.ProcessStudyCommandResult{
		StudyID:          study.StudyID,
		PreviewPath:      previewPath,
		LoadedWidth:      source.Width,
		LoadedHeight:     source.Height,
		Mode:             output.Mode,
		MeasurementScale: cloneMeasurementScale(study.MeasurementScale),
	}
	app.finishAsyncJob(jobID, fingerprint, completedJobSnapshot(jobID, contracts.JobKindProcessStudy, &study.StudyID, contracts.JobResult{
		Kind:    contracts.JobKindProcessStudy,
		Payload: result,
	}))
}

func (app *App) runAnalyzeJobAsync(jobID, fingerprint string, study contracts.StudyRecord) {
	var previewPath string
	var filledPreviewPath string
	if result, ok := app.runJobStage(jobID, fingerprint, 10, "validating", "Validating analysis request", nil, func() error {
		if err := validateInputFile(study.InputPath); err != nil {
			return err
		}
		path, err := app.cache.ArtifactPath("analyze", fingerprint, "bmp")
		if err != nil {
			return err
		}
		filledPath, err := app.cache.ArtifactPath("analyze-filled", fingerprint, "bmp")
		if err != nil {
			return err
		}
		previewPath = path
		filledPreviewPath = filledPath
		return nil
	}); !ok || result != nil {
		if result != nil {
			app.finishAsyncJob(jobID, fingerprint, failedJobSnapshot(jobID, contracts.JobKindAnalyzeStudy, &study.StudyID, toBackendError(result)))
		}
		return
	}

	source, err, ok := runJobStageValue(app, jobID, fingerprint, 30, "loadingStudy", "Loading source pixels", []string{previewPath, filledPreviewPath}, func() (bmp.DecodedSourcePreview, error) {
		return app.cachedSourcePreview(study)
	})
	if !ok {
		return
	}
	if err != nil {
		cleanup([]string{previewPath, filledPreviewPath})
		app.finishAsyncJob(jobID, fingerprint, failedJobSnapshot(jobID, contracts.JobKindAnalyzeStudy, &study.StudyID, toBackendError(err)))
		return
	}

	output, err, ok := runJobStageValue(app, jobID, fingerprint, 62, "analyzingPixels", "Detecting tooth and bone overlay", []string{previewPath, filledPreviewPath}, func() (analysis.OverlayResult, error) {
		return analysis.GenerateToothOverlay(render.Gray(source.Width, source.Height, source.Pixels))
	})
	if !ok {
		return
	}
	if err != nil {
		cleanup([]string{previewPath, filledPreviewPath})
		app.finishAsyncJob(jobID, fingerprint, failedJobSnapshot(jobID, contracts.JobKindAnalyzeStudy, &study.StudyID, toBackendError(err)))
		return
	}

	if result, ok := app.runJobStage(jobID, fingerprint, 86, "writingPreview", "Writing tooth and bone overlay preview", []string{previewPath, filledPreviewPath}, func() error {
		if err := render.SavePreviewBMP(previewPath, output.Preview); err != nil {
			return contracts.InternalError(err.Error())
		}
		app.trackArtifactBytes(previewPath)
		if err := render.SavePreviewBMP(filledPreviewPath, output.FilledPreview); err != nil {
			return contracts.InternalError(err.Error())
		}
		app.trackArtifactBytes(filledPreviewPath)
		return nil
	}); !ok || result != nil {
		cleanup([]string{previewPath, filledPreviewPath})
		if result != nil {
			app.finishAsyncJob(jobID, fingerprint, failedJobSnapshot(jobID, contracts.JobKindAnalyzeStudy, &study.StudyID, toBackendError(result)))
		}
		return
	}

	result := contracts.AnalyzeStudyCommandResult{
		StudyID:           study.StudyID,
		PreviewPath:       previewPath,
		FilledPreviewPath: filledPreviewPath,
		LoadedWidth:       source.Width,
		LoadedHeight:      source.Height,
		Mode:              output.Mode,
		MeasurementScale:  cloneMeasurementScale(study.MeasurementScale),
	}
	app.finishAsyncJob(jobID, fingerprint, completedJobSnapshot(jobID, contracts.JobKindAnalyzeStudy, &study.StudyID, contracts.JobResult{
		Kind:    contracts.JobKindAnalyzeStudy,
		Payload: result,
	}))
}

func (app *App) loadSourcePreview(study contracts.StudyRecord) (bmp.RenderedPreview, error) {
	source, err := app.cachedSourcePreview(study)
	if err != nil {
		return bmp.RenderedPreview{}, err
	}
	return bmp.RenderGrayscalePreviewFromSource(source), nil
}

func (app *App) cachedSourcePreview(study contracts.StudyRecord) (bmp.DecodedSourcePreview, error) {
	return app.sourcePreviewCache.GetOrTryInsertWith(inputCacheKey(study.InputPath), func() (bmp.DecodedSourcePreview, error) {
		source, err := bmp.DecodeSourcePreviewFile(study.InputPath)
		if err != nil {
			return bmp.DecodedSourcePreview{}, contracts.InvalidInputError(fmt.Sprintf("failed to render study: %v", err))
		}
		return source, nil
	})
}

func (app *App) renderFingerprint(study contracts.StudyRecord) (string, error) {
	return fingerprintJSON(map[string]any{
		"namespace":     "render-study",
		"sessionId":     app.cacheSessionID,
		"inputPath":     study.InputPath,
		"inputIdentity": currentInputFileIdentity(study.InputPath),
	})
}

func (app *App) analyzeFingerprint(study contracts.StudyRecord) (string, error) {
	return fingerprintJSON(map[string]any{
		"namespace":     "analyze-study",
		"outputVersion": "sections-reference-mask-v22",
		"sessionId":     app.cacheSessionID,
		"inputPath":     study.InputPath,
		"inputIdentity": currentInputFileIdentity(study.InputPath),
	})
}

func (app *App) processFingerprint(study contracts.StudyRecord, resolved processing.ResolvedProcessStudy) (string, error) {
	return fingerprintJSON(map[string]any{
		"namespace":     "process-study",
		"sessionId":     app.cacheSessionID,
		"inputPath":     study.InputPath,
		"inputIdentity": currentInputFileIdentity(study.InputPath),
		"invert":        resolved.Controls.Invert,
		"brightness":    resolved.Controls.Brightness,
		"contrast":      resolved.Controls.Contrast,
		"equalize":      resolved.Controls.Equalize,
		"compare":       resolved.Compare,
		"palette":       resolved.Palette.Label(),
	})
}

func (app *App) startCachedJob(fingerprint string, kind contracts.JobKind, studyID string) (contracts.StartedJob, bool, error) {
	app.resultCacheMu.Lock()
	cached, ok := app.resultCache.entries[fingerprint]
	app.resultCacheMu.Unlock()
	if !ok {
		return contracts.StartedJob{}, false, nil
	}
	if !resultArtifactsExist(cached) {
		app.resultCacheMu.Lock()
		app.resultCache.remove(fingerprint)
		app.resultCacheMu.Unlock()
		return contracts.StartedJob{}, false, nil
	}
	result, err := cachedResultForStudy(cached, studyID)
	if err != nil {
		return contracts.StartedJob{}, false, err
	}
	jobID := app.nextJobID()
	snapshot := cachedJobSnapshot(jobID, kind, &studyID, result)
	app.storeJobSnapshot(snapshot)
	app.publishJobUpdate(snapshot)
	return contracts.StartedJob{JobID: jobID}, true, nil
}

func (app *App) reserveAsyncJob(fingerprint string, kind contracts.JobKind, studyID string) (string, bool) {
	app.jobsMu.Lock()
	defer app.jobsMu.Unlock()
	app.activeMu.Lock()
	defer app.activeMu.Unlock()

	if jobID, ok := app.activeFingerprints[fingerprint]; ok {
		if snapshot, exists := app.jobs.snapshots[jobID]; exists && !isTerminalState(snapshot.State) {
			return jobID, false
		}
		delete(app.activeFingerprints, fingerprint)
	}

	jobID := app.nextJobID()
	snapshot := queuedJobSnapshot(jobID, kind, &studyID)
	app.jobs.insert(snapshot)
	app.activeFingerprints[fingerprint] = jobID
	go app.publishJobUpdate(snapshot)
	return jobID, true
}

func (app *App) updateJobProgress(jobID string, state contracts.JobState, percent int, stage, message string) {
	app.jobsMu.Lock()
	snapshot, ok := app.jobs.snapshots[jobID]
	if !ok || isTerminalState(snapshot.State) || snapshot.State == contracts.JobStateCancelling {
		app.jobsMu.Unlock()
		return
	}
	snapshot.State = state
	snapshot.Progress = contracts.JobProgress{Percent: percent, Stage: stage, Message: message}
	snapshot.Error = nil
	app.jobs.insert(snapshot)
	app.jobsMu.Unlock()
	app.publishJobUpdate(snapshot)
}

func (app *App) runJobStage(jobID, fingerprint string, percent int, stage, message string, cleanupPaths []string, work func() error) (error, bool) {
	if app.finishCancelledIfRequested(jobID, fingerprint, stage, cleanupPaths) {
		return nil, false
	}
	app.updateJobProgress(jobID, contracts.JobStateRunning, percent, stage, message)
	if app.finishCancelledIfRequested(jobID, fingerprint, stage, cleanupPaths) {
		return nil, false
	}
	result := work()
	if app.finishCancelledIfRequested(jobID, fingerprint, stage, cleanupPaths) {
		return nil, false
	}
	return result, true
}

func runJobStageValue[T any](app *App, jobID, fingerprint string, percent int, stage, message string, cleanupPaths []string, work func() (T, error)) (T, error, bool) {
	var zero T
	err, ok := app.runJobStage(jobID, fingerprint, percent, stage, message, cleanupPaths, func() error {
		value, err := work()
		if err == nil {
			zero = value
		}
		return err
	})
	return zero, err, ok
}

func (app *App) finishCancelledIfRequested(jobID, fingerprint, stage string, cleanupPaths []string) bool {
	app.jobsMu.Lock()
	snapshot, ok := app.jobs.snapshots[jobID]
	app.jobsMu.Unlock()
	if !ok {
		return true
	}
	if snapshot.State == contracts.JobStateCancelled {
		return true
	}
	if snapshot.State != contracts.JobStateCancelling {
		return false
	}

	cleanup(cleanupPaths)
	app.activeMu.Lock()
	if activeJobID, ok := app.activeFingerprints[fingerprint]; ok && activeJobID == jobID {
		delete(app.activeFingerprints, fingerprint)
	}
	app.activeMu.Unlock()

	snapshot.State = contracts.JobStateCancelled
	snapshot.Progress.Message = "Cancelled by user"
	if stage != "" {
		snapshot.Progress.Stage = stage
	}
	snapshot.Result = nil
	snapshot.Error = nil
	app.storeJobSnapshot(snapshot)
	app.publishJobUpdate(snapshot)
	return true
}

func (app *App) finishAsyncJob(jobID, fingerprint string, terminalSnapshot contracts.JobSnapshot) {
	app.activeMu.Lock()
	if activeJobID, ok := app.activeFingerprints[fingerprint]; ok && activeJobID == jobID {
		delete(app.activeFingerprints, fingerprint)
	}
	app.activeMu.Unlock()

	app.jobsMu.Lock()
	current, ok := app.jobs.snapshots[jobID]
	if ok && (current.State == contracts.JobStateCancelling || current.State == contracts.JobStateCancelled) {
		if terminalSnapshot.Result != nil {
			cleanup(resultArtifactPaths(*terminalSnapshot.Result))
		}
		terminalSnapshot = current
		terminalSnapshot.State = contracts.JobStateCancelled
		terminalSnapshot.Progress.Message = "Cancelled by user"
		terminalSnapshot.Result = nil
		terminalSnapshot.Error = nil
	} else if ok && isTerminalState(current.State) {
		terminalSnapshot = current
	}
	app.jobs.insert(terminalSnapshot)
	app.jobsMu.Unlock()

	if app.storeCompletedResult(fingerprint, terminalSnapshot) {
		app.evictArtifactsIfNeeded()
	}
	app.publishJobUpdate(terminalSnapshot)
}

func (app *App) storeCompletedResult(fingerprint string, snapshot contracts.JobSnapshot) bool {
	if snapshot.State != contracts.JobStateCompleted || snapshot.FromCache || snapshot.Result == nil {
		return false
	}
	app.resultCacheMu.Lock()
	defer app.resultCacheMu.Unlock()
	app.resultCache.insert(fingerprint, *snapshot.Result)
	return true
}

func (app *App) storeJobSnapshot(snapshot contracts.JobSnapshot) {
	app.jobsMu.Lock()
	defer app.jobsMu.Unlock()
	app.jobs.insert(snapshot)
}

func (app *App) publishJobUpdate(snapshot contracts.JobSnapshot) {
	update := cloneJobSnapshot(snapshot)
	app.subscribersMu.Lock()
	defer app.subscribersMu.Unlock()
	for _, subscriber := range app.subscribers {
		select {
		case subscriber <- update:
		default:
		}
	}
}

func (app *App) trackArtifactBytes(path string) {
	if metadata, err := os.Stat(path); err == nil {
		app.cache.AddArtifactBytes(uint64(metadata.Size()))
	}
}

func (app *App) evictArtifactsIfNeeded() {
	_, _ = app.cache.EvictArtifactsOverLimit(MaxArtifactBytes)
}

func (app *App) nextJobID() string {
	return fmt.Sprintf("job-%d", app.nextJobNumber.Add(1)-1)
}

func (app *App) removeActiveJob(jobID string) {
	app.activeMu.Lock()
	defer app.activeMu.Unlock()
	for fingerprint, activeJobID := range app.activeFingerprints {
		if activeJobID == jobID {
			delete(app.activeFingerprints, fingerprint)
		}
	}
}

func (registry *jobRegistry) insert(snapshot contracts.JobSnapshot) {
	jobID := snapshot.JobID
	previousState := contracts.JobState("")
	if previous, ok := registry.snapshots[jobID]; ok {
		previousState = previous.State
	}
	registry.snapshots[jobID] = cloneJobSnapshot(snapshot)
	registry.syncTerminalOrder(jobID, previousState)
	registry.evictOldTerminalJobs(jobID)
}

func (registry *jobRegistry) syncTerminalOrder(jobID string, previousState contracts.JobState) {
	wasTerminal := isTerminalState(previousState)
	isTerminal := isTerminalState(registry.snapshots[jobID].State)
	switch {
	case !wasTerminal && isTerminal:
		registry.terminalOrder = append(registry.terminalOrder, jobID)
	case wasTerminal && !isTerminal:
		registry.removeTerminal(jobID)
	}
}

func (registry *jobRegistry) removeTerminal(jobID string) {
	next := registry.terminalOrder[:0]
	for _, candidate := range registry.terminalOrder {
		if candidate != jobID {
			next = append(next, candidate)
		}
	}
	registry.terminalOrder = next
}

func (registry *jobRegistry) evictOldTerminalJobs(keepJobID string) {
	for len(registry.snapshots) > MaxTerminalJobs {
		evictIndex := -1
		for index, jobID := range registry.terminalOrder {
			if jobID != keepJobID && isTerminalState(registry.snapshots[jobID].State) {
				evictIndex = index
				break
			}
		}
		if evictIndex < 0 {
			return
		}
		evictJobID := registry.terminalOrder[evictIndex]
		registry.terminalOrder = append(registry.terminalOrder[:evictIndex], registry.terminalOrder[evictIndex+1:]...)
		delete(registry.snapshots, evictJobID)
	}
}

func (cache *resultCache) insert(fingerprint string, result contracts.JobResult) {
	if _, exists := cache.entries[fingerprint]; exists {
		cache.remove(fingerprint)
	}
	cache.entries[fingerprint] = cloneJobResult(result)
	cache.order = append(cache.order, fingerprint)
	for len(cache.entries) > MaxResultCacheEntries {
		evict := cache.order[0]
		cache.order = cache.order[1:]
		delete(cache.entries, evict)
	}
}

func (cache *resultCache) remove(fingerprint string) {
	delete(cache.entries, fingerprint)
	next := cache.order[:0]
	for _, candidate := range cache.order {
		if candidate != fingerprint {
			next = append(next, candidate)
		}
	}
	cache.order = next
}

func queuedJobSnapshot(jobID string, kind contracts.JobKind, studyID *string) contracts.JobSnapshot {
	return contracts.JobSnapshot{
		JobID:   jobID,
		JobKind: kind,
		StudyID: cloneString(studyID),
		State:   contracts.JobStateQueued,
		Progress: contracts.JobProgress{
			Percent: 0,
			Stage:   "queued",
			Message: "Queued",
		},
	}
}

func completedJobSnapshot(jobID string, kind contracts.JobKind, studyID *string, result contracts.JobResult) contracts.JobSnapshot {
	return contracts.JobSnapshot{
		JobID:   jobID,
		JobKind: kind,
		StudyID: cloneString(studyID),
		State:   contracts.JobStateCompleted,
		Progress: contracts.JobProgress{
			Percent: 100,
			Stage:   "completed",
			Message: "Completed",
		},
		Result: &result,
	}
}

func cachedJobSnapshot(jobID string, kind contracts.JobKind, studyID *string, result contracts.JobResult) contracts.JobSnapshot {
	return contracts.JobSnapshot{
		JobID:     jobID,
		JobKind:   kind,
		StudyID:   cloneString(studyID),
		State:     contracts.JobStateCompleted,
		FromCache: true,
		Progress: contracts.JobProgress{
			Percent: 100,
			Stage:   "cacheHit",
			Message: "Loaded from cache",
		},
		Result: &result,
	}
}

func failedJobSnapshot(jobID string, kind contracts.JobKind, studyID *string, backendErr contracts.BackendError) contracts.JobSnapshot {
	return contracts.JobSnapshot{
		JobID:   jobID,
		JobKind: kind,
		StudyID: cloneString(studyID),
		State:   contracts.JobStateFailed,
		Progress: contracts.JobProgress{
			Percent: 100,
			Stage:   "failed",
			Message: backendErr.Message,
		},
		Error: &backendErr,
	}
}

func isTerminalState(state contracts.JobState) bool {
	return state == contracts.JobStateCompleted || state == contracts.JobStateFailed || state == contracts.JobStateCancelled
}

type inputFileIdentity struct {
	State           string  `json:"state"`
	Size            *uint64 `json:"size,omitempty"`
	Mode            *uint32 `json:"mode,omitempty"`
	ModTimeUnixNano *int64  `json:"modTimeUnixNano,omitempty"`
}

func currentInputFileIdentity(inputPath string) inputFileIdentity {
	metadata, err := os.Stat(inputPath)
	if err != nil {
		return inputFileIdentity{State: "unavailable"}
	}
	size := uint64(metadata.Size())
	mode := fileMode(metadata)
	modTime := metadata.ModTime().UnixNano()
	state := "file"
	if metadata.IsDir() {
		state = "directory"
	}
	return inputFileIdentity{State: state, Size: &size, Mode: &mode, ModTimeUnixNano: &modTime}
}

func inputCacheKey(inputPath string) string {
	identity := currentInputFileIdentity(inputPath)
	return fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d",
		inputPath,
		identity.State,
		valueOrZero(identity.Size),
		valueOrZero(identity.Mode),
		valueOrZero(identity.ModTimeUnixNano),
	)
}

func fileMode(metadata os.FileInfo) uint32 {
	if runtime.GOOS == "windows" {
		return 0
	}
	if stat, ok := metadata.Sys().(*syscall.Stat_t); ok {
		return uint32(stat.Mode)
	}
	return uint32(metadata.Mode().Perm())
}

func fingerprintJSON(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", contracts.InternalError(fmt.Sprintf("serialize job fingerprint: %v", err))
	}
	hash := fnv.New64a()
	_, _ = hash.Write(payload)
	return fmt.Sprintf("%016x", hash.Sum64()), nil
}

func resultArtifactPaths(result contracts.JobResult) []string {
	switch payload := result.Payload.(type) {
	case contracts.RenderStudyCommandResult:
		return []string{payload.PreviewPath}
	case contracts.ProcessStudyCommandResult:
		return []string{payload.PreviewPath}
	case contracts.AnalyzeStudyCommandResult:
		return []string{payload.PreviewPath, payload.FilledPreviewPath}
	case map[string]any:
		if result.Kind == contracts.JobKindAnalyzeStudy {
			return []string{stringFromMap(payload, "previewPath"), stringFromMap(payload, "filledPreviewPath")}
		}
		return []string{stringFromMap(payload, "previewPath")}
	default:
		return nil
	}
}

func resultArtifactsExist(result contracts.JobResult) bool {
	paths := resultArtifactPaths(result)
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if path == "" {
			return false
		}
		if metadata, err := os.Stat(path); err != nil || !metadata.Mode().IsRegular() {
			return false
		}
	}
	return true
}

func cleanup(paths []string) {
	for _, path := range paths {
		if path != "" {
			_ = os.Remove(path)
		}
	}
}

func cachedResultForStudy(result contracts.JobResult, studyID string) (contracts.JobResult, error) {
	cloned := cloneJobResult(result)
	switch payload := cloned.Payload.(type) {
	case contracts.RenderStudyCommandResult:
		payload.StudyID = studyID
		cloned.Payload = payload
	case contracts.ProcessStudyCommandResult:
		payload.StudyID = studyID
		cloned.Payload = payload
	case contracts.AnalyzeStudyCommandResult:
		payload.StudyID = studyID
		cloned.Payload = payload
	case map[string]any:
		payload["studyId"] = studyID
		cloned.Payload = payload
	default:
		return contracts.JobResult{}, contracts.InternalError("deserialize cached result: unsupported payload shape")
	}
	return cloned, nil
}

func validateInputFile(inputPath string) error {
	metadata, err := os.Stat(inputPath)
	if err != nil {
		if os.IsNotExist(err) {
			return contracts.NotFoundError(fmt.Sprintf("input file does not exist: %s", inputPath))
		}
		return contracts.InternalError(fmt.Sprintf("failed to inspect input file %s: %v", inputPath, err))
	}
	if metadata.IsDir() {
		return contracts.InvalidInputError(fmt.Sprintf("input path must be a file: %s", inputPath))
	}
	return nil
}

func validateLineAnnotationPoints(annotation contracts.LineAnnotation) error {
	if !finite(annotation.Start.X) || !finite(annotation.Start.Y) || !finite(annotation.End.X) || !finite(annotation.End.Y) {
		return contracts.InvalidInputError("line annotation points must be finite numbers")
	}
	return nil
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func toBackendError(err error) contracts.BackendError {
	if backendErr, ok := err.(contracts.BackendError); ok {
		return backendErr
	}
	return contracts.InternalError(err.Error())
}

func cloneStudyRecord(study contracts.StudyRecord) contracts.StudyRecord {
	study.MeasurementScale = cloneMeasurementScale(study.MeasurementScale)
	return study
}

func cloneMeasurementScale(scale *contracts.MeasurementScale) *contracts.MeasurementScale {
	if scale == nil {
		return nil
	}
	copy := *scale
	return &copy
}

func cloneJobSnapshot(snapshot contracts.JobSnapshot) contracts.JobSnapshot {
	snapshot.StudyID = cloneString(snapshot.StudyID)
	if snapshot.Result != nil {
		result := cloneJobResult(*snapshot.Result)
		snapshot.Result = &result
	}
	if snapshot.Error != nil {
		backendErr := *snapshot.Error
		snapshot.Error = &backendErr
	}
	return snapshot
}

func cloneJobResult(result contracts.JobResult) contracts.JobResult {
	payloadBytes, err := json.Marshal(result.Payload)
	if err != nil {
		return result
	}
	var payload any
	switch result.Kind {
	case contracts.JobKindRenderStudy:
		var typed contracts.RenderStudyCommandResult
		if json.Unmarshal(payloadBytes, &typed) == nil {
			payload = typed
		}
	case contracts.JobKindProcessStudy:
		var typed contracts.ProcessStudyCommandResult
		if json.Unmarshal(payloadBytes, &typed) == nil {
			payload = typed
		}
	case contracts.JobKindAnalyzeStudy:
		var typed contracts.AnalyzeStudyCommandResult
		if json.Unmarshal(payloadBytes, &typed) == nil {
			payload = typed
		}
	}
	if payload == nil {
		_ = json.Unmarshal(payloadBytes, &payload)
	}
	result.Payload = payload
	return result
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func stringsTrim(value string) string {
	start := 0
	end := len(value)
	for start < end && (value[start] == ' ' || value[start] == '\t' || value[start] == '\n' || value[start] == '\r') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\t' || value[end-1] == '\n' || value[end-1] == '\r') {
		end--
	}
	return value[start:end]
}

func stringsTrimmedEmpty(value string) bool {
	return stringsTrim(value) == ""
}

func stringFromMap(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func valueOrZero[T ~uint64 | ~uint32 | ~int64](value *T) T {
	if value == nil {
		var zero T
		return zero
	}
	return *value
}
