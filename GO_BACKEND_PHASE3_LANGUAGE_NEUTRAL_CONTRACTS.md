# Phase 3 Replace Rust as Contract Source of Truth

This document completes phase 3 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). Contract ownership now lives in a language-neutral schema instead of Rust-authored TypeScript generation.

Primary implementation references:

- [contracts/backend-contract-v1.schema.json](contracts/backend-contract-v1.schema.json)
- [contracts/scripts/schema-tools.mjs](contracts/scripts/schema-tools.mjs)
- [contracts/scripts/generate-contract-bindings.mjs](contracts/scripts/generate-contract-bindings.mjs)
- [contracts/scripts/validate-contract-value.mjs](contracts/scripts/validate-contract-value.mjs)
- [frontend/scripts/generate-contracts.mjs](frontend/scripts/generate-contracts.mjs)
- [frontend/src/lib/generated/contracts.ts](frontend/src/lib/generated/contracts.ts)
- [go/contracts/contractv1/bindings.go](go/contracts/contractv1/bindings.go)
- [backend/tests/contracts.rs](backend/tests/contracts.rs)

## 1. New Source of Truth

Phase 3 moves the frozen v1 contract into [contracts/backend-contract-v1.schema.json](contracts/backend-contract-v1.schema.json).

That schema now owns:

- backend contract version metadata through `x-contract-version`
- the export order for generated bindings
- every request, response, union, enum, and nested DTO used by the frozen desktop/backend surface
- generation targets for TypeScript and Go validation bindings

Rust still defines runtime DTO structs because the backend is still Rust today, but Rust no longer generates or dictates the frontend contract file.

## 2. Generation Path

The committed generation flow is now:

1. edit the schema
2. run `npm --prefix frontend run generate:contracts`
3. regenerate:
   - [frontend/src/lib/generated/contracts.ts](frontend/src/lib/generated/contracts.ts)
   - [go/contracts/contractv1/bindings.go](go/contracts/contractv1/bindings.go)

Notes:

- the TypeScript file remains the frontend-facing contract surface
- the Go file is intentionally a validation-binding surface, not a speculative handwritten Go backend model
- the Go binding embeds the authoritative schema JSON and definition refs so a future Go backend can validate against the same contract immediately

## 3. Rust Removed from the Critical Path

Phase 3 removes the old Rust-authored contract generation path:

- [backend/src/bin/generate-contracts.rs](backend/src/bin/generate-contracts.rs) was deleted
- Rust no longer exports handwritten TypeScript generation helpers from [backend/src/api/contracts.rs](backend/src/api/contracts.rs)
- [frontend/scripts/generate-contracts.mjs](frontend/scripts/generate-contracts.mjs) now invokes the schema generator directly

This is the actual ownership transfer. The frontend contract file can now be regenerated without compiling or running the Rust generator.

## 4. Drift Protection and Validation

Phase 3 replaces the old "Rust generator output matches committed TypeScript" check with two stronger protections:

- [backend/tests/contracts.rs](backend/tests/contracts.rs) shells out to the schema generator in `--check` mode and fails if either committed generated output drifts from the schema
- the same Rust test suite validates representative Rust-serialized payloads against the schema through [contracts/scripts/validate-contract-value.mjs](contracts/scripts/validate-contract-value.mjs)

That means:

- the schema is authoritative
- generated artifacts must match the schema
- Rust runtime payloads must also still satisfy the schema

`BACKEND_CONTRACT_VERSION` still exists in Rust for now, but phase 3 demotes it to a compatibility constant. Tests now assert that it matches the schema-owned version instead of the other way around.

## 5. Validation Commands

Validate the phase 3 setup with:

```bash
# Regenerate TypeScript and Go validation bindings from the schema
npm --prefix frontend run generate:contracts

# Check backend drift protection and schema validation of Rust payloads
cargo test --manifest-path backend/Cargo.toml --test contracts

# Verify the frontend still compiles against the schema-generated TypeScript contracts
npm --prefix frontend run build
```

## 6. Exit Criteria Check

Phase 3 exit criteria are now met:

- Rust contract generation is no longer authoritative
- the repo has one language-neutral contract source
- TypeScript bindings are generated from that source
- Go validation bindings are generated from that source
- Rust payloads are validated against that source
