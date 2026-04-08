package main

import (
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

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
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
}

func NewDesktopApp() (*DesktopApp, error) {
	return &DesktopApp{
		sidecar: NewSidecarController(),
	}, nil
}

func (app *DesktopApp) startup(ctx context.Context) {
	app.ctx = ctx
	if !app.sidecar.Enabled() {
		wailsruntime.LogInfo(ctx, "xrayview shell running in mock mode")
		return
	}

	if err := app.sidecar.EnsureStarted(); err != nil {
		wailsruntime.LogErrorf(ctx, "xrayview sidecar startup failed: %s", err)
		return
	}

	wailsruntime.LogInfof(ctx, "xrayview shell ready against %s", app.sidecar.BaseURL())
}

func (app *DesktopApp) shutdown(context.Context) {
	app.sidecar.Stop()
}

func (app *DesktopApp) PickDicomFile() (string, error) {
	if app.ctx == nil {
		return "", errors.New("wails runtime context is not available yet")
	}

	return wailsruntime.OpenFileDialog(app.ctx, wailsruntime.OpenDialogOptions{
		Title: "Open DICOM Study",
		Filters: []wailsruntime.FileFilter{
			{
				DisplayName: "DICOM Files (*.dcm;*.dicom)",
				Pattern:     "*.dcm;*.dicom",
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

func (app *DesktopApp) InvokeBackendCommand(
	command string,
	payloadJSON string,
) backendCommandResponse {
	command = strings.TrimSpace(command)
	if command == "" {
		return errorResponse(http.StatusBadRequest, "backend command name is required", true)
	}

	if !app.sidecar.Enabled() {
		return errorResponse(
			http.StatusServiceUnavailable,
			"desktop backend is disabled while the desktop shell is running in mock mode",
			true,
		)
	}

	response, err := app.sidecar.InvokeCommand(command, payloadJSON)
	if err != nil {
		return errorResponse(http.StatusServiceUnavailable, err.Error(), true)
	}

	return response
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
