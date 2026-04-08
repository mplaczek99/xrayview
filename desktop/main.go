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
	app, err := NewDesktopApp()
	if err != nil {
		log.Fatal(err)
	}

	assetDir, err := resolveFrontendDistDir()
	if err != nil {
		log.Fatal(err)
	}

	err = wails.Run(&options.App{
		Title:     "XRayView",
		Width:     1280,
		Height:    900,
		MinWidth:  980,
		MinHeight: 720,
		AssetServer: &assetserver.Options{
			Assets:  os.DirFS(assetDir),
			Handler: http.HandlerFunc(app.ServeAsset),
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Fatal(fmt.Errorf("xrayview wails shell failed: %w", err))
	}
}
