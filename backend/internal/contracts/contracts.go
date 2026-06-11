package contracts

const (
	ServiceName            = "xrayview-backend"
	BackendContractVersion = 2
)

const (
	CommandGetProcessingManifest = "get_processing_manifest"
	CommandOpenStudy             = "open_study"
	CommandStartRenderJob        = "start_render_job"
	CommandStartAnalyzeJob       = "start_analyze_job"
	CommandStartProcessJob       = "start_process_job"
	CommandGetJob                = "get_job"
	CommandGetJobs               = "get_jobs"
	CommandCancelJob             = "cancel_job"
	CommandMeasureLineAnnotation = "measure_line_annotation"
	CommandSetStudyCalibration   = "set_study_calibration"
)

var SupportedCommands = []string{
	CommandGetProcessingManifest,
	CommandOpenStudy,
	CommandStartRenderJob,
	CommandStartAnalyzeJob,
	CommandStartProcessJob,
	CommandGetJob,
	CommandGetJobs,
	CommandCancelJob,
	CommandMeasureLineAnnotation,
	CommandSetStudyCalibration,
}

// PaletteName is the contract-level pseudocolor identifier. Keep the string
// values aligned with the JSON schema and TypeScript generated bindings.
type PaletteName string

const (
	PaletteNone PaletteName = "none"
	PaletteHot  PaletteName = "hot"
	PaletteBone PaletteName = "bone"
)

// ProcessingControls captures every adjustable processing knob in a preset.
type ProcessingControls struct {
	Brightness int         `json:"brightness"`
	Contrast   float64     `json:"contrast"`
	Invert     bool        `json:"invert"`
	Equalize   bool        `json:"equalize"`
	Palette    PaletteName `json:"palette"`
}

// ProcessingPreset is a named preset the frontend can select by ID.
type ProcessingPreset struct {
	ID       string             `json:"id"`
	Controls ProcessingControls `json:"controls"`
}

// ProcessingManifest is the full backend-provided preset menu.
type ProcessingManifest struct {
	DefaultPresetID string             `json:"defaultPresetId"`
	Presets         []ProcessingPreset `json:"presets"`
}

// DefaultProcessingManifest is the backend's hardcoded preset list.
func DefaultProcessingManifest() ProcessingManifest {
	return ProcessingManifest{
		DefaultPresetID: "default",
		Presets: []ProcessingPreset{
			{
				ID: "default",
				Controls: ProcessingControls{
					Brightness: 0,
					Contrast:   1.0,
					Invert:     false,
					Equalize:   false,
					Palette:    PaletteNone,
				},
			},
			{
				ID: "xray",
				Controls: ProcessingControls{
					Brightness: 10,
					Contrast:   1.4,
					Invert:     false,
					Equalize:   true,
					Palette:    PaletteBone,
				},
			},
			{
				ID: "high-contrast",
				Controls: ProcessingControls{
					Brightness: 0,
					Contrast:   1.8,
					Invert:     false,
					Equalize:   true,
					Palette:    PaletteNone,
				},
			},
		},
	}
}

// BackendErrorCode is the coarse error taxonomy surfaced to the frontend.
type BackendErrorCode string

const (
	InvalidInput   BackendErrorCode = "invalidInput"
	NotFound       BackendErrorCode = "notFound"
	Cancelled      BackendErrorCode = "cancelled"
	Conflict       BackendErrorCode = "conflict"
	CacheCorrupted BackendErrorCode = "cacheCorrupted"
	Internal       BackendErrorCode = "internal"
)

// BackendError is the structured error payload used by command handlers.
type BackendError struct {
	Code        BackendErrorCode `json:"code"`
	Message     string           `json:"message"`
	Details     []string         `json:"details,omitempty"`
	Recoverable bool             `json:"recoverable"`
}

func (err BackendError) Error() string {
	return err.Message
}

// NewBackendError applies the same recoverability rule as Rust: internal
// errors are not recoverable, everything else is.
func NewBackendError(code BackendErrorCode, message string) BackendError {
	return BackendError{
		Code:        code,
		Message:     message,
		Recoverable: code != Internal,
	}
}

func InvalidInputError(message string) BackendError {
	return NewBackendError(InvalidInput, message)
}

func NotFoundError(message string) BackendError {
	return NewBackendError(NotFound, message)
}

func InternalError(message string) BackendError {
	return NewBackendError(Internal, message)
}

// MeasurementScale records per-axis pixel spacing in millimeters. BMP inputs
// do not provide this metadata, but manual calibration can attach it later.
type MeasurementScale struct {
	RowSpacingMM    float64 `json:"rowSpacingMm"`
	ColumnSpacingMM float64 `json:"columnSpacingMm"`
	Source          string  `json:"source"`
}

// AnnotationSource identifies where an annotation came from. Today the app
// creates manual annotations, but the enum shape leaves room for generated
// suggestions without changing the wire envelope.
type AnnotationSource string

const (
	AnnotationManual AnnotationSource = "manual"
)

// AnnotationPoint stores image-space coordinates. Float64 preserves frontend
// fractional positions after zoom/pan transforms.
type AnnotationPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// LineMeasurement is a pixel distance plus an optional calibrated distance.
type LineMeasurement struct {
	PixelLength        float64  `json:"pixelLength"`
	CalibratedLengthMM *float64 `json:"calibratedLengthMm,omitempty"`
}

// LineAnnotation is a user-drawn line, optionally carrying a measurement.
type LineAnnotation struct {
	ID          string           `json:"id"`
	Label       string           `json:"label"`
	Source      AnnotationSource `json:"source"`
	Start       AnnotationPoint  `json:"start"`
	End         AnnotationPoint  `json:"end"`
	Editable    bool             `json:"editable"`
	Confidence  *float64         `json:"confidence,omitempty"`
	Measurement *LineMeasurement `json:"measurement,omitempty"`
}

// RectangleAnnotation represents an axis-aligned image-space rectangle.
type RectangleAnnotation struct {
	ID         string           `json:"id"`
	Label      string           `json:"label"`
	Source     AnnotationSource `json:"source"`
	X          float64          `json:"x"`
	Y          float64          `json:"y"`
	Width      float64          `json:"width"`
	Height     float64          `json:"height"`
	Editable   bool             `json:"editable"`
	Confidence *float64         `json:"confidence,omitempty"`
}

// PolylineAnnotation is an open or closed line strip. Closed polylines render
// as polygon outlines in the frontend.
type PolylineAnnotation struct {
	ID         string            `json:"id"`
	Label      string            `json:"label"`
	Source     AnnotationSource  `json:"source"`
	Points     []AnnotationPoint `json:"points"`
	Closed     bool              `json:"closed"`
	Editable   bool              `json:"editable"`
	Confidence *float64          `json:"confidence,omitempty"`
}

// AnnotationBundle groups annotations by shape for straightforward frontend
// rendering without a tagged-union switch.
type AnnotationBundle struct {
	Lines      []LineAnnotation      `json:"lines"`
	Rectangles []RectangleAnnotation `json:"rectangles"`
	Polylines  []PolylineAnnotation  `json:"polylines"`
}

// StudyRecord is the backend's session identity for an opened source image.
type StudyRecord struct {
	StudyID          string            `json:"studyId"`
	InputPath        string            `json:"inputPath"`
	InputName        string            `json:"inputName"`
	MeasurementScale *MeasurementScale `json:"measurementScale,omitempty"`
}

type OpenStudyCommand struct {
	InputPath string `json:"inputPath"`
}

type OpenStudyCommandResult struct {
	Study StudyRecord `json:"study"`
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

type AnalyzeStudyCommand struct {
	StudyID string `json:"studyId"`
}

type AnalyzeStudyCommandResult struct {
	StudyID           string            `json:"studyId"`
	PreviewPath       string            `json:"previewPath"`
	FilledPreviewPath string            `json:"filledPreviewPath"`
	LoadedWidth       uint32            `json:"loadedWidth"`
	LoadedHeight      uint32            `json:"loadedHeight"`
	Mode              string            `json:"mode"`
	MeasurementScale  *MeasurementScale `json:"measurementScale,omitempty"`
}

// ProcessStudyCommand is the processing command payload after JSON decoding.
// Pointer fields preserve Rust's Option<T> override semantics.
type ProcessStudyCommand struct {
	StudyID    string       `json:"studyId"`
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
	LoadedWidth      uint32            `json:"loadedWidth"`
	LoadedHeight     uint32            `json:"loadedHeight"`
	Mode             string            `json:"mode"`
	MeasurementScale *MeasurementScale `json:"measurementScale,omitempty"`
}

type MeasureLineAnnotationCommand struct {
	StudyID    string         `json:"studyId"`
	Annotation LineAnnotation `json:"annotation"`
}

type MeasureLineAnnotationCommandResult struct {
	StudyID    string         `json:"studyId"`
	Annotation LineAnnotation `json:"annotation"`
}

// CalibrationReference is a reference segment of known physical length used to
// derive a study's pixel spacing. BMP carries no pixel-spacing metadata, so
// calibration is manual: the user draws a line across a feature of known size
// and supplies its real-world length in mm. The pixel length is recomputed
// server-side from start/end so the measurement math stays single-sourced.
type CalibrationReference struct {
	Start         AnnotationPoint `json:"start"`
	End           AnnotationPoint `json:"end"`
	KnownLengthMM float64         `json:"knownLengthMm"`
}

// SetStudyCalibrationCommand sets or clears a study's calibration. A nil
// Reference clears any existing calibration; otherwise an isotropic
// MeasurementScale is derived from the reference segment.
type SetStudyCalibrationCommand struct {
	StudyID   string                `json:"studyId"`
	Reference *CalibrationReference `json:"reference,omitempty"`
}

type SetStudyCalibrationCommandResult struct {
	Study StudyRecord `json:"study"`
}

type JobCommand struct {
	JobID string `json:"jobId"`
}

type GetJobsCommand struct {
	JobIDs []string `json:"jobIds"`
}

type JobKind string

const (
	JobKindRenderStudy  JobKind = "renderStudy"
	JobKindAnalyzeStudy JobKind = "analyzeStudy"
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
