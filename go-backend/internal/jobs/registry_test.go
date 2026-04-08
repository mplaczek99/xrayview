package jobs

import (
	"context"
	"testing"

	"xrayview/go-backend/internal/contracts"
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
