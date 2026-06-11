// Command xrayview is the Go-native desktop shell. Wails owns the native
// webview window and binds the App methods directly as the IPC surface, so the
// backend orchestrator runs in-process — no Rust shell, no stdio sidecar.
package main

import (
	"embed"
	"io/fs"
	"log"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"xrayview/backend/shell"
)

// The built frontend is synced into ./dist before the build (see
// scripts/build-frontend.mjs); .gitkeep keeps the embed pattern valid.
//
//go:embed all:dist
var assets embed.FS

func main() {
	applyLinuxWebkitWorkarounds()

	cfg, err := shell.LoadConfig()
	if err != nil {
		log.Fatalf("load backend config: %v", err)
	}
	backend, err := shell.NewApp(cfg)
	if err != nil {
		log.Fatalf("init backend: %v", err)
	}
	if err := backend.Prepare(); err != nil {
		log.Fatalf("prepare backend: %v", err)
	}

	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		log.Fatalf("mount embedded assets: %v", err)
	}

	app := NewApp(backend)

	if err := wails.Run(&options.App{
		Title:     "XRayView",
		Width:     1280,
		Height:    900,
		MinWidth:  980,
		MinHeight: 720,
		// Frontend draws its own chrome (the former Tauri shell ran without
		// native decorations), so keep the window frameless.
		Frameless: true,
		AssetServer: &assetserver.Options{
			Assets:  dist,
			Handler: newPreviewHandler(cfg.Paths.CacheDir),
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind:       []any{app},
	}); err != nil {
		log.Fatalf("run desktop shell: %v", err)
	}
}

// applyLinuxWebkitWorkarounds mirrors the Wayland/WebKitGTK DMABUF workaround
// from the former Tauri shell: the DMABUF renderer crashes or shows black
// frames on some GPUs (notably NVIDIA), so force the safe fallback unless the
// user already set the variable. The env checks are inert off Linux.
func applyLinuxWebkitWorkarounds() {
	if _, set := os.LookupEnv("WEBKIT_DISABLE_DMABUF_RENDERER"); set {
		return
	}
	isWayland := os.Getenv("WAYLAND_DISPLAY") != "" ||
		strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland")
	if isWayland {
		_ = os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	}
}
