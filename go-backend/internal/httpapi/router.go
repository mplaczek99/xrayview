package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"xrayview/go-backend/internal/cache"
	"xrayview/go-backend/internal/config"
	"xrayview/go-backend/internal/contracts"
	"xrayview/go-backend/internal/dicommeta"
	"xrayview/go-backend/internal/jobs"
	"xrayview/go-backend/internal/persistence"
	"xrayview/go-backend/internal/studies"
)

type Dependencies struct {
	Config      config.Config
	Logger      *slog.Logger
	Cache       *cache.Store
	Persistence *persistence.Catalog
	Jobs        *jobs.Service
	Studies     *studies.Registry
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

func NewRouter(deps Dependencies) http.Handler {
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

		switch contracts.CommandName(commandName) {
		case contracts.CommandGetProcessingManifest:
			writeJSON(writer, http.StatusOK, contracts.DefaultProcessingManifest())
		case contracts.CommandOpenStudy:
			handleOpenStudy(writer, request, deps)
		case contracts.CommandStartRenderJob:
			handleStartRenderJob(writer, request, deps)
		case contracts.CommandStartProcessJob:
			handleStartProcessJob(writer, request, deps)
		case contracts.CommandGetJob:
			handleGetJob(writer, request, deps)
		case contracts.CommandCancelJob:
			handleCancelJob(writer, request, deps)
		default:
			deps.Logger.Info("go backend command not implemented", slog.String("command", commandName))
			writeJSON(writer, http.StatusNotImplemented, contracts.BackendError{
				Code:        contracts.BackendErrorCodeInternal,
				Message:     fmt.Sprintf("command %s is not implemented in the Go backend yet", commandName),
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

func buildRuntimeResponse(deps Dependencies) runtimeResponse {
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
		CacheDir:               deps.Cache.RootDir(),
		PersistenceDir:         deps.Persistence.RootDir(),
		SupportedCommands:      contracts.SupportedCommandStrings(),
		SupportedJobKinds:      deps.Jobs.SupportedKinds(),
		StudyCount:             deps.Studies.Count(),
		StartedAt:              deps.StartedAt.UTC().Format(time.RFC3339),
	}
}

func writeJSON(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("content-type", "application/json; charset=utf-8")
	writer.WriteHeader(statusCode)

	if err := json.NewEncoder(writer).Encode(payload); err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}

func handleOpenStudy(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	var command contracts.OpenStudyCommand
	if err := decodeJSONRequest(request, &command); err != nil {
		writeBackendError(writer, err)
		return
	}

	if command.InputPath == "" {
		writeBackendError(writer, contracts.InvalidInput("inputPath is required"))
		return
	}

	info, err := os.Stat(command.InputPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeBackendError(
				writer,
				contracts.NotFound(fmt.Sprintf("input file does not exist: %s", command.InputPath)),
			)
			return
		}

		writeBackendError(
			writer,
			contracts.Internal(fmt.Sprintf("failed to inspect input file %s: %v", command.InputPath, err)),
		)
		return
	}

	if info.IsDir() {
		writeBackendError(
			writer,
			contracts.InvalidInput(fmt.Sprintf("input path must be a file: %s", command.InputPath)),
		)
		return
	}

	metadata, err := dicommeta.ReadFile(command.InputPath)
	if err != nil {
		writeBackendError(
			writer,
			contracts.InvalidInput(fmt.Sprintf("failed to read DICOM metadata: %v", err)),
		)
		return
	}

	study, err := deps.Studies.Register(command.InputPath, metadata.MeasurementScale())
	if err != nil {
		writeBackendError(
			writer,
			contracts.Internal(fmt.Sprintf("failed to register study: %v", err)),
		)
		return
	}

	if err := deps.Persistence.RecordOpenedStudy(study); err != nil && deps.Logger != nil {
		deps.Logger.Warn(
			"failed to record opened study",
			slog.String("study_id", study.StudyID),
			slog.String("input_path", study.InputPath),
			slog.Any("error", err),
		)
	}

	writeJSON(writer, http.StatusOK, contracts.OpenStudyCommandResult{Study: study})
}

func handleStartRenderJob(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	var command contracts.RenderStudyCommand
	if err := decodeJSONRequest(request, &command); err != nil {
		writeBackendError(writer, err)
		return
	}

	started, err := deps.Jobs.StartRenderJob(command)
	if err != nil {
		writeBackendError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, started)
}

func handleStartProcessJob(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	var command contracts.ProcessStudyCommand
	if err := decodeJSONRequest(request, &command); err != nil {
		writeBackendError(writer, err)
		return
	}

	started, err := deps.Jobs.StartProcessJob(command)
	if err != nil {
		writeBackendError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, started)
}

func handleGetJob(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	var command contracts.JobCommand
	if err := decodeJSONRequest(request, &command); err != nil {
		writeBackendError(writer, err)
		return
	}

	snapshot, err := deps.Jobs.GetJob(command)
	if err != nil {
		writeBackendError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, snapshot)
}

func handleCancelJob(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	var command contracts.JobCommand
	if err := decodeJSONRequest(request, &command); err != nil {
		writeBackendError(writer, err)
		return
	}

	snapshot, err := deps.Jobs.CancelJob(command)
	if err != nil {
		writeBackendError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, snapshot)
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
