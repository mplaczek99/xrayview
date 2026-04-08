package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestServeAssetServesPreviewArtifact(t *testing.T) {
	app, err := NewDesktopApp()
	if err != nil {
		t.Fatalf("NewDesktopApp() error = %v", err)
	}

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
