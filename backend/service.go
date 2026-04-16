package backend

import (
	"log/slog"
	"path/filepath"

	internalapp "xrayview/backend/internal/app"
	internalconfig "xrayview/backend/internal/config"
)

type Service interface {
	OpenStudy(command OpenStudyCommand) (OpenStudyCommandResult, error)
	StartRenderJob(command RenderStudyCommand) (StartedJob, error)
	StartProcessJob(command ProcessStudyCommand) (StartedJob, error)
	StartAnalyzeJob(command AnalyzeStudyCommand) (StartedJob, error)
	GetJob(command JobCommand) (JobSnapshot, error)
	GetJobs(command GetJobsCommand) ([]JobSnapshot, error)
	CancelJob(command JobCommand) (JobSnapshot, error)
	GetProcessingManifest() ProcessingManifest
	MeasureLineAnnotation(
		command MeasureLineAnnotationCommand,
	) (MeasureLineAnnotationCommandResult, error)
	OnJobCompletion(callback func(JobSnapshot))
	OnJobUpdate(callback func(JobSnapshot))
}

type embeddedService struct {
	app *internalapp.App
}

func NewEmbeddedService(baseDir string, logger *slog.Logger) (Service, error) {
	cfg := internalconfig.Default()
	if baseDir != "" {
		cleanBaseDir := filepath.Clean(baseDir)
		cfg.Paths.BaseDir = cleanBaseDir
		cfg.Paths.CacheDir = filepath.Join(cleanBaseDir, "cache")
		cfg.Paths.PersistenceDir = filepath.Join(cleanBaseDir, "state")
	}

	application, err := internalapp.NewService(cfg, logger)
	if err != nil {
		return nil, err
	}

	return &embeddedService{app: application}, nil
}

func (service *embeddedService) OpenStudy(
	command OpenStudyCommand,
) (OpenStudyCommandResult, error) {
	return service.app.OpenStudy(command)
}

func (service *embeddedService) StartRenderJob(
	command RenderStudyCommand,
) (StartedJob, error) {
	return service.app.StartRenderJob(command)
}

func (service *embeddedService) StartProcessJob(
	command ProcessStudyCommand,
) (StartedJob, error) {
	return service.app.StartProcessJob(command)
}

func (service *embeddedService) StartAnalyzeJob(
	command AnalyzeStudyCommand,
) (StartedJob, error) {
	return service.app.StartAnalyzeJob(command)
}

func (service *embeddedService) GetJob(command JobCommand) (JobSnapshot, error) {
	return service.app.GetJob(command)
}

func (service *embeddedService) GetJobs(command GetJobsCommand) ([]JobSnapshot, error) {
	return service.app.GetJobs(command)
}

func (service *embeddedService) CancelJob(command JobCommand) (JobSnapshot, error) {
	return service.app.CancelJob(command)
}

func (service *embeddedService) GetProcessingManifest() ProcessingManifest {
	return service.app.GetProcessingManifest()
}

func (service *embeddedService) MeasureLineAnnotation(
	command MeasureLineAnnotationCommand,
) (MeasureLineAnnotationCommandResult, error) {
	return service.app.MeasureLineAnnotation(command)
}

func (service *embeddedService) OnJobCompletion(callback func(JobSnapshot)) {
	if callback == nil {
		service.app.OnJobCompletion(nil)
		return
	}

	service.app.OnJobCompletion(func(snapshot JobSnapshot) {
		callback(snapshot)
	})
}

func (service *embeddedService) OnJobUpdate(callback func(JobSnapshot)) {
	if callback == nil {
		service.app.OnJobUpdate(nil)
		return
	}

	service.app.OnJobUpdate(func(snapshot JobSnapshot) {
		callback(snapshot)
	})
}
