package httpapi

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"xrayview/backend/internal/contracts"
)

const (
	TransportKind           = "local-http-json"
	APIBasePath             = "/api/v1"
	RuntimePath             = APIBasePath + "/runtime"
	CommandsPath            = APIBasePath + "/commands"
	CommandEndpointTemplate = CommandsPath + "/{command}"
	EventsPath              = APIBasePath + "/events"
)

type statusCapturingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (writer *statusCapturingResponseWriter) WriteHeader(statusCode int) {
	writer.statusCode = statusCode
	writer.ResponseWriter.WriteHeader(statusCode)
}

func (writer *statusCapturingResponseWriter) Write(body []byte) (int, error) {
	if writer.statusCode == 0 {
		writer.statusCode = http.StatusOK
	}

	return writer.ResponseWriter.Write(body)
}

func wrapLocalTransport(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		recorder := &statusCapturingResponseWriter{ResponseWriter: writer}
		origin := strings.TrimSpace(request.Header.Get("Origin"))

		if origin != "" {
			if !isAllowedOrigin(origin) {
				writeJSON(
					recorder,
					http.StatusForbidden,
					contracts.InvalidInput(
						"request origin is not allowed for the local backend transport",
					).WithDetails(origin),
				)
				logRequest(logger, request, recorder.statusCode, startedAt, origin)
				return
			}

			applyCORSHeaders(recorder.Header(), origin)
		}

		if request.Method == http.MethodOptions {
			recorder.WriteHeader(http.StatusNoContent)
			logRequest(logger, request, recorder.statusCode, startedAt, origin)
			return
		}

		next.ServeHTTP(recorder, request)
		statusCode := recorder.statusCode
		if statusCode == 0 {
			statusCode = http.StatusOK
		}
		logRequest(logger, request, statusCode, startedAt, origin)
	})
}

func logRequest(
	logger *slog.Logger,
	request *http.Request,
	statusCode int,
	startedAt time.Time,
	origin string,
) {
	if logger == nil {
		return
	}

	attrs := []any{
		slog.String("transport", TransportKind),
		slog.String("method", request.Method),
		slog.String("path", request.URL.Path),
		slog.Int("status", statusCode),
		slog.Duration("duration", time.Since(startedAt)),
	}
	if origin != "" {
		attrs = append(attrs, slog.String("origin", origin))
	}

	logger.Info("backend transport request", attrs...)
}

func applyCORSHeaders(header http.Header, origin string) {
	addVary(header, "Origin")
	addVary(header, "Access-Control-Request-Method")
	addVary(header, "Access-Control-Request-Headers")
	header.Set("Access-Control-Allow-Origin", origin)
	header.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	header.Set("Access-Control-Allow-Headers", "Accept, Content-Type")
	header.Set("Access-Control-Max-Age", "600")
}

func addVary(header http.Header, value string) {
	for _, existing := range header.Values("Vary") {
		for _, part := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}

	header.Add("Vary", value)
}

func isAllowedOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}

	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
