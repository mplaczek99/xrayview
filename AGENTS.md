# Repository Guidelines

## Project Structure & Module Organization

This monorepo builds a BMP bitewing X-ray visualization workstation. `frontend/` contains the HTMX/TypeScript Vite UI, with app code in `frontend/src/` and Playwright tests in `frontend/tests/e2e/`. `backend-rs/` is the Rust backend library and headless CLI; most unit tests live inline in `backend-rs/src/*.rs`. `desktop-tauri/` is the Tauri 2 shell and IPC layer. `contracts/` contains the JSON schema and binding scripts. `images/` is for local sample assets; fixture folders may be gitignored.

## Build, Test, and Development Commands

- `npm install`: installs root tooling and frontend dependencies via `postinstall`.
- `npm run dev`: starts the browser mock UI with synthetic data.
- `npm run tauri:dev`: starts the full desktop app with the in-process Rust backend.
- `npm run backend:test`: runs Rust backend tests.
- `npm run test:e2e`: runs Playwright e2e tests.
- `npm run lint`: runs Rust clippy and frontend Biome checks.
- `npm run contracts:check`: verifies generated TypeScript bindings match the schema.
- `npm run release:smoke`: required pre-done gate; runs lint, contracts, backend tests, frontend build, and `tauri build --no-bundle`.

## Coding Style & Naming Conventions

Use Rust 2024 idioms and keep `cargo clippy --all-targets --locked -- -D warnings` clean. Frontend formatting is controlled by Biome: 2-space indentation, double quotes, semicolons, trailing commas, LF endings, and 100-column line width. Use camelCase for TypeScript functions/variables, PascalCase for exported types/classes, and snake_case for Rust items. Do not hand-edit `frontend/src/lib/generated/contracts.ts`.

## Testing Guidelines

Add focused Rust unit tests near the code under `#[cfg(test)]`. Name tests by behavior, for example `store_job_snapshot_caps_terminal_job_retention_and_keeps_latest`. Put browser workflow tests in `frontend/tests/e2e/*.spec.ts`. There is no declared coverage threshold; cover contracts, image processing, job lifecycle, and touched UI workflows.

## Commit & Pull Request Guidelines

Recent commits use short, imperative summaries such as `Show analysis running indicator` or `Format frontend with Biome and fix lint findings`. Keep commits scoped. Pull requests should describe the user-facing change, list verification commands, link issues, and include screenshots or recordings for visible UI changes.

## Contracts, Runtime, and Scope

`contracts/backend-contract-v1.schema.json` is the TypeScript contract source of truth. After schema edits, run `npm run contracts:generate`, manually mirror Rust types in `backend-rs/src/contracts.rs`, then run `npm run contracts:check`. The supported workflow is BMP-only and Tauri IPC-only; do not reintroduce DICOM, TIFF, or a standalone HTTP backend. This app is for image visualization only, not diagnosis or clinical decision-making.
