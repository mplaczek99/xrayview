package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"xrayview/backend/internal/cache"
	"xrayview/backend/internal/config"
	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/persistence"
)

type BackendService interface {
	OpenStudy(command contracts.OpenStudyCommand) (contracts.OpenStudyCommandResult, error)
	StartRenderJob(command contracts.RenderStudyCommand) (contracts.StartedJob, error)
	StartProcessJob(command contracts.ProcessStudyCommand) (contracts.StartedJob, error)
	StartAnalyzeJob(command contracts.AnalyzeStudyCommand) (contracts.StartedJob, error)
	GetJob(command contracts.JobCommand) (contracts.JobSnapshot, error)
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

func NewRouter(deps RouterDeps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, buildRuntimeResponse(deps))
	})

	mux.HandleFunc("GET "+RuntimePath, func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, buildRuntimeResponse(deps))
	})

	mux.HandleFunc("GET "+CommandsPath, func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, commandListResponse{
			Commands:               contracts.SupportedCommandStrings(),
			BackendContractVersion: contracts.BackendContractVersion,
		})
	})

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
	writer.Header().Set("content-type", "application/json; charset=utf-8")
	writer.WriteHeader(statusCode)

	if err := json.NewEncoder(writer).Encode(payload); err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}

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
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(payload); err != nil {
		return contracts.InvalidInput("invalid command payload").WithDetails(err.Error())
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return contracts.InvalidInput("invalid command payload").
				WithDetails("unexpected trailing JSON content")
		}

		return contracts.InvalidInput("invalid command payload").WithDetails(err.Error())
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
