package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func main() {
	app, err := NewPrototypeApp()
	if err != nil {
		log.Fatal(err)
	}

	assetDir, err := resolveFrontendDistDir(app.repoRoot)
	if err != nil {
		log.Fatal(err)
	}

	err = wails.Run(&options.App{
		Title:     "XRayView Wails Prototype",
		Width:     1200,
		Height:    860,
		MinWidth:  980,
		MinHeight: 720,
		AssetServer: &assetserver.Options{
			Assets:  os.DirFS(assetDir),
			Handler: http.HandlerFunc(app.ServePrototypeAsset),
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Fatal(fmt.Errorf("wails prototype failed: %w", err))
	}
}
