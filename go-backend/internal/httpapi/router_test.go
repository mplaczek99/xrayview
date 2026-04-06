package httpapi

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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

func testDependencies(t *testing.T) Dependencies {
	t.Helper()

	return Dependencies{
		Config:      config.Default(),
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Cache:       cache.New(t.TempDir()),
		Persistence: persistence.New(t.TempDir()),
		Jobs:        jobs.New(),
		Studies:     studies.New(),
		StartedAt:   time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
	}
}

func testRouter(t *testing.T) http.Handler {
	t.Helper()

	return NewRouter(testDependencies(t))
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

func TestOpenStudyRegistersStudyAndReturnsContractPayload(t *testing.T) {
	deps := testDependencies(t)
	handler := NewRouter(deps)
	inputPath := sampleDicomPath(t)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/open_study",
		strings.NewReader(`{"inputPath":"`+inputPath+`"}`),
	)
	request.Header.Set("content-type", "application/json")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload contracts.OpenStudyCommandResult
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if payload.Study.StudyID == "" {
		t.Fatal("studyId = empty, want generated identifier")
	}

	if got, want := payload.Study.InputPath, inputPath; got != want {
		t.Fatalf("inputPath = %q, want %q", got, want)
	}

	if got, want := payload.Study.InputName, "sample-dental-radiograph.dcm"; got != want {
		t.Fatalf("inputName = %q, want %q", got, want)
	}
	if payload.Study.MeasurementScale != nil {
		t.Fatalf("measurementScale = %+v, want nil for sample fixture", payload.Study.MeasurementScale)
	}

	if got, want := deps.Studies.Count(), 1; got != want {
		t.Fatalf("study count = %d, want %d", got, want)
	}

	catalog, err := deps.Persistence.Load()
	if err != nil {
		t.Fatalf("catalog load failed: %v", err)
	}

	if got, want := len(catalog.RecentStudies), 1; got != want {
		t.Fatalf("recent study count = %d, want %d", got, want)
	}
}

func TestOpenStudyIncludesMeasurementScaleWhenSpacingMetadataExists(t *testing.T) {
	deps := testDependencies(t)
	handler := NewRouter(deps)
	inputPath := filepath.Join(t.TempDir(), "scaled-study.dcm")
	if err := os.WriteFile(inputPath, buildScaledDicomFixture(), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/open_study",
		strings.NewReader(`{"inputPath":"`+inputPath+`"}`),
	)
	request.Header.Set("content-type", "application/json")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload contracts.OpenStudyCommandResult
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if payload.Study.MeasurementScale == nil {
		t.Fatal("measurementScale = nil, want PixelSpacing-derived scale")
	}
	if got, want := payload.Study.MeasurementScale.RowSpacingMM, 0.25; got != want {
		t.Fatalf("measurementScale.rowSpacingMm = %v, want %v", got, want)
	}
	if got, want := payload.Study.MeasurementScale.ColumnSpacingMM, 0.40; got != want {
		t.Fatalf("measurementScale.columnSpacingMm = %v, want %v", got, want)
	}
	if got, want := payload.Study.MeasurementScale.Source, "PixelSpacing"; got != want {
		t.Fatalf("measurementScale.source = %q, want %q", got, want)
	}
}

func TestOpenStudyRejectsNonDicomInput(t *testing.T) {
	inputPath := filepath.Join(t.TempDir(), "not-a-dicom.dcm")
	if err := os.WriteFile(inputPath, []byte("dicom"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/open_study",
		strings.NewReader(`{"inputPath":"`+inputPath+`"}`),
	)
	request.Header.Set("content-type", "application/json")
	testRouter(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	var payload contracts.BackendError
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if got, want := payload.Code, contracts.BackendErrorCodeInvalidInput; got != want {
		t.Fatalf("code = %q, want %q", got, want)
	}
}

func TestOpenStudyRejectsUnknownFields(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/open_study",
		strings.NewReader(`{"inputPath":"/tmp/sample.dcm","unexpected":true}`),
	)
	request.Header.Set("content-type", "application/json")
	testRouter(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	var payload contracts.BackendError
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if got, want := payload.Code, contracts.BackendErrorCodeInvalidInput; got != want {
		t.Fatalf("code = %q, want %q", got, want)
	}
}

func TestOpenStudyReturnsNotFoundForMissingInput(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/open_study",
		strings.NewReader(`{"inputPath":"/tmp/does-not-exist.dcm"}`),
	)
	request.Header.Set("content-type", "application/json")
	testRouter(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}

	var payload contracts.BackendError
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if got, want := payload.Code, contracts.BackendErrorCodeNotFound; got != want {
		t.Fatalf("code = %q, want %q", got, want)
	}
}

func TestUnimplementedCommandReturnsBackendError(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/commands/start_render_job",
		strings.NewReader(`{"studyId":"study-1"}`),
	)
	request.Header.Set("content-type", "application/json")
	testRouter(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotImplemented)
	}

	var payload contracts.BackendError
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if payload.Code != contracts.BackendErrorCodeInternal {
		t.Fatalf("code = %q, want %q", payload.Code, contracts.BackendErrorCodeInternal)
	}
}

func sampleDicomPath(t *testing.T) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller returned no file path")
	}

	return filepath.Clean(
		filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "images", "sample-dental-radiograph.dcm"),
	)
}

func buildScaledDicomFixture() []byte {
	var payload bytes.Buffer

	payload.Write(make([]byte, 128))
	payload.WriteString("DICM")
	writeExplicitLittleElement(
		&payload,
		0x0002,
		0x0010,
		"UI",
		encodePaddedString("1.2.840.10008.1.2.1", 0x00),
	)
	writeExplicitLittleElement(&payload, 0x0028, 0x0010, "US", encodeLittleEndianUint16(512))
	writeExplicitLittleElement(&payload, 0x0028, 0x0011, "US", encodeLittleEndianUint16(768))
	writeExplicitLittleElement(
		&payload,
		0x0028,
		0x0004,
		"CS",
		encodePaddedString("MONOCHROME2", ' '),
	)
	writeExplicitLittleElement(
		&payload,
		0x0028,
		0x0030,
		"DS",
		encodePaddedString("0.25\\0.40", ' '),
	)
	writeExplicitLittleElement(&payload, 0x7fe0, 0x0010, "OB", nil)

	return payload.Bytes()
}

func writeExplicitLittleElement(
	payload *bytes.Buffer,
	group uint16,
	element uint16,
	vr string,
	value []byte,
) {
	writeLittleEndianUint16(payload, group)
	writeLittleEndianUint16(payload, element)
	payload.WriteString(vr)

	if vr == "OB" {
		payload.Write([]byte{0x00, 0x00})
		writeLittleEndianUint32(payload, uint32(len(value)))
	} else {
		writeLittleEndianUint16(payload, uint16(len(value)))
	}

	payload.Write(value)
}

func writeLittleEndianUint16(payload *bytes.Buffer, value uint16) {
	var raw [2]byte
	binary.LittleEndian.PutUint16(raw[:], value)
	payload.Write(raw[:])
}

func writeLittleEndianUint32(payload *bytes.Buffer, value uint32) {
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], value)
	payload.Write(raw[:])
}

func encodeLittleEndianUint16(value uint16) []byte {
	var raw [2]byte
	binary.LittleEndian.PutUint16(raw[:], value)
	return raw[:]
}

func encodePaddedString(value string, padding byte) []byte {
	raw := []byte(value)
	if len(raw)%2 != 0 {
		raw = append(raw, padding)
	}
	return raw
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

	var payload contracts.BackendError
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if payload.Code != contracts.BackendErrorCodeInvalidInput {
		t.Fatalf("code = %q, want %q", payload.Code, contracts.BackendErrorCodeInvalidInput)
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
