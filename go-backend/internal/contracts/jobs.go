package contracts

type JobKind string

const (
	JobKindRenderStudy  JobKind = "renderStudy"
	JobKindProcessStudy JobKind = "processStudy"
	JobKindAnalyzeStudy JobKind = "analyzeStudy"
)

type JobState string

const (
	JobStateQueued     JobState = "queued"
	JobStateRunning    JobState = "running"
	JobStateCancelling JobState = "cancelling"
	JobStateCompleted  JobState = "completed"
	JobStateFailed     JobState = "failed"
	JobStateCancelled  JobState = "cancelled"
)

type JobProgress struct {
	Percent int    `json:"percent"`
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

type StartedJob struct {
	JobID string `json:"jobId"`
}

type JobCommand struct {
	JobID string `json:"jobId"`
}

type RenderStudyCommand struct {
	StudyID string `json:"studyId"`
}

type RenderStudyCommandResult struct {
	StudyID          string            `json:"studyId"`
	PreviewPath      string            `json:"previewPath"`
	LoadedWidth      uint32            `json:"loadedWidth"`
	LoadedHeight     uint32            `json:"loadedHeight"`
	MeasurementScale *MeasurementScale `json:"measurementScale,omitempty"`
}

type JobResult struct {
	Kind    JobKind `json:"kind"`
	Payload any     `json:"payload"`
}

type JobSnapshot struct {
	JobID     string        `json:"jobId"`
	JobKind   JobKind       `json:"jobKind"`
	StudyID   *string       `json:"studyId,omitempty"`
	State     JobState      `json:"state"`
	Progress  JobProgress   `json:"progress"`
	FromCache bool          `json:"fromCache"`
	Result    *JobResult    `json:"result,omitempty"`
	Error     *BackendError `json:"error,omitempty"`
}
