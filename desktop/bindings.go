package main

import (
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"xrayview/backend/shell"
)

// The methods below mirror the contract command surface one-to-one. Names match
// the former Tauri command handlers (PascalCased) so the webview reaches them as
// window.go.main.App.<Method>. Errors flow through bindErr to preserve the
// structured BackendError across the Wails boundary.

func (a *App) GetProcessingManifest() shell.ProcessingManifest {
	return a.backend.GetProcessingManifest()
}

func (a *App) OpenStudy(command shell.OpenStudyCommand) (shell.OpenStudyCommandResult, error) {
	result, err := a.backend.OpenStudy(command)
	return result, bindErr(err)
}

func (a *App) StartRenderJob(command shell.RenderStudyCommand) (shell.StartedJob, error) {
	result, err := a.backend.StartRenderJobAsync(command)
	return result, bindErr(err)
}

func (a *App) StartAnalyzeJob(command shell.AnalyzeStudyCommand) (shell.StartedJob, error) {
	result, err := a.backend.StartAnalyzeJobAsync(command)
	return result, bindErr(err)
}

func (a *App) StartProcessJob(command shell.ProcessStudyCommand) (shell.StartedJob, error) {
	result, err := a.backend.StartProcessJobAsync(command)
	return result, bindErr(err)
}

func (a *App) GetJob(command shell.JobCommand) (shell.JobSnapshot, error) {
	result, err := a.backend.GetJob(command)
	return result, bindErr(err)
}

func (a *App) GetJobs(command shell.GetJobsCommand) ([]shell.JobSnapshot, error) {
	result, err := a.backend.GetJobs(command)
	return result, bindErr(err)
}

func (a *App) CancelJob(command shell.JobCommand) (shell.JobSnapshot, error) {
	result, err := a.backend.CancelJob(command)
	return result, bindErr(err)
}

func (a *App) MeasureLineAnnotation(
	command shell.MeasureLineAnnotationCommand,
) (shell.MeasureLineAnnotationCommandResult, error) {
	result, err := a.backend.MeasureLineAnnotation(command)
	return result, bindErr(err)
}

func (a *App) SetStudyCalibration(
	command shell.SetStudyCalibrationCommand,
) (shell.SetStudyCalibrationCommandResult, error) {
	result, err := a.backend.SetStudyCalibration(command)
	return result, bindErr(err)
}

// PickBmpFile shows the native file picker for a single BMP. Returns "" when the
// user cancels (the frontend treats empty as "no selection"). Replaces the
// former tauri-plugin-dialog open().
func (a *App) PickBmpFile() (string, error) {
	selected, err := wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Open Bitewing X-ray",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "Bitewing X-ray (BMP)", Pattern: "*.bmp"},
		},
	})
	if err != nil {
		return "", bindErr(err)
	}
	return selected, nil
}
