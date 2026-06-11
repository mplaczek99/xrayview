# Repository Guidelines

## Project Structure & Module Organization

This monorepo builds a BMP bitewing X-ray visualization workstation. `frontend/` contains the HTMX/TypeScript Vite UI, with app code in `frontend/src/` and Playwright tests in `frontend/tests/e2e/`. `backend/` is the Go backend library, stdio IPC server, and headless CLI; package tests live under `backend/internal/**`, and `backend/shell` is the public seam the desktop app binds to. `desktop/` is the Go + Wails 2 desktop shell (its own module). `contracts/` contains the JSON schema and binding scripts. `images/` is for local sample assets; fixture folders may be gitignored.

## Build, Test, and Development Commands

- `npm install`: installs root tooling and frontend dependencies via `postinstall`.
- `npm run dev`: starts the browser mock UI with synthetic data.
- `npm run desktop:dev`: starts the full Wails desktop app (Go backend in-process) with hot reload.
- `npm run backend:test`: runs Go backend tests.
- `npm run test:e2e`: runs Playwright e2e tests.
- `npm run lint`: runs Go vet (backend) and frontend Biome checks; `npm run lint:desktop` vets the Wails module.
- `npm run contracts:check`: verifies generated TypeScript bindings match the schema.
- `npm run release:smoke`: required pre-done gate; runs lint, contracts, backend tests, frontend build, and the Wails desktop build.

## Coding Style & Naming Conventions

Use idiomatic Go for both the backend and the `desktop/` Wails module; keep `go test ./backend/...`, `go vet ./backend/...`, and `go vet -tags webkit2_41 ./desktop/...` clean. Frontend formatting is controlled by Biome: 2-space indentation, double quotes, semicolons, trailing commas, LF endings, and 100-column line width. Use camelCase for TypeScript functions/variables and PascalCase for exported types/classes. Do not hand-edit `frontend/src/lib/generated/contracts.ts`.

## Testing Guidelines

Add focused Go tests near the code in `*_test.go` files. Name tests by behavior, for example `TestStoreJobSnapshotCapsTerminalJobRetentionAndKeepsLatest`. Put browser workflow tests in `frontend/tests/e2e/*.spec.ts`. There is no declared coverage threshold; cover contracts, image processing, job lifecycle, and touched UI workflows.

## Commit & Pull Request Guidelines

Recent commits use short, imperative summaries such as `Show analysis running indicator` or `Format frontend with Biome and fix lint findings`. Keep commits scoped. Pull requests should describe the user-facing change, list verification commands, link issues, and include screenshots or recordings for visible UI changes.

## Contracts, Runtime, and Scope

`contracts/backend-contract-v1.schema.json` is the TypeScript contract source of truth. After schema edits, run `npm run contracts:generate`, manually mirror Go types in `backend/internal/contracts`, then run `npm run contracts:check`. The supported workflow is BMP-only and desktop (Wails IPC) only; do not reintroduce DICOM, TIFF, or a standalone HTTP backend. This app is for image visualization only, not diagnosis or clinical decision-making.
