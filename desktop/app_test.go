package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	backendapi "xrayview/contracts/contractv1"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func jsonTestResponse(t *testing.T, request *http.Request, statusCode int, payload any) *http.Response {
	t.Helper()

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		t.Fatalf("Encode test response: %v", err)
	}

	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"content-type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body.Bytes())),
		Request:    request,
	}
}

func newPreviewAssetTestApp(t *testing.T) (*DesktopApp, string) {
	t.Helper()
	rootDir := t.TempDir()
	return &DesktopApp{previewRoot: rootDir}, rootDir
}

func newPreviewAssetRequest(previewPath string) *http.Request {
	return httptest.NewRequest(
		http.MethodGet,
		previewEndpointPath+"?path="+url.QueryEscape(previewPath),
		nil,
	)
}

func TestServeAssetServesPreviewArtifact(t *testing.T) {
	app, rootDir := newPreviewAssetTestApp(t)

	previewPath := filepath.Join(rootDir, "preview.png")
	if err := os.WriteFile(previewPath, tinyPNG, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	request := newPreviewAssetRequest(previewPath)
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

func TestServeAssetSetsCacheControlAndETag(t *testing.T) {
	app, rootDir := newPreviewAssetTestApp(t)

	previewPath := filepath.Join(rootDir, "preview.png")
	if err := os.WriteFile(previewPath, tinyPNG, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	request := newPreviewAssetRequest(previewPath)
	recorder := httptest.NewRecorder()
	app.ServeAsset(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("ServeAsset() status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if cc := recorder.Header().Get("Cache-Control"); cc != "public, max-age=3600" {
		t.Errorf("Cache-Control = %q, want %q", cc, "public, max-age=3600")
	}
	if etag := recorder.Header().Get("ETag"); etag == "" {
		t.Error("ETag header missing")
	}
}

func TestServeAssetReturns304OnMatchingETag(t *testing.T) {
	app, rootDir := newPreviewAssetTestApp(t)

	previewPath := filepath.Join(rootDir, "preview.png")
	if err := os.WriteFile(previewPath, tinyPNG, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Cold request to capture ETag.
	cold := httptest.NewRecorder()
	app.ServeAsset(cold, newPreviewAssetRequest(previewPath))
	etag := cold.Header().Get("ETag")
	if etag == "" {
		t.Fatal("cold request returned no ETag")
	}

	// Conditional request with matching ETag must return 304.
	req := newPreviewAssetRequest(previewPath)
	req.Header.Set("If-None-Match", etag)
	recorder := httptest.NewRecorder()
	app.ServeAsset(recorder, req)

	if recorder.Code != http.StatusNotModified {
		t.Fatalf("ServeAsset() with matching ETag: status = %d, want %d", recorder.Code, http.StatusNotModified)
	}
	if recorder.Body.Len() != 0 {
		t.Errorf("ServeAsset() 304 response body not empty (len=%d)", recorder.Body.Len())
	}
}

func TestServeAssetReturns200OnStaleETag(t *testing.T) {
	app, rootDir := newPreviewAssetTestApp(t)

	previewPath := filepath.Join(rootDir, "preview.png")
	if err := os.WriteFile(previewPath, tinyPNG, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	req := newPreviewAssetRequest(previewPath)
	req.Header.Set("If-None-Match", `"stale-etag"`)
	recorder := httptest.NewRecorder()
	app.ServeAsset(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("ServeAsset() with stale ETag: status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if recorder.Body.Len() == 0 {
		t.Error("ServeAsset() stale ETag should return full body")
	}
}

func TestServeAssetRejectsPathOutsideCacheRoot(t *testing.T) {
	app, _ := newPreviewAssetTestApp(t)

	outsidePath := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outsidePath, tinyPNG, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	app.ServeAsset(recorder, newPreviewAssetRequest(outsidePath))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("ServeAsset() status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestServeAssetRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behaviour differs on Windows")
	}

	app, rootDir := newPreviewAssetTestApp(t)
	outsidePath := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outsidePath, tinyPNG, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	linkPath := filepath.Join(rootDir, "escape.png")
	if err := os.Symlink(outsidePath, linkPath); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	app.ServeAsset(recorder, newPreviewAssetRequest(linkPath))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("ServeAsset() status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestNewDesktopAppUsesRustSidecarByDefault(t *testing.T) {
	t.Setenv(sidecarRuntimeEnvKey, "desktop")
	t.Setenv(sidecarBaseURLEnvKey, "")
	t.Setenv(sidecarBaseDirEnvKey, t.TempDir())

	app, err := NewDesktopApp()
	if err != nil {
		t.Fatalf("NewDesktopApp() error = %v", err)
	}

	if app.sidecar == nil || !app.sidecar.Enabled() {
		t.Fatal("NewDesktopApp() did not create an enabled Rust sidecar controller")
	}
}

func TestOpenStudyInvokesSidecarHTTP(t *testing.T) {
	cacheDir := t.TempDir()
	baseURL := "http://127.0.0.1:38181"
	probeClient := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodGet {
				t.Fatalf("/healthz method = %s, want GET", request.Method)
			}
			if got, want := request.URL.String(), baseURL+"/healthz"; got != want {
				t.Fatalf("probe URL = %s, want %s", got, want)
			}

			return jsonTestResponse(t, request, http.StatusOK, backendHealth{
				Status:    "ok",
				Service:   expectedBackendService,
				Transport: expectedTransportKind,
				CacheDir:  cacheDir,
			}), nil
		}),
	}
	commandClient := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodPost {
				t.Fatalf("open_study method = %s, want POST", request.Method)
			}
			if got, want := request.URL.String(), baseURL+commandsPath+"/open_study"; got != want {
				t.Fatalf("command URL = %s, want %s", got, want)
			}

			var command backendapi.OpenStudyCommand
			if err := json.NewDecoder(request.Body).Decode(&command); err != nil {
				t.Fatalf("Decode open_study command: %v", err)
			}
			if command.InputPath != "/tmp/example.dcm" {
				t.Fatalf("OpenStudy() inputPath = %q, want %q", command.InputPath, "/tmp/example.dcm")
			}

			return jsonTestResponse(t, request, http.StatusOK, backendapi.OpenStudyCommandResult{
				Study: backendapi.StudyRecord{
					StudyID:   "study-1",
					InputPath: command.InputPath,
					InputName: "example.dcm",
				},
			}), nil
		}),
	}

	app := &DesktopApp{
		sidecar: &SidecarController{
			mode:        runtimeModeDesktop,
			baseURL:     baseURL,
			baseDir:     t.TempDir(),
			probeClient: probeClient,
			httpClient:  commandClient,
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
