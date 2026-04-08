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
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type PrototypeInfo struct {
	BackendBaseURL      string         `json:"backendBaseUrl"`
	PreviewEndpointPath string         `json:"previewEndpointPath"`
	AssetDir            string         `json:"assetDir"`
	SidecarBinaryPath   string         `json:"sidecarBinaryPath"`
	SidecarManaged      bool           `json:"sidecarManaged"`
	StartupError        *string        `json:"startupError"`
	SampleDicomPath     string         `json:"sampleDicomPath"`
	SamplePreviewPath   string         `json:"samplePreviewPath"`
	BackendHealth       *backendHealth `json:"backendHealth"`
}

type backendHealth struct {
	Status         string `json:"status"`
	Service        string `json:"service"`
	Transport      string `json:"transport"`
	ListenAddress  string `json:"listenAddress"`
	CacheDir       string `json:"cacheDir"`
	PersistenceDir string `json:"persistenceDir"`
	StudyCount     int    `json:"studyCount"`
	StartedAt      string `json:"startedAt"`
}

type backendError struct {
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Details     []string `json:"details"`
	Recoverable bool     `json:"recoverable"`
}

type openStudyCommand struct {
	InputPath string `json:"inputPath"`
}

type measurementScale struct {
	RowSpacingMM    float64 `json:"rowSpacingMm"`
	ColumnSpacingMM float64 `json:"columnSpacingMm"`
	Source          string  `json:"source"`
}

type studyRecord struct {
	StudyID          string            `json:"studyId"`
	InputPath        string            `json:"inputPath"`
	InputName        string            `json:"inputName"`
	MeasurementScale *measurementScale `json:"measurementScale"`
}

type openStudyCommandResult struct {
	Study studyRecord `json:"study"`
}

type OpenStudyResult struct {
	StudyID          string            `json:"studyId"`
	InputPath        string            `json:"inputPath"`
	InputName        string            `json:"inputName"`
	MeasurementScale *measurementScale `json:"measurementScale"`
	RoundTripMS      float64           `json:"roundTripMs"`
}

type PrototypeApp struct {
	ctx               context.Context
	repoRoot          string
	sampleDicomPath   string
	samplePreviewPath string
	sidecar           *SidecarController
	startupError      string
}

func NewPrototypeApp() (*PrototypeApp, error) {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return nil, err
	}

	return &PrototypeApp{
		repoRoot:          repoRoot,
		sampleDicomPath:   filepath.Join(repoRoot, "images", "sample-dental-radiograph.dcm"),
		samplePreviewPath: filepath.Join(repoRoot, "backend", "tests", "fixtures", "parity", "sample-dental-radiograph", "render-preview.png"),
		sidecar:           NewSidecarController(repoRoot),
	}, nil
}

func (app *PrototypeApp) startup(ctx context.Context) {
	app.ctx = ctx
	if err := app.sidecar.EnsureStarted(); err != nil {
		app.startupError = err.Error()
		wailsruntime.LogErrorf(ctx, "wails prototype sidecar startup failed: %s", err)
		return
	}

	app.startupError = ""
	wailsruntime.LogInfof(ctx, "wails prototype ready against %s", app.sidecar.BaseURL())
}

func (app *PrototypeApp) shutdown(context.Context) {
	app.sidecar.Stop()
}

func (app *PrototypeApp) PrototypeInfo() PrototypeInfo {
	health, _ := app.sidecar.Health()
	assetDir, err := resolveFrontendDistDir(app.repoRoot)
	assetPath := ""
	if err == nil {
		assetPath = assetDir
	}

	return PrototypeInfo{
		BackendBaseURL:      app.sidecar.BaseURL(),
		PreviewEndpointPath: previewEndpointPath,
		AssetDir:            assetPath,
		SidecarBinaryPath:   app.sidecar.BinaryPath(),
		SidecarManaged:      app.sidecar.Managed(),
		StartupError:        stringPointer(app.startupError),
		SampleDicomPath:     app.sampleDicomPath,
		SamplePreviewPath:   app.samplePreviewPath,
		BackendHealth:       health,
	}
}

func (app *PrototypeApp) PickDicomFile() (string, error) {
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

func (app *PrototypeApp) PickPreviewArtifact() (string, error) {
	if app.ctx == nil {
		return "", errors.New("wails runtime context is not available yet")
	}

	return wailsruntime.OpenFileDialog(app.ctx, wailsruntime.OpenDialogOptions{
		Title: "Open Preview Artifact",
		Filters: []wailsruntime.FileFilter{
			{
				DisplayName: "Image Files (*.png;*.jpg;*.jpeg)",
				Pattern:     "*.png;*.jpg;*.jpeg",
			},
		},
	})
}

func (app *PrototypeApp) PickSaveDicomPath(defaultName string) (string, error) {
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

func (app *PrototypeApp) OpenStudy(inputPath string) (*OpenStudyResult, error) {
	if strings.TrimSpace(inputPath) == "" {
		return nil, errors.New("inputPath is required")
	}

	if err := app.sidecar.EnsureStarted(); err != nil {
		app.startupError = err.Error()
		return nil, err
	}
	app.startupError = ""

	startedAt := time.Now()
	var payload openStudyCommandResult
	if err := app.sidecar.PostJSON(commandOpenStudyPath, openStudyCommand{InputPath: inputPath}, &payload); err != nil {
		return nil, err
	}

	result := &OpenStudyResult{
		StudyID:          payload.Study.StudyID,
		InputPath:        payload.Study.InputPath,
		InputName:        payload.Study.InputName,
		MeasurementScale: payload.Study.MeasurementScale,
		RoundTripMS:      float64(time.Since(startedAt).Microseconds()) / 1000,
	}

	return result, nil
}

func (app *PrototypeApp) ServePrototypeAsset(writer http.ResponseWriter, request *http.Request) {
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

	resolvedPath := rawPath
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(app.repoRoot, filepath.Clean(rawPath))
	}

	file, err := os.Open(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(writer, fmt.Sprintf("preview artifact not found: %s", resolvedPath), http.StatusNotFound)
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

	contentType := mime.TypeByExtension(filepath.Ext(resolvedPath))
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

func resolveFrontendDistDir(repoRoot string) (string, error) {
	distDir := filepath.Join(repoRoot, "wails-prototype", "frontend", "dist")
	info, err := os.Stat(distDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("missing %s; build the prototype frontend first with `npm --prefix frontend run wails:prototype:build`", distDir)
		}

		return "", err
	}

	if !info.IsDir() {
		return "", fmt.Errorf("prototype frontend path is not a directory: %s", distDir)
	}

	return distDir, nil
}

func resolveRepoRoot() (string, error) {
	candidates := []string{}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd)
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(executable))
	}

	for _, candidate := range candidates {
		if root, ok := findRepoRoot(candidate); ok {
			return root, nil
		}
	}

	return "", errors.New("unable to locate the xrayview repository root")
}

func findRepoRoot(start string) (string, bool) {
	current := filepath.Clean(start)
	for {
		if pathLooksLikeRepoRoot(current) {
			return current, true
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

func pathLooksLikeRepoRoot(path string) bool {
	required := []string{
		filepath.Join(path, "frontend"),
		filepath.Join(path, "go-backend"),
		filepath.Join(path, "backend"),
	}

	for _, requiredPath := range required {
		info, err := os.Stat(requiredPath)
		if err != nil || !info.IsDir() {
			return false
		}
	}

	return true
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}

	return &value
}

func decodeJSONResponse(reader io.Reader, target any) error {
	return json.NewDecoder(reader).Decode(target)
}
