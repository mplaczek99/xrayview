package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"xrayview/backend/internal/cache"
	"xrayview/backend/internal/config"
)

// buildPreviewRouter wires just enough of a router to exercise the /preview
// handler. The preview endpoint never reaches the command dispatch table, so
// a nil BackendService is safe here.
func buildPreviewRouter(t *testing.T) (http.Handler, *cache.Store) {
	t.Helper()

	rootDir := filepath.Join(t.TempDir(), "xrayview")
	store := cache.NewWithRoot(rootDir)
	if err := store.Ensure(); err != nil {
		t.Fatalf("cache ensure failed: %v", err)
	}

	handler := NewRouter(RouterDeps{
		Config: config.Default(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Cache:  store,
	})

	return handler, store
}

func previewRequest(t *testing.T, handler http.Handler, rawPath string) *httptest.ResponseRecorder {
	t.Helper()

	target := PreviewPath
	if rawPath != "" {
		target += "?path=" + url.QueryEscape(rawPath)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestPreviewServesArtifactInsideCacheRoot(t *testing.T) {
	handler, store := buildPreviewRouter(t)

	artifactPath := filepath.Join(store.RootDir(), "preview.png")
	want := []byte("artifact-bytes")
	if err := os.WriteFile(artifactPath, want, 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	recorder := previewRequest(t, handler, artifactPath)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%q), want %d", recorder.Code, recorder.Body.String(), http.StatusOK)
	}

	if got := recorder.Body.Bytes(); string(got) != string(want) {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestPreviewRejectsMissingPathQuery(t *testing.T) {
	handler, _ := buildPreviewRouter(t)

	recorder := previewRequest(t, handler, "")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestPreviewRejectsRelativePath(t *testing.T) {
	handler, _ := buildPreviewRouter(t)

	recorder := previewRequest(t, handler, "relative/path.png")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestPreviewReturnsNotFoundWhenArtifactMissing(t *testing.T) {
	handler, store := buildPreviewRouter(t)

	missing := filepath.Join(store.RootDir(), "missing.png")
	recorder := previewRequest(t, handler, missing)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestPreviewRejectsPathOutsideCacheRoot(t *testing.T) {
	handler, _ := buildPreviewRouter(t)

	// A real file in a sibling temp dir, outside the configured cache root.
	// Using t.TempDir() again guarantees the path exists so the handler
	// cannot short-circuit with "not found" before the containment check.
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, []byte("nope"), 0o644); err != nil {
		t.Fatalf("write outside artifact: %v", err)
	}

	recorder := previewRequest(t, handler, outside)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d (body=%q), want %d", recorder.Code, recorder.Body.String(), http.StatusForbidden)
	}
}

func TestPreviewRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behaviour differs on Windows")
	}

	handler, store := buildPreviewRouter(t)

	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	// A symlink that lives inside the cache root but dereferences to a file
	// outside of it. Without EvalSymlinks + filepath.Rel this would pass a
	// naive prefix check.
	linkPath := filepath.Join(store.RootDir(), "escape.png")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	recorder := previewRequest(t, handler, linkPath)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d (body=%q), want %d", recorder.Code, recorder.Body.String(), http.StatusForbidden)
	}
}

func TestPreviewRejectsNonGetMethods(t *testing.T) {
	handler, store := buildPreviewRouter(t)

	artifactPath := filepath.Join(store.RootDir(), "preview.png")
	if err := os.WriteFile(artifactPath, []byte("secret-bytes"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	target := PreviewPath + "?path=" + url.QueryEscape(artifactPath)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, target, nil)
	handler.ServeHTTP(recorder, request)

	// The router exposes only GET /preview. The exact non-GET response is
	// left to the mux (405 if the GET pattern is the only match, 404 via
	// the catch-all otherwise); the contract here is simply that a non-GET
	// request must not return the artifact body.
	if recorder.Code >= 200 && recorder.Code < 300 {
		t.Fatalf("non-GET unexpectedly succeeded: status = %d, body = %q",
			recorder.Code, recorder.Body.String())
	}

	if recorder.Body.Len() > 0 && string(recorder.Body.Bytes()) == "secret-bytes" {
		t.Fatalf("non-GET leaked artifact bytes: body = %q", recorder.Body.String())
	}
}
