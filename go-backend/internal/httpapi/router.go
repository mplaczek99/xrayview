package httpapi

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"xrayview/go-backend/internal/cache"
	"xrayview/go-backend/internal/config"
	"xrayview/go-backend/internal/contracts"
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

type backendError struct {
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Details     []string `json:"details,omitempty"`
	Recoverable bool     `json:"recoverable"`
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
			writeJSON(writer, http.StatusMethodNotAllowed, backendError{
				Code:        "invalidInput",
				Message:     "commands must be called with POST",
				Details:     []string{request.Method},
				Recoverable: true,
			})
			return
		}

		if commandName == "" || strings.Contains(commandName, "/") {
			writeJSON(writer, http.StatusNotFound, backendError{
				Code:        "notFound",
				Message:     "command not found",
				Recoverable: true,
			})
			return
		}

		if !contracts.IsSupportedCommand(commandName) {
			writeJSON(writer, http.StatusNotFound, backendError{
				Code:        "notFound",
				Message:     fmt.Sprintf("unsupported command: %s", commandName),
				Details:     contracts.SupportedCommandStrings(),
				Recoverable: true,
			})
			return
		}

		deps.Logger.Info("phase 7 placeholder command hit", slog.String("command", commandName))
		writeJSON(writer, http.StatusNotImplemented, backendError{
			Code:        "internal",
			Message:     fmt.Sprintf("command %s is not implemented in the Go backend yet", commandName),
			Details:     []string{"phase=7", "transport=" + TransportKind},
			Recoverable: true,
		})
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
