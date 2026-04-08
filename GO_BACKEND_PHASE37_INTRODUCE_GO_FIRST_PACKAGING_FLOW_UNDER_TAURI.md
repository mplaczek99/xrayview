# Phase 37 Introduce Go-First Packaging Flow Under Tauri

This document completes phase 37 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Tauri desktop release path now treats the Go sidecar as a first-class packaged dependency: the build pipeline prepares it intentionally, Linux AppImage packaging no longer depends on a last-minute runtime download, and the release smoke flow now launches the built desktop app to verify the sidecar actually comes up.

Primary implementation references:

- [frontend/scripts/tauri-build.mjs](frontend/scripts/tauri-build.mjs)
- [frontend/scripts/appimage-runtime.mjs](frontend/scripts/appimage-runtime.mjs)
- [frontend/scripts/release-smoke-test.mjs](frontend/scripts/release-smoke-test.mjs)
- [frontend/scripts/release-launch-smoke.mjs](frontend/scripts/release-launch-smoke.mjs)
- [frontend/src-tauri/tauri.conf.json](frontend/src-tauri/tauri.conf.json)
- [.github/workflows/build-release-artifacts.yml](.github/workflows/build-release-artifacts.yml)
- [.github/workflows/publish-release.yml](.github/workflows/publish-release.yml)
- [README.md](README.md)

## 1. The Tauri Build Path Now Prepares AppImage Runtime Inputs Deliberately

Phase 8 already made the Tauri build scripts compile the Go sidecar and declare it under `bundle.externalBin`.

Phase 37 closes the remaining Linux packaging hole: AppImage creation previously depended on `linuxdeploy` downloading the type-2 runtime during bundling. That made bundle success depend on external network state even after Tauri's own AppImage tooling had already been downloaded and cached.

The new [frontend/scripts/appimage-runtime.mjs](frontend/scripts/appimage-runtime.mjs) helper now:

- locates Tauri's cached AppImage tooling
- extracts the embedded runtime prefix from that cached AppImage
- writes a reusable local runtime file under `frontend/src-tauri/target/appimage-runtime/`

[frontend/scripts/tauri-build.mjs](frontend/scripts/tauri-build.mjs) then exports that path through `LDAI_RUNTIME_FILE` whenever Linux AppImage bundling is requested. That keeps AppImage generation inside the repository-owned build flow instead of requiring a second download at bundle time.

## 2. Release Smoke Validation Now Launches The Built Desktop App

Before this phase, the release smoke script only proved that:

- the Go sidecar binary was built into the expected Tauri inputs
- the desktop binary existed
- bundle directories were non-empty

That was not enough for a releasable migration phase because it never proved that the packaged app could actually launch and bring up the Go sidecar it depends on by default.

Phase 37 adds [frontend/scripts/release-launch-smoke.mjs](frontend/scripts/release-launch-smoke.mjs), which:

- launches the built desktop executable
- probes `GET /healthz` until the expected Go sidecar responds
- terminates the launched process tree and confirms the sidecar is gone

[frontend/scripts/release-smoke-test.mjs](frontend/scripts/release-smoke-test.mjs) now uses that helper for the no-bundle release binary on every platform. On Linux, when AppImage output is part of the requested bundle set, it also launches the bundled `.AppImage` and performs the same sidecar readiness check there.

When the current shell has no display server available, the local smoke script reports that launch validation was skipped instead of failing with a GTK initialization panic. The release workflows now install `xvfb` on Linux so CI still executes the real launch path in headless environments.

## 3. Release Workflows Now Use The Same Smoke-Tested Packaging Entry Point

The GitHub release workflows previously called `npm run tauri:build -- --bundles ...` directly.

Phase 37 switches both release workflows to the repository's smoke entry point:

```bash
npm run release:smoke -- --bundle --bundles <kind>
```

That means CI and tagged releases now execute the same higher-level validation path used locally:

- contract drift check
- Go backend tests
- frontend build
- Tauri bundle build
- desktop launch validation

The workflows also install Go explicitly and add `xvfb` on Linux so GUI launch smoke can run in headless CI.

## 4. Validation Coverage

Validated with:

```bash
npm run release:smoke
npm run release:smoke -- --bundle
```

This covers:

- Go sidecar preparation in the Tauri build path
- no-bundle desktop launch validation against the packaged release binary
- bundled Linux AppImage generation without a runtime download
- bundled Linux AppImage launch validation with the Go sidecar present in Linux CI and any local environment that provides GUI launch support

## 5. Exit Criteria Check

Phase 37 exit criteria are now met:

- the desktop build pipeline builds and packages the Go sidecar intentionally
- bundled Linux packaging no longer depends on a separate runtime download during `linuxdeploy`
- release smoke validation checks launch behavior instead of only file presence
- release workflows use the smoke-tested packaging entry point instead of a thinner raw build step
