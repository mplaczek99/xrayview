# Granular Go Migration Plan for `xrayview`

## Summary

This repository is currently a React desktop app with a thin Tauri shell and a thick Rust backend. The Rust side owns the actual product: DICOM loading, rendering, processing, analysis, export, caching, persistence, contract generation, and CLI behavior. The correct Go migration is not a single rewrite phase. It is a controlled transfer of ownership from Rust to Go, with the shell held stable first, the contracts made transport-neutral early, and the risky DICOM/export pieces isolated until proven in Go.

The recommended path is:

- keep the current React frontend
- keep Tauri temporarily
- introduce a Go backend process as the new application boundary
- migrate backend responsibilities in many narrow phases
- move to Wails only after the Go backend is real and stable
- delete Rust only after Go owns decode, processing, analysis, export, jobs, and packaging

## A. Current-State Assessment

### What the repo is today

Current structure:

- `frontend/src/`: React UI, store, viewer, processing controls, job UI
- `frontend/src-tauri/`: thin Tauri shell with file dialogs and Rust command bridge
- `backend/`: Rust application core and imaging engine
- `backend/src/bin/xrayview-backend.rs`: headless CLI
- `backend/src/bin/generate-contracts.rs`: Rust-driven TypeScript contract generation

Current runtime behavior:

- frontend opens a study through Tauri
- Rust backend registers the study, decodes DICOM metadata, returns a study ID
- frontend starts render/process/analyze jobs using the study ID
- Rust backend runs jobs in threads, stores artifacts under temp cache, returns job snapshots
- frontend polls jobs and loads preview artifacts through Tauri asset URL translation

Key references:

- [README.md](README.md)
- [frontend/src-tauri/src/main.rs](frontend/src-tauri/src/main.rs)
- [backend/src/app/state.rs](backend/src/app/state.rs)
- [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts)

### What is tightly coupled

Tightly coupled today:

- Tauri shell directly links Rust backend crate
- TypeScript contract generation is Rust-authored
- frontend backend adapter assumes Tauri command names and asset-path behavior
- job lifecycle semantics are shared implicitly across Rust + frontend
- artifact paths assume Tauri asset protocol scope

Relevant files:

- [frontend/src-tauri/Cargo.toml](frontend/src-tauri/Cargo.toml)
- [backend/src/api/contracts.rs](backend/src/api/contracts.rs)
- [frontend/src/lib/generated/contracts.ts](frontend/src/lib/generated/contracts.ts)
- [frontend/src-tauri/tauri.conf.json](frontend/src-tauri/tauri.conf.json)

### What is portable

Portable with relatively low conceptual rewrite risk:

- contract shapes
- preset semantics
- job state model
- study registry semantics
- recent-study persistence semantics
- line measurement logic
- annotation DTOs
- palette and compare behavior
- frontend UI
- frontend store shape
- mock runtime pattern

### What will be painful to migrate

Most painful:

- DICOM decode breadth and edge cases
- DICOM Secondary Capture export
- bespoke tooth-analysis implementation
- preview artifact path handling during shell transition
- replacing Rust as contract authority cleanly
- temporary packaging if Tauri + Go sidecar coexist

### What parts are most likely to break

Most likely failures during migration:

- compressed DICOM decoding
- window/level parity
- measurement scale extraction from tags
- processed DICOM output validity
- job cancellation and dedupe behavior
- preview path loading in the UI
- release packaging

## B. Go Feasibility Analysis

### What Go is a great fit for

Go is strong for:

- backend orchestration
- local service boundaries
- job runners
- persistence
- cache management
- CLI tooling
- transport and contracts
- application flow
- readable long-term maintenance by one developer

### What Go is only an acceptable fit for

Go is acceptable for:

- grayscale transforms
- palette application
- compare image generation
- PNG preview writing
- geometry and measurement logic
- tooth-analysis port if you are willing to spend the effort

### What Go is a poor fit for in this codebase

Go is comparatively weak for:

- mature DICOM decoding ecosystem
- low-level imaging edge cases
- native-image-performance ergonomics compared with Rust
- embedded in-process desktop backend if you want to avoid any process boundary before moving shells

### Where Go improves developer experience

Go should improve:

- day-to-day readability
- refactor confidence in application logic
- velocity for new backend features
- job/persistence/API work
- willingness to revisit architecture

### Where Go may regress technical quality versus Rust

Potential regressions:

- image/decode performance
- correctness on obscure DICOM variants
- confidence in output fidelity until parity suite exists
- memory efficiency on large studies

### Direct conclusion

A Go backend is feasible for this repository. It is not the best raw technical fit for every subsystem, but it is a viable fit overall if you architect around the weak spots deliberately and keep the migration incremental.

## C. Shell Decision Analysis

### Whether to keep the current shell temporarily

Yes.

Reason:

- the shell is thin
- the frontend is already centralized behind a runtime adapter
- replacing shell and backend together creates unnecessary parallel risk

### Whether to move to Wails

Yes, later.

Wails is the best long-term shell if your future state is Go-first desktop development. It reduces the permanent mismatch of "JS frontend + Tauri shell + separate Go runtime" and lets Go become the true application host.

### Whether to keep the frontend and swap only the backend

Yes, initially.

That is the right first move.

### Whether to split the system into engine/service/shell layers

Temporarily yes, but keep it local and simple.

Recommended transition layering:

- React frontend
- Tauri shell
- local Go backend process
- optional narrow Rust compatibility helper for DICOM decode/export only if needed

Recommended final layering:

- React frontend
- Wails shell
- Go application core
- no service split unless you later choose to support remote/web workflows

### Final recommendation

- keep Tauri during migration
- move backend ownership to Go first
- move to Wails only after the Go backend reaches parity
- do not replace shell first

## D. Migration Strategies Compared

### 1. Big-bang rewrite to Go

- Advantages: clean final result if it somehow works
- Disadvantages: highest risk, highest downtime, weakest parity confidence
- Risk level: very high
- Estimated complexity: very high
- Solo developer fit: poor
- Recommendation: no

### 2. Incremental strangler migration

- Advantages: capability-by-capability replacement, easier validation, safer cutover
- Disadvantages: temporary coexistence complexity
- Risk level: medium
- Estimated complexity: medium-high
- Solo developer fit: strong
- Recommendation: yes

### 3. Go sidecar first, shell later

- Advantages: best concrete version of the strangler approach, stable shell during backend port, future Wails migration becomes easier
- Disadvantages: temporary process/packaging complexity
- Risk level: medium
- Estimated complexity: medium-high
- Solo developer fit: very strong
- Recommendation: yes

### 4. Shell replacement first, backend later

- Advantages: aligns shell with Go quickly
- Disadvantages: solves the wrong problem first, doubles migration fronts immediately
- Risk level: high
- Estimated complexity: high
- Solo developer fit: weak
- Recommendation: no

## E. Final Recommendation

### Recommended migration strategy

Incremental strangler migration with Go sidecar first and shell later.

### Recommended target architecture

Transitional:

- React frontend stays
- Tauri stays temporarily
- Go backend process becomes primary application boundary
- Rust remains only as a temporary compatibility layer where Go lacks DICOM/export maturity

Final:

- React frontend stays
- Wails replaces Tauri
- Go owns backend, contracts, CLI, processing, jobs, export, analysis
- Rust removed unless a tiny compatibility helper remains justified by hard evidence

### Recommended shell choice

- temporary: Tauri
- final: Wails

### Biggest risks

- DICOM decode maturity in Go
- DICOM export fidelity
- tooth-analysis parity
- packaging complexity during sidecar phase

### Biggest unknowns

- exact Go DICOM library coverage on your study set
- whether pure Go export is sufficient
- whether Wails packaging is preferable on your target platforms once tested

### What to prototype first

Prototype a Go backend vertical slice for:

- `openStudy`
- `startRenderStudy`
- `getJob`
- preview artifact writing

Do not start with shell replacement.
Do not start with tooth analysis.
Do not start with full export rewrite.

## F. Detailed Phase-by-Phase Plan

## Phase 0: Repository Behavior Inventory

### Objective

Document what the Rust backend actually does today.

### Status

Completed. See [GO_BACKEND_PHASE0_REPOSITORY_BEHAVIOR_INVENTORY.md](GO_BACKEND_PHASE0_REPOSITORY_BEHAVIOR_INVENTORY.md).

### Why this phase exists

You need a migration target grounded in actual behavior, not inferred behavior.

### Exact code areas likely involved

- [backend/src/app/mod.rs](backend/src/app/mod.rs)
- [backend/src/app/state.rs](backend/src/app/state.rs)
- [backend/src/study/source_image.rs](backend/src/study/source_image.rs)
- [backend/src/save.rs](backend/src/save.rs)

### What should be implemented

- backend capability list
- entrypoint list
- job lifecycle list
- artifact path list
- current shell/backend/frontend data flow notes

### What should be avoided

- no rewriting
- no transport redesign yet

### Risks

- missing hidden behavior

### Validation/testing strategy

- compare docs to actual code
- list every frontend backend call path

### Exit criteria

- complete written inventory exists

### Reversible

Yes.

## Phase 1: Parity Fixture Collection

### Objective

Create a baseline input/output set for migration validation.

### Status

Completed. See [GO_BACKEND_PHASE1_PARITY_FIXTURE_SUITE.md](GO_BACKEND_PHASE1_PARITY_FIXTURE_SUITE.md).

### Why this phase exists

Without fixtures, parity claims are weak.

### Exact code areas likely involved

- sample DICOM under `images/`
- CLI paths in [backend/src/bin/xrayview-backend.rs](backend/src/bin/xrayview-backend.rs)
- tests in `backend/tests/` and module tests

### What should be implemented

Collect fixtures for:

- study metadata extraction
- preview render output
- processed preview output
- compare output
- analysis output
- exported DICOM output
- recent-study persistence
- cache behavior

### What should be avoided

- no new feature work
- no refactor combined with fixture collection

### Risks

- fixture set too narrow

### Validation/testing strategy

- keep a sample matrix
- store golden JSON and images where appropriate

### Exit criteria

- a parity suite definition exists for core workflows

### Reversible

Yes.

## Phase 2: Backend Contract Freeze

### Objective

Freeze the current backend API semantics before migration.

### Status

Completed. See [GO_BACKEND_PHASE2_BACKEND_CONTRACT_FREEZE.md](GO_BACKEND_PHASE2_BACKEND_CONTRACT_FREEZE.md).

### Why this phase exists

You want the backend to change language before it changes behavior.

### Exact code areas likely involved

- [backend/src/api/contracts.rs](backend/src/api/contracts.rs)
- [frontend/src/lib/generated/contracts.ts](frontend/src/lib/generated/contracts.ts)

### What should be implemented

Freeze current DTO semantics for:

- manifest
- study open
- render/process/analyze jobs
- job snapshots
- error payloads
- measurement payloads
- analysis payloads

### What should be avoided

- unnecessary renaming
- format churn

### Risks

- locking in bad shapes
- accidental contract drift later

### Validation/testing strategy

- compare generated TS types to frozen contract spec

### Exit criteria

- contract version 1 is declared and stable

### Reversible

Partially.

## Phase 3: Replace Rust as Contract Source of Truth

### Objective

Move contract ownership away from Rust.

### Status

Completed. See [GO_BACKEND_PHASE3_LANGUAGE_NEUTRAL_CONTRACTS.md](GO_BACKEND_PHASE3_LANGUAGE_NEUTRAL_CONTRACTS.md).

### Why this phase exists

Rust cannot remain the authority if Go is becoming primary backend language.

### Exact code areas likely involved

- [contracts/backend-contract-v1.schema.json](contracts/backend-contract-v1.schema.json)
- [backend/src/bin/generate-contracts.rs](backend/src/bin/generate-contracts.rs)
- [frontend/scripts/generate-contracts.mjs](frontend/scripts/generate-contracts.mjs)

### What should be implemented

Adopt one language-neutral contract source:

- OpenAPI or JSON Schema recommended

Use it to generate:

- TypeScript types
- Go DTOs or validation bindings

### What should be avoided

- handwritten duplicated types in two languages
- keeping Rust generator in the critical path

### Risks

- schema generation tooling friction

### Validation/testing strategy

- frontend compiles from new generated types
- current Rust payloads validate against schema

### Exit criteria

- Rust contract generator is no longer authoritative

### Reversible

Yes, but inconvenient.

## Phase 4: Introduce Frontend Runtime Abstraction Split

### Objective

Separate shell concerns from backend concerns in the frontend.

### Status

Completed. See [GO_BACKEND_PHASE4_FRONTEND_RUNTIME_ABSTRACTION_SPLIT.md](GO_BACKEND_PHASE4_FRONTEND_RUNTIME_ABSTRACTION_SPLIT.md).

### Why this phase exists

The frontend currently uses one adapter, but it still blends shell and backend details. That needs to split cleanly before migration.

### Exact code areas likely involved

- [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts)
- [frontend/src/lib/types.ts](frontend/src/lib/types.ts)

### What should be implemented

Split into clear interfaces:

- `ShellAPI`
- `BackendAPI`
- `RuntimeAdapter`

### What should be avoided

- UI calling transport-specific code directly
- direct Tauri assumptions outside shell adapter

### Risks

- preview URL regressions
- error normalization inconsistencies

### Validation/testing strategy

- mock mode still works
- legacy Tauri/Rust runtime still works

### Exit criteria

- UI talks only to runtime abstraction

### Reversible

Yes.

## Phase 5: Introduce Backend Runtime Selection

### Objective

Allow the app to run against multiple backend implementations during migration.

### Status

Completed. See [GO_BACKEND_PHASE5_BACKEND_RUNTIME_SELECTION.md](GO_BACKEND_PHASE5_BACKEND_RUNTIME_SELECTION.md).

### Why this phase exists

You need safe side-by-side migration.

### Exact code areas likely involved

- frontend runtime adapter
- Tauri startup config
- environment-based runtime flags

### What should be implemented

Supported runtimes:

- mock
- legacy Rust
- Go sidecar

### What should be avoided

- undocumented hidden toggles
- runtime selection sprinkled across the app

### Risks

- dev confusion
- test matrix expansion

### Validation/testing strategy

- smoke test each runtime mode

### Exit criteria

- runtime can be switched intentionally and safely

### Reversible

Yes.

## Phase 6: Stand Up Go Workspace and Backend Skeleton

### Objective

Create the Go backend project structure.

### Why this phase exists

Go needs to become a first-class part of the repo early.

### Exact code areas likely involved

New likely areas:

- `go-backend/go.mod`
- `go-backend/cmd/xrayviewd/`
- `go-backend/cmd/xrayview-cli/`
- `go-backend/internal/...`

### What should be implemented

Initial packages for:

- contracts
- http or local transport API
- config
- logging
- jobs
- studies
- cache
- persistence

### What should be avoided

- giant monolithic `main.go`
- premature plugin architecture

### Risks

- bad package boundaries early

### Validation/testing strategy

- backend starts
- health endpoint or equivalent works

### Exit criteria

- Go backend boots successfully

### Reversible

Yes.

## Phase 7: Define Local Backend Transport

### Objective

Choose the backend communication mechanism for migration.

### Status

Completed. See [GO_BACKEND_PHASE7_DEFINE_LOCAL_BACKEND_TRANSPORT.md](GO_BACKEND_PHASE7_DEFINE_LOCAL_BACKEND_TRANSPORT.md).

### Why this phase exists

The migration architecture depends on it.

### Exact code areas likely involved

- Go backend API layer
- frontend backend adapter
- Tauri shell startup logic

### What should be implemented

Recommended choice:

- localhost HTTP/JSON

Why:

- easy to debug
- transport-neutral
- future Wails migration can still collapse it later
- simpler than custom IPC for a solo dev

### What should be avoided

- overdesigned IPC
- remote-service assumptions
- browser-only constraints leaking into desktop design

### Risks

- local port lifecycle issues

### Validation/testing strategy

- request/response logging
- backend startup/shutdown handling
- one-client local-only enforcement if desired

### Exit criteria

- transport is chosen and documented

### Reversible

Yes.

## Phase 8: Add Tauri-Go Process Management

### Objective

Have the current shell manage the Go backend process lifecycle.

### Status

Completed. See [GO_BACKEND_PHASE8_ADD_TAURI_GO_PROCESS_MANAGEMENT.md](GO_BACKEND_PHASE8_ADD_TAURI_GO_PROCESS_MANAGEMENT.md).

### Why this phase exists

The user should still launch one desktop app, not manage two processes.

### Exact code areas likely involved

- [frontend/src-tauri/src/main.rs](frontend/src-tauri/src/main.rs)
- [frontend/src-tauri/tauri.conf.json](frontend/src-tauri/tauri.conf.json)
- frontend build scripts

### What should be implemented

- start Go backend on app startup
- stop backend on app exit
- pass config/env for cache/artifact roots
- expose shell-only functions unchanged

### What should be avoided

- frontend directly spawning backend
- opaque process lifecycle

### Risks

- race conditions during startup
- orphaned sidecars

### Validation/testing strategy

- launch/quit smoke tests
- stale-process detection

### Exit criteria

- Tauri can reliably manage Go backend

### Reversible

Yes.

## Phase 9: Implement Go Processing Manifest Endpoint

### Objective

Move the simplest stable backend response to Go first.

### Status

Completed. See [GO_BACKEND_PHASE9_IMPLEMENT_GO_PROCESSING_MANIFEST_ENDPOINT.md](GO_BACKEND_PHASE9_IMPLEMENT_GO_PROCESSING_MANIFEST_ENDPOINT.md).

### Why this phase exists

This is low risk and proves contracts + transport.

### Exact code areas likely involved

- manifest definitions in current Rust app module
- frontend load manifest path

### What should be implemented

- `getProcessingManifest` in Go
- current presets and defaults preserved exactly

### What should be avoided

- preset redesign

### Risks

- almost none

### Validation/testing strategy

- frontend manifest loads identically across runtimes

### Exit criteria

- frontend can read manifest from Go

### Reversible

Yes.

## Phase 10: Implement Go Study Registry and `openStudy`

### Objective

Move study registration and metadata response to Go.

### Status

Completed. See [GO_BACKEND_PHASE10_IMPLEMENT_GO_STUDY_REGISTRY_AND_OPEN_STUDY.md](GO_BACKEND_PHASE10_IMPLEMENT_GO_STUDY_REGISTRY_AND_OPEN_STUDY.md).

### Why this phase exists

This is the first real stateful backend capability.

### Exact code areas likely involved

- [backend/src/study/registry.rs](backend/src/study/registry.rs)
- [backend/src/study/model.rs](backend/src/study/model.rs)
- [backend/src/app/state.rs](backend/src/app/state.rs)

### What should be implemented

- study ID generation
- study registry
- open-study response model
- input path validation
- input name extraction
- recent-study recording hook

### What should be avoided

- coupling to full decode pipeline at first if metadata extraction can be isolated

### Risks

- measurement-scale dependency can force DICOM metadata work early

### Validation/testing strategy

- study IDs unique
- frontend receives same study shape

### Exit criteria

- `openStudy` can run via Go

### Reversible

Yes.

## Phase 11: Prototype Go DICOM Metadata Reader

### Objective

Prove Go can read the metadata needed for `openStudy`.

### Status

Completed. See [GO_BACKEND_PHASE11_PROTOTYPE_GO_DICOM_METADATA_READER.md](GO_BACKEND_PHASE11_PROTOTYPE_GO_DICOM_METADATA_READER.md).

### Why this phase exists

Metadata extraction is easier than full pixel decode and is the right first DICOM step.

### Exact code areas likely involved

- current metadata logic in [backend/src/study/source_image.rs](backend/src/study/source_image.rs)

### What should be implemented

Extract:

- rows
- columns
- pixel spacing tags
- imager pixel spacing
- nominal scanned pixel spacing
- window center/width
- photometric interpretation
- transfer syntax UID

### What should be avoided

- pretending metadata-only success means full decode is solved

### Risks

- Go DICOM library limitations appear here already

### Validation/testing strategy

- compare extracted metadata with Rust outputs

### Exit criteria

- metadata path is proven in Go or fallback helper is scoped

### Reversible

Yes.

## Phase 12: Decide DICOM Decode Strategy

### Objective

Make an explicit technical decision for pixel decode.

### Why this phase exists

This is the highest-risk subsystem.

### Exact code areas likely involved

- [backend/src/study/source_image.rs](backend/src/study/source_image.rs)

### What should be implemented

Evaluate two viable options:

- pure Go decode
- Go orchestration + narrow Rust decode helper

Choose based on real sample coverage, not preference.

### What should be avoided

- vague "we'll see later"
- broad Rust coexistence

### Risks

- weak library support
- underestimated complexity

### Validation/testing strategy

- decode sample studies end-to-end
- compare dimensions and pixel buffer sanity

### Exit criteria

- decode approach is locked

### Reversible

Only with real rework.

## Phase 13: Build Temporary Rust Decode Helper if Needed

### Objective

Preserve momentum if pure Go decode is not ready.

### Why this phase exists

You said you want a serious Go migration, not paralysis around one hard subsystem.

### Exact code areas likely involved

- a new narrow Rust helper binary
- Go process invocation layer

### What should be implemented

Helper outputs normalized data only:

- width
- height
- grayscale pixels or preview-ready bytes
- measurement scale
- preserved export metadata subset
- window defaults if needed

### What should be avoided

- reusing the whole Rust backend behind a thin wrapper
- hiding too much logic in the helper

### Risks

- helper becomes permanent by laziness

### Validation/testing strategy

- helper output consumed cleanly by Go
- helper scope documented

### Exit criteria

- Go backend can continue while decode remains temporarily delegated

### Reversible

Yes.

## Phase 14: Implement Go Preview Image Model

### Objective

Define the Go-native in-memory image representation.

### Why this phase exists

Every downstream subsystem depends on a stable image model.

### Exact code areas likely involved

- current Rust preview/image structs
- new Go render/preview packages

### What should be implemented

A stable Go struct for:

- width
- height
- pixel buffer
- image format
- min/max if needed
- default window
- invert flag

### What should be avoided

- leaking transport-specific encoding into core image model

### Risks

- wrong abstraction for later export

### Validation/testing strategy

- conversion tests between decoded image and preview output

### Exit criteria

- image model is stable enough for render pipeline work

### Reversible

Yes.

## Phase 15: Port Windowing Logic to Go

### Objective

Recreate Rust window mapping behavior in Go.

### Why this phase exists

Window behavior is foundational to render parity.

### Exact code areas likely involved

- [backend/src/render/windowing.rs](backend/src/render/windowing.rs)

### What should be implemented

- default window handling
- manual window support
- full-range mapping
- clamp behavior

### What should be avoided

- mixing this with processing controls yet

### Risks

- subtle visual mismatches

### Validation/testing strategy

- golden unit tests from Rust semantics

### Exit criteria

- grayscale mapping parity is acceptable

### Reversible

Yes.

## Phase 16: Port Base Render Pipeline to Go

### Objective

Render source image to grayscale preview in Go.

### Why this phase exists

This enables the first visual end-to-end parity checks.

### Exact code areas likely involved

- [backend/src/render/render_plan.rs](backend/src/render/render_plan.rs)

### What should be implemented

- render default preview
- render full-range preview if relevant
- PNG preview writing

### What should be avoided

- combining with processing pipeline in the same phase

### Risks

- preview byte mismatches
- performance surprises

### Validation/testing strategy

- compare Rust and Go preview outputs on sample files

### Exit criteria

- `renderStudy` can complete fully in Go

### Reversible

Yes.

## Phase 17: Cut `renderStudy` to Go

### Objective

Make preview rendering the first real user-facing Go backend feature.

### Why this phase exists

This is the first meaningful cutover.

### Exact code areas likely involved

- frontend adapter
- Tauri-Go process path
- Go render endpoint
- job registry

### What should be implemented

- `startRenderStudy`
- `getJob`
- preview artifact path handling

### What should be avoided

- process/analyze cutover at same time

### Risks

- preview URL loading
- job polling mismatch

### Validation/testing strategy

- open study -> render preview desktop test

### Exit criteria

- render jobs work via Go in the live app

### Reversible

Yes.

## Phase 18: Port Grayscale Processing Controls

### Objective

Move core processing math to Go.

### Why this phase exists

The processing tab is a major product feature and is simpler than export or analysis.

### Exact code areas likely involved

- [backend/src/processing.rs](backend/src/processing.rs)

### What should be implemented

- invert
- brightness LUT
- contrast LUT
- histogram equalization
- exact current operation order

### What should be avoided

- UX changes
- preset changes

### Risks

- visual drift

### Validation/testing strategy

- unit tests per operator
- golden image comparisons

### Exit criteria

- processing math is available in Go

### Reversible

Yes.

## Phase 19: Port Palette and Compare Logic

### Objective

Finish the preview-side processing pipeline in Go.

### Why this phase exists

Processing parity requires palette and side-by-side output.

### Exact code areas likely involved

- [backend/src/palette.rs](backend/src/palette.rs)
- [backend/src/compare.rs](backend/src/compare.rs)

### What should be implemented

- hot palette
- bone palette
- compare canvas composition
- correct output width and formats

### What should be avoided

- new palettes
- UI-only color experimentation

### Risks

- subtle color differences

### Validation/testing strategy

- image snapshots
- dimension assertions

### Exit criteria

- Go can produce all current preview processing modes

### Reversible

Yes.

## Phase 20: Port Process Job Orchestration to Go

### Objective

Make `processStudy` fully Go-owned except export if export is still blocked.

### Why this phase exists

This is where Go starts owning the core workstation workflow.

### Exact code areas likely involved

- [backend/src/app/state.rs](backend/src/app/state.rs)

### What should be implemented

- process job lifecycle
- validation
- source load
- processing pipeline
- preview artifact write
- output path handling

### What should be avoided

- full export cutover if DICOM write is not ready

### Risks

- job progress stage mismatches
- preview success but export lagging behind

### Validation/testing strategy

- process jobs through UI
- cache hit and duplicate request checks

### Exit criteria

- process preview path is Go-owned

### Reversible

Yes.

## Phase 21: Port Memory Cache to Go

### Objective

Move in-memory artifact/result caching into Go.

### Why this phase exists

Job reuse and responsiveness depend on it.

### Exact code areas likely involved

- [backend/src/cache/memory.rs](backend/src/cache/memory.rs)

### What should be implemented

- fingerprint-keyed result cache
- artifact existence checks
- typed result retrieval

### What should be avoided

- distributed or persistent cache ambitions

### Risks

- stale artifact references

### Validation/testing strategy

- delete artifact -> cache invalidates
- same fingerprint -> cache hit behavior

### Exit criteria

- cache semantics match Rust

### Reversible

Yes.

## Phase 22: Port Disk Cache Path Policy to Go

### Objective

Own artifact-path generation in Go.

### Why this phase exists

Artifact URLs and cache reuse depend on path stability.

### Exact code areas likely involved

- [backend/src/cache/disk.rs](backend/src/cache/disk.rs)

### What should be implemented

- temp root selection
- artifact namespaces
- persistence paths

### What should be avoided

- ad hoc path rules differing per feature

### Risks

- Tauri asset path config mismatch

### Validation/testing strategy

- artifact roots stable across runs
- existing asset scope expectations reviewed

### Exit criteria

- Go owns artifact path generation

### Reversible

Yes.

## Phase 23: Port Job Registry to Go

### Objective

Move job state machine semantics to Go.

### Why this phase exists

Without this, Go is not truly the backend.

### Exact code areas likely involved

- [backend/src/jobs/registry.rs](backend/src/jobs/registry.rs)

### What should be implemented

- start job
- duplicate active fingerprint reuse
- cached-complete jobs
- progress updates
- complete/fail/cancel states

### What should be avoided

- background queue frameworks
- overly generic job abstractions

### Risks

- concurrency bugs

### Validation/testing strategy

- explicit job-state tests
- cancellation/race tests

### Exit criteria

- Go job state machine matches frontend expectations

### Reversible

Yes.

## Phase 24: Port Recent-Study Persistence to Go

### Objective

Move simple persistence logic to Go.

### Why this phase exists

This is easy, visible, and reduces Rust surface area.

### Exact code areas likely involved

- [backend/src/persistence/catalog.rs](backend/src/persistence/catalog.rs)

### What should be implemented

- recent-study catalog JSON
- corruption handling
- truncate to 10 entries

### What should be avoided

- database introduction

### Risks

- path migration if shell changes later

### Validation/testing strategy

- reopen ordering tests
- invalid JSON handling tests

### Exit criteria

- recent studies work via Go

### Reversible

Yes.

## Phase 25: Port Line Measurement Logic to Go

### Objective

Move measurement helpers to Go.

### Why this phase exists

This is easy and isolates one small domain.

### Exact code areas likely involved

- [backend/src/analysis/measurement_service.rs](backend/src/analysis/measurement_service.rs)

### What should be implemented

- pixel length
- calibrated mm length
- rounding semantics

### What should be avoided

- changing measurement rounding rules

### Risks

- almost none

### Validation/testing strategy

- port existing tests

### Exit criteria

- `measureLineAnnotation` can be served by Go

### Reversible

Yes.

## Phase 26: Port Annotation Suggestion Mapping to Go

### Objective

Move the transformation from analysis result to editable annotations.

### Why this phase exists

This reduces coupling between analysis core and frontend annotations.

### Exact code areas likely involved

- [backend/src/analysis/auto_tooth.rs](backend/src/analysis/auto_tooth.rs)

### What should be implemented

- suggestion IDs
- line suggestions
- bounding-box rectangles
- confidence propagation

### What should be avoided

- changing annotation naming or editability rules

### Risks

- frontend assumptions on IDs

### Validation/testing strategy

- compare annotation bundles with Rust outputs

### Exit criteria

- suggestion generation is Go-owned

### Reversible

Yes.

## Phase 27: Port Tooth Analysis Primitives to Go

### Objective

Break the tooth-analysis port into manageable pieces.

### Why this phase exists

The current plan needs more granularity here. This is not one phase.

### Exact code areas likely involved

- [backend/src/tooth_measurement.rs](backend/src/tooth_measurement.rs)

### What should be implemented

Port in sub-steps:

- pixel normalization
- local gradient logic
- toothness map generation
- percentile helpers
- morphology ops
- connected-component collection
- candidate scoring
- candidate selection
- geometry extraction
- measurement bundling

### What should be avoided

- trying to port whole file in one shot
- changing algorithm during port

### Risks

- subtle candidate selection drift

### Validation/testing strategy

- unit tests for each primitive
- synthetic image tests
- sample-study tests

### Exit criteria

- all primitives exist in Go with comparable behavior

### Reversible

Yes.

## Phase 28: Implement Go Analyze Job

### Objective

Make `analyzeStudy` available through Go.

### Why this phase exists

Analysis is a user-visible pillar and one of the last major logic cutovers.

### Exact code areas likely involved

- analysis packages in Go
- job system
- frontend adapter

### What should be implemented

- render analysis preview
- run tooth analysis
- generate suggestions
- return job result

### What should be avoided

- UI changes during backend cutover

### Risks

- analysis confidence and geometry drift

### Validation/testing strategy

- live desktop analyze flow
- compare candidate counts and suggestion geometry

### Exit criteria

- `analyzeStudy` works through Go end-to-end

### Reversible

Yes.

## Phase 29: Implement Pure Go DICOM Export Prototype

### Objective

Prove whether Go can write valid Secondary Capture output.

### Why this phase exists

Export is too important to delay until the end without evidence.

### Exact code areas likely involved

- [backend/src/save.rs](backend/src/save.rs)
- new Go export package

### What should be implemented

Prototype writing:

- grayscale output
- preserved tags
- generated UIDs
- core metadata
- pixel data encoding

### What should be avoided

- assuming preview PNG write equals DICOM export

### Risks

- invalid datasets
- broken viewers

### Validation/testing strategy

- output opens in app
- output opens in external DICOM viewer
- metadata spot checks

### Exit criteria

- pure-Go export is judged viable or not

### Reversible

Yes.

## Phase 30: Add Temporary Rust Export Helper if Needed

### Objective

Keep migration moving if pure Go export is not ready.

### Why this phase exists

Export should not block Go ownership of the rest of the backend forever.

### Exact code areas likely involved

- narrow Rust helper binary
- Go invocation wrapper

### What should be implemented

A tiny Rust export helper that accepts:

- normalized processed image
- preserved metadata subset
- output path

### What should be avoided

- keeping full Rust app state involved
- exporting via legacy whole-backend bridge

### Risks

- helper scope creep

### Validation/testing strategy

- Go process owns the workflow; Rust helper only writes output

### Exit criteria

- process jobs can finish output path reliably while Go remains primary owner

### Reversible

Yes.

## Phase 31: Cut `processStudy` Fully to Go

### Objective

Switch process jobs to Go in the live app.

### Why this phase exists

This is the most important workstation flow after rendering.

### Exact code areas likely involved

- frontend adapter
- Go process endpoint
- job polling path

### What should be implemented

- preview processing
- artifact cache
- output path handling
- export handoff

### What should be avoided

- mixed per-substep ownership hidden from logs

### Risks

- processing regressions
- output mismatch

### Validation/testing strategy

- UI-driven process runs
- save path tests
- compare mode tests

### Exit criteria

- live processing path is Go-owned

### Reversible

Yes.

## Phase 32: Cut `measureLineAnnotation` to Go

### Objective

Remove one more live Rust dependency.

### Why this phase exists

This is easy cleanup with visible user impact.

### Exact code areas likely involved

- frontend adapter
- Go measurement endpoint

### What should be implemented

- measure line command in Go

### What should be avoided

- changing frontend measurement UX

### Risks

- minimal

### Validation/testing strategy

- draw/edit line and verify values

### Exit criteria

- measurement endpoint fully Go-backed

### Reversible

Yes.

## Phase 33: Cut `openStudy` to Go in Live Desktop Flow

### Objective

Make the initial study-open flow run through Go by default.

### Why this phase exists

This removes the shell's live dependency on Rust backend business logic.

### Exact code areas likely involved

- frontend adapter
- Tauri shell bridge
- Go metadata/open path

### What should be implemented

- default runtime switch for `openStudy` to Go

### What should be avoided

- switching analyze/process before open is stable

### Risks

- startup/open-state inconsistencies

### Validation/testing strategy

- open multiple studies
- recent-study state preserved

### Exit criteria

- live open path is Go-owned

### Reversible

Yes.

## Phase 34: Cut `analyzeStudy` to Go in Live Desktop Flow

### Objective

Make analysis run through Go by default.

### Why this phase exists

This removes another major Rust dependency from the interactive app.

### Exact code areas likely involved

- frontend adapter
- Go analyze endpoint

### What should be implemented

- default runtime switch for analysis to Go

### What should be avoided

- partial mixed analysis pipeline in production mode

### Risks

- suggestion mismatches

### Validation/testing strategy

- full analyze desktop flow
- compare live outputs to parity set

### Exit criteria

- live analysis path is Go-owned

### Reversible

Yes.

## Phase 35: Move CLI Ownership to Go

### Objective

Replace the Rust CLI with a Go CLI.

### Why this phase exists

CLI is part of backend ownership and a valuable validation harness.

### Exact code areas likely involved

- [backend/src/bin/xrayview-backend.rs](backend/src/bin/xrayview-backend.rs)
- new Go CLI command

### What should be implemented

Support current modes:

- describe presets
- describe study
- analyze tooth
- preview output
- processing output

### What should be avoided

- inventing a different CLI prematurely

### Risks

- CLI parity gaps expose backend gaps

### Validation/testing strategy

- run current documented CLI examples against Go CLI equivalent

### Exit criteria

- Go CLI replaces Rust CLI for supported workflows

### Reversible

Yes.

## Phase 36: Remove Rust Contract Generation and Legacy Frontend Build Dependency

### Objective

Delete the Rust-specific build path from frontend contract generation.

### Why this phase exists

By now, Rust should not sit in the frontend build chain.

### Exact code areas likely involved

- frontend generate-contracts script
- Rust generator removal

### What should be implemented

- contract generation from schema or Go-owned generator

### What should be avoided

- silently leaving Rust toolchain as hidden frontend prerequisite

### Risks

- build break if frontend type generation is not stable

### Validation/testing strategy

- clean frontend build without Rust contract generator

### Exit criteria

- frontend type generation no longer touches Rust

### Reversible

Yes.

## Phase 37: Introduce Go-First Packaging Flow Under Tauri

### Objective

Make the Tauri+Go sidecar desktop app releasable.

### Why this phase exists

Migration is not real until release flow works.

### Exact code areas likely involved

- build scripts
- sidecar packaging rules
- release smoke test

### What should be implemented

- Go backend build in desktop build pipeline
- sidecar bundling
- smoke tests updated for sidecar presence
- launch validation

### What should be avoided

- manual sidecar copying
- environment-dependent releases

### Risks

- cross-platform release breakage

### Validation/testing strategy

- dev launch
- no-bundle smoke
- bundled smoke

### Exit criteria

- Tauri + Go release path is stable

### Reversible

Partially.

## Phase 38: Retire Live Rust Backend Path

### Objective

Remove Rust from the default desktop runtime path.

### Why this phase exists

By now, Rust should only remain as an optional helper, if at all.

### Exact code areas likely involved

- runtime selector
- Tauri command handlers
- legacy Rust route code

### What should be implemented

- default Go runtime
- legacy Rust path behind a temporary fallback flag only

### What should be avoided

- permanent dual runtime

### Risks

- fallback removed too early

### Validation/testing strategy

- full regression suite on Go default runtime

### Exit criteria

- desktop app defaults entirely to Go

### Reversible

Yes, briefly.

## Phase 39: Evaluate Wails with a Focused Shell Prototype

### Objective

Confirm Wails is actually the right final shell in practice.

### Why this phase exists

This should be proven, not assumed.

### Exact code areas likely involved

- a separate prototype branch or small shell prototype
- reuse existing React frontend build

### What should be implemented

Prototype:

- app launch
- file dialog
- save dialog
- preview artifact access
- one backend call path

### What should be avoided

- full shell rewrite before prototype confidence

### Risks

- Wails packaging/runtime surprises

### Validation/testing strategy

- developer workflow comparison with Tauri
- artifact-access sanity checks

### Exit criteria

- explicit go/no-go on Wails

### Reversible

Yes.

## Phase 40: Replace Tauri Shell with Wails

### Objective

Move to the final Go-first shell.

### Why this phase exists

This eliminates the long-term stack mismatch.

### Exact code areas likely involved

- `frontend/src-tauri/`
- new Wails config/app files
- shell adapter bindings

### What should be implemented

- file dialogs
- save dialogs
- app startup
- preview path handling
- frontend build integration

### What should be avoided

- UI redesign
- backend redesign at same time

### Risks

- packaging differences
- shell-specific runtime bugs

### Validation/testing strategy

- same live desktop feature matrix used under Tauri

### Exit criteria

- Wails app runs full workflow successfully

### Reversible

Yes, but expensive.

## Phase 41: Remove Tauri-Specific Frontend Assumptions

### Objective

Clean frontend runtime assumptions that remain from Tauri.

### Why this phase exists

Even after shell replacement, old path logic may linger.

### Exact code areas likely involved

- frontend runtime adapter
- preview URL conversion logic
- shell utility functions

### What should be implemented

- remove Tauri-specific asset conversion if obsolete
- simplify runtime modes
- keep mock mode

### What should be avoided

- unnecessary frontend churn

### Risks

- preview loading regressions

### Validation/testing strategy

- desktop preview load tests
- mock mode unaffected

### Exit criteria

- frontend is shell-agnostic again

### Reversible

Yes.

## Phase 42: Remove Temporary Rust Decode/Export Helpers if Present

### Objective

Finish the backend language migration.

### Why this phase exists

A serious Go migration should not end with accidental permanent Rust helpers unless they are explicitly justified.

### Exact code areas likely involved

- helper binaries
- Go wrapper code
- build scripts

### What should be implemented

- replace helper usage with pure Go where possible
- if impossible, document the narrow retained helper as deliberate

### What should be avoided

- leaving helpers by inertia

### Risks

- parity loss if helper removed prematurely

### Validation/testing strategy

- repeat parity suite without helper

### Exit criteria

- helpers removed or formally accepted as deliberate exception

### Reversible

Yes.

## Phase 43: Remove Rust Backend Crate

### Objective

Delete the old backend once it no longer serves product runtime needs.

### Why this phase exists

This is the real migration completion point.

### Exact code areas likely involved

- `backend/`
- related docs and scripts

### What should be implemented

- delete old backend crate
- delete Rust CLI
- delete obsolete tests/scripts
- update docs

### What should be avoided

- deleting before parity and release validation are complete

### Risks

- losing fallback too early

### Validation/testing strategy

- clean checkout build
- desktop smoke tests
- release smoke tests

### Exit criteria

- app builds and ships without old Rust backend

### Reversible

Only with significant effort.

## Phase 44: Post-Migration Simplification

### Objective

Reduce migration-era complexity and normalize the repo.

### Why this phase exists

A good migration ends with a simpler repo, not a more complex one.

### Exact code areas likely involved

- docs
- scripts
- runtime flags
- build paths
- test harnesses

### What should be implemented

- remove migration toggles
- collapse temporary abstractions that are no longer needed
- keep only useful runtime abstraction seams
- rewrite README and architecture docs for Go-first reality

### What should be avoided

- carrying migration scaffolding forever

### Risks

- over-cleaning and deleting useful diagnostics

### Validation/testing strategy

- verify clean developer onboarding path
- verify build instructions are coherent

### Exit criteria

- repo tells a simple, final architecture story

### Reversible

Mostly yes.

## Responsibility Mapping

### Rewrite directly in Go

- contracts
- backend API
- CLI
- study registry
- job registry
- memory cache
- disk cache policy
- recent-study persistence
- render/windowing
- processing pipeline
- palette/compare logic
- preview writing
- measurement
- annotation suggestion mapping
- tooth analysis
- export pipeline if Go proves sufficient

### Wrap temporarily

- DICOM decode if Go library coverage is weak
- DICOM export if Go writing support is weak

### Keep as-is during transition

- React frontend
- current UI/store architecture
- Tauri shell
- mock mode
- current DTO semantics

### Delete entirely

- Rust-driven TypeScript contract generation
- direct Tauri-to-Rust business commands
- old Rust backend runtime path
- Tauri shell after Wails migration
- migration toggles after stabilization

## Assumptions and Defaults

- local-first remains the primary product shape
- Go is preferred for long-term maintainability and day-to-day development
- some performance loss is acceptable
- temporary coexistence is acceptable if tightly scoped
- shell replacement is not the first move
- Wails is the preferred long-term shell unless prototype evidence disproves it
