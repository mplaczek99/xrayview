package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"xrayview/backend/internal/cache"
	"xrayview/backend/internal/config"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/persistence"
)

// bodyPool reuses request body read buffers across handler calls.
var bodyPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// jsonWriterEntry pools a bytes.Buffer + json.Encoder pair for writeJSON.
// The encoder's writer is permanently wired to the buffer; callers Reset()
// the buffer before reuse. bytes.Buffer.Write never fails, so enc.err
// remains nil across uses.
type jsonWriterEntry struct {
	buf *bytes.Buffer
	enc *json.Encoder
}

var jsonWriterPool = sync.Pool{New: func() any {
	buf := new(bytes.Buffer)
	return &jsonWriterEntry{buf: buf, enc: json.NewEncoder(buf)}
}}

// BackendService is the router's view of the command surface. It is a
// narrower mirror of app.BackendService so this package doesn't import
// internal/app (keeps the router independent of app wiring for tests).
// Keep the command methods in sync with app.BackendService. The extra
// methods on that interface (job update callbacks, study count, etc.) are
// picked up via optional type assertions further down this file rather
// than by adding them here.
type BackendService interface {
	OpenStudy(command contracts.OpenStudyCommand) (contracts.OpenStudyCommandResult, error)
	StartRenderJob(command contracts.RenderStudyCommand) (contracts.StartedJob, error)
	StartProcessJob(command contracts.ProcessStudyCommand) (contracts.StartedJob, error)
	StartAnalyzeJob(command contracts.AnalyzeStudyCommand) (contracts.StartedJob, error)
	GetJob(command contracts.JobCommand) (contracts.JobSnapshot, error)
	GetJobs(command contracts.GetJobsCommand) ([]contracts.JobSnapshot, error)
	CancelJob(command contracts.JobCommand) (contracts.JobSnapshot, error)
	GetProcessingManifest() contracts.ProcessingManifest
	MeasureLineAnnotation(
		command contracts.MeasureLineAnnotationCommand,
	) (contracts.MeasureLineAnnotationCommandResult, error)
}

type supportedJobKindsProvider interface {
	SupportedJobKinds() []string
}

type studyCountProvider interface {
	StudyCount() int
}

type RouterDeps struct {
	Service     BackendService
	Config      config.Config
	Logger      *slog.Logger
	Cache       *cache.Store
	Persistence *persistence.Catalog
	StartedAt   time.Time
}

type runtimeResponse struct {
	Status                 string   `json:"status"`
	Service                string   `json:"service"`
	Transport              string   `json:"transport"`
	LocalOnly              bool     `json:"localOnly"`
	BackendContractVersion int      `json:"backendContractVersion"`
	BackendContractSchema  string   `json:"backendContractSchemaId"`
	APIBasePath            string   `json:"apiBasePath"`
	CommandEndpoint        string   `json:"commandEndpoint"`
	ListenAddress          string   `json:"listenAddress"`
	CacheDir               string   `json:"cacheDir"`
	PersistenceDir         string   `json:"persistenceDir"`
	SupportedCommands      []string `json:"supportedCommands"`
	SupportedJobKinds      []string `json:"supportedJobKinds"`
	StudyCount             int      `json:"studyCount"`
	StartedAt              string   `json:"startedAt"`
}

type commandListResponse struct {
	Commands               []string `json:"commands"`
	BackendContractVersion int      `json:"backendContractVersion"`
}

// runtimeCacheEntry holds the pre-serialized JSON bytes for the runtime/healthz
// response together with the study count used to build them. An atomic.Value
// stores this so readers never take a lock on the hot path.
type runtimeCacheEntry struct {
	json       []byte
	studyCount int
}

// jobUpdateSubscriber is an optional interface that BackendService implementations
// may satisfy to receive all job-state transitions (progress + terminal).
// The router uses a type assertion so the interface is not required.
type jobUpdateSubscriber interface {
	OnJobUpdate(func(contracts.JobSnapshot))
}

func NewRouter(deps RouterDeps) http.Handler {
	mux := http.NewServeMux()

	// Wire the SSE hub. If the service supports OnJobUpdate, every job
	// transition (progress or terminal) is broadcast to connected SSE clients.
	hub := newSSEHub()
	if subscriber, ok := deps.Service.(jobUpdateSubscriber); ok {
		subscriber.OnJobUpdate(hub.broadcast)
	}

	// runtimeCache caches the serialized healthz/runtime JSON body keyed on
	// study count (the only field that changes at runtime). All other fields in
	// runtimeResponse are static after startup. atomic.Value gives lock-free
	// reads on the polling hot path; a harmless rebuild race on miss is fine
	// because the same study count always produces identical bytes.
	var runtimeCache atomic.Value // stores runtimeCacheEntry

	getRuntimeJSON := func() []byte {
		currentCount := resolveStudyCount(deps.Service)
		if entry, ok := runtimeCache.Load().(runtimeCacheEntry); ok && entry.studyCount == currentCount {
			return entry.json
		}
		data, _ := json.Marshal(buildRuntimeResponse(deps))
		data = append(data, '\n')
		runtimeCache.Store(runtimeCacheEntry{json: data, studyCount: currentCount})
		return data
	}

	writeRuntimeJSON := func(writer http.ResponseWriter) {
		data := getRuntimeJSON()
		writer.Header().Set("content-type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		writer.Write(data) //nolint:errcheck
	}

	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, request *http.Request) {
		writeRuntimeJSON(writer)
	})

	// /preview serves cached preview artifacts to browser clients that talk to
	// the loopback backend directly. The desktop shell has its own /preview
	// handler with different trust assumptions; do not reuse that implementation
	// here.
	mux.HandleFunc("GET "+PreviewPath, newPreviewHandler(deps.Cache, deps.Config))

	mux.HandleFunc("GET "+RuntimePath, func(writer http.ResponseWriter, request *http.Request) {
		writeRuntimeJSON(writer)
	})

	mux.HandleFunc("GET "+EventsPath, func(writer http.ResponseWriter, request *http.Request) {
		hub.serveSSE(writer, request)
	})

	mux.HandleFunc("GET "+CommandsPath, func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, commandListResponse{
			Commands:               contracts.SupportedCommandStrings(),
			BackendContractVersion: contracts.BackendContractVersion,
		})
	})

	// Single dispatch table for every backend command. To add a new command:
	// declare it in contracts/backend-contract-v1.schema.json, run
	// `npm run contracts:generate` to refresh the TS + Go bindings, then add
	// a case below plus a handleXxx that decodes the payload and forwards to
	// deps.Service. Anything that isn't a POST to a known command name is
	// rejected here before it reaches the service.
	mux.HandleFunc(CommandsPath+"/", func(writer http.ResponseWriter, request *http.Request) {
		commandName := strings.TrimPrefix(request.URL.Path, CommandsPath+"/")
		if request.Method != http.MethodPost {
			writeJSON(
				writer,
				http.StatusMethodNotAllowed,
				contracts.InvalidInput("commands must be called with POST").WithDetails(request.Method),
			)
			return
		}

		if commandName == "" || strings.Contains(commandName, "/") {
			writeJSON(writer, http.StatusNotFound, contracts.NotFound("command not found"))
			return
		}

		if !contracts.IsSupportedCommand(commandName) {
			writeJSON(
				writer,
				http.StatusNotFound,
				contracts.NotFound(fmt.Sprintf("unsupported command: %s", commandName)).
					WithDetails(contracts.SupportedCommandStrings()...),
			)
			return
		}
		if deps.Service == nil {
			writeJSON(
				writer,
				http.StatusInternalServerError,
				contracts.Internal("backend service is not configured"),
			)
			return
		}

		switch contracts.CommandName(commandName) {
		case contracts.CommandGetProcessingManifest:
			writeJSON(writer, http.StatusOK, deps.Service.GetProcessingManifest())
		case contracts.CommandOpenStudy:
			handleOpenStudy(writer, request, deps)
		case contracts.CommandStartRenderJob:
			handleStartRenderJob(writer, request, deps)
		case contracts.CommandStartProcessJob:
			handleStartProcessJob(writer, request, deps)
		case contracts.CommandStartAnalyzeJob:
			handleStartAnalyzeJob(writer, request, deps)
		case contracts.CommandGetJob:
			handleGetJob(writer, request, deps)
		case contracts.CommandGetJobs:
			handleGetJobs(writer, request, deps)
		case contracts.CommandCancelJob:
			handleCancelJob(writer, request, deps)
		case contracts.CommandMeasureLineAnnotation:
			handleMeasureLineAnnotation(writer, request, deps)
		default:
			deps.Logger.Info("backend command not implemented", slog.String("command", commandName))
			writeJSON(writer, http.StatusNotImplemented, contracts.BackendError{
				Code:        contracts.BackendErrorCodeInternal,
				Message:     fmt.Sprintf("command %s is not implemented in the backend yet", commandName),
				Details:     []string{"transport=" + TransportKind},
				Recoverable: true,
			})
		}
	})

	mux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/" {
			http.NotFound(writer, request)
			return
		}

		writeJSON(writer, http.StatusOK, map[string]any{
			"service": contracts.ServiceName,
			"status":  "ok",
		})
	})

	return wrapLocalTransport(mux, deps.Logger)
}

func buildRuntimeResponse(deps RouterDeps) runtimeResponse {
	cacheDir := deps.Config.Paths.CacheDir
	if deps.Cache != nil {
		cacheDir = deps.Cache.RootDir()
	}

	persistenceDir := deps.Config.Paths.PersistenceDir
	if deps.Persistence != nil {
		persistenceDir = deps.Persistence.RootDir()
	}

	return runtimeResponse{
		Status:                 "ok",
		Service:                deps.Config.ServiceName,
		Transport:              TransportKind,
		LocalOnly:              true,
		BackendContractVersion: contracts.BackendContractVersion,
		BackendContractSchema:  contracts.BackendContractSchemaID,
		APIBasePath:            APIBasePath,
		CommandEndpoint:        CommandEndpointTemplate,
		ListenAddress:          deps.Config.ListenAddress(),
		CacheDir:               cacheDir,
		PersistenceDir:         persistenceDir,
		SupportedCommands:      contracts.SupportedCommandStrings(),
		SupportedJobKinds:      resolveSupportedJobKinds(deps.Service),
		StudyCount:             resolveStudyCount(deps.Service),
		StartedAt:              deps.StartedAt.UTC().Format(time.RFC3339),
	}
}

func resolveSupportedJobKinds(service BackendService) []string {
	if provider, ok := service.(supportedJobKindsProvider); ok {
		return provider.SupportedJobKinds()
	}

	return []string{
		string(contracts.JobKindRenderStudy),
		string(contracts.JobKindProcessStudy),
		string(contracts.JobKindAnalyzeStudy),
	}
}

func resolveStudyCount(service BackendService) int {
	if provider, ok := service.(studyCountProvider); ok {
		return provider.StudyCount()
	}

	return 0
}

func writeJSON(writer http.ResponseWriter, statusCode int, payload any) {
	je := jsonWriterPool.Get().(*jsonWriterEntry)
	je.buf.Reset()
	if err := je.enc.Encode(payload); err != nil {
		jsonWriterPool.Put(je)
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	writer.Header().Set("content-type", "application/json; charset=utf-8")
	writer.WriteHeader(statusCode)
	writer.Write(je.buf.Bytes()) //nolint:errcheck
	jsonWriterPool.Put(je)
}

// Every handleXxx below follows the same shape: decode the request body
// into a contracts.*Command, forward to the service, map any error through
// writeBackendError, and otherwise write the result as JSON. If you're
// adding a new command, copy this and swap the types.
func handleOpenStudy(writer http.ResponseWriter, request *http.Request, deps RouterDeps) {
	var command contracts.OpenStudyCommand
	if err := decodeJSONRequest(request, &command); err != nil {
		writeBackendError(writer, err)
		return
	}

	result, err := deps.Service.OpenStudy(command)
	if err != nil {
		writeBackendError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, result)
}

func handleStartRenderJob(writer http.ResponseWriter, request *http.Request, deps RouterDeps) {
	var command contracts.RenderStudyCommand
	if err := decodeJSONRequest(request, &command); err != nil {
		writeBackendError(writer, err)
		return
	}

	started, err := deps.Service.StartRenderJob(command)
	if err != nil {
		writeBackendError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, started)
}

func handleStartProcessJob(writer http.ResponseWriter, request *http.Request, deps RouterDeps) {
	var command contracts.ProcessStudyCommand
	if err := decodeJSONRequest(request, &command); err != nil {
		writeBackendError(writer, err)
		return
	}

	started, err := deps.Service.StartProcessJob(command)
	if err != nil {
		writeBackendError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, started)
}

func handleStartAnalyzeJob(writer http.ResponseWriter, request *http.Request, deps RouterDeps) {
	var command contracts.AnalyzeStudyCommand
	if err := decodeJSONRequest(request, &command); err != nil {
		writeBackendError(writer, err)
		return
	}

	started, err := deps.Service.StartAnalyzeJob(command)
	if err != nil {
		writeBackendError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, started)
}

func handleGetJob(writer http.ResponseWriter, request *http.Request, deps RouterDeps) {
	var command contracts.JobCommand
	if err := decodeJSONRequest(request, &command); err != nil {
		writeBackendError(writer, err)
		return
	}

	snapshot, err := deps.Service.GetJob(command)
	if err != nil {
		writeBackendError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, snapshot)
}

func handleGetJobs(writer http.ResponseWriter, request *http.Request, deps RouterDeps) {
	var command contracts.GetJobsCommand
	if err := decodeJSONRequest(request, &command); err != nil {
		writeBackendError(writer, err)
		return
	}

	snapshots, err := deps.Service.GetJobs(command)
	if err != nil {
		writeBackendError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, snapshots)
}

func handleCancelJob(writer http.ResponseWriter, request *http.Request, deps RouterDeps) {
	var command contracts.JobCommand
	if err := decodeJSONRequest(request, &command); err != nil {
		writeBackendError(writer, err)
		return
	}

	snapshot, err := deps.Service.CancelJob(command)
	if err != nil {
		writeBackendError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, snapshot)
}

func handleMeasureLineAnnotation(writer http.ResponseWriter, request *http.Request, deps RouterDeps) {
	var command contracts.MeasureLineAnnotationCommand
	if err := decodeJSONRequest(request, &command); err != nil {
		writeBackendError(writer, err)
		return
	}

	result, err := deps.Service.MeasureLineAnnotation(command)
	if err != nil {
		writeBackendError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, result)
}

func decodeJSONRequest(request *http.Request, payload any) error {
	buf := bodyPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bodyPool.Put(buf)

	if _, err := buf.ReadFrom(request.Body); err != nil {
		return contracts.InvalidInput("invalid command payload").WithDetails(err.Error())
	}

	// Capture the full body slice before the decoder reads from buf.
	// After Decode, InputOffset() is the byte position from buf's origin,
	// so bodyBytes[InputOffset():] gives all trailing bytes without alloc.
	bodyBytes := buf.Bytes()
	decoder := json.NewDecoder(buf)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(payload); err != nil {
		return contracts.InvalidInput("invalid command payload").WithDetails(err.Error())
	}

	// Fast trailing-content check: scan raw bytes from InputOffset instead of a
	// second full decoder pass. InputOffset returns the exact byte position
	// after the decoded value, covering all body bytes without extra allocation.
	for _, b := range bodyBytes[decoder.InputOffset():] {
		if b != ' ' && b != '\t' && b != '\r' && b != '\n' {
			return contracts.InvalidInput("invalid command payload").
				WithDetails("unexpected trailing JSON content")
		}
	}

	return nil
}

func writeBackendError(writer http.ResponseWriter, err error) {
	backendErr, ok := err.(contracts.BackendError)
	if !ok {
		backendErr = contracts.Internal(err.Error())
	}

	writeJSON(writer, statusCodeForBackendError(backendErr), backendErr)
}

func statusCodeForBackendError(err contracts.BackendError) int {
	switch err.Code {
	case contracts.BackendErrorCodeInvalidInput:
		return http.StatusBadRequest
	case contracts.BackendErrorCodeNotFound:
		return http.StatusNotFound
	case contracts.BackendErrorCodeConflict:
		return http.StatusConflict
	case contracts.BackendErrorCodeCancelled:
		return http.StatusConflict
	case contracts.BackendErrorCodeCacheCorrupted:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
