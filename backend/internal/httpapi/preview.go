package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"xrayview/backend/internal/cache"
	"xrayview/backend/internal/config"
)

// PreviewPath is the endpoint the browser-hosted frontend uses to load
// preview artifacts produced by render/process/analyze jobs. It is only
// reachable through wrapLocalTransport, which already rejects non-loopback
// Origin headers, so the trust boundary is "any local process on this
// machine" rather than "anything on the network". Even so, the handler
// confines requested paths to the configured cache root so a request
// cannot use ../ traversal or a stray symlink to read arbitrary files.
const PreviewPath = "/preview"

// newPreviewHandler returns the handler registered at GET /preview.
//
// The request carries the absolute filesystem path of a preview artifact
// in the `path` query parameter. Agents receive these paths in job
// results; the transitional shape matches the Wails shell's /preview
// endpoint so the frontend can use the same URL template for both
// runtimes. Future work will replace the path query with opaque artifact
// ids so agents never see the filesystem shape — see AGENTS.md.
func newPreviewHandler(cacheStore *cache.Store, cfg config.Config) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		rawPath := strings.TrimSpace(request.URL.Query().Get("path"))
		if rawPath == "" {
			http.Error(writer, "preview path is required", http.StatusBadRequest)
			return
		}

		if !filepath.IsAbs(rawPath) {
			http.Error(writer, "preview path must be absolute", http.StatusBadRequest)
			return
		}

		rootDir := previewCacheRoot(cacheStore, cfg)
		if rootDir == "" {
			http.Error(writer, "cache root not configured", http.StatusInternalServerError)
			return
		}

		// Resolve symlinks on both sides before the containment check.
		// Without this, a symlink inside the cache root that points at an
		// arbitrary file would pass a naive prefix comparison.
		resolvedRoot, err := filepath.EvalSymlinks(rootDir)
		if err != nil {
			http.Error(
				writer,
				fmt.Sprintf("cache root unavailable: %v", err),
				http.StatusInternalServerError,
			)
			return
		}

		resolvedTarget, err := filepath.EvalSymlinks(rawPath)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(
					writer,
					fmt.Sprintf("preview artifact not found: %s", rawPath),
					http.StatusNotFound,
				)
				return
			}

			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}

		// filepath.Rel is more robust than a string prefix check: it
		// normalises separators and avoids the classic "/cache-root-evil"
		// false positive that HasPrefix("/cache-root") would accept.
		rel, err := filepath.Rel(resolvedRoot, resolvedTarget)
		if err != nil ||
			rel == ".." ||
			strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			http.Error(writer, "preview path outside cache root", http.StatusForbidden)
			return
		}

		file, err := os.Open(resolvedTarget)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(
					writer,
					fmt.Sprintf("preview artifact not found: %s", rawPath),
					http.StatusNotFound,
				)
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
			http.Error(writer, "preview path is a directory", http.StatusForbidden)
			return
		}

		// ServeContent handles content-type sniffing, range requests, and
		// If-Modified-Since — all of which browsers use for previews.
		http.ServeContent(writer, request, filepath.Base(resolvedTarget), info.ModTime(), file)
	}
}

func previewCacheRoot(cacheStore *cache.Store, cfg config.Config) string {
	if cacheStore != nil {
		return cacheStore.RootDir()
	}

	return cfg.Paths.CacheDir
}
