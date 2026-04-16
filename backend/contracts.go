package backend

import internalcontracts "xrayview/backend/internal/contracts"

type (
	AnalyzeStudyCommand                = internalcontracts.AnalyzeStudyCommand
	AnalyzeStudyCommandResult          = internalcontracts.AnalyzeStudyCommandResult
	AnnotationBundle                   = internalcontracts.AnnotationBundle
	AnnotationPoint                    = internalcontracts.AnnotationPoint
	AnnotationSource                   = internalcontracts.AnnotationSource
	BackendError                       = internalcontracts.BackendError
	BackendErrorCode                   = internalcontracts.BackendErrorCode
	GetJobsCommand                     = internalcontracts.GetJobsCommand
	JobCommand                         = internalcontracts.JobCommand
	JobKind                            = internalcontracts.JobKind
	JobProgress                        = internalcontracts.JobProgress
	JobResult                          = internalcontracts.JobResult
	JobSnapshot                        = internalcontracts.JobSnapshot
	JobState                           = internalcontracts.JobState
	LineAnnotation                     = internalcontracts.LineAnnotation
	LineMeasurement                    = internalcontracts.LineMeasurement
	MeasurementScale                   = internalcontracts.MeasurementScale
	MeasureLineAnnotationCommand       = internalcontracts.MeasureLineAnnotationCommand
	MeasureLineAnnotationCommandResult = internalcontracts.MeasureLineAnnotationCommandResult
	OpenStudyCommand                   = internalcontracts.OpenStudyCommand
	OpenStudyCommandResult             = internalcontracts.OpenStudyCommandResult
	PaletteName                        = internalcontracts.PaletteName
	ProcessStudyCommand                = internalcontracts.ProcessStudyCommand
	ProcessStudyCommandResult          = internalcontracts.ProcessStudyCommandResult
	ProcessingManifest                 = internalcontracts.ProcessingManifest
	ProcessingPreset                   = internalcontracts.ProcessingPreset
	RectangleAnnotation                = internalcontracts.RectangleAnnotation
	RenderStudyCommand                 = internalcontracts.RenderStudyCommand
	RenderStudyCommandResult           = internalcontracts.RenderStudyCommandResult
	StartedJob                         = internalcontracts.StartedJob
	StudyRecord                        = internalcontracts.StudyRecord
)

const (
	AnnotationSourceManual    = internalcontracts.AnnotationSourceManual
	AnnotationSourceAutoTooth = internalcontracts.AnnotationSourceAutoTooth

	BackendErrorCodeInvalidInput   = internalcontracts.BackendErrorCodeInvalidInput
	BackendErrorCodeNotFound       = internalcontracts.BackendErrorCodeNotFound
	BackendErrorCodeCancelled      = internalcontracts.BackendErrorCodeCancelled
	BackendErrorCodeConflict       = internalcontracts.BackendErrorCodeConflict
	BackendErrorCodeCacheCorrupted = internalcontracts.BackendErrorCodeCacheCorrupted
	BackendErrorCodeInternal       = internalcontracts.BackendErrorCodeInternal

	JobKindRenderStudy  = internalcontracts.JobKindRenderStudy
	JobKindProcessStudy = internalcontracts.JobKindProcessStudy
	JobKindAnalyzeStudy = internalcontracts.JobKindAnalyzeStudy

	JobStateQueued     = internalcontracts.JobStateQueued
	JobStateRunning    = internalcontracts.JobStateRunning
	JobStateCancelling = internalcontracts.JobStateCancelling
	JobStateCompleted  = internalcontracts.JobStateCompleted
	JobStateFailed     = internalcontracts.JobStateFailed
	JobStateCancelled  = internalcontracts.JobStateCancelled

	PaletteNone = internalcontracts.PaletteNone
	PaletteHot  = internalcontracts.PaletteHot
	PaletteBone = internalcontracts.PaletteBone
)

func DefaultProcessingManifest() ProcessingManifest {
	return internalcontracts.DefaultProcessingManifest()
}
