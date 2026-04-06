# Phase 7 Define Local Backend Transport

This document completes phase 7 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The repository now treats the Go sidecar transport as an explicit local HTTP/JSON contract instead of a temporary frontend assumption.

Primary implementation references:

- [go-backend/internal/httpapi/transport.go](go-backend/internal/httpapi/transport.go)
- [go-backend/internal/httpapi/router.go](go-backend/internal/httpapi/router.go)
- [go-backend/internal/httpapi/router_test.go](go-backend/internal/httpapi/router_test.go)
- [go-backend/internal/config/config.go](go-backend/internal/config/config.go)
- [go-backend/internal/config/config_test.go](go-backend/internal/config/config_test.go)
- [frontend/src/lib/runtimeConfig.ts](frontend/src/lib/runtimeConfig.ts)
- [frontend/scripts/runtime-env.mjs](frontend/scripts/runtime-env.mjs)
- [README.md](README.md)
- [go-backend/README.md](go-backend/README.md)

## 1. Transport Decision

Phase 7 formally chooses:

- local loopback HTTP/JSON
- one backend process per desktop app instance
- versioned API paths under `/api/v1`
- command execution through `POST /api/v1/commands/{command}`

The default base URL remains:

- `http://127.0.0.1:38181`

The Go runtime metadata now advertises transport details directly through `GET /healthz` and `GET /api/v1/runtime`:

- `transport: "local-http-json"`
- `localOnly: true`
- `apiBasePath: "/api/v1"`
- `commandEndpoint: "/api/v1/commands/{command}"`

That removes the previous ambiguity where phase 5 and phase 6 implied HTTP/JSON but did not yet treat it as the repository’s declared local transport contract.

## 2. Local-Only Enforcement

Phase 7 now enforces the transport boundary on both sides:

- the Go backend rejects `XRAYVIEW_GO_BACKEND_HOST` values that are not loopback hosts
- the frontend rejects `XRAYVIEW_GO_BACKEND_URL` values that are not absolute loopback `http://` URLs
- the frontend also rejects sidecar URLs that include proxy-style paths, query strings, hashes, or credentials

Allowed backend hosts are intentionally narrow:

- `127.0.0.1`
- `localhost`
- `::1`

This keeps the migration aligned with a desktop sidecar design instead of drifting toward an accidental remote-service architecture.

## 3. Tauri/Webview Compatibility

The transport now includes the HTTP behavior the Tauri webview actually needs:

- CORS headers for allowed local origins
- `OPTIONS` preflight handling for JSON `POST` requests
- request logging with method, path, status, duration, and origin when present

Allowed origins are limited to local desktop/dev origins:

- `tauri://localhost`
- `http://tauri.localhost`
- loopback `http://` or `https://` origins such as `http://localhost:1420`

That is the minimum needed to let the React/Tauri frontend call the local Go backend using `fetch` without opening the transport to arbitrary remote origins.

## 4. Why This Transport

Phase 7 keeps the migration on the simplest viable boundary:

- HTTP/JSON is easy to inspect with browser devtools, `curl`, and logs
- the transport stays independent of Tauri internals
- the future Wails migration can reuse the same backend semantics or collapse them later
- the frontend backend adapter already maps cleanly onto command-based HTTP calls

This phase deliberately does not add process management yet. Tauri-side startup and shutdown ownership remains phase 8.

## 5. Validation

Validate the phase 7 transport with:

```bash
go -C go-backend test ./...
go -C go-backend run ./cmd/xrayview-cli print-config
npm --prefix frontend run build
```

Manual HTTP checks:

```bash
curl -s http://127.0.0.1:38181/healthz
curl -s http://127.0.0.1:38181/api/v1/runtime
curl -i -X OPTIONS \
  -H 'Origin: http://localhost:1420' \
  -H 'Access-Control-Request-Method: POST' \
  -H 'Access-Control-Request-Headers: content-type' \
  http://127.0.0.1:38181/api/v1/commands/open_study
```

## 6. Exit Criteria Check

Phase 7 exit criteria are now met:

- the migration transport is chosen explicitly
- the transport is documented at repo level
- the backend enforces local-only binding
- the frontend enforces local-only sidecar URLs
- the Go server supports the CORS/preflight behavior required by the Tauri webview
