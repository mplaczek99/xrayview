# XRayView Architecture Migration Plan

**Target**: Hybrid in-process integration — the desktop app constructs the backend
service directly, eliminating the sidecar process for normal runtime while preserving
`xrayviewd` and `xrayview-cli` as standalone entrypoints over the same core.

**Author audience**: A single senior Go/TypeScript developer executing sequentially.

---

## 0. Baseline Benchmarks to Establish Before Any Change

No `Bench*` functions exist in the backend test suite today. Establish these before
touching any code so every phase has a before/after comparison.

### 0.1 Metrics to record

| Metric | How to measure | Why |
|---|---|---|
| DecodeStudy wall time | Benchmark (see below) | Bottleneck #2 — decoded 2-3× per workflow |
| RenderSourceImage wall time | Benchmark | Shared-preview reuse baseline |
| ProcessSourceImage wall time | Benchmark | Processing pipeline baseline |
| AnalyzePreview wall time | Benchmark | Analysis pipeline baseline |
| Open-study → preview visible | Manual stopwatch / devtools | End-to-end user-perceived latency |
| Job completion → UI update | Add `performance.now()` delta in `receiveJobUpdate` (`workbenchStore.ts:621`) | Measures poll lag |
| Peak RSS (sidecar) | `go tool pprof` heap or `/proc/$PID/status` VmRSS | Two-process baseline |
| Peak RSS (desktop shell) | Same approach on the Wails process | Two-process baseline |
| DecodeStudy calls per workflow | Add a temporary counter log in `service.go` job executors | Should be 1 after Phase 1.2 |
| Artifact bytes written | `du -sh /tmp/xrayview/cache/artifacts/` after one full workflow | Preview pipeline baseline |
| Sidecar startup time | Timestamps around `EnsureStarted` in `sidecar.go:130-210` | Eliminated in Phase 3 |

### 0.2 Benchmark file to create

Create `backend/internal/jobs/bench_test.go`:

```go
package jobs

import (
	"context"
	"os"
	"testing"

	"xrayview/backend/internal/cache"
	"xrayview/backend/internal/dicommeta"
	"xrayview/backend/internal/processing"
	"xrayview/backend/internal/render"
)

const benchmarkDicomPath = "../../../images/sample-dental-radiograph.dcm"

func BenchmarkDecodeStudy(b *testing.B) {
	if _, err := os.Stat(benchmarkDicomPath); err != nil {
		b.Skip("benchmark DICOM not available")
	}

	decoder := dicommeta.NewDecoder()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := decoder.DecodeStudy(ctx, benchmarkDicomPath); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenderSourceImage(b *testing.B) {
	if _, err := os.Stat(benchmarkDicomPath); err != nil {
		b.Skip("benchmark DICOM not available")
	}

	decoder := dicommeta.NewDecoder()
	study, err := decoder.DecodeStudy(context.Background(), benchmarkDicomPath)
	if err != nil {
		b.Fatal(err)
	}

	plan := render.DefaultRenderPlan()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		render.RenderSourceImage(study.Image, plan)
	}
}

func BenchmarkProcessSourceImage(b *testing.B) {
	if _, err := os.Stat(benchmarkDicomPath); err != nil {
		b.Skip("benchmark DICOM not available")
	}

	decoder := dicommeta.NewDecoder()
	study, err := decoder.DecodeStudy(context.Background(), benchmarkDicomPath)
	if err != nil {
		b.Fatal(err)
	}

	plan := render.DefaultRenderPlan()
	controls := processing.GrayscaleControls{Contrast: 1.0}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := processing.ProcessSourceImage(
			study.Image, plan, controls, "none", false,
		); err != nil {
			b.Fatal(err)
		}
	}
}
```

### 0.3 Commands to run

```bash
# Go benchmarks
go -C backend test ./internal/jobs -bench=. -benchmem -count=3

# CLI timing (render pipeline end-to-end)
time go -C backend run ./cmd/xrayview-cli render-preview \
  images/sample-dental-radiograph.dcm /tmp/bench-preview.png

# CLI timing (process pipeline end-to-end)
time go -C backend run ./cmd/xrayview-cli process-preview \
  images/sample-dental-radiograph.dcm /tmp/bench-process.png

# Sidecar startup (manual)
# In desktop/sidecar.go, wrap EnsureStarted with time logging, then:
npm run wails:run

# RSS measurement
# After opening a study and running all three jobs, record:
#   cat /proc/$(pgrep xrayview-backend)/status | grep VmRSS
#   cat /proc/$(pgrep xrayview)/status | grep VmRSS
```

### 0.4 Frontend timing shim

Add temporarily to `frontend/src/features/jobs/useJobs.ts` to measure poll lag:

```typescript
// Temporary: remove after Phase 2
const jobSubmitTimes = new Map<string, number>();
export function recordJobSubmit(jobId: string) {
  jobSubmitTimes.set(jobId, performance.now());
}
// In pollPendingJobs, after receiveJobUpdate:
//   if (job.state === "completed" && jobSubmitTimes.has(jobId)) {
//     console.info(`[bench] ${jobId} visible in ${performance.now() - jobSubmitTimes.get(jobId)!}ms`);
//     jobSubmitTimes.delete(jobId);
//   }
```

**Estimated effort**: 1 day  
**Deliverable**: Benchmark numbers recorded in a local `BENCHMARKS.md` for comparison.

---

## Phase 1 — Caching & Responsiveness (zero architectural risk)

No module structure changes. No new dependencies. Every change is internal to one
package and testable in isolation.

---

### 1.1 Adaptive Job Polling

**Problem**: `POLL_INTERVAL_MS` is fixed at 1500ms (`frontend/src/features/jobs/useJobs.ts:5`).
Backend jobs complete in ~235ms (render) to ~601ms (analyze). A completed job waits
up to 1500ms before the UI sees it.

**Implementation**:

Modify `frontend/src/features/jobs/useJobs.ts`:

```typescript
const FAST_POLL_MS = 200;
const SLOW_POLL_MS = 2000;
const IDLE_POLL_MS = 0; // no polling when nothing is pending

export function useJobs() {
  useEffect(() => {
    let cancelled = false;
    let timer: number | undefined;

    async function pollPendingJobs() {
      const state = workbenchActions.getState();
      const pendingIds = Object.values(state.jobs)
        .filter(
          (job) =>
            job.state === "queued" ||
            job.state === "running" ||
            job.state === "cancelling",
        )
        .map((job) => job.jobId);

      if (pendingIds.length === 0) {
        scheduleNext(IDLE_POLL_MS);
        return;
      }

      await Promise.all(
        pendingIds.map(async (jobId) => {
          try {
            const job = await runtime.getJob(jobId);
            if (!cancelled) {
              workbenchActions.receiveJobUpdate(job);
            }
          } catch {
            // Keep polling; individual failures are transient.
          }
        }),
      );

      // All jobs settled? Slow down. Still pending? Stay fast.
      const stillPending = Object.values(workbenchActions.getState().jobs).some(
        (job) =>
          job.state === "queued" ||
          job.state === "running" ||
          job.state === "cancelling",
      );
      scheduleNext(stillPending ? FAST_POLL_MS : SLOW_POLL_MS);
    }

    function scheduleNext(intervalMs: number) {
      if (cancelled) return;
      if (timer !== undefined) window.clearTimeout(timer);
      if (intervalMs <= 0) return;
      timer = window.setTimeout(() => void pollPendingJobs(), intervalMs);
    }

    // Kick off immediately, then adaptive.
    void pollPendingJobs();

    return () => {
      cancelled = true;
      if (timer !== undefined) window.clearTimeout(timer);
    };
  }, []);
}
```

**Files modified**: `frontend/src/features/jobs/useJobs.ts`

**Test strategy**: Manual — open a study, observe render job completion time in devtools
console. With the Phase 0 timing shim, job-visible latency should drop from ~1500ms to
~200ms.

**Success metric**: Median job-completion-to-UI-visible time < 300ms (down from ~1500ms).

---

### 1.2 Decoded-Study LRU Cache

**Problem**: `DecodeStudy` is called independently in:
- `executeRenderJob` at `service.go:445`
- `executeProcessJob` at `service.go:562`
- `executeAnalyzeJob` at `service.go:714`

A typical workflow (open → render → process → analyze) decodes the same DICOM file 3×.
`DecodeStudy` is the most expensive operation per job (~100-300ms for a typical dental radiograph).

**Implementation**:

Create `backend/internal/studies/decode_cache.go`:

```go
package studies

import (
	"context"
	"sync"

	"xrayview/backend/internal/dicommeta"
)

const defaultDecodeCacheCapacity = 4

type decodeCacheEntry struct {
	study dicommeta.SourceStudy
	prev  *decodeCacheEntry
	next  *decodeCacheEntry
}

// DecodeCache is an LRU cache for decoded DICOM studies keyed by input path.
// It is safe for concurrent use.
type DecodeCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[string]*decodeCacheEntry
	head     *decodeCacheEntry // most recently used
	tail     *decodeCacheEntry // least recently used
}

func NewDecodeCache(capacity int) *DecodeCache {
	if capacity < 1 {
		capacity = defaultDecodeCacheCapacity
	}

	return &DecodeCache{
		capacity: capacity,
		entries:  make(map[string]*decodeCacheEntry, capacity),
	}
}

// GetOrDecode returns a cached SourceStudy or decodes the file and caches it.
// The returned SourceStudy must NOT be mutated by the caller.
func (cache *DecodeCache) GetOrDecode(
	ctx context.Context,
	path string,
	decoder dicommeta.Decoder,
) (dicommeta.SourceStudy, error) {
	cache.mu.Lock()
	if entry, ok := cache.entries[path]; ok {
		cache.moveToFrontLocked(entry)
		study := entry.study
		cache.mu.Unlock()
		return study, nil
	}
	cache.mu.Unlock()

	// Decode outside the lock — this is the expensive part.
	study, err := decoder.DecodeStudy(ctx, path)
	if err != nil {
		return dicommeta.SourceStudy{}, err
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()

	// Double-check: another goroutine may have inserted while we decoded.
	if entry, ok := cache.entries[path]; ok {
		cache.moveToFrontLocked(entry)
		return entry.study, nil
	}

	entry := &decodeCacheEntry{study: study}
	cache.entries[path] = entry
	cache.pushFrontLocked(entry)

	if len(cache.entries) > cache.capacity {
		cache.evictLocked()
	}

	return study, nil
}

func (cache *DecodeCache) Len() int {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return len(cache.entries)
}

func (cache *DecodeCache) moveToFrontLocked(entry *decodeCacheEntry) {
	if cache.head == entry {
		return
	}
	cache.removeLocked(entry)
	cache.pushFrontLocked(entry)
}

func (cache *DecodeCache) pushFrontLocked(entry *decodeCacheEntry) {
	entry.prev = nil
	entry.next = cache.head
	if cache.head != nil {
		cache.head.prev = entry
	}
	cache.head = entry
	if cache.tail == nil {
		cache.tail = entry
	}
}

func (cache *DecodeCache) removeLocked(entry *decodeCacheEntry) {
	if entry.prev != nil {
		entry.prev.next = entry.next
	} else {
		cache.head = entry.next
	}
	if entry.next != nil {
		entry.next.prev = entry.prev
	} else {
		cache.tail = entry.prev
	}
	entry.prev = nil
	entry.next = nil
}

func (cache *DecodeCache) evictLocked() {
	if cache.tail == nil {
		return
	}

	victim := cache.tail
	cache.removeLocked(victim)
	for path, entry := range cache.entries {
		if entry == victim {
			delete(cache.entries, path)
			break
		}
	}
}
```

**Wire into `jobs.Service`**:

Modify `backend/internal/jobs/service.go`:

1. Add field to `Service` struct (line 33-41):

```go
type Service struct {
	// ... existing fields ...
	decodeCache *studies.DecodeCache // NEW
}
```

2. Initialize in `newService` (line 280-322):

```go
// After line 305 (decoderFactory nil check):
decodeCache := studies.NewDecodeCache(defaultDecodeCacheCapacity)
```

And add `decodeCache: decodeCache` to the return struct.

3. Replace the `decoder.DecodeStudy(ctx, study.InputPath)` calls with cache lookups in
   all three job executors. Example for `executeRenderJob` (replace lines 436-452):

```go
decoder, err := service.newDecoder()
if err != nil {
	service.failJob(jobID, contracts.Internal(fmt.Sprintf("configure DICOM decoder: %v", err)))
	return
}

sourceStudy, err := service.decodeCache.GetOrDecode(ctx, study.InputPath, decoder)
if err != nil {
	if service.finishCancelledIfRequested(ctx, jobID, "loadingStudy", "") {
		return
	}
	service.failJob(jobID, contracts.Internal(fmt.Sprintf("load source study: %v", err)))
	return
}
```

Apply the same pattern to `executeProcessJob` (lines 553-569) and
`executeAnalyzeJob` (lines 706-724).

**Files created**: `backend/internal/studies/decode_cache.go`, `backend/internal/studies/decode_cache_test.go`  
**Files modified**: `backend/internal/jobs/service.go`

**Test strategy**:
- Unit test `DecodeCache`: concurrent GetOrDecode with same path returns same data,
  eviction after capacity, context cancellation.
- Integration: add a decode-counting wrapper in `service_test.go` to verify that a
  render→process→analyze sequence calls `DecodeStudy` exactly once.

**Success metric**: DecodeStudy calls per render→process→analyze workflow = 1 (down from 3).

---

### 1.3 Shared Source-Preview Reuse

**Problem**: Both `executeAnalyzeJob` (`service.go:737`) and `ProcessSourceImage`
(`processing/pipeline.go:65`) call `render.RenderSourceImage` independently to produce the
same grayscale preview from the same source pixels. The analyze job additionally writes
this preview to disk as a PNG (`service.go:738`), duplicating work already done by a
prior render job.

**Implementation**:

Add a source-preview cache to `cache.Memory`. A source preview is deterministic given
the input path (since `DefaultRenderPlan` is constant), so key it on input path.

Add to `backend/internal/cache/memory.go`:

```go
type Memory struct {
	mu             sync.Mutex
	logger         *slog.Logger
	entries        map[string]contracts.JobResult
	sourcePreviews map[string]imaging.PreviewImage // NEW: inputPath → rendered preview
}

func NewMemory(logger *slog.Logger) *Memory {
	return &Memory{
		logger:         logger,
		entries:        make(map[string]contracts.JobResult),
		sourcePreviews: make(map[string]imaging.PreviewImage),
	}
}

func (memory *Memory) StoreSourcePreview(inputPath string, preview imaging.PreviewImage) {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	memory.sourcePreviews[inputPath] = preview
}

func (memory *Memory) LoadSourcePreview(inputPath string) (imaging.PreviewImage, bool) {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	preview, ok := memory.sourcePreviews[inputPath]
	return preview, ok
}
```

Modify `executeRenderJob` in `service.go` — after `render.RenderSourceImage`
(line 468), cache the preview:

```go
preview := render.RenderSourceImage(sourceStudy.Image, render.DefaultRenderPlan())
service.memoryCache.StoreSourcePreview(study.InputPath, preview)
```

Modify `executeAnalyzeJob` in `service.go` — before rendering (around line 737),
try the cache first:

```go
preview, cached := service.memoryCache.LoadSourcePreview(study.InputPath)
if !cached {
	preview = render.RenderSourceImage(sourceStudy.Image, render.DefaultRenderPlan())
	service.memoryCache.StoreSourcePreview(study.InputPath, preview)
}
```

For `ProcessSourceImage` (`processing/pipeline.go:54-67`): add an alternative entry
point that accepts a pre-rendered preview:

```go
// ProcessWithPreview applies the processing pipeline to a pre-rendered preview
// instead of re-rendering from source. This avoids a redundant RenderSourceImage
// call when the caller already has the base preview.
func ProcessWithPreview(
	sourcePreview imaging.PreviewImage,
	controls GrayscaleControls,
	palette string,
	compare bool,
) (PipelineOutput, error) {
	return ProcessRenderedPreview(sourcePreview, controls, palette, compare)
}
```

Note: `ProcessRenderedPreview` already exists at `pipeline.go:15` and does exactly
what we need. The new function is just an alias for discoverability — or callers can
invoke `ProcessRenderedPreview` directly.

Then modify `executeProcessJob` (around lines 585-595) to use the cached preview:

```go
var output processing.PipelineOutput
if cachedPreview, ok := service.memoryCache.LoadSourcePreview(study.InputPath); ok {
	var err error
	output, err = processing.ProcessRenderedPreview(
		cachedPreview, resolved.Controls, resolved.Palette, resolved.Compare,
	)
	if err != nil {
		service.failJob(jobID, contracts.Internal(fmt.Sprintf("process source study: %v", err)))
		return
	}
} else {
	var err error
	output, err = processing.ProcessSourceImage(
		sourceStudy.Image, render.DefaultRenderPlan(), resolved.Controls, resolved.Palette, resolved.Compare,
	)
	if err != nil {
		service.failJob(jobID, contracts.Internal(fmt.Sprintf("process source study: %v", err)))
		return
	}
}
```

**Files modified**: `backend/internal/cache/memory.go`, `backend/internal/jobs/service.go`  
**Files optionally modified**: `backend/internal/processing/pipeline.go` (add `ProcessWithPreview` alias)

**Test strategy**: Unit test in `memory_test.go` for Store/Load round-trip. Integration
test: run render then process, verify `RenderSourceImage` called once total.

**Success metric**: `RenderSourceImage` calls per render→process→analyze workflow = 1
(down from 3).

---

### 1.4 Remove `outputPath` from `processFingerprint`

**Problem**: `processFingerprint` at `service.go:938-971` includes `OutputPath` (line 945)
in the fingerprint hash. Two process jobs with identical pixel operations but different
save paths produce different fingerprints, defeating the memory cache and causing redundant
computation.

**Implementation**:

Edit `processFingerprint` in `backend/internal/jobs/service.go` (lines 938-971):

```go
func processFingerprint(
	study contracts.StudyRecord,
	command contracts.ProcessStudyCommand,
) (string, error) {
	payload, err := json.Marshal(struct {
		Namespace  string                 `json:"namespace"`
		InputPath  string                 `json:"inputPath"`
		// OutputPath removed — does not affect pixel computation
		PresetID   string                 `json:"presetId"`
		Invert     bool                   `json:"invert"`
		Brightness *int                   `json:"brightness"`
		Contrast   *float64               `json:"contrast"`
		Equalize   bool                   `json:"equalize"`
		Compare    bool                   `json:"compare"`
		Palette    *contracts.PaletteName `json:"palette"`
	}{
		Namespace:  "process-study-v3", // bump version to invalidate old cache
		InputPath:  study.InputPath,
		PresetID:   command.PresetID,
		Invert:     command.Invert,
		Brightness: command.Brightness,
		Contrast:   command.Contrast,
		Equalize:   command.Equalize,
		Compare:    command.Compare,
		Palette:    command.Palette,
	})
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
```

Key changes:
- Remove `OutputPath *string` from the struct (was line 945)
- Bump `Namespace` from `"process-study-v2"` to `"process-study-v3"` to avoid stale
  cache collisions with the old fingerprint format

**Files modified**: `backend/internal/jobs/service.go`

**Test strategy**: Existing `service_test.go` tests that exercise process jobs. Add a
specific test: submit two process jobs with identical controls but different `OutputPath`
values, assert the second returns `FromCache: true`.

**Success metric**: Process job with different output path for identical controls
returns a cache hit.

---

### 1.5 Bounded Concurrency for Jobs

**Problem**: All three `Start*Job` methods launch goroutines immediately with `go`:
- `service.go:131` — `go service.executeRenderJob(...)`
- `service.go:195` — `go service.executeProcessJob(...)`
- `service.go:257` — `go service.executeAnalyzeJob(...)`

No concurrency limit. A user repeatedly clicking "process" with different settings spawns
unbounded goroutines, each holding a decoded DICOM study (potentially 50-100MB of float32
pixels) in memory simultaneously.

**Implementation**:

Add a semaphore to `Service`. Use `golang.org/x/sync/semaphore` or a simple channel-based
approach. Channel-based avoids a new dependency:

Add to `backend/internal/jobs/service.go`, in `Service` struct (line 33):

```go
type Service struct {
	// ... existing fields ...
	concurrencyLimit chan struct{} // NEW: buffered channel as semaphore
}
```

In `newService` (around line 310):

```go
const maxConcurrentJobs = 3

// In the returned struct:
concurrencyLimit: make(chan struct{}, maxConcurrentJobs),
```

Modify each goroutine launch. Example for render (replace line 131):

```go
go func() {
	service.concurrencyLimit <- struct{}{} // acquire
	defer func() { <-service.concurrencyLimit }() // release
	service.executeRenderJob(ctx, outcome.Snapshot.JobID, study, fingerprint)
}()
```

Apply same wrapper at `service.go:195` (process) and `service.go:257` (analyze).

**Files modified**: `backend/internal/jobs/service.go`

**Test strategy**: In `service_test.go`, submit 5 jobs simultaneously, verify only 3
run concurrently (e.g., use a slow mock decoder and check running-state counts).

**Success metric**: At most 3 concurrent job goroutines at any time. Steady-state RSS
under concurrent load drops proportionally.

---

### 1.6 LRU Eviction for `jobs.Registry` and `cache.Memory`

**Problem**: Both `Registry.jobs` (`registry.go:19`) and `Memory.entries`
(`memory.go:15`) are unbounded maps. Over a long session, completed jobs accumulate
indefinitely. `studies.Registry.studies` (`studies/registry.go:18`) also has no eviction.

**Implementation**:

**For `jobs.Registry`** — add eviction of terminal jobs when the map exceeds a threshold.

Add to `backend/internal/jobs/registry.go`:

```go
const maxTerminalJobs = 64

// Call after every Complete, Fail, or MarkCancelled.
func (registry *Registry) evictOldTerminalJobsLocked() {
	if len(registry.jobs) <= maxTerminalJobs {
		return
	}

	// Collect terminal entries. Since we don't track insertion order,
	// evict any terminal entry beyond the cap.
	terminal := make([]string, 0, len(registry.jobs)-maxTerminalJobs)
	for id, entry := range registry.jobs {
		if isTerminalState(entry.snapshot.State) && entry.fingerprint == "" {
			terminal = append(terminal, id)
		}
	}

	// Evict oldest first. Without timestamps, remove from the front of the collected slice.
	excess := len(registry.jobs) - maxTerminalJobs
	if excess > len(terminal) {
		excess = len(terminal)
	}
	for i := 0; i < excess; i++ {
		delete(registry.jobs, terminal[i])
	}
}
```

Call `registry.evictOldTerminalJobsLocked()` at the end of `Complete` (line 193),
`Fail` (line 225), and `markCancelledLocked` (line 317), while the lock is still held.

**For `cache.Memory`** — add a max-entries cap and LRU eviction. This is a smaller
map (keyed by fingerprint), so a simple cap suffices:

Add to `backend/internal/cache/memory.go`:

```go
const maxMemoryCacheEntries = 32

func (memory *Memory) storeLocked(fingerprint string, result contracts.JobResult) {
	memory.mu.Lock()
	defer memory.mu.Unlock()

	memory.entries[fingerprint] = result

	// Simple eviction: if over cap, remove one arbitrary entry.
	if len(memory.entries) > maxMemoryCacheEntries {
		for key := range memory.entries {
			if key != fingerprint {
				delete(memory.entries, key)
				break
			}
		}
	}
}
```

For a proper LRU, track access order. But given the small cap and low churn rate,
random eviction is acceptable for now.

**For `studies.Registry`** — studies are lightweight (just path + ID + measurement
scale). Cap at 32 entries with the same approach in `Register` (`studies/registry.go:32`).

**Files modified**: `backend/internal/jobs/registry.go`, `backend/internal/cache/memory.go`,
`backend/internal/studies/registry.go`

**Test strategy**: Unit tests that register >cap entries and verify the map stays
bounded. Verify that accessing a recently-used entry does not evict it.

**Success metric**: `len(registry.jobs)` stays ≤ 64 after 100 job submissions.

---

### Phase 1 Summary

**Estimated effort**: 4-5 days  
**Risk level**: Low — all changes are internal, no API or module boundary changes  
**Success metric**: Render→process→analyze workflow completes with 1 DecodeStudy call,
1 RenderSourceImage call, job visible in UI within 300ms of completion, all registries
bounded.

---

## Phase 2 — Push-Style Job Delivery (medium risk)

### 2.1 Extract a `BackendService` Interface

**Problem**: The backend's business logic is tightly coupled to HTTP transport. The
`httpapi.Dependencies` struct (`router.go:23-31`) holds concrete types:

```go
type Dependencies struct {
	Config      config.Config
	Logger      *slog.Logger
	Cache       *cache.Store
	Persistence *persistence.Catalog
	Jobs        *jobs.Service
	Studies     *studies.Registry
	StartedAt   time.Time
}
```

The HTTP handlers in `router.go:100-126` manually dispatch commands to `deps.Jobs.*`
and `deps.Studies.*`. To embed the backend in-process, we need a single interface that
both the HTTP router and the Wails binding layer can call.

**Implementation**:

Create `backend/internal/app/service.go`:

```go
package app

import (
	"xrayview/backend/internal/contracts"
)

// BackendService is the command interface for the xrayview backend.
// It is implemented by the App struct and consumed by both the HTTP router
// and the desktop Wails binding layer.
type BackendService interface {
	// Studies
	OpenStudy(command contracts.OpenStudyCommand) (contracts.OpenStudyCommandResult, error)

	// Jobs
	StartRenderJob(command contracts.RenderStudyCommand) (contracts.StartedJob, error)
	StartProcessJob(command contracts.ProcessStudyCommand) (contracts.StartedJob, error)
	StartAnalyzeJob(command contracts.AnalyzeStudyCommand) (contracts.StartedJob, error)
	GetJob(command contracts.JobCommand) (contracts.JobSnapshot, error)
	CancelJob(command contracts.JobCommand) (contracts.JobSnapshot, error)

	// Processing
	GetProcessingManifest() contracts.ProcessingManifest

	// Annotations
	MeasureLineAnnotation(command contracts.MeasureLineAnnotationCommand) (contracts.MeasureLineAnnotationCommandResult, error)
}
```

Implement on `*App` in `backend/internal/app/app.go` by extracting logic currently
inlined in the HTTP handlers (`router.go:172-345`). The `handleOpenStudy` function
(router.go:172-237) becomes `App.OpenStudy`, the job handlers delegate to
`app.jobs.Start*Job`, etc.

Then modify `httpapi.NewRouter` to accept `BackendService` instead of `Dependencies`:

```go
type RouterDeps struct {
	Service   BackendService
	Config    config.Config
	Logger    *slog.Logger
	Cache     *cache.Store
	StartedAt time.Time
}
```

Each handler becomes a thin JSON decode → `deps.Service.Method(command)` → JSON encode
adapter.

**Files created**: `backend/internal/app/service.go`  
**Files modified**: `backend/internal/app/app.go`, `backend/internal/httpapi/router.go`

**Test strategy**: All existing `httpapi/router_test.go` tests pass unchanged (they
hit the same router, which now delegates to the service interface). Add a test that
constructs a mock `BackendService` and verifies the router dispatches correctly.

---

### 2.2 Job Completion Callbacks

**Problem**: Job completions are invisible to callers until polled via `GetJob`.
The `Registry.Complete` method (`registry.go:157-194`) updates internal state but has
no notification mechanism.

**Implementation**:

Add an optional callback to `jobs.Service`:

```go
// In backend/internal/jobs/service.go, add to Service struct:
type JobCompletionCallback func(snapshot contracts.JobSnapshot)

type Service struct {
	// ... existing fields ...
	onJobCompletion JobCompletionCallback // NEW
}
```

Add setter method:

```go
func (service *Service) OnJobCompletion(callback JobCompletionCallback) {
	service.onJobCompletion = callback
}
```

Fire it at the end of `completeRenderJob` (after line 823), `completeProcessJob`
(after line 843), `completeAnalyzeJob` (after line 862), and `failJob` (after line 870):

```go
if service.onJobCompletion != nil {
	service.onJobCompletion(snapshot)
}
```

Also fire on cancellation in `finishCancelledIfRequested` (after line 803).

**Files modified**: `backend/internal/jobs/service.go`

---

### 2.3 Wails Event Emission

**Problem**: The desktop shell currently has no event emission. Wails v2 provides
`wailsruntime.EventsEmit(ctx, eventName, data...)` for Go→JS push.

**Implementation**:

In `desktop/app.go`, after constructing the backend service (Phase 3), register the
callback. For now, this phase prepares the frontend to receive events while the
backend still runs as a sidecar:

Add event constants to `desktop/app.go`:

```go
const (
	eventJobUpdate = "xrayview:job-update"
)
```

The callback (wired in Phase 3) will be:

```go
service.OnJobCompletion(func(snapshot contracts.JobSnapshot) {
	wailsruntime.EventsEmit(app.ctx, eventJobUpdate, snapshot)
})
```

**Frontend event listener** — modify `frontend/src/features/jobs/useJobs.ts` to
subscribe to Wails events when available:

```typescript
import type { JobSnapshot as ContractJobSnapshot } from "../../lib/generated/contracts";

// Wails v2 runtime events API (injected at runtime by the Wails webview)
declare global {
  interface Window {
    runtime?: {
      EventsOn(eventName: string, callback: (...args: unknown[]) => void): () => void;
    };
  }
}

export function useJobs() {
  useEffect(() => {
    let cancelled = false;
    let timer: number | undefined;
    let unsubscribeEvent: (() => void) | undefined;

    // Push path: if Wails runtime events are available, subscribe.
    if (window.runtime?.EventsOn) {
      unsubscribeEvent = window.runtime.EventsOn(
        "xrayview:job-update",
        (snapshot: ContractJobSnapshot) => {
          if (!cancelled) {
            // normalizeJobSnapshot lives in runtime.ts — import it
            const normalized = normalizeJobSnapshot(snapshot, "desktop");
            workbenchActions.receiveJobUpdate(normalized);
          }
        },
      );
    }

    // Poll path: kept as fallback for sidecar mode and mock mode.
    // When events are available, poll less aggressively.
    const basePollMs = unsubscribeEvent ? SLOW_POLL_MS : FAST_POLL_MS;

    async function pollPendingJobs() {
      // ... same adaptive logic from Phase 1.1, using basePollMs ...
    }

    void pollPendingJobs();

    return () => {
      cancelled = true;
      if (timer !== undefined) window.clearTimeout(timer);
      unsubscribeEvent?.();
    };
  }, []);
}
```

**Files modified**: `frontend/src/features/jobs/useJobs.ts`, `frontend/src/lib/runtime.ts`
(export `normalizeJobSnapshot`)

**Test strategy**:
- Unit: mock `window.runtime.EventsOn`, emit a job snapshot, verify
  `workbenchActions.receiveJobUpdate` is called.
- Integration: in sidecar mode, events are not available, verify polling fallback works.
- In Phase 3 (in-process mode), verify event push delivers job updates.

---

### Phase 2 Summary

**Estimated effort**: 3-4 days  
**Risk level**: Medium — introduces a new interface boundary and callback mechanism,
but the HTTP router continues to work unchanged  
**Success metric**: In-process mode (Phase 3) delivers job completions to the UI in
< 50ms without polling. Sidecar mode continues to work via polling fallback.

---

## Phase 3 — In-Process Integration (medium-high risk)

### 3.1 Module Dependency Changes

**Current state**: `desktop/go.mod` depends only on Wails (`desktop/go.mod:6`). The
`backend/go.mod` depends on `contracts` via replace (`backend/go.mod:10`). There is no
dependency from `desktop` → `backend`.

**Target state**: `desktop/go.mod` imports `xrayview/backend` packages directly.

**Exact `desktop/go.mod` changes**:

```
module xrayview/desktop

go 1.26.0

require (
	github.com/wailsapp/wails/v2 v2.11.0
	xrayview/backend v0.0.0
	xrayview/contracts v0.0.0
)

replace (
	xrayview/backend => ../backend
	xrayview/contracts => ../contracts
)

// ... indirect dependencies from both backend and wails ...
```

After editing, run:

```bash
cd desktop && go mod tidy
```

This will pull in `golang.org/x/image` (from backend's dependency) and
`xrayview/contracts` (transitive) as indirect dependencies.

**Files modified**: `desktop/go.mod`, `desktop/go.sum`

---

### 3.2 Construct Backend Service In-Process

**Current state**: `desktop/app.go:34-38` creates a `DesktopApp` with only a
`SidecarController`. In `startup` (line 41-54), it starts the sidecar subprocess.

**Target state**: When `XRAYVIEW_BACKEND_URL` is not explicitly set, construct the
backend service directly in-process. Fall back to sidecar when the env var is set
(for development or headless use).

Modify `desktop/app.go`:

```go
import (
	// ... existing imports ...
	"xrayview/backend/internal/app"
	backendconfig "xrayview/backend/internal/config"
	"xrayview/backend/internal/contracts"
)

type DesktopApp struct {
	ctx     context.Context
	sidecar *SidecarController
	backend app.BackendService // NEW: nil when using sidecar
}

func NewDesktopApp() (*DesktopApp, error) {
	controller := NewSidecarController()

	// If the user explicitly set a backend URL, use the sidecar path.
	// Otherwise, construct the backend in-process.
	if controller.mode == runtimeModeDesktop && !hasExplicitBackendURL() {
		cfg := backendconfig.Default()
		cfg.Paths.BaseDir = controller.baseDir
		cfg.Paths.CacheDir = filepath.Join(controller.baseDir, "cache")
		cfg.Paths.PersistenceDir = filepath.Join(controller.baseDir, "state")

		backendApp, err := app.New(cfg, nil)
		if err != nil {
			return nil, fmt.Errorf("construct in-process backend: %w", err)
		}

		return &DesktopApp{
			sidecar: controller,
			backend: backendApp,
		}, nil
	}

	return &DesktopApp{
		sidecar: controller,
	}, nil
}

func hasExplicitBackendURL() bool {
	return firstEnv(sidecarBaseURLEnvKey, legacySidecarBaseURLEnvKey) != ""
}
```

Modify `startup` (line 41-54):

```go
func (app *DesktopApp) startup(ctx context.Context) {
	app.ctx = ctx

	if app.backend != nil {
		// In-process mode: wire up job completion events.
		app.backend.OnJobCompletion(func(snapshot contracts.JobSnapshot) {
			wailsruntime.EventsEmit(ctx, eventJobUpdate, snapshot)
		})
		wailsruntime.LogInfo(ctx, "xrayview shell running with in-process backend")
		return
	}

	if !app.sidecar.Enabled() {
		wailsruntime.LogInfo(ctx, "xrayview shell running in mock mode")
		return
	}

	if err := app.sidecar.EnsureStarted(); err != nil {
		wailsruntime.LogErrorf(ctx, "xrayview sidecar startup failed: %s", err)
		return
	}

	wailsruntime.LogInfof(ctx, "xrayview shell ready against %s", app.sidecar.BaseURL())
}
```

**Note**: `app.New` from `backend/internal/app/app.go:31` currently creates an
`http.Server`. For in-process use, the server is not needed. Refactor `app.New` to
separate service construction from server creation:

```go
// In backend/internal/app/app.go:

// NewService constructs the backend service without starting an HTTP server.
// Used for in-process embedding.
func NewService(cfg config.Config, logger *slog.Logger) (*App, error) {
	return newApp(cfg, logger, nil, nil, nil)
}

// The existing New and NewFromEnvironment continue to work unchanged.
```

The `App` struct already has all the service methods (after Phase 2.1). The HTTP server
field is only used by `Run()`, which the desktop shell won't call in-process mode.

**Files modified**: `desktop/app.go`, `backend/internal/app/app.go`

---

### 3.3 Typed Wails Bindings

**Current state**: `InvokeBackendCommand` (`desktop/app.go:97-120`) is a single stringly-typed
method. The frontend calls it via `window.go.main.DesktopApp.InvokeBackendCommand(command, jsonPayload)`
(`wails.ts:11-14`). The response is a `{status, body}` pair that the frontend must JSON-parse.

**Target state**: One typed Wails binding method per command. Wails auto-generates
TypeScript bindings for each exported method on the bound struct.

Add typed methods to `DesktopApp` in `desktop/app.go`:

```go
func (app *DesktopApp) OpenStudy(command contracts.OpenStudyCommand) (contracts.OpenStudyCommandResult, error) {
	if app.backend != nil {
		return app.backend.OpenStudy(command)
	}
	return invokeViaHTTP[contracts.OpenStudyCommandResult](app, "open_study", command)
}

func (app *DesktopApp) StartRenderJob(command contracts.RenderStudyCommand) (contracts.StartedJob, error) {
	if app.backend != nil {
		return app.backend.StartRenderJob(command)
	}
	return invokeViaHTTP[contracts.StartedJob](app, "start_render_job", command)
}

func (app *DesktopApp) StartProcessJob(command contracts.ProcessStudyCommand) (contracts.StartedJob, error) {
	if app.backend != nil {
		return app.backend.StartProcessJob(command)
	}
	return invokeViaHTTP[contracts.StartedJob](app, "start_process_job", command)
}

func (app *DesktopApp) StartAnalyzeJob(command contracts.AnalyzeStudyCommand) (contracts.StartedJob, error) {
	if app.backend != nil {
		return app.backend.StartAnalyzeJob(command)
	}
	return invokeViaHTTP[contracts.StartedJob](app, "start_analyze_job", command)
}

func (app *DesktopApp) GetJobSnapshot(command contracts.JobCommand) (contracts.JobSnapshot, error) {
	if app.backend != nil {
		return app.backend.GetJob(command)
	}
	return invokeViaHTTP[contracts.JobSnapshot](app, "get_job", command)
}

func (app *DesktopApp) CancelJobByID(command contracts.JobCommand) (contracts.JobSnapshot, error) {
	if app.backend != nil {
		return app.backend.CancelJob(command)
	}
	return invokeViaHTTP[contracts.JobSnapshot](app, "cancel_job", command)
}

func (app *DesktopApp) GetProcessingManifest() contracts.ProcessingManifest {
	if app.backend != nil {
		return app.backend.GetProcessingManifest()
	}
	// Sidecar fallback — call via HTTP
	result, err := invokeViaHTTP[contracts.ProcessingManifest](app, "get_processing_manifest", nil)
	if err != nil {
		return contracts.DefaultProcessingManifest()
	}
	return result
}

func (app *DesktopApp) MeasureLineAnnotation(
	command contracts.MeasureLineAnnotationCommand,
) (contracts.MeasureLineAnnotationCommandResult, error) {
	if app.backend != nil {
		return app.backend.MeasureLineAnnotation(command)
	}
	return invokeViaHTTP[contracts.MeasureLineAnnotationCommandResult](app, "measure_line_annotation", command)
}

// invokeViaHTTP is the sidecar fallback — delegates to InvokeBackendCommand.
func invokeViaHTTP[T any](app *DesktopApp, command string, payload any) (T, error) {
	var zero T
	payloadJSON := ""
	if payload != nil {
		bytes, err := json.Marshal(payload)
		if err != nil {
			return zero, err
		}
		payloadJSON = string(bytes)
	}

	response := app.InvokeBackendCommand(command, payloadJSON)
	if response.Status >= 400 {
		var backendErr contracts.BackendError
		if err := json.Unmarshal([]byte(response.Body), &backendErr); err != nil {
			return zero, fmt.Errorf("backend command %s failed with status %d", command, response.Status)
		}
		return zero, backendErr
	}

	var result T
	if err := json.Unmarshal([]byte(response.Body), &result); err != nil {
		return zero, err
	}
	return result, nil
}
```

**Note on `contracts` import**: The `desktop` module now imports `xrayview/backend/internal/contracts`
(via the `xrayview/contracts` module transitively). Since `contracts.OpenStudyCommand` etc.
are defined in `xrayview/contracts/contractv1` and re-exported by
`xrayview/backend/internal/contracts`, the types will be available. [ASSUMPTION: Wails v2
can serialize/deserialize `contracts.*` types via its automatic JSON binding. Wails v2
uses `encoding/json` conventions, which all contract types follow.]

---

### 3.4 Frontend Binding Changes

**Current state**: `frontend/src/lib/wails.ts` defines:

```typescript
interface DesktopBindings {
  PickDicomFile(): Promise<string>;
  PickSaveDicomPath(defaultName?: string): Promise<string>;
  InvokeBackendCommand(command: string, payloadJson?: string): Promise<WailsBackendCommandResponse>;
}
```

And `frontend/src/lib/backend.ts:348-390` wraps every command through
`invokeDesktopBackend` → `invokeDesktopBackendCommand` → `InvokeBackendCommand`.

**Target state**: Call typed methods directly. Update `wails.ts`:

```typescript
interface DesktopBindings {
  // Shell methods (unchanged)
  PickDicomFile(): Promise<string>;
  PickSaveDicomPath(defaultName?: string): Promise<string>;

  // Legacy stringly-typed (kept for sidecar fallback)
  InvokeBackendCommand(command: string, payloadJson?: string): Promise<WailsBackendCommandResponse>;

  // Typed methods (new — from Phase 3.3 Go bindings)
  OpenStudy(command: OpenStudyCommand): Promise<OpenStudyCommandResult>;
  StartRenderJob(command: RenderStudyCommand): Promise<StartedJob>;
  StartProcessJob(command: ProcessStudyCommand): Promise<StartedJob>;
  StartAnalyzeJob(command: AnalyzeStudyCommand): Promise<StartedJob>;
  GetJobSnapshot(command: JobCommand): Promise<ContractJobSnapshot>;
  CancelJobByID(command: JobCommand): Promise<ContractJobSnapshot>;
  GetProcessingManifest(): Promise<ProcessingManifest>;
  MeasureLineAnnotation(command: MeasureLineAnnotationCommand): Promise<MeasureLineAnnotationCommandResult>;
}
```

Update `createDesktopBackendAPI` in `backend.ts` to use typed methods when available:

```typescript
export function createDesktopBackendAPI(): BackendAPI {
  const bindings = requireBindings();
  const hasTypedBindings = typeof bindings.OpenStudy === "function";

  if (hasTypedBindings) {
    return {
      mode: "desktop",
      loadProcessingManifest: () => bindings.GetProcessingManifest(),
      openStudy: (inputPath) => bindings.OpenStudy({ inputPath }),
      startRenderStudyJob: (studyId) => bindings.StartRenderJob({ studyId }),
      startProcessStudyJob: (studyId, request) =>
        bindings.StartProcessJob(buildProcessStudyCommand(studyId, request)),
      startAnalyzeStudyJob: (studyId) => bindings.StartAnalyzeJob({ studyId }),
      getJob: (jobId) => bindings.GetJobSnapshot({ jobId }),
      cancelJob: (jobId) => bindings.CancelJobByID({ jobId }),
      measureLineAnnotation: async (studyId, annotation) => {
        const result = await bindings.MeasureLineAnnotation({ studyId, annotation });
        return result.annotation;
      },
    };
  }

  // Fallback: legacy InvokeBackendCommand path (sidecar mode)
  return {
    mode: "desktop",
    loadProcessingManifest: () => invokeDesktopBackend<ProcessingManifest>("get_processing_manifest"),
    // ... same as current createDesktopBackendAPI (lines 349-390) ...
  };
}
```

**Files modified**: `frontend/src/lib/wails.ts`, `frontend/src/lib/backend.ts`

**What breaks and how to fix it**:

1. **`desktop/` tests** (`desktop/app_test.go` if it exists, plus `go -C desktop test ./...`):
   Tests that call `InvokeBackendCommand` will still work because the method is preserved.
   New typed methods need test coverage — test them against a mock `BackendService`.

2. **Frontend `mock` mode**: Unaffected. `createMockBackendAPI()` (`backend.ts:231-346`)
   is completely independent of Wails bindings.

3. **Wails binding generation**: Wails v2 auto-generates `frontend/wailsjs/` bindings
   from exported Go methods. After adding the typed methods, run `wails generate module`
   to regenerate. [ASSUMPTION: The project currently relies on manual `window.go.main`
   bindings rather than generated `wailsjs/` imports. If generated bindings are used,
   they will update automatically on `wails build`.]

---

### 3.5 Remove Sidecar from Normal Startup

After Phases 3.1-3.4 are verified working:

1. In `startup` (`desktop/app.go`), the in-process path is the default. The sidecar
   path only activates when `XRAYVIEW_BACKEND_URL` is explicitly set.

2. In `shutdown` (`desktop/app.go:56-58`), only call `app.sidecar.Stop()` if the sidecar
   was actually started.

3. **Do not delete `sidecar.go`** — it remains for the fallback path and for development
   (running the backend separately with hot-reload).

---

### Phase 3 Summary

**Estimated effort**: 5-7 days  
**Risk level**: Medium-high — changes module boundaries, adds new Wails bindings,
alters startup sequence  
**Success metric**: Desktop app starts with one OS process. `pgrep xrayview` returns
one PID. All existing functionality works. RSS is ~40-60% of the two-process baseline.

---

## Phase 4 — Preview Pipeline Optimization (low-medium risk)

### 4.1 In-Memory Preview Registry

**Problem**: Preview images follow a disk roundtrip:
1. Backend writes PNG to `cache/artifacts/{namespace}/{fingerprint}.png`
   (`render/preview_png.go:13-29`)
2. Desktop shell reads it back from disk in `ServeAsset` (`desktop/app.go:122-184`)
3. Webview decodes the PNG for display

After Phase 3, both the writer and the reader are in the same process, making the disk
roundtrip pure overhead.

**Implementation**:

Create `backend/internal/cache/preview_registry.go`:

```go
package cache

import (
	"sync"

	"xrayview/backend/internal/imaging"
)

const maxPreviewRegistryEntries = 16

type PreviewRegistryEntry struct {
	PNG []byte // encoded PNG bytes, ready to serve
}

type PreviewRegistry struct {
	mu      sync.RWMutex
	entries map[string]PreviewRegistryEntry // keyed by artifact path
	order   []string                        // LRU order, most recent last
}

func NewPreviewRegistry() *PreviewRegistry {
	return &PreviewRegistry{
		entries: make(map[string]PreviewRegistryEntry, maxPreviewRegistryEntries),
		order:   make([]string, 0, maxPreviewRegistryEntries),
	}
}

func (registry *PreviewRegistry) Store(path string, pngBytes []byte) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	registry.entries[path] = PreviewRegistryEntry{PNG: pngBytes}
	registry.touchLocked(path)
	registry.evictLocked()
}

func (registry *PreviewRegistry) Load(path string) ([]byte, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	entry, ok := registry.entries[path]
	if !ok {
		return nil, false
	}
	return entry.PNG, true
}

func (registry *PreviewRegistry) touchLocked(path string) {
	for i, p := range registry.order {
		if p == path {
			registry.order = append(registry.order[:i], registry.order[i+1:]...)
			break
		}
	}
	registry.order = append(registry.order, path)
}

func (registry *PreviewRegistry) evictLocked() {
	for len(registry.entries) > maxPreviewRegistryEntries && len(registry.order) > 0 {
		victim := registry.order[0]
		registry.order = registry.order[1:]
		delete(registry.entries, victim)
	}
}
```

**Wire into the preview pipeline**: After `render.SavePreviewPNG` calls in the job
executors (e.g., `service.go:489-492`), also encode to bytes and store in the registry.
Alternatively, intercept `SavePreviewPNG` to tee the output.

**Wire into `ServeAsset`**: In `desktop/app.go:122-184`, check the in-memory registry
before opening the file from disk:

```go
func (app *DesktopApp) ServeAsset(writer http.ResponseWriter, request *http.Request) {
	// ... existing path validation (lines 123-141) ...

	// Try in-memory registry first (in-process mode only).
	if app.previewRegistry != nil {
		if pngBytes, ok := app.previewRegistry.Load(rawPath); ok {
			writer.Header().Set("content-type", "image/png")
			writer.Header().Set("content-length", strconv.Itoa(len(pngBytes)))
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(pngBytes)
			return
		}
	}

	// Fall through to disk path (existing code, lines 143-183).
	// ...
}
```

### 4.2 Evaluation: Is This Worth Doing?

After Phase 3, the disk roundtrip is local and fast (SSD latency ~0.1ms for a
typical preview PNG of 200-500KB). The webview still must decode the PNG regardless
of source.

**Recommendation**: Implement Phase 4 only if profiling after Phase 3 shows that
preview serving is a measurable bottleneck (> 5% of open-study-to-preview-visible time).
The disk path through `ServeAsset` is robust, cache-friendly (OS page cache), and
doesn't require managing memory lifetimes for pixel buffers.

If the main latency is PNG encode+decode rather than disk I/O, a better optimization
would be serving raw pixel data (base64 or data URL) and decoding in a Canvas — but
that changes the entire preview pipeline and is out of scope.

**Files created**: `backend/internal/cache/preview_registry.go`  
**Files modified**: `desktop/app.go`, `backend/internal/jobs/service.go`

**Estimated effort**: 2 days  
**Risk level**: Low-medium  
**Success metric**: Preview visible time reduced by ≥ 20ms compared to Phase 3 baseline.
If improvement is < 10ms, revert and skip this phase.

---

## Phase 5 — Cleanup & Hardening

### 5.1 Remove Sidecar Dead Code from Normal Path

- In `desktop/app.go`, ensure `SidecarController` is only constructed when the sidecar
  fallback is active (env var set). Currently `NewDesktopApp` always creates it
  (line 36-38).
- Remove the `InvokeBackendCommand` method from the Wails binding surface (stop
  exporting it) once the typed methods are proven stable. The frontend `hasTypedBindings`
  check (`backend.ts`) can be removed and the legacy path deleted.

### 5.2 Cache Size Budgets (Bytes, Not Just Item Count)

The Phase 1.6 eviction uses item count caps. For production hardening:

- `DecodeCache` (Phase 1.2): Track `SourceImage.Pixels` byte size. Evict when total
  exceeds e.g. 512MB. A 2000×1500 gray-float32 image is ~12MB of pixel data.
- `sourcePreviews` in `cache.Memory` (Phase 1.3): Track `PreviewImage.Pixels` byte size.
  A 2000×1500 gray8 preview is ~3MB.
- `PreviewRegistry` (Phase 4): Track PNG byte size. Cap at e.g. 64MB.

Add a `byteSize()` method to `imaging.SourceImage` and `imaging.PreviewImage`:

```go
// In backend/internal/imaging/model.go:
func (image SourceImage) ByteSize() uint64 {
	return uint64(len(image.Pixels)) * 4 // float32 = 4 bytes
}

func (image PreviewImage) ByteSize() uint64 {
	return uint64(len(image.Pixels))
}
```

### 5.3 Artifact Eviction Policy

`cache.Store.ArtifactPath` (`cache/store.go:68-77`) creates directories and files but
never cleans them up. Add a background sweep or on-demand eviction:

```go
// In backend/internal/cache/store.go:

func (store *Store) EvictArtifactsOverLimit(maxTotalBytes int64) error {
	artifactDir := filepath.Join(store.rootDir, artifactDirName)
	// Walk, collect (path, modtime, size), sort by modtime ascending,
	// delete oldest until total < maxTotalBytes.
	// ...
}
```

Call periodically or after each job completion.

### 5.4 Benchmark Coverage

Extend the Phase 0 benchmark file with end-to-end benchmarks that exercise the full
job pipeline (decode → render → process → analyze) to catch regressions:

```go
func BenchmarkFullWorkflow(b *testing.B) {
	// Build a real Service with a test cache dir.
	// For each iteration: OpenStudy → StartRenderJob → wait → StartProcessJob → wait → StartAnalyzeJob → wait.
}
```

### 5.5 Memory Profile

After all phases, run a sustained session test:

```bash
# Open 10 different studies in sequence, run all three jobs on each.
# Record RSS at each step.
# Verify RSS plateaus (eviction working) rather than growing linearly.
```

Use `runtime.ReadMemStats` in a test or `go tool pprof -http=:6060 http://localhost:38181/debug/pprof/heap`.
[ASSUMPTION: The backend does not currently expose pprof endpoints. Add
`_ "net/http/pprof"` behind a build tag for profiling.]

### Phase 5 Summary

**Estimated effort**: 3-4 days  
**Risk level**: Low  
**Success metric**: Steady-state RSS after opening 10 studies stays within 2× the
single-study RSS. Artifact directory size is bounded.

---

## Appendix A — What NOT to Do (and Why)

### A.1 Do NOT replace HTTP with native IPC while keeping two processes

This is the worst of both worlds: you still pay the two-process overhead (dual heaps,
startup latency, process lifecycle management) but lose the debuggability of HTTP
(curl, browser devtools, access logs). The HTTP transport is NOT the bottleneck — it
adds < 2ms per request on loopback. If you're going to change the transport, go all the
way to in-process (Phase 3).

### A.2 Do NOT rewrite the contract system

`contracts/backend-contract-v1.schema.json` and the generated bindings work. The
contract system is load-bearing for type safety across the Go/TypeScript boundary.
Replacing it with protobuf, gRPC, or a different schema format would be high effort
for zero user-visible benefit. The schema is fine; the transport around it is what's
changing.

### A.3 Do NOT change the CLI/server entrypoints

`backend/cmd/xrayviewd/main.go` and `backend/cmd/xrayview-cli/main.go` are thin
wrappers over `app.NewFromEnvironment()` and the domain packages. They must continue
to work for headless/scripted workflows. All refactoring happens in `backend/internal/*`.

### A.4 Do NOT break mock mode

Mock mode (`frontend/src/lib/backend.ts:231-346`) is the primary development workflow
for frontend work. It has zero backend dependency by design. Every frontend change must
be tested in mock mode (`npm run dev`) before testing in desktop mode.

### A.5 Do NOT convert `contracts` types to Go interfaces

The contract types (`contracts.OpenStudyCommand`, etc.) are concrete structs that
serialize to JSON. They are used on both sides of the Wails binding. Converting them
to interfaces would break JSON serialization and add unnecessary indirection.

---

## Appendix B — Decision Log

### Phase 1 Decisions

| Decision | Considered Alternative | Why Chosen | Confidence |
|---|---|---|---|
| LRU decode cache keyed by file path | Keyed by file hash (SHA256 of file content) | File path is cheaper to compute; same path always means same file in this desktop-local context. If the user modifies the file on disk and re-opens, they'd get a stale cache — but the study ID changes on re-register, so a new render job with a new fingerprint is issued anyway. | High |
| Channel-based semaphore for concurrency | `golang.org/x/sync/semaphore` | Avoids adding a new dependency for a trivial use case. A buffered channel with `maxConcurrentJobs` capacity is idiomatic Go. | High |
| Adaptive polling with setTimeout (not setInterval) | WebSocket | setTimeout is simpler, has zero backend changes, and Phase 2 replaces it with push events anyway. | High |
| Bump processFingerprint namespace to v3 | Provide a cache migration | Old cache entries are simply not found and re-computed. No data loss — just one extra computation per unique parameter set. | High |
| Random eviction for `cache.Memory` | Full LRU with doubly-linked list | The map is small (≤32 entries). LRU bookkeeping overhead isn't justified. Phase 5 can upgrade if profiling shows thrash. | Medium |

### Phase 2 Decisions

| Decision | Considered Alternative | Why Chosen | Confidence |
|---|---|---|---|
| Callback-based job completion (not channels) | `chan contracts.JobSnapshot` | Callbacks are simpler for the single consumer (Wails event emission). Channels would need a goroutine to drain. The callback is called from within the job goroutine, which is already bounded (Phase 1.5). | High |
| `BackendService` as an interface (not a concrete struct method set) | Export methods directly on `*App` without an interface | An interface lets the HTTP router and Wails layer depend on the same contract without importing the full `app` package. Also enables testing with mocks. | High |
| Keep polling as fallback alongside events | Events only | Sidecar mode and mock mode cannot emit Wails events. Polling fallback ensures these modes work without code duplication. | High |

### Phase 3 Decisions

| Decision | Considered Alternative | Why Chosen | Confidence |
|---|---|---|---|
| `replace` directive in `desktop/go.mod` | Go workspace (`go.work`) | The project explicitly avoids a Go workspace (per CLAUDE.md: "There is no Go workspace file; modules use `replace` directives for local deps"). Stay consistent. | High |
| Typed Wails bindings per command | Single generic method with Go generics | Wails v2 binding generation requires exported methods with concrete types. Generics won't generate TypeScript stubs. One method per command aligns with Wails conventions. | High |
| Keep `InvokeBackendCommand` during transition | Remove immediately | The sidecar fallback needs it. Frontend detects typed bindings at runtime (`typeof bindings.OpenStudy === "function"`) and falls back. Zero downtime migration. | High |
| `hasExplicitBackendURL` as sidecar trigger | A dedicated `XRAYVIEW_FORCE_SIDECAR` env var | Uses the existing env var semantic. If you're setting `XRAYVIEW_BACKEND_URL`, you're telling the shell to connect to an external backend — which is exactly the sidecar use case. No new env vars needed. | Medium |

### Phase 4 Decisions

| Decision | Considered Alternative | Why Chosen | Confidence |
|---|---|---|---|
| Conditional implementation (measure first) | Always implement | Disk I/O for a 300KB PNG on a local SSD is < 1ms. The optimization may not be perceptible. Measure after Phase 3, implement only if warranted. | Medium |

### Phase 5 Decisions

| Decision | Considered Alternative | Why Chosen | Confidence |
|---|---|---|---|
| Byte-based cache budgets | Time-based eviction | Byte budgets directly control memory usage. Time-based eviction would still allow unbounded memory if many studies are opened rapidly. | High |
| Background artifact sweep | Reference counting | Reference counting is complex and error-prone (leaked refs = leaked files). A periodic sweep based on file modtime is simple and self-healing. | Medium |

---

## Timeline Summary

| Phase | Effort | Risk | Depends On | Key Metric |
|---|---|---|---|---|
| 0. Benchmarks | 1 day | None | — | Numbers recorded |
| 1. Caching & Responsiveness | 4-5 days | Low | Phase 0 | 1 decode, 1 render per workflow; < 300ms poll lag |
| 2. Push-Style Job Delivery | 3-4 days | Medium | Phase 1 | < 50ms job-to-UI in-process |
| 3. In-Process Integration | 5-7 days | Medium-High | Phase 2 | 1 OS process; 40-60% RSS reduction |
| 4. Preview Optimization | 2 days | Low-Medium | Phase 3 | Conditional — measure first |
| 5. Cleanup & Hardening | 3-4 days | Low | Phase 3 | Bounded RSS over long session |
| **Total** | **18-23 days** | | | |

Each phase is independently shippable and testable. Phase 1 alone delivers the highest
user-impact improvements. If time is constrained, Phase 1 + Phase 2 provide 80% of the
benefit without the module restructuring risk of Phase 3.
