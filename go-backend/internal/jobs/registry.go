package jobs

import (
	"context"
	"fmt"
	"sync"

	"xrayview/go-backend/internal/contracts"
)

type StartJobOutcome struct {
	Snapshot contracts.JobSnapshot
	Created  bool
}

type Registry struct {
	mu                 sync.RWMutex
	newJobID           idGenerator
	jobs               map[string]*registryEntry
	activeFingerprints map[string]string
}

type registryEntry struct {
	fingerprint           string
	cancellationRequested bool
	snapshot              contracts.JobSnapshot
	cancel                context.CancelFunc
}

func NewRegistry(jobIDFactory idGenerator) *Registry {
	if jobIDFactory == nil {
		jobIDFactory = generateJobID
	}

	return &Registry{
		newJobID:           jobIDFactory,
		jobs:               make(map[string]*registryEntry),
		activeFingerprints: make(map[string]string),
	}
}

func (registry *Registry) StartJob(
	jobKind contracts.JobKind,
	studyID string,
	fingerprint string,
) (StartJobOutcome, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	if existingJobID, ok := registry.activeFingerprints[fingerprint]; ok {
		if existing, exists := registry.jobs[existingJobID]; exists &&
			!isTerminalState(existing.snapshot.State) {
			return StartJobOutcome{
				Snapshot: cloneJobSnapshot(existing.snapshot),
				Created:  false,
			}, nil
		}

		delete(registry.activeFingerprints, fingerprint)
	}

	jobID, err := registry.newJobID()
	if err != nil {
		return StartJobOutcome{}, contracts.Internal(fmt.Sprintf("generate job id: %v", err))
	}

	snapshot := queuedJobSnapshot(jobID, jobKind, studyID)
	registry.jobs[jobID] = &registryEntry{
		fingerprint: fingerprint,
		snapshot:    snapshot,
	}
	registry.activeFingerprints[fingerprint] = jobID

	return StartJobOutcome{
		Snapshot: cloneJobSnapshot(snapshot),
		Created:  true,
	}, nil
}

func (registry *Registry) AttachCancel(jobID string, cancel context.CancelFunc) error {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	entry, ok := registry.jobs[jobID]
	if !ok {
		return contracts.NotFound(fmt.Sprintf("job not found: %s", jobID))
	}

	entry.cancel = cancel
	if entry.cancellationRequested && cancel != nil {
		cancel()
	}

	return nil
}

func (registry *Registry) CreateCachedJob(
	jobKind contracts.JobKind,
	studyID string,
	result contracts.JobResult,
) (contracts.JobSnapshot, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	jobID, err := registry.newJobID()
	if err != nil {
		return contracts.JobSnapshot{}, contracts.Internal(fmt.Sprintf("generate job id: %v", err))
	}

	snapshot := completedJobSnapshot(jobID, jobKind, studyID, true, result)
	registry.jobs[jobID] = &registryEntry{snapshot: snapshot}

	return cloneJobSnapshot(snapshot), nil
}

func (registry *Registry) Get(jobID string) (contracts.JobSnapshot, error) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	entry, ok := registry.jobs[jobID]
	if !ok {
		return contracts.JobSnapshot{}, contracts.NotFound(fmt.Sprintf("job not found: %s", jobID))
	}

	return cloneJobSnapshot(entry.snapshot), nil
}

func (registry *Registry) UpdateProgress(
	jobID string,
	state contracts.JobState,
	percent int,
	stage string,
	message string,
) (contracts.JobSnapshot, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	entry, ok := registry.jobs[jobID]
	if !ok {
		return contracts.JobSnapshot{}, contracts.NotFound(fmt.Sprintf("job not found: %s", jobID))
	}
	if isTerminalState(entry.snapshot.State) || entry.snapshot.State == contracts.JobStateCancelling {
		return cloneJobSnapshot(entry.snapshot), nil
	}

	entry.snapshot.State = state
	entry.snapshot.Progress = contracts.JobProgress{
		Percent: percent,
		Stage:   stage,
		Message: message,
	}
	entry.snapshot.Error = nil

	return cloneJobSnapshot(entry.snapshot), nil
}

func (registry *Registry) Complete(
	jobID string,
	result contracts.JobResult,
) (contracts.JobSnapshot, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	entry, ok := registry.jobs[jobID]
	if !ok {
		return contracts.JobSnapshot{}, contracts.NotFound(fmt.Sprintf("job not found: %s", jobID))
	}

	if registry.shouldCancelLocked(entry) {
		registry.markCancelledLocked(entry, entry.snapshot.Progress.Stage, "Cancelled by user")
		return cloneJobSnapshot(entry.snapshot), nil
	}
	if isTerminalState(entry.snapshot.State) {
		return cloneJobSnapshot(entry.snapshot), nil
	}

	entry.snapshot.State = contracts.JobStateCompleted
	entry.snapshot.Progress = contracts.JobProgress{
		Percent: 100,
		Stage:   "completed",
		Message: "Completed",
	}
	entry.snapshot.FromCache = false
	entry.snapshot.Result = &contracts.JobResult{
		Kind:    result.Kind,
		Payload: result.Payload,
	}
	entry.snapshot.Error = nil
	entry.cancellationRequested = false
	entry.cancel = nil
	registry.releaseFingerprintLocked(entry)

	return cloneJobSnapshot(entry.snapshot), nil
}

func (registry *Registry) Fail(jobID string, err error) (contracts.JobSnapshot, error) {
	backendErr, ok := err.(contracts.BackendError)
	if !ok {
		backendErr = contracts.Internal(err.Error())
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	entry, ok := registry.jobs[jobID]
	if !ok {
		return contracts.JobSnapshot{}, contracts.NotFound(fmt.Sprintf("job not found: %s", jobID))
	}

	if registry.shouldCancelLocked(entry) {
		registry.markCancelledLocked(entry, entry.snapshot.Progress.Stage, "Cancelled by user")
		return cloneJobSnapshot(entry.snapshot), nil
	}
	if isTerminalState(entry.snapshot.State) {
		return cloneJobSnapshot(entry.snapshot), nil
	}

	entry.snapshot.State = contracts.JobStateFailed
	entry.snapshot.Progress.Message = "Failed"
	entry.snapshot.Result = nil
	entry.snapshot.Error = &backendErr
	entry.cancel = nil
	registry.releaseFingerprintLocked(entry)

	return cloneJobSnapshot(entry.snapshot), nil
}

func (registry *Registry) Cancel(jobID string) (contracts.JobSnapshot, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	entry, ok := registry.jobs[jobID]
	if !ok {
		return contracts.JobSnapshot{}, contracts.NotFound(fmt.Sprintf("job not found: %s", jobID))
	}

	switch entry.snapshot.State {
	case contracts.JobStateQueued:
		entry.cancellationRequested = true
		entry.snapshot.State = contracts.JobStateCancelled
		entry.snapshot.Progress.Message = "Cancelled before start"
		entry.snapshot.Result = nil
		entry.snapshot.Error = nil
		if entry.cancel != nil {
			entry.cancel()
		}
		entry.cancel = nil
		registry.releaseFingerprintLocked(entry)
	case contracts.JobStateRunning, contracts.JobStateCancelling:
		entry.cancellationRequested = true
		entry.snapshot.State = contracts.JobStateCancelling
		entry.snapshot.Progress.Message = "Cancellation requested"
		entry.snapshot.Error = nil
		if entry.cancel != nil {
			entry.cancel()
		}
	case contracts.JobStateCompleted, contracts.JobStateFailed, contracts.JobStateCancelled:
	}

	return cloneJobSnapshot(entry.snapshot), nil
}

func (registry *Registry) MarkCancelled(
	jobID string,
	stage string,
	message string,
) (contracts.JobSnapshot, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	entry, ok := registry.jobs[jobID]
	if !ok {
		return contracts.JobSnapshot{}, contracts.NotFound(fmt.Sprintf("job not found: %s", jobID))
	}
	if entry.snapshot.State == contracts.JobStateCompleted || entry.snapshot.State == contracts.JobStateFailed {
		return cloneJobSnapshot(entry.snapshot), nil
	}

	registry.markCancelledLocked(entry, stage, message)
	return cloneJobSnapshot(entry.snapshot), nil
}

func (registry *Registry) IsCancellationRequested(jobID string) (bool, error) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	entry, ok := registry.jobs[jobID]
	if !ok {
		return false, contracts.NotFound(fmt.Sprintf("job not found: %s", jobID))
	}

	return entry.cancellationRequested, nil
}

func (registry *Registry) shouldCancelLocked(entry *registryEntry) bool {
	return entry.cancellationRequested ||
		entry.snapshot.State == contracts.JobStateCancelling ||
		entry.snapshot.State == contracts.JobStateCancelled
}

func (registry *Registry) markCancelledLocked(
	entry *registryEntry,
	stage string,
	message string,
) {
	entry.cancellationRequested = true
	entry.snapshot.State = contracts.JobStateCancelled
	if stage != "" {
		entry.snapshot.Progress.Stage = stage
	}
	if message != "" {
		entry.snapshot.Progress.Message = message
	}
	entry.snapshot.Result = nil
	entry.snapshot.Error = nil
	entry.cancel = nil
	registry.releaseFingerprintLocked(entry)
}

func (registry *Registry) releaseFingerprintLocked(entry *registryEntry) {
	if entry.fingerprint == "" {
		return
	}

	delete(registry.activeFingerprints, entry.fingerprint)
	entry.fingerprint = ""
}

func cloneJobSnapshot(snapshot contracts.JobSnapshot) contracts.JobSnapshot {
	cloned := snapshot

	if snapshot.StudyID != nil {
		cloned.StudyID = studyIDPointer(*snapshot.StudyID)
	}
	if snapshot.Result != nil {
		result := *snapshot.Result
		cloned.Result = &result
	}
	if snapshot.Error != nil {
		backendErr := *snapshot.Error
		if backendErr.Details != nil {
			backendErr.Details = append([]string(nil), backendErr.Details...)
		}
		cloned.Error = &backendErr
	}

	return cloned
}
