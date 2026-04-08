# Phase 36 Remove Rust Contract Generation And Legacy Frontend Build Dependency

This document completes phase 36 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The frontend-owned contract generation and release-validation path now stays on the schema/Node/Go side of the migration and no longer reaches into the legacy Rust backend crate as part of its normal workflow.

Primary implementation references:

- [package.json](package.json)
- [frontend/package.json](frontend/package.json)
- [frontend/scripts/release-smoke-test.mjs](frontend/scripts/release-smoke-test.mjs)
- [contracts/scripts/schema-tools.mjs](contracts/scripts/schema-tools.mjs)
- [frontend/src/lib/generated/contracts.ts](frontend/src/lib/generated/contracts.ts)
- [README.md](README.md)

## 1. Contract Generation Is Now Workspace-Owned Instead Of Frontend-Local

Phase 3 already moved contract generation away from Rust and onto the language-neutral schema in `contracts/`.

Phase 36 removes the last frontend-local ownership signal from that flow:

- the workspace root now exposes `npm run contracts:generate`
- the workspace root now exposes `npm run contracts:check`
- the frontend `generate:contracts` script delegates to the workspace-level contract generator instead of owning a separate local script path

That makes contract generation an explicit shared repository concern rather than a special frontend wrapper around migration-era behavior.

## 2. Frontend Release Validation No Longer Reaches Into `backend/Cargo.toml`

Before this phase, the frontend-owned release smoke script still ran:

```bash
cargo test --manifest-path backend/Cargo.toml
```

That kept the legacy Rust backend crate in the release-validation critical path even though contract generation itself had already moved out of Rust.

Phase 36 replaces that dependency with a schema-owned drift check:

- `npm run contracts:check`

The release smoke flow still builds the Tauri shell, which necessarily uses Rust until later migration phases, but it no longer treats the legacy Rust backend crate as part of frontend contract/build validation.

## 3. Generated TypeScript Guidance Now Points At The Shared Contract Command

The generated TypeScript header now instructs developers to run:

```bash
npm run contracts:generate
```

instead of a frontend-specific wrapper command. This keeps the source of truth and the regeneration command aligned with the actual repository ownership boundary.

## 4. Documentation Now Distinguishes Contract Generation From Legacy Rust Compatibility Tests

The repository README now makes two points explicit:

- contract binding generation and drift checking live under the shared schema workflow
- Rust-side contract tests are compatibility coverage for the still-linked migration shell, not a prerequisite for frontend contract generation

That clarification matters because the Tauri shell still contains Rust during phase 36, but the frontend build chain no longer depends on the legacy Rust backend crate to produce or verify frontend contract bindings.

## 5. Validation Coverage

Validated with:

```bash
npm run contracts:generate
npm run contracts:check
npm --prefix frontend run build
npm run release:smoke
```

This covers:

- shared contract regeneration from the schema
- no-drift checking without invoking the legacy Rust backend contract test path
- a clean frontend build against the schema-generated TypeScript bindings
- the frontend-owned no-bundle release smoke path, including Go backend validation and Tauri packaging

## 6. Exit Criteria Check

Phase 36 exit criteria are now met:

- frontend type generation no longer uses a Rust-owned generation path
- the frontend-owned release validation path no longer depends on `backend/Cargo.toml`
- contract generation is explicitly owned by the shared schema tooling instead of a legacy frontend-local wrapper
