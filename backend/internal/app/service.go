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

// BackendService is the command surface shared by HTTP transport and the desktop shell.
type BackendService interface {
	OpenStudy(command contracts.OpenStudyCommand) (contracts.OpenStudyCommandResult, error)
	StartRenderJob(command contracts.RenderStudyCommand) (contracts.StartedJob, error)
	StartProcessJob(command contracts.ProcessStudyCommand) (contracts.StartedJob, error)
	StartAnalyzeJob(command contracts.AnalyzeStudyCommand) (contracts.StartedJob, error)
	GetJob(command contracts.JobCommand) (contracts.JobSnapshot, error)
	CancelJob(command contracts.JobCommand) (contracts.JobSnapshot, error)
	GetProcessingManifest() contracts.ProcessingManifest
	MeasureLineAnnotation(
		command contracts.MeasureLineAnnotationCommand,
	) (contracts.MeasureLineAnnotationCommandResult, error)
	OnJobCompletion(callback jobs.JobCompletionCallback)
	SupportedJobKinds() []string
	StudyCount() int
}

var _ BackendService = (*App)(nil)

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

func (app *App) StartAnalyzeJob(
	command contracts.AnalyzeStudyCommand,
) (contracts.StartedJob, error) {
	return app.jobs.StartAnalyzeJob(command)
}

func (app *App) GetJob(command contracts.JobCommand) (contracts.JobSnapshot, error) {
	return app.jobs.GetJob(command)
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

func (app *App) SupportedJobKinds() []string {
	return app.jobs.SupportedKinds()
}

func (app *App) StudyCount() int {
	return app.studies.Count()
}
