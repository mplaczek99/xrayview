// Package shell is the public boundary that the desktop module (a separate Go
// module) binds to. Go forbids importing another module's internal packages, so
// this package — which lives inside the backend module and may therefore use the
// internals — re-exports the backend constructors and the contract command/result
// types the Wails shell needs. It adds no behavior; it is purely the seam.
package shell

import (
	"xrayview/backend/internal/app"
	"xrayview/backend/internal/config"
	"xrayview/backend/internal/contracts"
)

// Backend orchestrator + config, re-exported for the desktop shell.
type (
	App                   = app.App
	JobUpdateSubscription = app.JobUpdateSubscription
	Config                = config.Config
)

// Contract types used in the Wails-bound method signatures (window.go.main.App.*).
type (
	ProcessingManifest                 = contracts.ProcessingManifest
	OpenStudyCommand                   = contracts.OpenStudyCommand
	OpenStudyCommandResult             = contracts.OpenStudyCommandResult
	RenderStudyCommand                 = contracts.RenderStudyCommand
	AnalyzeStudyCommand                = contracts.AnalyzeStudyCommand
	ProcessStudyCommand                = contracts.ProcessStudyCommand
	JobCommand                         = contracts.JobCommand
	GetJobsCommand                     = contracts.GetJobsCommand
	MeasureLineAnnotationCommand       = contracts.MeasureLineAnnotationCommand
	MeasureLineAnnotationCommandResult = contracts.MeasureLineAnnotationCommandResult
	SetStudyCalibrationCommand         = contracts.SetStudyCalibrationCommand
	SetStudyCalibrationCommandResult   = contracts.SetStudyCalibrationCommandResult
	StartedJob                         = contracts.StartedJob
	JobSnapshot                        = contracts.JobSnapshot
	BackendError                       = contracts.BackendError
)

// LoadConfig reads backend configuration from the environment.
func LoadConfig() (Config, error) { return config.Load() }

// NewApp builds the backend orchestrator.
func NewApp(cfg Config) (*App, error) { return app.New(cfg) }

// InternalError wraps a message as an internal BackendError.
func InternalError(message string) BackendError { return contracts.InternalError(message) }
