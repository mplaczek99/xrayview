package httpapi

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/bmp"

	"xrayview/backend/internal/annotations"
	"xrayview/backend/internal/cache"
	"xrayview/backend/internal/config"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/dicommeta"
	"xrayview/backend/internal/jobs"
	"xrayview/backend/internal/persistence"
	"xrayview/backend/internal/studies"
)

type testDeps struct {
	RouterDeps
	Cache       *cache.Store
	Persistence *persistence.Catalog
	Jobs        *jobs.Service
	Studies     *studies.Registry
}

type testBackendService struct {
	logger      *slog.Logger
	persistence *persistence.Catalog
	jobs        *jobs.Service
	studies     *studies.Registry
}

type mockBackendService struct {
	openStudy             func(contracts.OpenStudyCommand) (contracts.OpenStudyCommandResult, error)
	startRenderJob        func(contracts.RenderStudyCommand) (contracts.StartedJob, error)
	startProcessJob       func(contracts.ProcessStudyCommand) (contracts.StartedJob, error)
	startAnalyzeJob       func(contracts.AnalyzeStudyCommand) (contracts.StartedJob, error)
	getJob                func(contracts.JobCommand) (contracts.JobSnapshot, error)
	cancelJob             func(contracts.JobCommand) (contracts.JobSnapshot, error)
	getProcessingManifest func() contracts.ProcessingManifest
	measureLineAnnotation func(contracts.MeasureLineAnnotationCommand) (contracts.MeasureLineAnnotationCommandResult, error)
}

func (service mockBackendService) OpenStudy(
	command contracts.OpenStudyCommand,
) (contracts.OpenStudyCommandResult, error) {
	return service.openStudy(command)
}

func (service mockBackendService) StartRenderJob(
	command contracts.RenderStudyCommand,
) (contracts.StartedJob, error) {
	return service.startRenderJob(command)
}

func (service mockBackendService) StartProcessJob(
	command contracts.ProcessStudyCommand,
) (contracts.StartedJob, error) {
	return service.startProcessJob(command)
}

func (service mockBackendService) StartAnalyzeJob(
	command contracts.AnalyzeStudyCommand,
) (contracts.StartedJob, error) {
	return service.startAnalyzeJob(command)
}

func (service mockBackendService) GetJob(command contracts.JobCommand) (contracts.JobSnapshot, error) {
	return service.getJob(command)
}

func (service mockBackendService) CancelJob(
	command contracts.JobCommand,
) (contracts.JobSnapshot, error) {
	return service.cancelJob(command)
}

func (service mockBackendService) GetProcessingManifest() contracts.ProcessingManifest {
	return service.getProcessingManifest()
}

func (service mockBackendService) MeasureLineAnnotation(
	command contracts.MeasureLineAnnotationCommand,
) (contracts.MeasureLineAnnotationCommandResult, error) {
	return service.measureLineAnnotation(command)
}

func (service testBackendService) OpenStudy(
	command contracts.OpenStudyCommand,
) (contracts.OpenStudyCommandResult, error) {
	if command.InputPath == "" {
		return contracts.OpenStudyCommandResult{}, contracts.InvalidInput("inputPath is required")
	}

	info, err := os.Stat(command.InputPath)
	if err != nil {
		if os.IsNotExist(err) {
			return contracts.OpenStudyCommandResult{}, contracts.NotFound(
				fmt.Sprintf("input file does not exist: %s", command.InputPath),
			)
		}

		return contracts.OpenStudyCommandResult{}, contracts.Internal(
			fmt.Sprintf("failed to inspect input file %s: %v", command.InputPath, err),
		)
	}
	if info.IsDir() {
		return contracts.OpenStudyCommandResult{}, contracts.InvalidInput(
			fmt.Sprintf("input path must be a file: %s", command.InputPath),
		)
	}

	metadata, err := dicommeta.ReadFile(command.InputPath)
	if err != nil {
		return contracts.OpenStudyCommandResult{}, contracts.InvalidInput(
			fmt.Sprintf("failed to read study metadata: %v", err),
		)
	}

	study, err := service.studies.Register(command.InputPath, metadata.MeasurementScale())
	if err != nil {
		return contracts.OpenStudyCommandResult{}, contracts.Internal(
			fmt.Sprintf("failed to register study: %v", err),
		)
	}

	if err := service.persistence.RecordOpenedStudy(study); err != nil && service.logger != nil {
		service.logger.Warn(
			"failed to record opened study",
			slog.String("study_id", study.StudyID),
			slog.String("input_path", study.InputPath),
			slog.Any("error", err),
		)
	}

	return contracts.OpenStudyCommandResult{Study: study}, nil
}

func (service testBackendService) StartRenderJob(
	command contracts.RenderStudyCommand,
) (contracts.StartedJob, error) {
	return service.jobs.StartRenderJob(command)
}

func (service testBackendService) StartProcessJob(
	command contracts.ProcessStudyCommand,
) (contracts.StartedJob, error) {
	return service.jobs.StartProcessJob(command)
}

func (service testBackendService) StartAnalyzeJob(
	command contracts.AnalyzeStudyCommand,
) (contracts.StartedJob, error) {
	return service.jobs.StartAnalyzeJob(command)
}

func (service testBackendService) GetJob(command contracts.JobCommand) (contracts.JobSnapshot, error) {
	return service.jobs.GetJob(command)
}

func (service testBackendService) CancelJob(
	command contracts.JobCommand,
) (contracts.JobSnapshot, error) {
	return service.jobs.CancelJob(command)
}

func (service testBackendService) GetProcessingManifest() contracts.ProcessingManifest {
	return contracts.DefaultProcessingManifest()
}

func (service testBackendService) MeasureLineAnnotation(
	command contracts.MeasureLineAnnotationCommand,
) (contracts.MeasureLineAnnotationCommandResult, error) {
	studyID := strings.TrimSpace(command.StudyID)
	if studyID == "" {
		return contracts.MeasureLineAnnotationCommandResult{}, contracts.InvalidInput("studyId is required")
	}

	study, ok := service.studies.Get(studyID)
	if !ok {
		return contracts.MeasureLineAnnotationCommandResult{}, contracts.NotFound(
			fmt.Sprintf("study not found: %s", studyID),
		)
	}

	return contracts.MeasureLineAnnotationCommandResult{
		StudyID: study.StudyID,
		Annotation: annotations.MeasureLineAnnotation(
			command.Annotation,
			study.MeasurementScale,
		),
	}, nil
}

func (service testBackendService) SupportedJobKinds() []string {
	return service.jobs.SupportedKinds()
}

func (service testBackendService) StudyCount() int {
	return service.studies.Count()
}

func testDependencies(t *testing.T) testDeps {
	t.Helper()

	rootDir := filepath.Join(t.TempDir(), "xrayview")
	cacheStore := cache.NewWithRoot(rootDir)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	studyRegistry := studies.New()
	jobService := jobs.New(cacheStore, studyRegistry, logger)
	cfg := config.Default()
	persistenceCatalog := persistence.New(cacheStore.PersistenceDir())
	service := testBackendService{
		logger:      logger,
		persistence: persistenceCatalog,
		jobs:        jobService,
		studies:     studyRegistry,
	}

	return testDeps{
		RouterDeps: RouterDeps{
			Service:     service,
			Config:      cfg,
			Logger:      logger,
			Cache:       cacheStore,
			Persistence: persistenceCatalog,
			StartedAt:   time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
		},
		Cache:       cacheStore,
		Persistence: persistenceCatalog,
		Jobs:        jobService,
		Studies:     studyRegistry,
	}
}

func testRouter(t *testing.T) http.Handler {
	t.Helper()

	return NewRouter(testDependencies(t).RouterDeps)
}

func TestHealthzIncludesContractMetadata(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	testRouter(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload runtimeResponse
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if payload.Service != contracts.ServiceName {
		t.Fatalf("service = %q, want %q", payload.Service, contracts.ServiceName)
	}

	if payload.Transport != TransportKind {
		t.Fatalf("transport = %q, want %q", payload.Transport, TransportKind)
	}

	if !payload.LocalOnly {
		t.Fatal("localOnly = false, want true")
	}

	if payload.BackendContractVersion != contracts.BackendContractVersion {
		t.Fatalf("contract version = %d, want %d", payload.BackendContractVersion, contracts.BackendContractVersion)
	}

	if payload.APIBasePath != APIBasePath {
		t.Fatalf("api base path = %q, want %q", payload.APIBasePath, APIBasePath)
	}

	if payload.CommandEndpoint != CommandEndpointTemplate {
		t.Fatalf("command endpoint = %q, want %q", payload.CommandEndpoint, CommandEndpointTemplate)
	}
}

func TestCommandsEndpointListsSupportedCommands(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/commands", nil)
	testRouter(t).ServeHTTP(recorder, request)

	var payload commandListResponse
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(payload.Commands) != len(contracts.SupportedCommands) {
		t.Fatalf("command count = %d, want %d", len(payload.Commands), len(contracts.SupportedCommands))
	}
}

func TestRouterDispatchesOpenStudyToBackendService(t *testing.T) {
	inputPath := sampleDicomPath(t)
	called := false
	handler := NewRouter(RouterDeps{
		Config: config.Default(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Service: mockBackendService{
			openStudy: func(command contracts.OpenStudyCommand) (contracts.OpenStudyCommandResult, error) {
				called = true
				if got, want := command.InputPath, inputPath; got != want {
					t.Fatalf("InputPath = %q, want %q", got, want)
				}

				return contracts.OpenStudyCommandResult{
					Study: contracts.StudyRecord{
						StudyID:   "study-1",
						InputPath: command.InputPath,
						InputName: filepath.Base(command.InputPath),
					},
				}, nil
			},
			startRenderJob: func(contracts.RenderStudyCommand) (contracts.StartedJob, error) {
				return contracts.StartedJob{}, fmt.Errorf("unexpected StartRenderJob call")
			},
			startProcessJob: func(contracts.ProcessStudyCommand) (contracts.StartedJob, error) {
				return contracts.StartedJob{}, fmt.Errorf("unexpected StartProcessJob call")
			},
			startAnalyzeJob: func(contracts.AnalyzeStudyCommand) (contracts.StartedJob, error) {
				return contracts.StartedJob{}, fmt.Errorf("unexpected StartAnalyzeJob call")
			},
			getJob: func(contracts.JobCommand) (contracts.JobSnapshot, error) {
				return contracts.JobSnapshot{}, fmt.Errorf("unexpected GetJob call")
			},
			cancelJob: func(contracts.JobCommand) (contracts.JobSnapshot, error) {
				return contracts.JobSnapshot{}, fmt.Errorf("unexpected CancelJob call")
			},
			getProcessingManifest: contracts.DefaultProcessingManifest,
			measureLineAnnotation: func(contracts.MeasureLineAnnotationCommand) (contracts.MeasureLineAnnotationCommandResult, error) {
				return contracts.MeasureLineAnnotationCommandResult{}, fmt.Errorf("unexpected MeasureLineAnnotation call")
			},
		},
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/open_study",
		strings.NewReader(`{"inputPath":"`+inputPath+`"}`),
	)
	request.Header.Set("content-type", "application/json")
	handler.ServeHTTP(recorder, request)

	if !called {
		t.Fatal("OpenStudy was not dispatched to backend service")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestRouterDispatchesCancelJobToBackendService(t *testing.T) {
	called := false
	handler := NewRouter(RouterDeps{
		Config: config.Default(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Service: mockBackendService{
			openStudy: func(contracts.OpenStudyCommand) (contracts.OpenStudyCommandResult, error) {
				return contracts.OpenStudyCommandResult{}, fmt.Errorf("unexpected OpenStudy call")
			},
			startRenderJob: func(contracts.RenderStudyCommand) (contracts.StartedJob, error) {
				return contracts.StartedJob{}, fmt.Errorf("unexpected StartRenderJob call")
			},
			startProcessJob: func(contracts.ProcessStudyCommand) (contracts.StartedJob, error) {
				return contracts.StartedJob{}, fmt.Errorf("unexpected StartProcessJob call")
			},
			startAnalyzeJob: func(contracts.AnalyzeStudyCommand) (contracts.StartedJob, error) {
				return contracts.StartedJob{}, fmt.Errorf("unexpected StartAnalyzeJob call")
			},
			getJob: func(contracts.JobCommand) (contracts.JobSnapshot, error) {
				return contracts.JobSnapshot{}, fmt.Errorf("unexpected GetJob call")
			},
			cancelJob: func(command contracts.JobCommand) (contracts.JobSnapshot, error) {
				called = true
				if got, want := command.JobID, "job-1"; got != want {
					t.Fatalf("JobID = %q, want %q", got, want)
				}

				return contracts.JobSnapshot{
					JobID:   command.JobID,
					JobKind: contracts.JobKindRenderStudy,
					State:   contracts.JobStateCancelled,
					Progress: contracts.JobProgress{
						Percent: 35,
						Stage:   "loadingStudy",
						Message: "Cancelled by user",
					},
				}, nil
			},
			getProcessingManifest: contracts.DefaultProcessingManifest,
			measureLineAnnotation: func(contracts.MeasureLineAnnotationCommand) (contracts.MeasureLineAnnotationCommandResult, error) {
				return contracts.MeasureLineAnnotationCommandResult{}, fmt.Errorf("unexpected MeasureLineAnnotation call")
			},
		},
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/cancel_job",
		strings.NewReader(`{"jobId":"job-1"}`),
	)
	request.Header.Set("content-type", "application/json")
	handler.ServeHTTP(recorder, request)

	if !called {
		t.Fatal("CancelJob was not dispatched to backend service")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload contracts.JobSnapshot
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if got, want := payload.JobID, "job-1"; got != want {
		t.Fatalf("JobID = %q, want %q", got, want)
	}
	if got, want := payload.State, contracts.JobStateCancelled; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}
}

func TestCommandEndpointRejectsTrailingJSONContent(t *testing.T) {
	called := false
	handler := NewRouter(RouterDeps{
		Config: config.Default(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Service: mockBackendService{
			openStudy: func(contracts.OpenStudyCommand) (contracts.OpenStudyCommandResult, error) {
				called = true
				return contracts.OpenStudyCommandResult{}, nil
			},
			startRenderJob: func(contracts.RenderStudyCommand) (contracts.StartedJob, error) {
				return contracts.StartedJob{}, fmt.Errorf("unexpected StartRenderJob call")
			},
			startProcessJob: func(contracts.ProcessStudyCommand) (contracts.StartedJob, error) {
				return contracts.StartedJob{}, fmt.Errorf("unexpected StartProcessJob call")
			},
			startAnalyzeJob: func(contracts.AnalyzeStudyCommand) (contracts.StartedJob, error) {
				return contracts.StartedJob{}, fmt.Errorf("unexpected StartAnalyzeJob call")
			},
			getJob: func(contracts.JobCommand) (contracts.JobSnapshot, error) {
				return contracts.JobSnapshot{}, fmt.Errorf("unexpected GetJob call")
			},
			cancelJob: func(contracts.JobCommand) (contracts.JobSnapshot, error) {
				return contracts.JobSnapshot{}, fmt.Errorf("unexpected CancelJob call")
			},
			getProcessingManifest: contracts.DefaultProcessingManifest,
			measureLineAnnotation: func(contracts.MeasureLineAnnotationCommand) (contracts.MeasureLineAnnotationCommandResult, error) {
				return contracts.MeasureLineAnnotationCommandResult{}, fmt.Errorf("unexpected MeasureLineAnnotation call")
			},
		},
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/open_study",
		strings.NewReader(`{"inputPath":"`+sampleDicomPath(t)+`"} true`),
	)
	request.Header.Set("content-type", "application/json")
	handler.ServeHTTP(recorder, request)

	if called {
		t.Fatal("OpenStudy was called for invalid trailing JSON payload")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	var payload contracts.BackendError
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if got, want := payload.Code, contracts.BackendErrorCodeInvalidInput; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if got, want := payload.Details[0], "unexpected trailing JSON content"; got != want {
		t.Fatalf("error detail = %q, want %q", got, want)
	}
}

func TestGetProcessingManifestReturnsFrozenPayload(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/get_processing_manifest",
		nil,
	)
	testRouter(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload any
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	expected := decodeJSONValue(t, `{
		"defaultPresetId": "default",
		"presets": [
			{
				"id": "default",
				"controls": {
					"brightness": 0,
					"contrast": 1.0,
					"invert": false,
					"equalize": false,
					"palette": "none"
				}
			},
			{
				"id": "xray",
				"controls": {
					"brightness": 10,
					"contrast": 1.4,
					"invert": false,
					"equalize": true,
					"palette": "bone"
				}
			},
			{
				"id": "high-contrast",
				"controls": {
					"brightness": 0,
					"contrast": 1.8,
					"invert": false,
					"equalize": true,
					"palette": "none"
				}
			}
		]
	}`)

	if !reflect.DeepEqual(payload, expected) {
		t.Fatalf("manifest = %#v, want %#v", payload, expected)
	}
}

func TestOpenStudyRegistersStudyAndReturnsContractPayload(t *testing.T) {
	deps := testDependencies(t)
	handler := NewRouter(deps.RouterDeps)
	inputPath := sampleDicomPath(t)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/open_study",
		strings.NewReader(`{"inputPath":"`+inputPath+`"}`),
	)
	request.Header.Set("content-type", "application/json")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload contracts.OpenStudyCommandResult
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if payload.Study.StudyID == "" {
		t.Fatal("studyId = empty, want generated identifier")
	}

	if got, want := payload.Study.InputPath, inputPath; got != want {
		t.Fatalf("inputPath = %q, want %q", got, want)
	}

	if got, want := payload.Study.InputName, "sample-dental-radiograph.dcm"; got != want {
		t.Fatalf("inputName = %q, want %q", got, want)
	}
	if payload.Study.MeasurementScale != nil {
		t.Fatalf("measurementScale = %+v, want nil for sample fixture", payload.Study.MeasurementScale)
	}

	if got, want := deps.Studies.Count(), 1; got != want {
		t.Fatalf("study count = %d, want %d", got, want)
	}

	catalog, err := deps.Persistence.Load()
	if err != nil {
		t.Fatalf("catalog load failed: %v", err)
	}

	if got, want := len(catalog.RecentStudies), 1; got != want {
		t.Fatalf("recent study count = %d, want %d", got, want)
	}
}

func TestOpenStudyIncludesMeasurementScaleWhenSpacingMetadataExists(t *testing.T) {
	deps := testDependencies(t)
	handler := NewRouter(deps.RouterDeps)
	inputPath := filepath.Join(t.TempDir(), "scaled-study.dcm")
	if err := os.WriteFile(inputPath, buildScaledDicomFixture(), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/open_study",
		strings.NewReader(`{"inputPath":"`+inputPath+`"}`),
	)
	request.Header.Set("content-type", "application/json")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload contracts.OpenStudyCommandResult
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if payload.Study.MeasurementScale == nil {
		t.Fatal("measurementScale = nil, want PixelSpacing-derived scale")
	}
	if got, want := payload.Study.MeasurementScale.RowSpacingMM, 0.25; got != want {
		t.Fatalf("measurementScale.rowSpacingMm = %v, want %v", got, want)
	}
	if got, want := payload.Study.MeasurementScale.ColumnSpacingMM, 0.40; got != want {
		t.Fatalf("measurementScale.columnSpacingMm = %v, want %v", got, want)
	}
	if got, want := payload.Study.MeasurementScale.Source, "PixelSpacing"; got != want {
		t.Fatalf("measurementScale.source = %q, want %q", got, want)
	}
}

func TestOpenStudyAcceptsStandaloneBMPInput(t *testing.T) {
	deps := testDependencies(t)
	handler := NewRouter(deps.RouterDeps)

	inputPath := filepath.Join(t.TempDir(), "standalone.bmp")
	writeStandaloneBMPFixture(t, inputPath)

	payload := openStudyViaRouter(t, handler, inputPath)

	if got, want := payload.Study.InputPath, inputPath; got != want {
		t.Fatalf("inputPath = %q, want %q", got, want)
	}
	if got, want := payload.Study.InputName, "standalone.bmp"; got != want {
		t.Fatalf("inputName = %q, want %q", got, want)
	}
	if payload.Study.MeasurementScale != nil {
		t.Fatalf("measurementScale = %+v, want nil", payload.Study.MeasurementScale)
	}
	if got, want := deps.Studies.Count(), 1; got != want {
		t.Fatalf("study count = %d, want %d", got, want)
	}
}

func TestOpenStudyAcceptsActualBMPFixture(t *testing.T) {
	deps := testDependencies(t)
	handler := NewRouter(deps.RouterDeps)

	inputPath := actualBMPPath(t)
	payload := openStudyViaRouter(t, handler, inputPath)

	if got, want := payload.Study.InputPath, inputPath; got != want {
		t.Fatalf("inputPath = %q, want %q", got, want)
	}
	if got, want := payload.Study.InputName, "xrays1.bmp"; got != want {
		t.Fatalf("inputName = %q, want %q", got, want)
	}
}

func TestOpenStudyAcceptsActualTIFFFixture(t *testing.T) {
	deps := testDependencies(t)
	handler := NewRouter(deps.RouterDeps)

	inputPath := actualTIFFPath(t)
	payload := openStudyViaRouter(t, handler, inputPath)

	if got, want := payload.Study.InputPath, inputPath; got != want {
		t.Fatalf("inputPath = %q, want %q", got, want)
	}
	if got, want := payload.Study.InputName, "xrays1.tif"; got != want {
		t.Fatalf("inputName = %q, want %q", got, want)
	}
}

func TestOpenStudyReordersExistingRecentStudyWithoutDuplicate(t *testing.T) {
	deps := testDependencies(t)
	handler := NewRouter(deps.RouterDeps)
	firstPath := copySampleDicom(t, "first-open.dcm")
	secondPath := copySampleDicom(t, "second-open.dcm")

	for _, inputPath := range []string{firstPath, secondPath, firstPath} {
		openStudyViaRouter(t, handler, inputPath)
	}

	catalog, err := deps.Persistence.Load()
	if err != nil {
		t.Fatalf("catalog load failed: %v", err)
	}

	if got, want := len(catalog.RecentStudies), 2; got != want {
		t.Fatalf("recent study count = %d, want %d", got, want)
	}
	if got, want := catalog.RecentStudies[0].InputPath, firstPath; got != want {
		t.Fatalf("first recent study path = %q, want %q", got, want)
	}
	if got, want := catalog.RecentStudies[1].InputPath, secondPath; got != want {
		t.Fatalf("second recent study path = %q, want %q", got, want)
	}
}

func TestOpenStudyTruncatesRecentStudyCatalogToTenEntries(t *testing.T) {
	deps := testDependencies(t)
	handler := NewRouter(deps.RouterDeps)

	openedPaths := make([]string, 0, 12)
	for index := 0; index < 12; index++ {
		inputPath := copySampleDicom(t, fmt.Sprintf("study-%02d.dcm", index))
		openedPaths = append(openedPaths, inputPath)
		openStudyViaRouter(t, handler, inputPath)
	}

	catalog, err := deps.Persistence.Load()
	if err != nil {
		t.Fatalf("catalog load failed: %v", err)
	}

	if got, want := len(catalog.RecentStudies), 10; got != want {
		t.Fatalf("recent study count = %d, want %d", got, want)
	}
	if got, want := catalog.RecentStudies[0].InputPath, openedPaths[11]; got != want {
		t.Fatalf("first recent study path = %q, want %q", got, want)
	}
	if got, want := catalog.RecentStudies[9].InputPath, openedPaths[2]; got != want {
		t.Fatalf("last retained recent study path = %q, want %q", got, want)
	}
}

func TestOpenStudyRenamesCorruptedCatalogAndContinues(t *testing.T) {
	deps := testDependencies(t)
	handler := NewRouter(deps.RouterDeps)
	catalogPath := deps.Persistence.Path()
	if err := os.MkdirAll(filepath.Dir(catalogPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(catalogPath, []byte("{ not json"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	openStudyViaRouter(t, handler, sampleDicomPath(t))

	if _, err := os.Stat(filepath.Join(deps.Persistence.RootDir(), "catalog.corrupt.json")); err != nil {
		t.Fatalf("corrupt catalog was not renamed: %v", err)
	}

	catalog, err := deps.Persistence.Load()
	if err != nil {
		t.Fatalf("catalog load failed: %v", err)
	}
	if got, want := len(catalog.RecentStudies), 1; got != want {
		t.Fatalf("recent study count = %d, want %d", got, want)
	}
}

func TestOpenStudyRejectsNonDicomInput(t *testing.T) {
	inputPath := filepath.Join(t.TempDir(), "not-a-dicom.dcm")
	if err := os.WriteFile(inputPath, []byte("dicom"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/open_study",
		strings.NewReader(`{"inputPath":"`+inputPath+`"}`),
	)
	request.Header.Set("content-type", "application/json")
	testRouter(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	var payload contracts.BackendError
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if got, want := payload.Code, contracts.BackendErrorCodeInvalidInput; got != want {
		t.Fatalf("code = %q, want %q", got, want)
	}
}

func TestOpenStudyRejectsUnknownFields(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/open_study",
		strings.NewReader(`{"inputPath":"/tmp/sample.dcm","unexpected":true}`),
	)
	request.Header.Set("content-type", "application/json")
	testRouter(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	var payload contracts.BackendError
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if got, want := payload.Code, contracts.BackendErrorCodeInvalidInput; got != want {
		t.Fatalf("code = %q, want %q", got, want)
	}
}

func TestOpenStudyReturnsNotFoundForMissingInput(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/open_study",
		strings.NewReader(`{"inputPath":"/tmp/does-not-exist.dcm"}`),
	)
	request.Header.Set("content-type", "application/json")
	testRouter(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}

	var payload contracts.BackendError
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if got, want := payload.Code, contracts.BackendErrorCodeNotFound; got != want {
		t.Fatalf("code = %q, want %q", got, want)
	}
}

func TestMeasureLineAnnotationReturnsMeasuredPixels(t *testing.T) {
	deps := testDependencies(t)
	handler := NewRouter(deps.RouterDeps)
	study, err := deps.Studies.Register("/tmp/manual-measurement.dcm", nil)
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/measure_line_annotation",
		strings.NewReader(
			fmt.Sprintf(
				`{"studyId":%q,"annotation":{"id":"line-1","label":"Measurement 1","source":"manual","start":{"x":12,"y":18},"end":{"x":15,"y":22},"editable":true}}`,
				study.StudyID,
			),
		),
	)
	request.Header.Set("content-type", "application/json")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload contracts.MeasureLineAnnotationCommandResult
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if got, want := payload.StudyID, study.StudyID; got != want {
		t.Fatalf("studyId = %q, want %q", got, want)
	}
	if payload.Annotation.Measurement == nil {
		t.Fatal("annotation.measurement = nil, want populated measurement")
	}
	if got, want := payload.Annotation.Measurement.PixelLength, 5.0; got != want {
		t.Fatalf("annotation.measurement.pixelLength = %v, want %v", got, want)
	}
	if payload.Annotation.Measurement.CalibratedLengthMM != nil {
		t.Fatalf(
			"annotation.measurement.calibratedLengthMm = %v, want nil",
			*payload.Annotation.Measurement.CalibratedLengthMM,
		)
	}
}

func TestMeasureLineAnnotationReturnsCalibratedLength(t *testing.T) {
	deps := testDependencies(t)
	handler := NewRouter(deps.RouterDeps)
	study, err := deps.Studies.Register(
		"/tmp/calibrated-measurement.dcm",
		&contracts.MeasurementScale{
			RowSpacingMM:    0.2,
			ColumnSpacingMM: 0.3,
			Source:          "PixelSpacing",
		},
	)
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/measure_line_annotation",
		strings.NewReader(
			fmt.Sprintf(
				`{"studyId":%q,"annotation":{"id":"line-1","label":"Measurement 1","source":"manual","start":{"x":10,"y":8},"end":{"x":14,"y":11},"editable":true}}`,
				study.StudyID,
			),
		),
	)
	request.Header.Set("content-type", "application/json")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload contracts.MeasureLineAnnotationCommandResult
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if payload.Annotation.Measurement == nil {
		t.Fatal("annotation.measurement = nil, want populated measurement")
	}
	if payload.Annotation.Measurement.CalibratedLengthMM == nil {
		t.Fatal("annotation.measurement.calibratedLengthMm = nil, want 1.3")
	}
	if got, want := *payload.Annotation.Measurement.CalibratedLengthMM, 1.3; got != want {
		t.Fatalf("annotation.measurement.calibratedLengthMm = %v, want %v", got, want)
	}
}

func TestMeasureLineAnnotationRejectsUnknownStudy(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/measure_line_annotation",
		strings.NewReader(
			`{"studyId":"missing-study","annotation":{"id":"line-1","label":"Measurement 1","source":"manual","start":{"x":0,"y":0},"end":{"x":1,"y":1},"editable":true}}`,
		),
	)
	request.Header.Set("content-type", "application/json")
	testRouter(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}

	var payload contracts.BackendError
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if got, want := payload.Code, contracts.BackendErrorCodeNotFound; got != want {
		t.Fatalf("code = %q, want %q", got, want)
	}
}

func TestRenderJobEndpointsCompletePreview(t *testing.T) {
	assertRenderJobCompletesPreview(t, sampleDicomPath(t))
}

func TestRenderJobEndpointsCompletePreviewForActualBMPFixture(t *testing.T) {
	assertRenderJobCompletesPreview(t, actualBMPPath(t))
}

func TestRenderJobEndpointsCompletePreviewForActualTIFFFixture(t *testing.T) {
	assertRenderJobCompletesPreview(t, actualTIFFPath(t))
}

func assertRenderJobCompletesPreview(t *testing.T, inputPath string) {
	t.Helper()

	deps := testDependencies(t)
	handler := NewRouter(deps.RouterDeps)

	openRecorder := httptest.NewRecorder()
	openRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/open_study",
		strings.NewReader(`{"inputPath":"`+inputPath+`"}`),
	)
	openRequest.Header.Set("content-type", "application/json")
	handler.ServeHTTP(openRecorder, openRequest)

	if openRecorder.Code != http.StatusOK {
		t.Fatalf("open status = %d, want %d", openRecorder.Code, http.StatusOK)
	}

	var opened contracts.OpenStudyCommandResult
	if err := json.NewDecoder(openRecorder.Body).Decode(&opened); err != nil {
		t.Fatalf("decode open payload failed: %v", err)
	}

	startRecorder := httptest.NewRecorder()
	startRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/start_render_job",
		strings.NewReader(`{"studyId":"`+opened.Study.StudyID+`"}`),
	)
	startRequest.Header.Set("content-type", "application/json")
	handler.ServeHTTP(startRecorder, startRequest)

	if startRecorder.Code != http.StatusOK {
		t.Fatalf("start_render_job status = %d, want %d", startRecorder.Code, http.StatusOK)
	}

	var started contracts.StartedJob
	if err := json.NewDecoder(startRecorder.Body).Decode(&started); err != nil {
		t.Fatalf("decode started job failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		getRecorder := httptest.NewRecorder()
		getRequest := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/commands/get_job",
			strings.NewReader(`{"jobId":"`+started.JobID+`"}`),
		)
		getRequest.Header.Set("content-type", "application/json")
		handler.ServeHTTP(getRecorder, getRequest)

		if getRecorder.Code != http.StatusOK {
			t.Fatalf("get_job status = %d, want %d", getRecorder.Code, http.StatusOK)
		}

		var snapshot contracts.JobSnapshot
		if err := json.NewDecoder(getRecorder.Body).Decode(&snapshot); err != nil {
			t.Fatalf("decode job snapshot failed: %v", err)
		}

		switch snapshot.State {
		case contracts.JobStateQueued, contracts.JobStateRunning, contracts.JobStateCancelling:
			time.Sleep(25 * time.Millisecond)
		case contracts.JobStateFailed:
			t.Fatalf("render job failed: %#v", snapshot.Error)
		case contracts.JobStateCancelled:
			t.Fatal("render job was cancelled unexpectedly")
		case contracts.JobStateCompleted:
			if snapshot.Result == nil {
				t.Fatal("completed render job returned nil result")
			}
			if got, want := snapshot.Result.Kind, contracts.JobKindRenderStudy; got != want {
				t.Fatalf("result kind = %q, want %q", got, want)
			}

			payload, ok := snapshot.Result.Payload.(map[string]any)
			if !ok {
				t.Fatalf("result payload type = %T, want map[string]any", snapshot.Result.Payload)
			}

			previewPath, ok := payload["previewPath"].(string)
			if !ok || previewPath == "" {
				t.Fatalf("previewPath = %#v, want non-empty string", payload["previewPath"])
			}
			if info, err := os.Stat(previewPath); err != nil || info.IsDir() {
				t.Fatalf("preview artifact missing or invalid: %v", err)
			}
			return
		}
	}

	t.Fatal("render job did not complete before timeout")
}

func TestProcessJobEndpointCompletesPreview(t *testing.T) {
	deps := testDependencies(t)
	handler := NewRouter(deps.RouterDeps)
	inputPath := sampleDicomPath(t)

	openRecorder := httptest.NewRecorder()
	openRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/open_study",
		strings.NewReader(`{"inputPath":"`+inputPath+`"}`),
	)
	openRequest.Header.Set("content-type", "application/json")
	handler.ServeHTTP(openRecorder, openRequest)

	if openRecorder.Code != http.StatusOK {
		t.Fatalf("open status = %d, want %d", openRecorder.Code, http.StatusOK)
	}

	var opened contracts.OpenStudyCommandResult
	if err := json.NewDecoder(openRecorder.Body).Decode(&opened); err != nil {
		t.Fatalf("decode open payload failed: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "processed-output.dcm")
	startRecorder := httptest.NewRecorder()
	startRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/start_process_job",
		strings.NewReader(
			fmt.Sprintf(
				`{"studyId":%q,"outputPath":%q,"presetId":"xray","invert":false,"equalize":false,"compare":true}`,
				opened.Study.StudyID,
				outputPath,
			),
		),
	)
	startRequest.Header.Set("content-type", "application/json")
	handler.ServeHTTP(startRecorder, startRequest)

	if startRecorder.Code != http.StatusOK {
		t.Fatalf("start_process_job status = %d, want %d", startRecorder.Code, http.StatusOK)
	}

	var started contracts.StartedJob
	if err := json.NewDecoder(startRecorder.Body).Decode(&started); err != nil {
		t.Fatalf("decode started job failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		getRecorder := httptest.NewRecorder()
		getRequest := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/commands/get_job",
			strings.NewReader(`{"jobId":"`+started.JobID+`"}`),
		)
		getRequest.Header.Set("content-type", "application/json")
		handler.ServeHTTP(getRecorder, getRequest)

		if getRecorder.Code != http.StatusOK {
			t.Fatalf("get_job status = %d, want %d", getRecorder.Code, http.StatusOK)
		}

		var snapshot contracts.JobSnapshot
		if err := json.NewDecoder(getRecorder.Body).Decode(&snapshot); err != nil {
			t.Fatalf("decode job snapshot failed: %v", err)
		}

		switch snapshot.State {
		case contracts.JobStateQueued, contracts.JobStateRunning, contracts.JobStateCancelling:
			time.Sleep(25 * time.Millisecond)
		case contracts.JobStateFailed:
			t.Fatalf("process job failed: %#v", snapshot.Error)
		case contracts.JobStateCancelled:
			t.Fatal("process job was cancelled unexpectedly")
		case contracts.JobStateCompleted:
			if snapshot.Result == nil {
				t.Fatal("completed process job returned nil result")
			}
			if got, want := snapshot.Result.Kind, contracts.JobKindProcessStudy; got != want {
				t.Fatalf("result kind = %q, want %q", got, want)
			}

			payload, ok := snapshot.Result.Payload.(map[string]any)
			if !ok {
				t.Fatalf("result payload type = %T, want map[string]any", snapshot.Result.Payload)
			}

			previewPath, ok := payload["previewPath"].(string)
			if !ok || previewPath == "" {
				t.Fatalf("previewPath = %#v, want non-empty string", payload["previewPath"])
			}
			if info, err := os.Stat(previewPath); err != nil || info.IsDir() {
				t.Fatalf("preview artifact missing or invalid: %v", err)
			}

			dicomPath, ok := payload["dicomPath"].(string)
			if !ok || dicomPath == "" {
				t.Fatalf("dicomPath = %#v, want non-empty string", payload["dicomPath"])
			}
			if got, want := dicomPath, outputPath; got != want {
				t.Fatalf("dicomPath = %q, want %q", got, want)
			}
			if info, err := os.Stat(dicomPath); err != nil || info.IsDir() {
				t.Fatalf("dicom artifact missing or invalid: %v", err)
			}
			metadata, err := dicommeta.ReadFile(dicomPath)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			if got, want := metadata.TransferSyntaxUID, "1.2.840.10008.1.2.1"; got != want {
				t.Fatalf("TransferSyntaxUID = %q, want %q", got, want)
			}
			return
		}
	}

	t.Fatal("process job did not complete before timeout")
}

func TestAnalyzeJobEndpointCompletesPreview(t *testing.T) {
	deps := testDependencies(t)
	handler := NewRouter(deps.RouterDeps)
	inputPath := sampleDicomPath(t)

	openRecorder := httptest.NewRecorder()
	openRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/open_study",
		strings.NewReader(`{"inputPath":"`+inputPath+`"}`),
	)
	openRequest.Header.Set("content-type", "application/json")
	handler.ServeHTTP(openRecorder, openRequest)

	if openRecorder.Code != http.StatusOK {
		t.Fatalf("open status = %d, want %d", openRecorder.Code, http.StatusOK)
	}

	var opened contracts.OpenStudyCommandResult
	if err := json.NewDecoder(openRecorder.Body).Decode(&opened); err != nil {
		t.Fatalf("decode open payload failed: %v", err)
	}

	startRecorder := httptest.NewRecorder()
	startRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/start_analyze_job",
		strings.NewReader(`{"studyId":"`+opened.Study.StudyID+`"}`),
	)
	startRequest.Header.Set("content-type", "application/json")
	handler.ServeHTTP(startRecorder, startRequest)

	if startRecorder.Code != http.StatusOK {
		t.Fatalf("start_analyze_job status = %d, want %d", startRecorder.Code, http.StatusOK)
	}

	var started contracts.StartedJob
	if err := json.NewDecoder(startRecorder.Body).Decode(&started); err != nil {
		t.Fatalf("decode started job failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		getRecorder := httptest.NewRecorder()
		getRequest := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/commands/get_job",
			strings.NewReader(`{"jobId":"`+started.JobID+`"}`),
		)
		getRequest.Header.Set("content-type", "application/json")
		handler.ServeHTTP(getRecorder, getRequest)

		if getRecorder.Code != http.StatusOK {
			t.Fatalf("get_job status = %d, want %d", getRecorder.Code, http.StatusOK)
		}

		var snapshot contracts.JobSnapshot
		if err := json.NewDecoder(getRecorder.Body).Decode(&snapshot); err != nil {
			t.Fatalf("decode job snapshot failed: %v", err)
		}

		switch snapshot.State {
		case contracts.JobStateQueued, contracts.JobStateRunning, contracts.JobStateCancelling:
			time.Sleep(25 * time.Millisecond)
		case contracts.JobStateFailed:
			t.Fatalf("analyze job failed: %#v", snapshot.Error)
		case contracts.JobStateCancelled:
			t.Fatal("analyze job was cancelled unexpectedly")
		case contracts.JobStateCompleted:
			if snapshot.Result == nil {
				t.Fatal("completed analyze job returned nil result")
			}
			if got, want := snapshot.Result.Kind, contracts.JobKindAnalyzeStudy; got != want {
				t.Fatalf("result kind = %q, want %q", got, want)
			}

			payload, ok := snapshot.Result.Payload.(map[string]any)
			if !ok {
				t.Fatalf("result payload type = %T, want map[string]any", snapshot.Result.Payload)
			}

			previewPath, ok := payload["previewPath"].(string)
			if !ok || previewPath == "" {
				t.Fatalf("previewPath = %#v, want non-empty string", payload["previewPath"])
			}
			if info, err := os.Stat(previewPath); err != nil || info.IsDir() {
				t.Fatalf("preview artifact missing or invalid: %v", err)
			}

			analysisPayload, ok := payload["analysis"].(map[string]any)
			if !ok {
				t.Fatalf("analysis payload type = %T, want map[string]any", payload["analysis"])
			}
			imagePayload, ok := analysisPayload["image"].(map[string]any)
			if !ok {
				t.Fatalf("analysis.image type = %T, want map[string]any", analysisPayload["image"])
			}
			if got, ok := imagePayload["width"].(float64); !ok || got <= 0 {
				t.Fatalf("analysis.image.width = %#v, want positive number", imagePayload["width"])
			}

			suggestedAnnotations, ok := payload["suggestedAnnotations"].(map[string]any)
			if !ok {
				t.Fatalf("suggestedAnnotations type = %T, want map[string]any", payload["suggestedAnnotations"])
			}
			lines, ok := suggestedAnnotations["lines"].([]any)
			if !ok || len(lines) == 0 {
				t.Fatalf("suggestedAnnotations.lines = %#v, want non-empty array", suggestedAnnotations["lines"])
			}
			return
		}
	}

	t.Fatal("analyze job did not complete before timeout")
}

func sampleDicomPath(t *testing.T) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller returned no file path")
	}

	return filepath.Clean(
		filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "images", "sample-dental-radiograph.dcm"),
	)
}

func actualBMPPath(t *testing.T) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller returned no file path")
	}

	return filepath.Clean(
		filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "images", "BMP", "xrays1.bmp"),
	)
}

func actualTIFFPath(t *testing.T) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller returned no file path")
	}

	return filepath.Clean(
		filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "images", "TIF", "xrays1.tif"),
	)
}

func copySampleDicom(t *testing.T, name string) string {
	t.Helper()

	contents, err := os.ReadFile(sampleDicomPath(t))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	targetPath := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(targetPath, contents, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	return targetPath
}

func openStudyViaRouter(t *testing.T, handler http.Handler, inputPath string) contracts.OpenStudyCommandResult {
	t.Helper()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/open_study",
		strings.NewReader(`{"inputPath":"`+inputPath+`"}`),
	)
	request.Header.Set("content-type", "application/json")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("open_study status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var payload contracts.OpenStudyCommandResult
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	return payload
}

func writeStandaloneBMPFixture(t *testing.T, path string) {
	t.Helper()

	img := image.NewGray(image.Rect(0, 0, 2, 1))
	img.SetGray(0, 0, color.Gray{Y: 0})
	img.SetGray(1, 0, color.Gray{Y: 255})

	var payload bytes.Buffer
	if err := bmp.Encode(&payload, img); err != nil {
		t.Fatalf("bmp.Encode returned error: %v", err)
	}
	if err := os.WriteFile(path, payload.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func buildScaledDicomFixture() []byte {
	var payload bytes.Buffer

	payload.Write(make([]byte, 128))
	payload.WriteString("DICM")
	writeExplicitLittleElement(
		&payload,
		0x0002,
		0x0010,
		"UI",
		encodePaddedString("1.2.840.10008.1.2.1", 0x00),
	)
	writeExplicitLittleElement(&payload, 0x0028, 0x0010, "US", encodeLittleEndianUint16(512))
	writeExplicitLittleElement(&payload, 0x0028, 0x0011, "US", encodeLittleEndianUint16(768))
	writeExplicitLittleElement(
		&payload,
		0x0028,
		0x0004,
		"CS",
		encodePaddedString("MONOCHROME2", ' '),
	)
	writeExplicitLittleElement(
		&payload,
		0x0028,
		0x0030,
		"DS",
		encodePaddedString("0.25\\0.40", ' '),
	)
	writeExplicitLittleElement(&payload, 0x7fe0, 0x0010, "OB", nil)

	return payload.Bytes()
}

func writeExplicitLittleElement(
	payload *bytes.Buffer,
	group uint16,
	element uint16,
	vr string,
	value []byte,
) {
	writeLittleEndianUint16(payload, group)
	writeLittleEndianUint16(payload, element)
	payload.WriteString(vr)

	if vr == "OB" {
		payload.Write([]byte{0x00, 0x00})
		writeLittleEndianUint32(payload, uint32(len(value)))
	} else {
		writeLittleEndianUint16(payload, uint16(len(value)))
	}

	payload.Write(value)
}

func writeLittleEndianUint16(payload *bytes.Buffer, value uint16) {
	var raw [2]byte
	binary.LittleEndian.PutUint16(raw[:], value)
	payload.Write(raw[:])
}

func writeLittleEndianUint32(payload *bytes.Buffer, value uint32) {
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], value)
	payload.Write(raw[:])
}

func encodeLittleEndianUint16(value uint16) []byte {
	var raw [2]byte
	binary.LittleEndian.PutUint16(raw[:], value)
	return raw[:]
}

func encodePaddedString(value string, padding byte) []byte {
	raw := []byte(value)
	if len(raw)%2 != 0 {
		raw = append(raw, padding)
	}
	return raw
}

func TestAllowedOriginReceivesCORSHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("Origin", "http://localhost:1420")
	testRouter(t).ServeHTTP(recorder, request)

	if got, want := recorder.Header().Get("Access-Control-Allow-Origin"), "http://localhost:1420"; got != want {
		t.Fatalf("allow origin = %q, want %q", got, want)
	}
}

func TestOptionsPreflightReturnsNoContent(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, CommandsPath+"/open_study", nil)
	request.Header.Set("Origin", "tauri://localhost")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "content-type")
	testRouter(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}

	if got, want := recorder.Header().Get("Access-Control-Allow-Origin"), "tauri://localhost"; got != want {
		t.Fatalf("allow origin = %q, want %q", got, want)
	}

	if got := recorder.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodPost) {
		t.Fatalf("allow methods = %q, want POST included", got)
	}
}

func TestDisallowedOriginIsRejected(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("Origin", "https://example.com")
	testRouter(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}

	var payload contracts.BackendError
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if payload.Code != contracts.BackendErrorCodeInvalidInput {
		t.Fatalf("code = %q, want %q", payload.Code, contracts.BackendErrorCodeInvalidInput)
	}
}

func decodeJSONValue(t *testing.T, raw string) any {
	t.Helper()

	var value any
	if err := json.NewDecoder(strings.NewReader(raw)).Decode(&value); err != nil {
		t.Fatalf("decode expected JSON failed: %v", err)
	}

	return value
}

func BenchmarkHandleGetJob(b *testing.B) {
	snapshot := contracts.JobSnapshot{
		JobID:   "bench-job-id-1234-5678-abcd",
		JobKind: contracts.JobKindRenderStudy,
		State:   contracts.JobStateRunning,
		Progress: contracts.JobProgress{
			Percent: 50,
			Stage:   "rendering",
			Message: "Applying window transform",
		},
	}
	handler := NewRouter(RouterDeps{
		Config: config.Default(),
		Service: mockBackendService{
			getJob: func(cmd contracts.JobCommand) (contracts.JobSnapshot, error) {
				return snapshot, nil
			},
		},
		StartedAt: time.Now(),
	})

	payload := `{"jobId":"bench-job-id-1234-5678-abcd"}`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, CommandsPath+"/get_job", strings.NewReader(payload))
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkWriteJSON(b *testing.B) {
	studyID := "bench-study-id-1234"
	snapshot := contracts.JobSnapshot{
		JobID:   "bench-job-id-1234-5678-abcd",
		JobKind: contracts.JobKindRenderStudy,
		StudyID: &studyID,
		State:   contracts.JobStateRunning,
		Progress: contracts.JobProgress{
			Percent: 50,
			Stage:   "rendering",
			Message: "Applying window transform",
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		writeJSON(rec, http.StatusOK, snapshot)
	}
}

func BenchmarkDecodeJSONRequest(b *testing.B) {
	payload := `{"jobId":"bench-job-id-1234-5678-abcd"}`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, CommandsPath+"/get_job", strings.NewReader(payload))
		var cmd contracts.JobCommand
		if err := decodeJSONRequest(req, &cmd); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHealthz(b *testing.B) {
	handler := NewRouter(RouterDeps{
		Config:    config.Default(),
		StartedAt: time.Now(),
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		handler.ServeHTTP(rec, req)
	}
}
