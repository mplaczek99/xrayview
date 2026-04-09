package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	backendapi "xrayview/backend"
)

var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
	0x00, 0x03, 0x01, 0x01, 0x00, 0xc9, 0xfe, 0x92,
	0xef, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
	0x44, 0xae, 0x42, 0x60, 0x82,
}

type stubBackendService struct {
	openStudyFn             func(backendapi.OpenStudyCommand) (backendapi.OpenStudyCommandResult, error)
	startRenderJobFn        func(backendapi.RenderStudyCommand) (backendapi.StartedJob, error)
	startProcessJobFn       func(backendapi.ProcessStudyCommand) (backendapi.StartedJob, error)
	startAnalyzeJobFn       func(backendapi.AnalyzeStudyCommand) (backendapi.StartedJob, error)
	getJobFn                func(backendapi.JobCommand) (backendapi.JobSnapshot, error)
	cancelJobFn             func(backendapi.JobCommand) (backendapi.JobSnapshot, error)
	getProcessingManifestFn func() backendapi.ProcessingManifest
	measureLineFn           func(
		backendapi.MeasureLineAnnotationCommand,
	) (backendapi.MeasureLineAnnotationCommandResult, error)
}

func (service stubBackendService) OpenStudy(
	command backendapi.OpenStudyCommand,
) (backendapi.OpenStudyCommandResult, error) {
	if service.openStudyFn != nil {
		return service.openStudyFn(command)
	}

	return backendapi.OpenStudyCommandResult{}, nil
}

func (service stubBackendService) StartRenderJob(
	command backendapi.RenderStudyCommand,
) (backendapi.StartedJob, error) {
	if service.startRenderJobFn != nil {
		return service.startRenderJobFn(command)
	}

	return backendapi.StartedJob{}, nil
}

func (service stubBackendService) StartProcessJob(
	command backendapi.ProcessStudyCommand,
) (backendapi.StartedJob, error) {
	if service.startProcessJobFn != nil {
		return service.startProcessJobFn(command)
	}

	return backendapi.StartedJob{}, nil
}

func (service stubBackendService) StartAnalyzeJob(
	command backendapi.AnalyzeStudyCommand,
) (backendapi.StartedJob, error) {
	if service.startAnalyzeJobFn != nil {
		return service.startAnalyzeJobFn(command)
	}

	return backendapi.StartedJob{}, nil
}

func (service stubBackendService) GetJob(
	command backendapi.JobCommand,
) (backendapi.JobSnapshot, error) {
	if service.getJobFn != nil {
		return service.getJobFn(command)
	}

	return backendapi.JobSnapshot{}, nil
}

func (service stubBackendService) CancelJob(
	command backendapi.JobCommand,
) (backendapi.JobSnapshot, error) {
	if service.cancelJobFn != nil {
		return service.cancelJobFn(command)
	}

	return backendapi.JobSnapshot{}, nil
}

func (service stubBackendService) GetProcessingManifest() backendapi.ProcessingManifest {
	if service.getProcessingManifestFn != nil {
		return service.getProcessingManifestFn()
	}

	return backendapi.DefaultProcessingManifest()
}

func (service stubBackendService) MeasureLineAnnotation(
	command backendapi.MeasureLineAnnotationCommand,
) (backendapi.MeasureLineAnnotationCommandResult, error) {
	if service.measureLineFn != nil {
		return service.measureLineFn(command)
	}

	return backendapi.MeasureLineAnnotationCommandResult{}, nil
}

func (service stubBackendService) OnJobCompletion(callback func(backendapi.JobSnapshot)) {
}

func TestServeAssetServesPreviewArtifact(t *testing.T) {
	app := &DesktopApp{}

	previewPath := filepath.Join(t.TempDir(), "preview.png")
	if err := os.WriteFile(previewPath, tinyPNG, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		previewEndpointPath+"?path="+previewPath,
		nil,
	)
	recorder := httptest.NewRecorder()

	app.ServeAsset(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("ServeAsset() status = %d, want %d", recorder.Code, http.StatusOK)
	}

	if contentType := recorder.Header().Get("content-type"); !strings.HasPrefix(contentType, "image/png") {
		t.Fatalf("ServeAsset() content-type = %q, want image/png", contentType)
	}

	if recorder.Body.Len() == 0 {
		t.Fatal("ServeAsset() returned an empty body")
	}
}

func TestNewDesktopAppUsesInProcessBackendByDefault(t *testing.T) {
	t.Setenv(sidecarRuntimeEnvKey, "desktop")
	t.Setenv(sidecarBaseURLEnvKey, "")
	t.Setenv(legacySidecarBaseURLEnvKey, "")
	t.Setenv(sidecarBaseDirEnvKey, t.TempDir())
	t.Setenv(legacySidecarBaseDirEnvKey, "")

	app, err := NewDesktopApp()
	if err != nil {
		t.Fatalf("NewDesktopApp() error = %v", err)
	}

	if app.backend == nil {
		t.Fatal("NewDesktopApp() did not construct an embedded backend")
	}

	if app.sidecar != nil {
		t.Fatal("NewDesktopApp() should not create a sidecar controller in default embedded mode")
	}
}

func TestOpenStudyUsesEmbeddedBackend(t *testing.T) {
	app := &DesktopApp{
		backend: stubBackendService{
			openStudyFn: func(
				command backendapi.OpenStudyCommand,
			) (backendapi.OpenStudyCommandResult, error) {
				if command.InputPath != "/tmp/example.dcm" {
					t.Fatalf("OpenStudy() inputPath = %q, want %q", command.InputPath, "/tmp/example.dcm")
				}

				return backendapi.OpenStudyCommandResult{
					Study: backendapi.StudyRecord{
						StudyID:   "study-1",
						InputPath: command.InputPath,
						InputName: "example.dcm",
					},
				}, nil
			},
		},
	}

	result, err := app.OpenStudy(backendapi.OpenStudyCommand{InputPath: "/tmp/example.dcm"})
	if err != nil {
		t.Fatalf("OpenStudy() error = %v", err)
	}

	if result.Study.StudyID != "study-1" {
		t.Fatalf("OpenStudy() studyId = %q, want %q", result.Study.StudyID, "study-1")
	}
}

func TestInvokeBackendCommandUsesEmbeddedBackend(t *testing.T) {
	app := &DesktopApp{
		backend: stubBackendService{
			openStudyFn: func(
				command backendapi.OpenStudyCommand,
			) (backendapi.OpenStudyCommandResult, error) {
				return backendapi.OpenStudyCommandResult{
					Study: backendapi.StudyRecord{
						StudyID:   "study-embedded",
						InputPath: command.InputPath,
						InputName: "embedded.dcm",
					},
				}, nil
			},
		},
	}

	response := app.InvokeBackendCommand("open_study", `{"inputPath":"/tmp/embedded.dcm"}`)
	if response.Status != http.StatusOK {
		t.Fatalf("InvokeBackendCommand() status = %d, want %d", response.Status, http.StatusOK)
	}

	var payload backendapi.OpenStudyCommandResult
	if err := json.Unmarshal([]byte(response.Body), &payload); err != nil {
		t.Fatalf("InvokeBackendCommand() body was not valid JSON: %v", err)
	}

	if payload.Study.StudyID != "study-embedded" {
		t.Fatalf(
			"InvokeBackendCommand() studyId = %q, want %q",
			payload.Study.StudyID,
			"study-embedded",
		)
	}
}

func TestResolveFrontendDistDirRequiresBuildOutput(t *testing.T) {
	distDir, err := resolveFrontendDistDir()
	if err == nil {
		if filepath.Base(distDir) != "dist" {
			t.Fatalf("resolveFrontendDistDir() = %q, want path ending in dist", distDir)
		}
		return
	}

	if !strings.Contains(err.Error(), "npm --prefix frontend run wails:build") {
		t.Fatalf("resolveFrontendDistDir() error = %v, want build guidance", err)
	}
}
