package ipc

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	backendapp "xrayview/backend/internal/app"
	"xrayview/backend/internal/config"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/render"
)

func TestDispatcherRejectsUnknownPayloadFields(t *testing.T) {
	dispatcher := NewDispatcher(newTestApp(t))

	_, err := dispatcher.Dispatch(contracts.CommandOpenStudy, json.RawMessage(`{"inputPath":"study.bmp","extra":true}`))
	if err == nil {
		t.Fatal("expected error")
	}
	backendErr, ok := err.(contracts.BackendError)
	if !ok || backendErr.Code != contracts.InvalidInput {
		t.Fatalf("error = %#v", err)
	}
}

func TestDispatcherRunsRenderWorkflow(t *testing.T) {
	app := newTestApp(t)
	inputPath := filepath.Join(t.TempDir(), "study.bmp")
	writeGrayBMP(t, inputPath)
	dispatcher := NewDispatcher(app)

	openedAny, err := dispatcher.Dispatch(contracts.CommandOpenStudy, mustJSON(t, contracts.OpenStudyCommand{InputPath: inputPath}))
	if err != nil {
		t.Fatal(err)
	}
	opened := openedAny.(contracts.OpenStudyCommandResult)
	startedAny, err := dispatcher.Dispatch(contracts.CommandStartRenderJob, mustJSON(t, contracts.RenderStudyCommand{StudyID: opened.Study.StudyID}))
	if err != nil {
		t.Fatal(err)
	}
	started := startedAny.(contracts.StartedJob)
	snapshot := waitForJobState(t, app, started.JobID, contracts.JobStateCompleted)
	payload := snapshot.Result.Payload.(contracts.RenderStudyCommandResult)
	if payload.PreviewPath == "" || !fileExists(payload.PreviewPath) {
		t.Fatalf("preview path missing: %+v", payload)
	}
}

func TestServerWritesJSONResponseEnvelope(t *testing.T) {
	app := newTestApp(t)
	var output bytes.Buffer
	input := bytes.NewBufferString(`{"id":"manifest-1","command":"get_processing_manifest","payload":{}}` + "\n")

	if err := NewServer(app).Serve(input, &output); err != nil {
		t.Fatal(err)
	}
	var response struct {
		ID     string                       `json:"id"`
		OK     bool                         `json:"ok"`
		Result contracts.ProcessingManifest `json:"result"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != "manifest-1" || !response.OK || response.Result.DefaultPresetID != "default" {
		t.Fatalf("response = %+v", response)
	}
}

func newTestApp(t *testing.T) *backendapp.App {
	t.Helper()
	root := t.TempDir()
	cfg := config.Default()
	cfg.Paths.BaseDir = root
	cfg.Paths.CacheDir = filepath.Join(root, "cache")
	cfg.Paths.PersistenceDir = filepath.Join(root, "state")
	app, err := backendapp.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Prepare(); err != nil {
		t.Fatal(err)
	}
	return app
}

func writeGrayBMP(t *testing.T, path string) {
	t.Helper()
	encoded, err := render.EncodeGrayBMP(4, 2, []byte{0, 36, 72, 108, 144, 180, 216, 255})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o666); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func waitForJobState(t *testing.T, app *backendapp.App, jobID string, state contracts.JobState) contracts.JobSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := app.GetJob(contracts.JobCommand{JobID: jobID})
		if err == nil && snapshot.State == state {
			return snapshot
		}
		time.Sleep(5 * time.Millisecond)
	}
	snapshot, _ := app.GetJob(contracts.JobCommand{JobID: jobID})
	t.Fatalf("timed out waiting for %s, last snapshot %+v", state, snapshot)
	return contracts.JobSnapshot{}
}

func fileExists(path string) bool {
	metadata, err := os.Stat(path)
	return err == nil && metadata.Mode().IsRegular()
}
