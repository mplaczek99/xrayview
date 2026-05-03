# XrayView Cleanup Plan

## Goal

Make the code easier to read, navigate, and reason about. The win we want is *clarity*, not *brevity*. A 40-line function that someone can grok in 30 seconds beats a 12-line function that requires three jumps to understand.

## Non-goals

- **Premature abstraction.** We will only collapse duplication where the shape genuinely repeats — three or more sites with the same skeleton. Two slightly-similar functions stay separate.
- **Performance refactors.** Hot paths (the unsafe-pointer LUT loop in `processing/grayscale.go`, the `runWorker` priority drain) stay as-is unless a clarity change is independently safe.
- **Architecture changes.** No new packages, no module restructuring, no swapping the runtime adapter pattern. The contract layer, the registry/service split, and the mock/desktop bifurcation all stand.
- **Rewriting comments that explain "why".** Several files have load-bearing comments (e.g., `finishCancelledIfRequested`, `cloneJobSnapshot`, the `Cancel` state machine). Those stay verbatim.

## Principles

1. **Separate data from code.** When a file is 95% checked-in literals, the literals belong in an embedded asset, not in `.go`.
2. **Extract when the *shape* repeats, not when names rhyme.** Three functions with identical control flow and three differing types are a generic helper waiting to happen. Three functions that happen to start with `start` but diverge after line 5 are not.
3. **Co-locate by concern, not by file size.** Splitting a 600-line file into three 200-line files only helps if each piece is independently understandable. If callers always need two of the three, leave them together.
4. **Delete dead code before refactoring.** Refactoring code nobody calls is wasted effort and adds risk.
5. **One concern per file.** A store file should not also define memoization helpers. A backend adapter file should not also implement a fake.

---

## Phase 0 — Demolition (low risk, high signal-to-noise)

These are pure deletions. Do them first so the later phases operate on less surface area.

### 0.1 Remove dead code in `backend/internal/analysis/`

Confirmed unused (no callers anywhere in the tree):

- `teeth.go:149` — `otsuThreshold`
- `teeth.go:494` — `combineMasks`
- `teeth.go:512` — `subtractUint8`
- `teeth.go:533` — `maxUint8`
- `teeth.go:327` — `keepToothComponent` (note: this is a substantial helper; double-check via `grep -r keepToothComponent` before deleting)

These are leftovers from prior tooth-analysis algorithm iterations (cf. recent commits `5672694 Remove legacy tooth analysis pipeline`, `9870ed2 Refine tooth overlay cleanup`). Removing them takes ~80 lines off `teeth.go` for free.

### 0.2 Remove the empty `backend/cmd/write-overlay-debug/` directory

Empty since it appeared on `May 2 14:13`. Either restore the missing `main.go` or delete the directory. Almost certainly the latter — it's not referenced by any build script.

### 0.3 Audit cross-package utility duplication

Spot two definitions:

- `analysis/teeth.go:540` defines `absInt`
- `analysis/learned_model.go:2746` defines `absFloat64`

These (and `minInt`, `maxInt`, `clampInt` in `teeth.go`) are package-private and only used inside `analysis/`. They should stay package-private; **do not** create a cross-package `internal/mathutil`. The point is: confirm none leak into other packages before phase 2 runs.

---

## Phase 1 — Split data from code (single biggest clarity win)

### 1.1 Extract the trained model out of `backend/internal/analysis/learned_model.go`

**Diagnosis.** The file is 2,751 lines. Lines 1–2,680 are a hand-coded `[][]learnedNode` literal — checked-in trained weights for a gradient-boosted segmenter. The actual *code* (feature extraction, tree evaluation, gradient utilities) lives in the last ~100 lines. Anyone opening this file has to scroll past 2,600 lines of opaque numbers to find the logic.

**Proposed shape.**

- Serialize the tree literals into a compact binary (gob, or simple `binary.Write` of `[len][nodes...]` per tree) and check it in at `backend/internal/analysis/learned_model.bin`.
- Embed it via `//go:embed learned_model.bin` and decode at package init (or first call).
- The remaining `learned_model.go` becomes ~100 lines: feature extraction, tree evaluation, the embed declaration, and `init()`.
- Consider also moving `feature_table_model.go`'s data the same way if it follows a similar pattern.

**Why this matters.** It's the single largest "open this file and understand what's here" improvement available. The `analysis/` package shrinks from feeling like a model-weights dump to feeling like a small image-segmentation library.

**Care.** The serialization choice is load-bearing — pick something deterministic so the binary is reviewable in `git log -p` (size only, not contents). Keep a one-line comment in the `.go` file pointing at the script that regenerated `learned_model.bin`, plus check that script in.

---

## Phase 2 — Collapse triplicated job code in `backend/internal/jobs/service.go`

**Diagnosis.** `service.go` is 1,364 lines because it contains three parametric copies of the same job lifecycle. The triples:

| Concept                    | render                          | analyze                          | process                         | Lines each |
| -------------------------- | ------------------------------- | -------------------------------- | ------------------------------- | ---------- |
| `Start*Job(command)`       | `StartRenderJob` @ 155          | `StartAnalyzeJob` @ 206          | `StartProcessJob` @ 262         | ~50        |
| `execute*Job(...)`         | `executeRenderJob` @ 565        | `executeAnalyzeJob` @ 675        | `executeProcessJob` @ 788       | ~110       |
| `cached*Snapshot(fp, sid)` | `cachedRenderSnapshot` @ 485    | `cachedAnalyzeSnapshot` @ 533    | `cachedProcessSnapshot` @ 509   | ~25        |
| `complete*Job(...)`        | `completeRenderJob` @ 983       | `completeAnalyzeJob` @ 1005      | `completeProcessJob` @ 1030     | ~22        |
| `*Fingerprint(...)`        | `renderFingerprint` @ 1191      | `analyzeFingerprint` @ 1207      | `processFingerprint` @ 1225     | ~16        |

That's roughly **700 lines** that are structurally identical with different command/result types and different stage strings.

**Proposed shape.**

The shared structure is:

```
Start: validate inputs → fingerprint → cache check → registry.StartJob → attach cancel → launch
Execute: walk a list of (percent, stage, message, work) tuples, polling cancellation between each
Cached snapshot: memoryCache.LoadX → registry.CreateCachedJob
Complete: registry.Complete → check cancelled → store in memoryCache → notify → evict
```

This wants to become a `jobLifecycle[Cmd, Result]` (Go generics) with the kind-specific bits supplied as a struct of small functions. Sketch:

```go
type jobSpec[Cmd any, Result any] struct {
    kind         contracts.JobKind
    fingerprint  func(study contracts.StudyRecord, cmd Cmd) (string, error)
    cacheLoad    func(fp string) (Result, bool)
    cacheStore   func(fp string, r Result)
    stages       []jobStage[Result]   // each: percent, stage, message, work fn
}
```

Each call site (`StartRenderJob` etc.) becomes 5–10 lines that constructs the spec and calls a single helper. Each `executeXJob` becomes the stage list, not the stage *machine*.

**Why this matters.**

- Adding a fourth job kind today means writing ~225 lines and remembering eight places to edit. After this, it means writing one spec.
- The cancel/cleanup choreography in `executeProcessJob` (the only one that writes a second output file) is genuinely different from the other two; the spec form makes that difference *explicit* instead of hidden inside otherwise-identical 100-line copies.
- The "stage names are part of the frontend contract" warning currently sits on top of `executeRenderJob` only. After consolidation, the stages are a list of typed values that are obviously the contract surface.

**Care.**

- Don't try to unify the three Cancel paths in `Cancel()` itself (`registry.go:264`) — those branches really *are* different (queued cancels finalize immediately, running cancels go through the latch). Leave that switch alone.
- `executeProcessJob` cleans up an extra DICOM file on cancel; the spec needs an optional secondary-cleanup hook.
- `analyzeFingerprint` includes `analysis.AnalyzeAlgorithmVersion`; `renderFingerprint` and `processFingerprint` use a literal namespace string. Don't accidentally collapse these — the version embedding is intentional.

---

## Phase 3 — Collapse the HTTP handler boilerplate in `backend/internal/httpapi/router.go`

**Diagnosis.** Eight `handleX` functions (lines 326–452) are exactly the same shape:

```go
var command contracts.XCommand
if err := decodeJSONRequest(request, &command); err != nil { writeBackendError(writer, err); return }
result, err := deps.Service.X(command)
if err != nil { writeBackendError(writer, err); return }
writeJSON(writer, http.StatusOK, result)
```

The comment at line 322 even acknowledges this: *"Every handleXxx below follows the same shape... If you're adding a new command, copy this and swap the types."* "Copy and swap the types" is a generic.

**Proposed shape.** Single helper:

```go
func handleCommand[Cmd any, Result any](
    w http.ResponseWriter, r *http.Request,
    fn func(Cmd) (Result, error),
)
```

The dispatch in `mux.HandleFunc(CommandsPath+"/", ...)` then calls `handleCommand(w, r, deps.Service.OpenStudy)` etc. Each command costs one line in the dispatch switch instead of eight lines + a 14-line helper.

**Care.**

- `CommandGetProcessingManifest` doesn't take a body — keep its inline path or add a no-arg variant.
- The empty-body / trailing-content check inside `decodeJSONRequest` stays as-is; it's the part doing real work.

---

## Phase 4 — Split `frontend/src/app/store/workbenchStore.ts` (851 lines)

**Diagnosis.** One file currently holds:

1. The `WorkbenchStore` class (~270 lines of methods)
2. Three near-identical `applyXJob` reducers — render/analyze/process (~165 lines, 5-state switch each)
3. `createSelector` and `createSelector2` memoization helpers
4. All exported selectors
5. The `useWorkbenchStore` React hook

Items 2–5 don't depend on the class internals; they depend only on `WorkbenchState`.

**Proposed shape.**

- `app/store/workbenchStore.ts` keeps the class + the hook + the (subscribe / getState / setState) plumbing.
- `app/store/applyJob.ts` exports `applyJobToStudy` and the three per-kind reducers. The three reducers stay separate — they really do diverge meaningfully (analyze produces a different status string per `mode`, process has a `runStatus` machine, render is the simplest). Keeping them three readable functions is better than one 80-line conditional.
- `app/store/selectors.ts` exports `createSelector` (generalized below), and the `selectX` exports.
- Replace `createSelector` + `createSelector2` with a single n-ary `createSelector(inputSelectors[], resultFn)` that uses `Object.is` per input. Two functions become one without losing the inline shapes — the call sites change very little.

**Why this matters.** When someone needs to change how analyze results land in the store, they open `applyJob.ts` and find ~45 lines in front of them, not a 12-line slice in the middle of an 850-line file. The store class itself becomes something you can read top to bottom.

**Care.**

- `setProcessingControls`'s rAF debounce is genuinely tricky and has comments worth preserving — leave it inside the class.
- `jobSnapshotEqual` is the no-op-poll guard with load-bearing comments — leave it next to `receiveJobUpdate`.

---

## Phase 5 — Split `frontend/src/features/jobs/progressTiming.ts` (570 lines)

**Diagnosis.** Four concerns share one file:

1. **State advancement** — `advanceJobProgressTiming` (timing samples, smoothing, percent regress detection)
2. **Rate estimation** — `estimateRate`, `estimateConfidence`, `calculateOverallRate`, `calculateRecentRateWeight`, `blendRates`, `smoothRate`, `rateAgreement`
3. **Presentation** — `describeProgress`, `formatActiveEtaLabel`, `formatEtaLabel`, `bucketRemainingMs`, `formatDuration`, `resolveDisplayMode`
4. **React hook** — `useProgressClock`

The file is internally consistent (the constants at top govern sections 1 and 2 together), but a reader looking up "how is the ETA bucket computed" wades through a state machine and a hook to get there.

**Proposed shape.**

- `progressTiming.ts` keeps `advanceJobProgressTiming` + `isTerminalJobState` / `isPendingJobState` (the predicates are used elsewhere).
- `progressEstimator.ts` holds `estimateRate` and friends. Internal-only by convention; export only what `describeProgress` needs.
- `progressFormatting.ts` holds `describeProgress` and the formatting helpers.
- `useProgressClock.ts` holds the hook.

Each new file ends up ~120–200 lines and answers exactly one question.

**Care.**

- The constants block at top (`FAST_TASK_MS`, `RATE_EMA_ALPHA`, etc.) needs to be split between estimator and formatter files. Don't centralize them into a shared constants module — that just adds an indirection. Each constant lives next to the only file that uses it.
- Tests likely import `describeProgress` directly. Verify the import sites compile after the split.

---

## Phase 6 — Smaller targeted cleanups

These are small but worth doing once Phase 0–5 land.

### 6.1 `backend/internal/httpapi/router.go` — duplicated `BackendService` interface

Both `httpapi.BackendService` and `app.BackendService` exist with a comment saying "keep these in sync." The `app.BackendService` is the wider one (adds `OnJobUpdate`, `SupportedJobKinds`, `StudyCount`). The narrower `httpapi.BackendService` exists to avoid `httpapi → app` import.

Two reasonable shapes:

- **(a)** Move `BackendService` (the narrow one) to `internal/contracts` and have both packages import it from there. Drop the duplicate. Optional methods (`OnJobUpdate` etc.) stay as the existing optional-interface pattern (`jobUpdateSubscriber`, `studyCountProvider`). This is the cleanest.
- **(b)** Leave it but delete the "keep these in sync" comment because the optional-interface pattern means they don't actually need to be in sync — they just need to be subsets. The comment is misleading.

Pick (a) if it doesn't introduce an import cycle; (b) if it does.

### 6.2 `desktop/app.go` — eight near-identical command forwarders

Lines 172–267 contain eight methods that look like:

```go
func (app *DesktopApp) StartRenderJob(command backendapi.RenderStudyCommand) (backendapi.StartedJob, error) {
    if app.backend != nil {
        return app.backend.StartRenderJob(command)
    }
    return invokeViaHTTP[backendapi.StartedJob](app, "start_render_job", command)
}
```

Each one is small, but together they're ~95 lines of "if embedded then call the method else call HTTP." Wails *requires* concrete methods on the bound app — you can't replace them with a generic dispatcher visible to the binding. So the methods stay. But each method body can collapse to one line via a small generic:

```go
func dispatch[Cmd any, Result any](
    app *DesktopApp, embedded func(Cmd) (Result, error),
    httpCommand string, command Cmd,
) (Result, error)
```

Each method becomes `return dispatch(app, app.backend.StartRenderJob, "start_render_job", command)` (with a nil-check inside `dispatch`). Trims maybe 60 lines.

### 6.3 `frontend/src/lib/backend.ts` — split mock from desktop

The mock implementation (mock job sequence, `scheduleMockCompletion`, `updateMockJob`) takes ~210 lines; the desktop adapter takes ~30 lines. They share nothing meaningful. Move the mock half to `lib/mockBackend.ts` (pairs with the existing `mockProcessingManifest.ts`, `mockRuntime.ts`, `mockStudy.ts`) and leave `backend.ts` as the desktop adapter only — or rename `backend.ts` to `desktopBackend.ts` if it'd remove ambiguity.

### 6.4 `frontend/src/features/viewer/ViewerCanvas.tsx` — extract interaction hooks

Currently a 472-line component holding (a) frame-size observer, (b) wheel-zoom handler, (c) pointer state machine for pan / draw / edit, (d) HUD, (e) image element, (f) annotation overlay. The state-machine in (c) is the hardest part to read because it's interleaved with the React effects.

Extract three custom hooks:

- `useViewportFrame(ref)` → resize-observed `{ width, height }`
- `useWheelZoom(ref, ...)` → manages the zoom callback registration
- `usePointerInteractions({...})` → owns `interaction`, `draftLine`, returns `{ onPointerDown, ... }`

The component then becomes ~150 lines of layout. Don't extract the HUD chips into their own component — they're trivial JSX and inlining keeps the canvas readable.

### 6.5 `backend/internal/analysis/teeth.go` — group by concern within the file

Even after Phase 0 deletions, ~470 lines remain. They're already mostly coherent, but the order is confusing:

- Top: public API (`GenerateToothOverlay`)
- Middle: pixel utilities (gray, normalize, percentile, blur)
- Then: morphology (erode, dilate, open, close, fill-holes, components)
- Bottom: tiny math helpers

Reorder so the file reads top-down: public API → pipeline (`detectToothMask` and the helpers it directly calls) → morphology → math helpers. No code changes; just move blocks around. This is safe and cheap.

---

## Order of operations

1. **Phase 0** (demolition) — independent, lands in one PR.
2. **Phase 6.5** (file reorganizations) — also independent.
3. **Phase 1** (model extraction) — needs a regeneration script + binary asset; review carefully.
4. **Phase 4** + **Phase 5** (frontend splits) — independent of each other and of the backend.
5. **Phase 2** (jobs service) — biggest backend change; do alone in a single PR; rely on existing tests.
6. **Phase 3** + **Phase 6.1, 6.2** (router/handler/desktop) — small generics PRs; can be one PR or three.
7. **Phase 6.3, 6.4** (frontend file splits + viewer hooks) — independent.

Each phase should leave the test suite green. The job-service collapse (Phase 2) is the only one large enough to consider gating behind extra integration testing — `service_test.go` is 1,535 lines, so the safety net is already substantial.

## What we are *not* doing

- Not introducing a generic `Result[T]` / functional plumbing layer in Go; that fights the language.
- Not splitting `backend/internal/dicommeta/decode.go` (990 lines). The DICOM decoding state machine is a single concern, and chopping it across files makes following a tag through the parse harder, not easier. The functions are well named and the file has navigation-friendly structure (`grep -n "^func "` gets you anywhere).
- Not unifying `app.BackendService` and `embeddedService` in `backend/service.go`. The thin façade is intentional — it's the public surface that desktop's separate Go module imports. Touching it ripples into Wails bindings.
- Not consolidating the `jobs.registry`'s state-machine into a generic. The Cancel/Complete/Fail/MarkCancelled methods *look* similar but each enforces a different transition guard with subtle ordering. The current shape is the cleanest representation of the actual rules.
- Not touching the SSE / events / polling fallback in `frontend/src/features/jobs/useJobs.ts`. It's tricky on purpose; the comments earn their keep.

## Rough size impact

| Area                                     | Before  | After (est.) |
| ---------------------------------------- | ------- | ------------ |
| `analysis/learned_model.go`              | 2,751   | ~120         |
| `analysis/teeth.go`                      | 555     | ~470 (Phase 0) |
| `jobs/service.go`                        | 1,364   | ~700         |
| `httpapi/router.go`                      | 511     | ~370         |
| `desktop/app.go`                         | 413     | ~360         |
| `app/store/workbenchStore.ts`            | 851     | ~430 + 2 new files |
| `features/jobs/progressTiming.ts`        | 570     | ~180 + 3 new files |
| `features/viewer/ViewerCanvas.tsx`       | 472     | ~280 + 3 hooks |

Total source LOC drops, but that's a side effect. The real measure is: open any file from the right column and you can hold its purpose in your head. Today, several files in the left column you cannot.
