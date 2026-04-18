package jobs

import (
	"context"
	"fmt"
	"sync"

	"xrayview/backend/internal/contracts"
)

type StartJobOutcome struct {
	Snapshot contracts.JobSnapshot
	Created  bool
}

// Registry owns the lifecycle of every job the service runs. It hands out
// job IDs, serializes snapshot mutations under a single write lock, and
// tracks which fingerprints are currently in flight via activeFingerprints
// — the dedupe map. While a fingerprint has a live entry there, a second
// StartJob with the same fingerprint returns the existing snapshot with
// Created=false instead of launching a duplicate; entries are inserted at
// StartJob and released by whichever path drives the job to a terminal
// state (Complete, Fail, markCancelledLocked). Snapshots returned to callers
// are deep-cloned so they can be read and mutated without holding the lock.
type Registry struct {
	mu                 sync.RWMutex
	newJobID           idGenerator
	jobs               map[string]*registryEntry
	activeFingerprints map[string]string
}

// maxTerminalJobs caps how many done/failed/cancelled entries we hold onto
// so late Get() calls can still find their result. Eviction runs on every
// terminal transition via evictOldTerminalJobsLocked.
const maxTerminalJobs = 64

type registryEntry struct {
	fingerprint string
	// cancellationRequested is a sticky latch the execute loop polls between
	// stages. Distinct from the three JobState cancel variants:
	// JobStateCancelling is what Cancel() writes on a running job,
	// JobStateCancelled is the terminal state markCancelledLocked drives to,
	// and this flag is what survives an UpdateProgress state rewrite so the
	// worker still sees the cancellation at its next stage boundary.
	cancellationRequested bool
	snapshot              contracts.JobSnapshot
	// cancel is nilled by Complete, Fail, markCancelledLocked, and Cancel's
	// queued branch — never invoke it after one of those paths has run, it
	// is either already fired or belongs to a context we have moved past.
	cancel context.CancelFunc
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
	registry.evictOldTerminalJobsLocked(jobID)

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
	registry.evictOldTerminalJobsLocked(jobID)

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
	registry.evictOldTerminalJobsLocked(jobID)

	return cloneJobSnapshot(entry.snapshot), nil
}

// Cancel drives a job toward cancellation. Three outcomes:
//   - Queued: transition directly to Cancelled (the worker never ran, so no
//     cleanup is needed).
//   - Running or Cancelling: flip to Cancelling, set the latch, and fire the
//     context cancel so any in-flight decode or write unblocks. The execute
//     loop observes the latch at its next stage boundary via
//     finishCancelledIfRequested and finalizes the Cancelled transition
//     there — this function intentionally does not finalize.
//   - Completed, Failed, or already Cancelled: no-op; the snapshot is
//     returned unchanged.
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
		registry.evictOldTerminalJobsLocked(jobID)
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
	registry.evictOldTerminalJobsLocked(entry.snapshot.JobID)
}

func (registry *Registry) releaseFingerprintLocked(entry *registryEntry) {
	if entry.fingerprint == "" {
		return
	}

	delete(registry.activeFingerprints, entry.fingerprint)
	entry.fingerprint = ""
}

// evictOldTerminalJobsLocked trims the jobs map back under maxTerminalJobs.
// Only terminal entries are eligible — a running job still owns a cancel
// func and cannot be dropped — and keepJobID is held back so the entry that
// just transitioned is still readable by the next Get. There is no recency
// tracking; which terminal entries go is map-iteration order, so do not
// rely on LRU semantics.
func (registry *Registry) evictOldTerminalJobsLocked(keepJobID string) {
	if len(registry.jobs) <= maxTerminalJobs {
		return
	}

	terminalJobIDs := make([]string, 0, len(registry.jobs)-maxTerminalJobs)
	for jobID, entry := range registry.jobs {
		if jobID != keepJobID && isTerminalState(entry.snapshot.State) {
			terminalJobIDs = append(terminalJobIDs, jobID)
		}
	}

	excess := len(registry.jobs) - maxTerminalJobs
	if excess > len(terminalJobIDs) {
		excess = len(terminalJobIDs)
	}

	for index := 0; index < excess; index++ {
		delete(registry.jobs, terminalJobIDs[index])
	}
}

// cloneJobSnapshot deep-copies a snapshot's pointer fields so returned
// snapshots can be read and mutated outside the registry lock. Do not
// "optimize" this into a direct return — the pointer fields would alias the
// live entry and a later registry mutation would race the caller.
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
