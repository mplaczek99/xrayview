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
	commandOpenStudyPath   = "/api/v1/commands/open_study"
	sidecarBinaryEnvKey    = "XRAYVIEW_WAILS_PROTOTYPE_GO_BACKEND_BINARY"
	sidecarBaseURLEnvKey   = "XRAYVIEW_WAILS_PROTOTYPE_GO_BACKEND_URL"
	sidecarBaseDirEnvKey   = "XRAYVIEW_WAILS_PROTOTYPE_GO_BACKEND_BASE_DIR"
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

type SidecarController struct {
	repoRoot    string
	baseURL     string
	baseDir     string
	probeClient *http.Client
	httpClient  *http.Client
	binaryPath  string
	mu          sync.Mutex
	child       *exec.Cmd
	lastHealth  *backendHealth
	lastManaged bool
}

func NewSidecarController(repoRoot string) *SidecarController {
	baseURL := strings.TrimSpace(os.Getenv(sidecarBaseURLEnvKey))
	if baseURL == "" {
		baseURL = defaultBackendBaseURL
	}

	baseDir := strings.TrimSpace(os.Getenv(sidecarBaseDirEnvKey))
	if baseDir == "" {
		baseDir = filepath.Join(os.TempDir(), "xrayview", "wails-prototype")
	}

	return &SidecarController{
		repoRoot: repoRoot,
		baseURL:  strings.TrimRight(baseURL, "/"),
		baseDir:  baseDir,
		probeClient: &http.Client{
			Timeout: sidecarProbeTimeout,
		},
		httpClient: &http.Client{
			Timeout: sidecarRequestTimeout,
		},
	}
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

	binaryName := sidecarBinaryNameBase
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	return filepath.Join(controller.repoRoot, "wails-prototype", "build", "bin", binaryName)
}

func (controller *SidecarController) Managed() bool {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.lastManaged
}

func (controller *SidecarController) Health() (*backendHealth, error) {
	health, err := controller.probeHealth()
	if err == nil {
		controller.mu.Lock()
		controller.lastHealth = health
		controller.mu.Unlock()
		return health, nil
	}

	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.lastHealth != nil {
		return controller.lastHealth, nil
	}

	return nil, err
}

func (controller *SidecarController) EnsureStarted() error {
	controller.mu.Lock()
	defer controller.mu.Unlock()

	if health, err := controller.probeHealthLocked(); err == nil {
		controller.lastHealth = health
		controller.lastManaged = controller.child != nil
		return nil
	} else if !errors.Is(err, errSidecarUnavailable) {
		return err
	}

	binaryPath := controller.BinaryPath()
	if _, err := os.Stat(binaryPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf(
				"missing go backend sidecar binary at %s; build the prototype with `npm run wails:prototype:build` first",
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
		health, probeErr := controller.probeHealthLocked()
		if probeErr == nil {
			controller.child = command
			controller.binaryPath = binaryPath
			controller.lastHealth = health
			controller.lastManaged = true
			return nil
		}
		if !errors.Is(probeErr, errSidecarUnavailable) {
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

func (controller *SidecarController) PostJSON(path string, payload any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	request, err := http.NewRequest(http.MethodPost, controller.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("accept", "application/json")
	request.Header.Set("content-type", "application/json")

	response, err := controller.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		var backendErr backendError
		if decodeErr := decodeJSONResponse(response.Body, &backendErr); decodeErr == nil && backendErr.Message != "" {
			return fmt.Errorf("%s (%s)", backendErr.Message, backendErr.Code)
		}

		responseBody, _ := io.ReadAll(response.Body)
		return fmt.Errorf("go backend request failed with status %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	if target == nil {
		io.Copy(io.Discard, response.Body)
		return nil
	}

	return decodeJSONResponse(response.Body, target)
}

func (controller *SidecarController) probeHealth() (*backendHealth, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.probeHealthLocked()
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
	if err := decodeJSONResponse(response.Body, &health); err != nil {
		return nil, err
	}

	if health.Service != expectedBackendService {
		return nil, fmt.Errorf("refusing to use %s because it is served by %q instead of %q", controller.baseURL, health.Service, expectedBackendService)
	}

	if health.Transport != expectedTransportKind {
		return nil, fmt.Errorf("refusing to use %s because transport %q does not match %q", controller.baseURL, health.Transport, expectedTransportKind)
	}

	return &health, nil
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
