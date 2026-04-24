package app

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"xrayview/backend/internal/annotations"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/dicommeta"
	"xrayview/backend/internal/jobs"
)

// BackendService is the command surface the HTTP router in internal/httpapi
// and the desktop shell (via backend/service.go) both sit on top of. The
// contracts package defines the wire types; this interface is what actually
// gets invoked. Keep it in sync with httpapi.BackendService — the router
// declares its own narrower copy so it doesn't depend on this package, and
// the two will drift if nobody's watching.
type BackendService interface {
	OpenStudy(command contracts.OpenStudyCommand) (contracts.OpenStudyCommandResult, error)
	StartRenderJob(command contracts.RenderStudyCommand) (contracts.StartedJob, error)
	StartProcessJob(command contracts.ProcessStudyCommand) (contracts.StartedJob, error)
	GetJob(command contracts.JobCommand) (contracts.JobSnapshot, error)
	GetJobs(command contracts.GetJobsCommand) ([]contracts.JobSnapshot, error)
	CancelJob(command contracts.JobCommand) (contracts.JobSnapshot, error)
	GetProcessingManifest() contracts.ProcessingManifest
	MeasureLineAnnotation(
		command contracts.MeasureLineAnnotationCommand,
	) (contracts.MeasureLineAnnotationCommandResult, error)
	OnJobCompletion(callback jobs.JobCompletionCallback)
	OnJobUpdate(callback jobs.JobCompletionCallback)
	SupportedJobKinds() []string
	StudyCount() int
}

var _ BackendService = (*App)(nil)

// OpenStudy is the canonical study-ingest path. Every DICOM file the UI
// touches enters the backend here:
//
//  1. validate the input path exists and is a regular file,
//  2. parse metadata via dicommeta (headers only, no pixel decode),
//  3. register the study with a generated ID and measurement scale,
//  4. record it to the on-disk catalog — best-effort only; a catalog
//     failure is logged but does not fail the command, because the
//     in-memory registration is the source of truth for an active session.
func (app *App) OpenStudy(
	command contracts.OpenStudyCommand,
) (contracts.OpenStudyCommandResult, error) {
	if command.InputPath == "" {
		return contracts.OpenStudyCommandResult{}, contracts.InvalidInput("inputPath is required")
	}

	info, err := os.Stat(command.InputPath)
	if err != nil {
		if os.IsNotExist(err) {
			return contracts.OpenStudyCommandResult{}, contracts.NotFound(
				fmt.Sprintf("input file does not exist: %s", command.InputPath),
			)
		}

		return contracts.OpenStudyCommandResult{}, contracts.Internal(
			fmt.Sprintf("failed to inspect input file %s: %v", command.InputPath, err),
		)
	}
	if info.IsDir() {
		return contracts.OpenStudyCommandResult{}, contracts.InvalidInput(
			fmt.Sprintf("input path must be a file: %s", command.InputPath),
		)
	}

	metadata, err := dicommeta.ReadFile(command.InputPath)
	if err != nil {
		return contracts.OpenStudyCommandResult{}, contracts.InvalidInput(
			fmt.Sprintf("failed to read study metadata: %v", err),
		)
	}

	study, err := app.studies.Register(command.InputPath, metadata.MeasurementScale())
	if err != nil {
		return contracts.OpenStudyCommandResult{}, contracts.Internal(
			fmt.Sprintf("failed to register study: %v", err),
		)
	}

	if err := app.persistence.RecordOpenedStudy(study); err != nil && app.logger != nil {
		app.logger.Warn(
			"failed to record opened study",
			slog.String("study_id", study.StudyID),
			slog.String("input_path", study.InputPath),
			slog.Any("error", err),
		)
	}

	return contracts.OpenStudyCommandResult{Study: study}, nil
}

func (app *App) StartRenderJob(
	command contracts.RenderStudyCommand,
) (contracts.StartedJob, error) {
	return app.jobs.StartRenderJob(command)
}

func (app *App) StartProcessJob(
	command contracts.ProcessStudyCommand,
) (contracts.StartedJob, error) {
	return app.jobs.StartProcessJob(command)
}

func (app *App) GetJob(command contracts.JobCommand) (contracts.JobSnapshot, error) {
	return app.jobs.GetJob(command)
}

func (app *App) GetJobs(command contracts.GetJobsCommand) ([]contracts.JobSnapshot, error) {
	return app.jobs.GetJobs(command)
}

func (app *App) CancelJob(command contracts.JobCommand) (contracts.JobSnapshot, error) {
	return app.jobs.CancelJob(command)
}

func (app *App) GetProcessingManifest() contracts.ProcessingManifest {
	return contracts.DefaultProcessingManifest()
}

func (app *App) MeasureLineAnnotation(
	command contracts.MeasureLineAnnotationCommand,
) (contracts.MeasureLineAnnotationCommandResult, error) {
	studyID := strings.TrimSpace(command.StudyID)
	if studyID == "" {
		return contracts.MeasureLineAnnotationCommandResult{}, contracts.InvalidInput("studyId is required")
	}

	study, ok := app.studies.Get(studyID)
	if !ok {
		return contracts.MeasureLineAnnotationCommandResult{}, contracts.NotFound(
			fmt.Sprintf("study not found: %s", studyID),
		)
	}

	return contracts.MeasureLineAnnotationCommandResult{
		StudyID: study.StudyID,
		Annotation: annotations.MeasureLineAnnotation(
			command.Annotation,
			study.MeasurementScale,
		),
	}, nil
}

func (app *App) OnJobCompletion(callback jobs.JobCompletionCallback) {
	app.jobs.OnJobCompletion(callback)
}

func (app *App) OnJobUpdate(callback jobs.JobCompletionCallback) {
	app.jobs.OnJobUpdate(callback)
}

func (app *App) SupportedJobKinds() []string {
	return app.jobs.SupportedKinds()
}

func (app *App) StudyCount() int {
	return app.studies.Count()
}
