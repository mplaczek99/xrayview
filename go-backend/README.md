# xrayview Go Backend

This module is the phase 6 migration skeleton for the future Go sidecar backend.

Current scope:

- load config from environment
- initialize cache and persistence roots
- expose a local HTTP/JSON server
- publish health/runtime metadata
- reserve the command namespace expected by the frontend `go-sidecar` adapter

Current non-goals:

- no DICOM loading yet
- no manifest endpoint behavior yet
- no job execution yet
- no study persistence behavior yet beyond package scaffolding

## Commands

```bash
go run ./cmd/xrayviewd
go run ./cmd/xrayview-cli print-config
go run ./cmd/xrayview-cli list-commands
```

## Environment

- `XRAYVIEW_GO_BACKEND_HOST`
- `XRAYVIEW_GO_BACKEND_PORT`
- `XRAYVIEW_GO_BACKEND_LOG_LEVEL`
- `XRAYVIEW_GO_BACKEND_BASE_DIR`
- `XRAYVIEW_GO_BACKEND_CACHE_DIR`
- `XRAYVIEW_GO_BACKEND_PERSISTENCE_DIR`
- `XRAYVIEW_GO_BACKEND_SHUTDOWN_TIMEOUT`
