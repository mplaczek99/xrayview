package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultBackendBaseURL  = "http://127.0.0.1:38181"
	previewEndpointPath    = "/preview"
	commandsPath           = "/api/v1/commands"
	sidecarBinaryEnvKey    = "XRAYVIEW_WAILS_GO_BACKEND_BINARY"
	sidecarBaseURLEnvKey   = "XRAYVIEW_GO_BACKEND_URL"
	sidecarBaseDirEnvKey   = "XRAYVIEW_WAILS_GO_BACKEND_BASE_DIR"
	sidecarRuntimeEnvKey   = "XRAYVIEW_BACKEND_RUNTIME"
	expectedBackendService = "xrayview-go-backend"
	expectedTransportKind  = "local-http-json"
	sidecarStartupTimeout  = 10 * time.Second
	sidecarPollInterval    = 125 * time.Millisecond
	sidecarProbeTimeout    = 350 * time.Millisecond
	sidecarRequestTimeout  = 15 * time.Second
	sidecarShutdownTimeout = 4 * time.Second
	sidecarBinaryNameBase  = "xrayview-go-backend"
)

var errSidecarUnavailable = errors.New("go backend is not reachable")

type runtimeMode string

const (
	runtimeModeMock    runtimeMode = "mock"
	runtimeModeDesktop runtimeMode = "desktop"
)

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

type SidecarController struct {
	mode        runtimeMode
	baseURL     string
	baseDir     string
	probeClient *http.Client
	httpClient  *http.Client
	binaryPath  string
	mu          sync.Mutex
	child       *exec.Cmd
	lastManaged bool
}

func NewSidecarController() *SidecarController {
	baseURL := strings.TrimSpace(os.Getenv(sidecarBaseURLEnvKey))
	if baseURL == "" {
		baseURL = defaultBackendBaseURL
	}

	baseDir := strings.TrimSpace(os.Getenv(sidecarBaseDirEnvKey))
	if baseDir == "" {
		baseDir = filepath.Join(os.TempDir(), "xrayview", "desktop")
	}

	return &SidecarController{
		mode:    resolveRuntimeMode(),
		baseURL: strings.TrimRight(baseURL, "/"),
		baseDir: baseDir,
		probeClient: &http.Client{
			Timeout: sidecarProbeTimeout,
		},
		httpClient: &http.Client{
			Timeout: sidecarRequestTimeout,
		},
	}
}

func (controller *SidecarController) Enabled() bool {
	return controller.mode == runtimeModeDesktop
}

func (controller *SidecarController) BaseURL() string {
	return controller.baseURL
}

func (controller *SidecarController) BinaryPath() string {
	if override := strings.TrimSpace(os.Getenv(sidecarBinaryEnvKey)); override != "" {
		return override
	}

	if controller.binaryPath != "" {
		return controller.binaryPath
	}

	for _, candidate := range sidecarBinaryCandidates() {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}

	return sidecarBinaryCandidates()[0]
}

func (controller *SidecarController) EnsureStarted() error {
	if !controller.Enabled() {
		return nil
	}

	controller.mu.Lock()
	defer controller.mu.Unlock()

	if _, err := controller.probeHealthLocked(); err == nil {
		controller.lastManaged = controller.child != nil
		return nil
	} else if !errors.Is(err, errSidecarUnavailable) {
		return err
	}

	binaryPath := controller.BinaryPath()
	if _, err := os.Stat(binaryPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf(
				"missing go backend sidecar binary at %s; build the desktop shell with `npm run wails:build` first",
				binaryPath,
			)
		}

		return err
	}

	if err := os.MkdirAll(controller.baseDir, 0o755); err != nil {
		return err
	}

	baseURL, err := url.Parse(controller.baseURL)
	if err != nil {
		return fmt.Errorf("invalid %s value %q: %w", sidecarBaseURLEnvKey, controller.baseURL, err)
	}

	host := baseURL.Hostname()
	port := baseURL.Port()
	if host == "" || port == "" {
		return fmt.Errorf("backend base URL must include host and port: %s", controller.baseURL)
	}

	command := exec.Command(binaryPath)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Env = append(
		os.Environ(),
		"XRAYVIEW_GO_BACKEND_HOST="+host,
		"XRAYVIEW_GO_BACKEND_PORT="+port,
		"XRAYVIEW_GO_BACKEND_BASE_DIR="+controller.baseDir,
	)

	if err := command.Start(); err != nil {
		return fmt.Errorf("failed to start go backend sidecar: %w", err)
	}

	deadline := time.Now().Add(sidecarStartupTimeout)
	for time.Now().Before(deadline) {
		if _, probeErr := controller.probeHealthLocked(); probeErr == nil {
			controller.child = command
			controller.binaryPath = binaryPath
			controller.lastManaged = true
			return nil
		} else if !errors.Is(probeErr, errSidecarUnavailable) {
			_ = terminateProcess(command)
			controller.child = nil
			controller.lastManaged = false
			return probeErr
		}

		time.Sleep(sidecarPollInterval)
	}

	_ = terminateProcess(command)
	controller.child = nil
	controller.lastManaged = false
	return fmt.Errorf("timed out waiting for go backend sidecar at %s", controller.baseURL)
}

func (controller *SidecarController) Stop() {
	controller.mu.Lock()
	defer controller.mu.Unlock()

	if controller.child == nil {
		return
	}

	_ = terminateProcess(controller.child)
	controller.child = nil
	controller.lastManaged = false
}

func (controller *SidecarController) InvokeCommand(
	command string,
	payloadJSON string,
) (backendCommandResponse, error) {
	if err := controller.EnsureStarted(); err != nil {
		return backendCommandResponse{}, err
	}

	requestURL := controller.baseURL + commandsPath + "/" + command
	var bodyReader io.Reader
	if strings.TrimSpace(payloadJSON) != "" {
		bodyReader = bytes.NewBufferString(payloadJSON)
	}

	request, err := http.NewRequest(http.MethodPost, requestURL, bodyReader)
	if err != nil {
		return backendCommandResponse{}, err
	}
	request.Header.Set("accept", "application/json")
	if bodyReader != nil {
		request.Header.Set("content-type", "application/json")
	}

	response, err := controller.httpClient.Do(request)
	if err != nil {
		return backendCommandResponse{}, err
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return backendCommandResponse{}, err
	}

	return backendCommandResponse{
		Status: response.StatusCode,
		Body:   string(responseBody),
	}, nil
}

func (controller *SidecarController) probeHealthLocked() (*backendHealth, error) {
	request, err := http.NewRequest(http.MethodGet, controller.baseURL+"/healthz", nil)
	if err != nil {
		return nil, err
	}

	response, err := controller.probeClient.Do(request)
	if err != nil {
		return nil, errSidecarUnavailable
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected backend health status: %d", response.StatusCode)
	}

	var health backendHealth
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		return nil, err
	}

	if health.Service != expectedBackendService {
		return nil, fmt.Errorf(
			"refusing to use %s because it is served by %q instead of %q",
			controller.baseURL,
			health.Service,
			expectedBackendService,
		)
	}

	if health.Transport != expectedTransportKind {
		return nil, fmt.Errorf(
			"refusing to use %s because transport %q does not match %q",
			controller.baseURL,
			health.Transport,
			expectedTransportKind,
		)
	}

	return &health, nil
}

func resolveRuntimeMode() runtimeMode {
	raw := strings.TrimSpace(os.Getenv(sidecarRuntimeEnvKey))
	switch strings.ToLower(raw) {
	case "", string(runtimeModeDesktop):
		return runtimeModeDesktop
	case string(runtimeModeMock):
		return runtimeModeMock
	default:
		return runtimeModeDesktop
	}
}

func resolveExecutableDir() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}

	return filepath.Dir(executable), nil
}

func sidecarBinaryCandidates() []string {
	binaryName := sidecarBinaryNameBase
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	paths := []string{}
	if executableDir, err := resolveExecutableDir(); err == nil {
		paths = append(paths, filepath.Join(executableDir, binaryName))
	}

	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths,
			filepath.Join(cwd, "build", "bin", binaryName),
			filepath.Join(cwd, "desktop", "build", "bin", binaryName),
		)
	}

	return uniquePaths(paths)
}

func uniquePaths(paths []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}

		cleaned := filepath.Clean(path)
		if _, ok := seen[cleaned]; ok {
			continue
		}

		seen[cleaned] = struct{}{}
		result = append(result, cleaned)
	}

	return result
}

func terminateProcess(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}

	if runtime.GOOS == "windows" {
		return command.Process.Kill()
	}

	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		return command.Process.Kill()
	}

	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(sidecarShutdownTimeout):
		return command.Process.Kill()
	}
}
