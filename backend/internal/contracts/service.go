package contracts

// BackendService is the shared command surface consumed by transport adapters.
// Implementations may expose optional runtime hooks in addition to these
// contract-defined commands.
type BackendService interface {
	OpenStudy(command OpenStudyCommand) (OpenStudyCommandResult, error)
	StartRenderJob(command RenderStudyCommand) (StartedJob, error)
	StartAnalyzeJob(command AnalyzeStudyCommand) (StartedJob, error)
	StartProcessJob(command ProcessStudyCommand) (StartedJob, error)
	GetJob(command JobCommand) (JobSnapshot, error)
	GetJobs(command GetJobsCommand) ([]JobSnapshot, error)
	CancelJob(command JobCommand) (JobSnapshot, error)
	GetProcessingManifest() ProcessingManifest
	MeasureLineAnnotation(
		command MeasureLineAnnotationCommand,
	) (MeasureLineAnnotationCommandResult, error)
}
