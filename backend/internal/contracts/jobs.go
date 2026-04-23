package contracts

type JobKind string

const (
	JobKindRenderStudy  JobKind = "renderStudy"
	JobKindProcessStudy JobKind = "processStudy"
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

type GetJobsCommand struct {
	JobIDs []string `json:"jobIds"`
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

type ProcessStudyCommand struct {
	StudyID    string       `json:"studyId"`
	OutputPath *string      `json:"outputPath,omitempty"`
	PresetID   string       `json:"presetId"`
	Invert     bool         `json:"invert"`
	Brightness *int         `json:"brightness,omitempty"`
	Contrast   *float64     `json:"contrast,omitempty"`
	Equalize   bool         `json:"equalize"`
	Compare    bool         `json:"compare"`
	Palette    *PaletteName `json:"palette,omitempty"`
}

type ProcessStudyCommandResult struct {
	StudyID          string            `json:"studyId"`
	PreviewPath      string            `json:"previewPath"`
	DicomPath        string            `json:"dicomPath"`
	LoadedWidth      uint32            `json:"loadedWidth"`
	LoadedHeight     uint32            `json:"loadedHeight"`
	Mode             string            `json:"mode"`
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
