package main

import (
	"context"
	"encoding/json"
	"errors"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"xrayview/backend/shell"
)

// App is the Wails-bound IPC surface. It wraps the backend orchestrator and is
// exposed to the webview as window.go.main.App.<Method>. Command methods live
// in bindings.go.
type App struct {
	backend *shell.App

	ctx          context.Context
	subscription shell.JobUpdateSubscription
	stopEvents   chan struct{}
}

func NewApp(backend *shell.App) *App {
	return &App{backend: backend}
}

// startup stores the Wails context and forwards backend job-update snapshots to
// the webview as "job-update" events — the same stream the stdio server emitted
// and the Tauri shell re-broadcast.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.subscription = a.backend.SubscribeJobUpdates()
	a.stopEvents = make(chan struct{})
	go func() {
		for {
			select {
			case snapshot, ok := <-a.subscription.Receiver:
				if !ok {
					return
				}
				wailsruntime.EventsEmit(ctx, "job-update", snapshot)
			case <-a.stopEvents:
				return
			}
		}
	}()
}

func (a *App) shutdown(_ context.Context) {
	if a.stopEvents != nil {
		close(a.stopEvents)
	}
	a.backend.UnsubscribeJobUpdates(a.subscription.ID)
}

// bindErr re-encodes backend errors as JSON so the structured BackendError
// (code/details/recoverable) survives Wails' error stringification, which
// otherwise only preserves Error() — the bare message. The frontend's
// normalizeBackendError parses this JSON back into a BackendError.
func bindErr(err error) error {
	if err == nil {
		return nil
	}
	var backendErr shell.BackendError
	if !errors.As(err, &backendErr) {
		backendErr = shell.InternalError(err.Error())
	}
	data, marshalErr := json.Marshal(backendErr)
	if marshalErr != nil {
		return err
	}
	return errors.New(string(data))
}
