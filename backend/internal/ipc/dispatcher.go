package ipc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	backendapp "xrayview/backend/internal/app"
	"xrayview/backend/internal/contracts"
)

// Dispatcher maps contract command names to App methods. It is the process
// boundary used by the stdio IPC server, so payload decoding is strict:
// unknown frontend fields should fail loudly instead of drifting silently.
type Dispatcher struct {
	app *backendapp.App
}

func NewDispatcher(app *backendapp.App) Dispatcher {
	return Dispatcher{app: app}
}

func (dispatcher Dispatcher) Dispatch(command string, payload json.RawMessage) (any, error) {
	switch command {
	case contracts.CommandGetProcessingManifest:
		return dispatcher.app.GetProcessingManifest(), nil
	case contracts.CommandOpenStudy:
		var decoded contracts.OpenStudyCommand
		if err := decodeStrict(payload, &decoded); err != nil {
			return nil, invalidPayload(command, err)
		}
		return dispatcher.app.OpenStudy(decoded)
	case contracts.CommandStartRenderJob:
		var decoded contracts.RenderStudyCommand
		if err := decodeStrict(payload, &decoded); err != nil {
			return nil, invalidPayload(command, err)
		}
		return dispatcher.app.StartRenderJobAsync(decoded)
	case contracts.CommandStartAnalyzeJob:
		var decoded contracts.AnalyzeStudyCommand
		if err := decodeStrict(payload, &decoded); err != nil {
			return nil, invalidPayload(command, err)
		}
		return dispatcher.app.StartAnalyzeJobAsync(decoded)
	case contracts.CommandStartProcessJob:
		var decoded contracts.ProcessStudyCommand
		if err := decodeStrict(payload, &decoded); err != nil {
			return nil, invalidPayload(command, err)
		}
		return dispatcher.app.StartProcessJobAsync(decoded)
	case contracts.CommandGetJob:
		var decoded contracts.JobCommand
		if err := decodeStrict(payload, &decoded); err != nil {
			return nil, invalidPayload(command, err)
		}
		return dispatcher.app.GetJob(decoded)
	case contracts.CommandGetJobs:
		var decoded contracts.GetJobsCommand
		if err := decodeStrict(payload, &decoded); err != nil {
			return nil, invalidPayload(command, err)
		}
		return dispatcher.app.GetJobs(decoded)
	case contracts.CommandCancelJob:
		var decoded contracts.JobCommand
		if err := decodeStrict(payload, &decoded); err != nil {
			return nil, invalidPayload(command, err)
		}
		return dispatcher.app.CancelJob(decoded)
	case contracts.CommandMeasureLineAnnotation:
		var decoded contracts.MeasureLineAnnotationCommand
		if err := decodeStrict(payload, &decoded); err != nil {
			return nil, invalidPayload(command, err)
		}
		return dispatcher.app.MeasureLineAnnotation(decoded)
	case contracts.CommandSetStudyCalibration:
		var decoded contracts.SetStudyCalibrationCommand
		if err := decodeStrict(payload, &decoded); err != nil {
			return nil, invalidPayload(command, err)
		}
		return dispatcher.app.SetStudyCalibration(decoded)
	default:
		return nil, contracts.InvalidInputError(fmt.Sprintf("unsupported command: %s", command))
	}
}

func decodeStrict(payload json.RawMessage, destination any) error {
	if len(bytes.TrimSpace(payload)) == 0 || bytes.Equal(bytes.TrimSpace(payload), []byte("null")) {
		payload = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("payload must contain exactly one JSON value")
	}
	return nil
}

func invalidPayload(command string, err error) contracts.BackendError {
	return contracts.InvalidInputError(fmt.Sprintf("invalid %s payload: %v", command, err))
}
