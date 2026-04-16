package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	backendapi "xrayview/backend"
)

const (
	eventJobUpdate = "xrayview:job-update"
)

type backendCommandResponse struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}

type backendErrorPayload struct {
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Details     []string `json:"details"`
	Recoverable bool     `json:"recoverable"`
}

type DesktopApp struct {
	ctx     context.Context
	sidecar *SidecarController
	backend backendapi.Service
}

func NewDesktopApp() (*DesktopApp, error) {
	mode := resolveRuntimeMode()
	if mode == runtimeModeMock {
		return &DesktopApp{}, nil
	}

	if hasExplicitBackendURL() {
		return &DesktopApp{
			sidecar: NewSidecarController(),
		}, nil
	}

	backend, err := backendapi.NewEmbeddedService(resolveSidecarBaseDir(), nil)
	if err != nil {
		return nil, fmt.Errorf("construct in-process backend: %w", err)
	}

	return &DesktopApp{
		backend: backend,
	}, nil
}

func (app *DesktopApp) startup(ctx context.Context) {
	app.ctx = ctx
	if app.backend != nil {
		// In-process: subscribe to ALL job transitions (progress + terminal).
		app.backend.OnJobUpdate(func(snapshot backendapi.JobSnapshot) {
			wailsruntime.EventsEmit(ctx, eventJobUpdate, snapshot)
		})
		wailsruntime.LogInfo(ctx, "xrayview shell running with in-process backend")
		return
	}

	if app.sidecar == nil || !app.sidecar.Enabled() {
		wailsruntime.LogInfo(ctx, "xrayview shell running in mock mode")
		return
	}

	if err := app.sidecar.EnsureStarted(); err != nil {
		wailsruntime.LogErrorf(ctx, "xrayview sidecar startup failed: %s", err)
		return
	}

	// Sidecar: bridge the backend SSE stream into Wails events so the
	// frontend receives job-update events without polling.
	go app.bridgeSidecarSSEToEvents(ctx)

	wailsruntime.LogInfof(ctx, "xrayview shell ready against %s", app.sidecar.BaseURL())
}

// bridgeSidecarSSEToEvents reads the backend SSE stream and re-emits each
// job-update frame as a Wails event. Reconnects automatically on error until
// ctx is cancelled.
func (app *DesktopApp) bridgeSidecarSSEToEvents(ctx context.Context) {
	eventsURL := app.sidecar.BaseURL() + "/api/v1/events"
	client := &http.Client{} // no timeout — long-lived SSE connection

	for {
		if ctx.Err() != nil {
			return
		}

		func() {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, eventsURL, nil)
			if err != nil {
				return
			}
			req.Header.Set("Accept", "text/event-stream")

			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				var snapshot backendapi.JobSnapshot
				if err := json.Unmarshal([]byte(line[6:]), &snapshot); err != nil {
					continue
				}
				wailsruntime.EventsEmit(ctx, eventJobUpdate, snapshot)
			}
		}()

		// Brief pause before reconnect to avoid tight error loops.
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (app *DesktopApp) shutdown(context.Context) {
	if app.sidecar != nil {
		app.sidecar.Stop()
	}
}

func (app *DesktopApp) PickDicomFile() (string, error) {
	if app.ctx == nil {
		return "", errors.New("wails runtime context is not available yet")
	}

	return wailsruntime.OpenFileDialog(app.ctx, wailsruntime.OpenDialogOptions{
		Title: "Open Study or BMP/TIFF",
		Filters: []wailsruntime.FileFilter{
			{
				DisplayName: "Supported Files (*.dcm;*.dicom;*.bmp;*.tif;*.tiff)",
				Pattern:     "*.dcm;*.dicom;*.bmp;*.tif;*.tiff",
			},
			{
				DisplayName: "All Files (*)",
				Pattern:     "*",
			},
		},
	})
}

func (app *DesktopApp) PickSaveDicomPath(defaultName string) (string, error) {
	if app.ctx == nil {
		return "", errors.New("wails runtime context is not available yet")
	}

	return wailsruntime.SaveFileDialog(app.ctx, wailsruntime.SaveDialogOptions{
		Title:           "Save Processed DICOM",
		DefaultFilename: defaultName,
		Filters: []wailsruntime.FileFilter{
			{
				DisplayName: "DICOM Files (*.dcm;*.dicom)",
				Pattern:     "*.dcm;*.dicom",
			},
		},
	})
}

// invokeBackendCommand dispatches a command to the sidecar process over HTTP.
// Only used as a fallback when the sidecar is active (hasExplicitBackendURL).
func (app *DesktopApp) invokeBackendCommand(
	command string,
	payloadJSON string,
) backendCommandResponse {
	command = strings.TrimSpace(command)
	if command == "" {
		return errorResponse(http.StatusBadRequest, "backend command name is required", true)
	}

	if app.sidecar == nil || !app.sidecar.Enabled() {
		return errorResponse(
			http.StatusServiceUnavailable,
			"desktop backend is not available",
			true,
		)
	}

	response, err := app.sidecar.InvokeCommand(command, payloadJSON)
	if err != nil {
		return errorResponse(http.StatusServiceUnavailable, err.Error(), true)
	}

	return response
}

func (app *DesktopApp) OpenStudy(
	command backendapi.OpenStudyCommand,
) (backendapi.OpenStudyCommandResult, error) {
	if app.backend != nil {
		return app.backend.OpenStudy(command)
	}

	return invokeViaHTTP[backendapi.OpenStudyCommandResult](app, "open_study", command)
}

func (app *DesktopApp) StartRenderJob(
	command backendapi.RenderStudyCommand,
) (backendapi.StartedJob, error) {
	if app.backend != nil {
		return app.backend.StartRenderJob(command)
	}

	return invokeViaHTTP[backendapi.StartedJob](app, "start_render_job", command)
}

func (app *DesktopApp) StartProcessJob(
	command backendapi.ProcessStudyCommand,
) (backendapi.StartedJob, error) {
	if app.backend != nil {
		return app.backend.StartProcessJob(command)
	}

	return invokeViaHTTP[backendapi.StartedJob](app, "start_process_job", command)
}

func (app *DesktopApp) StartAnalyzeJob(
	command backendapi.AnalyzeStudyCommand,
) (backendapi.StartedJob, error) {
	if app.backend != nil {
		return app.backend.StartAnalyzeJob(command)
	}

	return invokeViaHTTP[backendapi.StartedJob](app, "start_analyze_job", command)
}

func (app *DesktopApp) GetJobSnapshot(
	command backendapi.JobCommand,
) (backendapi.JobSnapshot, error) {
	if app.backend != nil {
		return app.backend.GetJob(command)
	}

	return invokeViaHTTP[backendapi.JobSnapshot](app, "get_job", command)
}

func (app *DesktopApp) CancelJobByID(
	command backendapi.JobCommand,
) (backendapi.JobSnapshot, error) {
	if app.backend != nil {
		return app.backend.CancelJob(command)
	}

	return invokeViaHTTP[backendapi.JobSnapshot](app, "cancel_job", command)
}

func (app *DesktopApp) GetProcessingManifest() backendapi.ProcessingManifest {
	if app.backend != nil {
		return app.backend.GetProcessingManifest()
	}

	result, err := invokeViaHTTP[backendapi.ProcessingManifest](app, "get_processing_manifest", nil)
	if err != nil {
		return backendapi.DefaultProcessingManifest()
	}

	return result
}

func (app *DesktopApp) MeasureLineAnnotation(
	command backendapi.MeasureLineAnnotationCommand,
) (backendapi.MeasureLineAnnotationCommandResult, error) {
	if app.backend != nil {
		return app.backend.MeasureLineAnnotation(command)
	}

	return invokeViaHTTP[backendapi.MeasureLineAnnotationCommandResult](
		app,
		"measure_line_annotation",
		command,
	)
}

func (app *DesktopApp) ServeAsset(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if request.URL.Path != previewEndpointPath {
		http.NotFound(writer, request)
		return
	}

	rawPath := strings.TrimSpace(request.URL.Query().Get("path"))
	if rawPath == "" {
		http.Error(writer, "preview path is required", http.StatusBadRequest)
		return
	}

	if !filepath.IsAbs(rawPath) {
		http.Error(writer, "preview path must be absolute", http.StatusBadRequest)
		return
	}

	file, err := os.Open(rawPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(writer, fmt.Sprintf("preview artifact not found: %s", rawPath), http.StatusNotFound)
			return
		}

		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	if info.IsDir() {
		http.Error(writer, "preview path must point to a file", http.StatusBadRequest)
		return
	}

	contentType := mime.TypeByExtension(filepath.Ext(rawPath))
	if contentType == "" {
		header := make([]byte, 512)
		n, readErr := file.Read(header)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			http.Error(writer, readErr.Error(), http.StatusInternalServerError)
			return
		}
		contentType = http.DetectContentType(header[:n])
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	writer.Header().Set("content-type", contentType)
	http.ServeContent(writer, request, info.Name(), info.ModTime(), file)
}

func errorResponse(
	status int,
	message string,
	recoverable bool,
	details ...string,
) backendCommandResponse {
	payload := backendErrorPayload{
		Code:        "internal",
		Message:     message,
		Details:     details,
		Recoverable: recoverable,
	}
	switch status {
	case http.StatusBadRequest:
		payload.Code = "invalidInput"
	case http.StatusNotFound:
		payload.Code = "notFound"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		body = []byte(`{"code":"internal","message":"failed to encode shell error","details":[],"recoverable":false}`)
	}

	return backendCommandResponse{
		Status: status,
		Body:   string(body),
	}
}

func invokeViaHTTP[T any](app *DesktopApp, command string, payload any) (T, error) {
	var zero T
	payloadJSON := ""
	if payload != nil {
		bytes, err := json.Marshal(payload)
		if err != nil {
			return zero, err
		}
		payloadJSON = string(bytes)
	}

	response := app.invokeBackendCommand(command, payloadJSON)
	if response.Status >= http.StatusBadRequest {
		var backendErr backendapi.BackendError
		if err := json.Unmarshal([]byte(response.Body), &backendErr); err != nil {
			return zero, fmt.Errorf("backend command %s failed with status %d", command, response.Status)
		}

		return zero, backendErr
	}

	var result T
	if err := json.Unmarshal([]byte(response.Body), &result); err != nil {
		return zero, err
	}

	return result, nil
}

func resolveFrontendDistDir() (string, error) {
	for _, candidate := range frontendDistCandidates() {
		info, err := os.Stat(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return "", err
		}

		if info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf(
		"missing Wails frontend assets; build them first with `npm --prefix frontend run wails:build`",
	)
}

func frontendDistCandidates() []string {
	paths := []string{}

	if executableDir, err := resolveExecutableDir(); err == nil {
		paths = append(paths, filepath.Join(executableDir, "..", "frontend", "dist"))
	}

	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths,
			filepath.Join(cwd, "build", "frontend", "dist"),
			filepath.Join(cwd, "desktop", "build", "frontend", "dist"),
		)
	}

	return uniquePaths(paths)
}
