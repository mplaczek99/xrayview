package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"xrayview/go-backend/internal/cache"
	"xrayview/go-backend/internal/config"
	"xrayview/go-backend/internal/contracts"
	"xrayview/go-backend/internal/jobs"
	"xrayview/go-backend/internal/persistence"
	"xrayview/go-backend/internal/studies"
)

func testRouter(t *testing.T) http.Handler {
	t.Helper()

	return NewRouter(Dependencies{
		Config:      config.Default(),
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Cache:       cache.New(t.TempDir()),
		Persistence: persistence.New(t.TempDir()),
		Jobs:        jobs.New(),
		Studies:     studies.New(),
		StartedAt:   time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
	})
}

func TestHealthzIncludesContractMetadata(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	testRouter(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload runtimeResponse
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if payload.Service != contracts.ServiceName {
		t.Fatalf("service = %q, want %q", payload.Service, contracts.ServiceName)
	}

	if payload.Transport != TransportKind {
		t.Fatalf("transport = %q, want %q", payload.Transport, TransportKind)
	}

	if !payload.LocalOnly {
		t.Fatal("localOnly = false, want true")
	}

	if payload.BackendContractVersion != contracts.BackendContractVersion {
		t.Fatalf("contract version = %d, want %d", payload.BackendContractVersion, contracts.BackendContractVersion)
	}

	if payload.APIBasePath != APIBasePath {
		t.Fatalf("api base path = %q, want %q", payload.APIBasePath, APIBasePath)
	}

	if payload.CommandEndpoint != CommandEndpointTemplate {
		t.Fatalf("command endpoint = %q, want %q", payload.CommandEndpoint, CommandEndpointTemplate)
	}
}

func TestCommandsEndpointListsSupportedCommands(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/commands", nil)
	testRouter(t).ServeHTTP(recorder, request)

	var payload commandListResponse
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(payload.Commands) != len(contracts.SupportedCommands) {
		t.Fatalf("command count = %d, want %d", len(payload.Commands), len(contracts.SupportedCommands))
	}
}

func TestGetProcessingManifestReturnsFrozenPayload(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/get_processing_manifest",
		nil,
	)
	testRouter(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload any
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	expected := decodeJSONValue(t, `{
		"defaultPresetId": "default",
		"presets": [
			{
				"id": "default",
				"controls": {
					"brightness": 0,
					"contrast": 1.0,
					"invert": false,
					"equalize": false,
					"palette": "none"
				}
			},
			{
				"id": "xray",
				"controls": {
					"brightness": 10,
					"contrast": 1.4,
					"invert": false,
					"equalize": true,
					"palette": "bone"
				}
			},
			{
				"id": "high-contrast",
				"controls": {
					"brightness": 0,
					"contrast": 1.8,
					"invert": false,
					"equalize": true,
					"palette": "none"
				}
			}
		]
	}`)

	if !reflect.DeepEqual(payload, expected) {
		t.Fatalf("manifest = %#v, want %#v", payload, expected)
	}
}

func TestUnimplementedCommandReturnsBackendError(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/open_study",
		strings.NewReader(`{"inputPath":"/tmp/sample.dcm"}`),
	)
	request.Header.Set("content-type", "application/json")
	testRouter(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotImplemented)
	}

	var payload backendError
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if payload.Code != "internal" {
		t.Fatalf("code = %q, want %q", payload.Code, "internal")
	}
}

func TestAllowedOriginReceivesCORSHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("Origin", "http://localhost:1420")
	testRouter(t).ServeHTTP(recorder, request)

	if got, want := recorder.Header().Get("Access-Control-Allow-Origin"), "http://localhost:1420"; got != want {
		t.Fatalf("allow origin = %q, want %q", got, want)
	}
}

func TestOptionsPreflightReturnsNoContent(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, CommandsPath+"/open_study", nil)
	request.Header.Set("Origin", "tauri://localhost")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "content-type")
	testRouter(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}

	if got, want := recorder.Header().Get("Access-Control-Allow-Origin"), "tauri://localhost"; got != want {
		t.Fatalf("allow origin = %q, want %q", got, want)
	}

	if got := recorder.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodPost) {
		t.Fatalf("allow methods = %q, want POST included", got)
	}
}

func TestDisallowedOriginIsRejected(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("Origin", "https://example.com")
	testRouter(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}

	var payload backendError
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if payload.Code != "invalidInput" {
		t.Fatalf("code = %q, want %q", payload.Code, "invalidInput")
	}
}

func decodeJSONValue(t *testing.T, raw string) any {
	t.Helper()

	var value any
	if err := json.NewDecoder(strings.NewReader(raw)).Decode(&value); err != nil {
		t.Fatalf("decode expected JSON failed: %v", err)
	}

	return value
}
