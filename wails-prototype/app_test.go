package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestServePrototypeAssetServesFixturePreview(t *testing.T) {
	app, err := NewPrototypeApp()
	if err != nil {
		t.Fatalf("NewPrototypeApp() error = %v", err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		previewEndpointPath+"?path="+app.samplePreviewPath,
		nil,
	)
	recorder := httptest.NewRecorder()

	app.ServePrototypeAsset(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("ServePrototypeAsset() status = %d, want %d", recorder.Code, http.StatusOK)
	}

	if contentType := recorder.Header().Get("content-type"); !strings.HasPrefix(contentType, "image/png") {
		t.Fatalf("ServePrototypeAsset() content-type = %q, want image/png", contentType)
	}

	if recorder.Body.Len() == 0 {
		t.Fatal("ServePrototypeAsset() returned an empty body")
	}
}

func TestResolveFrontendDistDirRequiresBuildOutput(t *testing.T) {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		t.Fatalf("resolveRepoRoot() error = %v", err)
	}

	distDir, err := resolveFrontendDistDir(repoRoot)
	if err == nil {
		if filepath.Base(distDir) != "dist" {
			t.Fatalf("resolveFrontendDistDir() = %q, want path ending in dist", distDir)
		}
		return
	}

	if !strings.Contains(err.Error(), "build the prototype frontend first") {
		t.Fatalf("resolveFrontendDistDir() error = %v, want build guidance", err)
	}
}
