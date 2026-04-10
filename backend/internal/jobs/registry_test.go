package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"xrayview/backend/internal/contracts"
)

func TestRegistryStartJobReusesActiveFingerprint(t *testing.T) {
	registry := NewRegistry(sequenceJobIDs("job-1"))

	first, err := registry.StartJob(
		contracts.JobKindProcessStudy,
		"study-1",
		"fingerprint-1",
	)
	if err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}
	if !first.Created {
		t.Fatal("first Created = false, want true")
	}

	second, err := registry.StartJob(
		contracts.JobKindProcessStudy,
		"study-1",
		"fingerprint-1",
	)
	if err != nil {
		t.Fatalf("duplicate StartJob returned error: %v", err)
	}
	if second.Created {
		t.Fatal("second Created = true, want false")
	}
	if got, want := second.Snapshot.JobID, first.Snapshot.JobID; got != want {
		t.Fatalf("duplicate JobID = %q, want reused %q", got, want)
	}
}

func TestRegistryCreateCachedJobReturnsCompletedCacheHit(t *testing.T) {
	registry := NewRegistry(sequenceJobIDs("job-1"))

	snapshot, err := registry.CreateCachedJob(
		contracts.JobKindRenderStudy,
		"study-1",
		contracts.JobResult{
			Kind: contracts.JobKindRenderStudy,
			Payload: contracts.RenderStudyCommandResult{
				StudyID:     "study-1",
				PreviewPath: "/tmp/preview.png",
			},
		},
	)
	if err != nil {
		t.Fatalf("CreateCachedJob returned error: %v", err)
	}

	if got, want := snapshot.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}
	if !snapshot.FromCache {
		t.Fatal("FromCache = false, want true")
	}
	if got, want := snapshot.Progress.Stage, "cacheHit"; got != want {
		t.Fatalf("Progress.Stage = %q, want %q", got, want)
	}
	if snapshot.Result == nil {
		t.Fatal("Result = nil, want cached payload")
	}
}

func TestRegistryCompleteReleasesFingerprintForFutureRuns(t *testing.T) {
	registry := NewRegistry(sequenceJobIDs("job-1", "job-2"))

	started, err := registry.StartJob(
		contracts.JobKindProcessStudy,
		"study-1",
		"fingerprint-1",
	)
	if err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}

	snapshot, err := registry.Complete(
		started.Snapshot.JobID,
		contracts.JobResult{
			Kind: contracts.JobKindProcessStudy,
			Payload: contracts.ProcessStudyCommandResult{
				StudyID:     "study-1",
				PreviewPath: "/tmp/preview.png",
				DicomPath:   "/tmp/output.dcm",
			},
		},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := snapshot.State, contracts.JobStateCompleted; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}

	next, err := registry.StartJob(
		contracts.JobKindProcessStudy,
		"study-1",
		"fingerprint-1",
	)
	if err != nil {
		t.Fatalf("replacement StartJob returned error: %v", err)
	}
	if !next.Created {
		t.Fatal("replacement Created = false, want true")
	}
	if got, want := next.Snapshot.JobID, "job-2"; got != want {
		t.Fatalf("replacement JobID = %q, want %q", got, want)
	}
}

func TestRegistryCancelQueuedMarksCancelledAndReleasesFingerprint(t *testing.T) {
	registry := NewRegistry(sequenceJobIDs("job-1", "job-2"))

	started, err := registry.StartJob(
		contracts.JobKindRenderStudy,
		"study-1",
		"fingerprint-1",
	)
	if err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}

	snapshot, err := registry.Cancel(started.Snapshot.JobID)
	if err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}
	if got, want := snapshot.State, contracts.JobStateCancelled; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}
	if got, want := snapshot.Progress.Message, "Cancelled before start"; got != want {
		t.Fatalf("Progress.Message = %q, want %q", got, want)
	}

	next, err := registry.StartJob(
		contracts.JobKindRenderStudy,
		"study-1",
		"fingerprint-1",
	)
	if err != nil {
		t.Fatalf("replacement StartJob returned error: %v", err)
	}
	if !next.Created {
		t.Fatal("replacement Created = false, want true")
	}
}

func TestRegistryCancelRunningPreventsLaterProgressAndCompletion(t *testing.T) {
	registry := NewRegistry(sequenceJobIDs("job-1", "job-2"))

	started, err := registry.StartJob(
		contracts.JobKindProcessStudy,
		"study-1",
		"fingerprint-1",
	)
	if err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}

	cancelledCtx := false
	if err := registry.AttachCancel(started.Snapshot.JobID, func() {
		cancelledCtx = true
	}); err != nil {
		t.Fatalf("AttachCancel returned error: %v", err)
	}

	if _, err := registry.UpdateProgress(
		started.Snapshot.JobID,
		contracts.JobStateRunning,
		30,
		"loadingStudy",
		"Loading source pixels",
	); err != nil {
		t.Fatalf("UpdateProgress returned error: %v", err)
	}

	cancelling, err := registry.Cancel(started.Snapshot.JobID)
	if err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}
	if got, want := cancelling.State, contracts.JobStateCancelling; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}
	if !cancelledCtx {
		t.Fatal("cancel func not invoked")
	}

	stillCancelling, err := registry.UpdateProgress(
		started.Snapshot.JobID,
		contracts.JobStateRunning,
		90,
		"writingPreview",
		"Writing preview",
	)
	if err != nil {
		t.Fatalf("UpdateProgress after cancel returned error: %v", err)
	}
	if got, want := stillCancelling.State, contracts.JobStateCancelling; got != want {
		t.Fatalf("post-cancel state = %q, want %q", got, want)
	}

	final, err := registry.Complete(
		started.Snapshot.JobID,
		contracts.JobResult{
			Kind: contracts.JobKindProcessStudy,
			Payload: contracts.ProcessStudyCommandResult{
				StudyID:     "study-1",
				PreviewPath: "/tmp/preview.png",
				DicomPath:   "/tmp/output.dcm",
			},
		},
	)
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if got, want := final.State, contracts.JobStateCancelled; got != want {
		t.Fatalf("final State = %q, want %q", got, want)
	}
	if got, want := final.Progress.Message, "Cancelled by user"; got != want {
		t.Fatalf("final Progress.Message = %q, want %q", got, want)
	}
	if final.Result != nil {
		t.Fatalf("Result = %#v, want nil", final.Result)
	}

	replacement, err := registry.StartJob(
		contracts.JobKindProcessStudy,
		"study-1",
		"fingerprint-1",
	)
	if err != nil {
		t.Fatalf("replacement StartJob returned error: %v", err)
	}
	if !replacement.Created {
		t.Fatal("replacement Created = false, want true")
	}
}

func TestRegistryFailDuringCancellationMarksCancelled(t *testing.T) {
	registry := NewRegistry(sequenceJobIDs("job-1"))

	started, err := registry.StartJob(
		contracts.JobKindRenderStudy,
		"study-1",
		"fingerprint-1",
	)
	if err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}
	if err := registry.AttachCancel(started.Snapshot.JobID, context.CancelFunc(func() {})); err != nil {
		t.Fatalf("AttachCancel returned error: %v", err)
	}
	if _, err := registry.UpdateProgress(
		started.Snapshot.JobID,
		contracts.JobStateRunning,
		35,
		"loadingStudy",
		"Loading source study",
	); err != nil {
		t.Fatalf("UpdateProgress returned error: %v", err)
	}
	if _, err := registry.Cancel(started.Snapshot.JobID); err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}

	final, err := registry.Fail(started.Snapshot.JobID, contracts.Internal("decode failed"))
	if err != nil {
		t.Fatalf("Fail returned error: %v", err)
	}
	if got, want := final.State, contracts.JobStateCancelled; got != want {
		t.Fatalf("final State = %q, want %q", got, want)
	}
	if final.Error != nil {
		t.Fatalf("Error = %#v, want nil", final.Error)
	}
}

func TestRegistryBoundsRetainedTerminalJobs(t *testing.T) {
	nextID := 0
	registry := NewRegistry(func() (string, error) {
		nextID++
		return fmt.Sprintf("job-%03d", nextID), nil
	})

	for index := 0; index < maxTerminalJobs+16; index++ {
		snapshot, err := registry.CreateCachedJob(
			contracts.JobKindRenderStudy,
			"study-1",
			contracts.JobResult{
				Kind: contracts.JobKindRenderStudy,
				Payload: contracts.RenderStudyCommandResult{
					StudyID:     "study-1",
					PreviewPath: fmt.Sprintf("/tmp/preview-%03d.png", index),
				},
			},
		)
		if err != nil {
			t.Fatalf("CreateCachedJob returned error: %v", err)
		}
		if got, want := snapshot.State, contracts.JobStateCompleted; got != want {
			t.Fatalf("snapshot.State = %q, want %q", got, want)
		}
	}

	if got, want := len(registry.jobs), maxTerminalJobs; got != want {
		t.Fatalf("len(jobs) = %d, want %d", got, want)
	}
}

func TestRegistryJobIDGenerationFailuresReturnInternalError(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Registry) error
	}{
		{
			name: "start job",
			run: func(registry *Registry) error {
				_, err := registry.StartJob(
					contracts.JobKindRenderStudy,
					"study-1",
					"fingerprint-1",
				)
				return err
			},
		},
		{
			name: "create cached job",
			run: func(registry *Registry) error {
				_, err := registry.CreateCachedJob(
					contracts.JobKindRenderStudy,
					"study-1",
					contracts.JobResult{
						Kind: contracts.JobKindRenderStudy,
						Payload: contracts.RenderStudyCommandResult{
							StudyID:     "study-1",
							PreviewPath: "/tmp/preview.png",
						},
					},
				)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := NewRegistry(func() (string, error) {
				return "", errors.New("boom")
			})

			err := test.run(registry)
			if err == nil {
				t.Fatal("returned nil error, want internal error")
			}

			backendErr, ok := err.(contracts.BackendError)
			if !ok {
				t.Fatalf("error type = %T, want contracts.BackendError", err)
			}
			if got, want := backendErr.Code, contracts.BackendErrorCodeInternal; got != want {
				t.Fatalf("error code = %q, want %q", got, want)
			}
			if !strings.Contains(backendErr.Message, "generate job id: boom") {
				t.Fatalf("error message = %q, want generate-job-id context", backendErr.Message)
			}
		})
	}
}

func TestRegistryAttachCancelInvokesCallbackImmediatelyAfterQueuedCancellation(t *testing.T) {
	registry := NewRegistry(sequenceJobIDs("job-1"))

	started, err := registry.StartJob(
		contracts.JobKindRenderStudy,
		"study-1",
		"fingerprint-1",
	)
	if err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}

	if _, err := registry.Cancel(started.Snapshot.JobID); err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}

	cancelCalled := false
	if err := registry.AttachCancel(started.Snapshot.JobID, func() {
		cancelCalled = true
	}); err != nil {
		t.Fatalf("AttachCancel returned error: %v", err)
	}
	if !cancelCalled {
		t.Fatal("cancel func not invoked for already-cancelled job")
	}

	snapshot, err := registry.Get(started.Snapshot.JobID)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got, want := snapshot.State, contracts.JobStateCancelled; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}
}

func TestRegistryFailWrapsPlainErrorsAsInternal(t *testing.T) {
	registry := NewRegistry(sequenceJobIDs("job-1"))

	started, err := registry.StartJob(
		contracts.JobKindRenderStudy,
		"study-1",
		"fingerprint-1",
	)
	if err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}

	snapshot, err := registry.Fail(started.Snapshot.JobID, errors.New("boom"))
	if err != nil {
		t.Fatalf("Fail returned error: %v", err)
	}
	if got, want := snapshot.State, contracts.JobStateFailed; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}
	if snapshot.Error == nil {
		t.Fatal("Error = nil, want backend error payload")
	}
	if got, want := snapshot.Error.Code, contracts.BackendErrorCodeInternal; got != want {
		t.Fatalf("Error.Code = %q, want %q", got, want)
	}
	if got, want := snapshot.Error.Message, "boom"; got != want {
		t.Fatalf("Error.Message = %q, want %q", got, want)
	}
}

func TestRegistryGetReturnsIndependentSnapshotCopies(t *testing.T) {
	registry := NewRegistry(sequenceJobIDs("job-1"))

	started, err := registry.StartJob(
		contracts.JobKindAnalyzeStudy,
		"study-1",
		"fingerprint-1",
	)
	if err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}

	if _, err := registry.Fail(
		started.Snapshot.JobID,
		contracts.InvalidInput("bad request").WithDetails("detail-1"),
	); err != nil {
		t.Fatalf("Fail returned error: %v", err)
	}

	first, err := registry.Get(started.Snapshot.JobID)
	if err != nil {
		t.Fatalf("first Get returned error: %v", err)
	}
	if first.StudyID == nil {
		t.Fatal("StudyID = nil, want populated study id")
	}
	if first.Error == nil || len(first.Error.Details) != 1 {
		t.Fatalf("Error = %#v, want cloned backend error details", first.Error)
	}

	*first.StudyID = "mutated-study"
	first.Error.Message = "mutated message"
	first.Error.Details[0] = "mutated detail"

	second, err := registry.Get(started.Snapshot.JobID)
	if err != nil {
		t.Fatalf("second Get returned error: %v", err)
	}
	if second.StudyID == nil {
		t.Fatal("second StudyID = nil, want populated study id")
	}
	if got, want := *second.StudyID, "study-1"; got != want {
		t.Fatalf("StudyID = %q, want %q", got, want)
	}
	if second.Error == nil {
		t.Fatal("second Error = nil, want backend error payload")
	}
	if got, want := second.Error.Message, "bad request"; got != want {
		t.Fatalf("Error.Message = %q, want %q", got, want)
	}
	if got, want := second.Error.Details[0], "detail-1"; got != want {
		t.Fatalf("Error.Details[0] = %q, want %q", got, want)
	}
}
