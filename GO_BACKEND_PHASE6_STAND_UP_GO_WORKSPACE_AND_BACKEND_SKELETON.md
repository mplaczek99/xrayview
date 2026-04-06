# Phase 6 Stand Up Go Workspace and Backend Skeleton

This document completes phase 6 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The repository now contains a runnable Go backend workspace instead of only a reserved frontend `go-sidecar` slot.

Primary implementation references:

- [go.work](go.work)
- [go/contracts/go.mod](go/contracts/go.mod)
- [go-backend/go.mod](go-backend/go.mod)
- [go-backend/cmd/xrayviewd/main.go](go-backend/cmd/xrayviewd/main.go)
- [go-backend/cmd/xrayview-cli/main.go](go-backend/cmd/xrayview-cli/main.go)
- [go-backend/internal/app/app.go](go-backend/internal/app/app.go)
- [go-backend/internal/httpapi/router.go](go-backend/internal/httpapi/router.go)
- [go-backend/internal/config/config.go](go-backend/internal/config/config.go)
- [go-backend/internal/contracts/contracts.go](go-backend/internal/contracts/contracts.go)

## 1. Workspace Shape

Phase 6 adds an explicit Go workspace:

- `go.work`
- `go/contracts` as a small module for generated contract bindings
- `go-backend` as the backend module

That keeps contract ownership separate from backend runtime code while still letting the Go backend import the frozen v1 schema metadata directly.

## 2. Backend Skeleton Packages

The new Go backend now has first-pass packages for the phase 6 responsibilities:

- `internal/contracts`
- `internal/httpapi`
- `internal/config`
- `internal/logging`
- `internal/jobs`
- `internal/studies`
- `internal/cache`
- `internal/persistence`
- `internal/app`

These are intentionally small, but they are real packages with concrete wiring, not empty placeholders.

## 3. Runnable Binaries

Phase 6 adds two Go entrypoints:

- `xrayviewd` starts the HTTP backend directly
- `xrayview-cli` supports `serve`, `print-config`, `list-commands`, and `version`

The default listen address is `127.0.0.1:38181`, which matches the frontend `go-sidecar` default from phase 5.

## 4. Transport Skeleton

The backend now boots an HTTP/JSON surface with:

- `GET /healthz`
- `GET /api/v1/runtime`
- `GET /api/v1/commands`
- `POST /api/v1/commands/{command}`

The command endpoints intentionally return structured `BackendError`-shaped placeholder responses for the supported phase 5 command names. That gives the migration a stable process boundary and command namespace without pretending phase 9+ behavior already exists.

## 5. Validation

Phase 6 can now be validated with:

```bash
go -C go-backend test ./...
go -C go-backend build ./cmd/xrayviewd ./cmd/xrayview-cli
go -C go-backend run ./cmd/xrayview-cli print-config
go -C go-backend run ./cmd/xrayviewd
```

Once running, confirm the health surface:

```bash
curl -s http://127.0.0.1:38181/healthz
curl -s http://127.0.0.1:38181/api/v1/commands
```

## 6. Exit Criteria Check

Phase 6 exit criteria are now met:

- the repo contains a first-class Go workspace
- the Go backend has deliberate package boundaries
- the backend boots successfully as a process
- a health/runtime endpoint works
- the transport surface and command namespace exist for later phases
