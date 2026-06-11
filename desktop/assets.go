package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// newPreviewHandler serves rendered preview artifacts (BMP) to the webview. It
// replaces Tauri's asset:// protocol / convertFileSrc: the frontend requests
// /previews?path=<absolute artifact path> and we stream the file. Only files
// inside the backend cache directory are served — anything else is rejected so
// the webview can't read arbitrary disk paths.
func newPreviewHandler(cacheDir string) http.Handler {
	root := filepath.Clean(cacheDir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/previews" {
			http.NotFound(w, r)
			return
		}
		requested := r.URL.Query().Get("path")
		if requested == "" {
			http.Error(w, "missing path", http.StatusBadRequest)
			return
		}
		clean := filepath.Clean(requested)
		if !filepath.IsAbs(clean) || !withinRoot(root, clean) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		file, err := os.Open(clean)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/bmp")
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, info.Name(), info.ModTime(), file)
	})
}

// withinRoot reports whether path is root itself or nested under it, guarding
// against ../ escapes after cleaning.
func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
