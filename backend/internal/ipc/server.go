package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	backendapp "xrayview/backend/internal/app"
	"xrayview/backend/internal/contracts"
)

const maxIPCLineBytes = 4 * 1024 * 1024

type Request struct {
	ID      json.RawMessage `json:"id,omitempty"`
	Command string          `json:"command"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Response struct {
	ID     json.RawMessage         `json:"id,omitempty"`
	OK     bool                    `json:"ok"`
	Result any                     `json:"result,omitempty"`
	Error  *contracts.BackendError `json:"error,omitempty"`
}

type Event struct {
	Event   string `json:"event"`
	Payload any    `json:"payload"`
}

// Server speaks newline-delimited JSON over stdio. Request/response IDs let a
// client multiplex synchronous calls, while job updates are emitted as event
// records on the same stream. The Wails desktop shell calls the backend in
// process instead; this server backs the headless CLI and is kept under test.
type Server struct {
	app        *backendapp.App
	dispatcher Dispatcher
}

func NewServer(app *backendapp.App) Server {
	return Server{app: app, dispatcher: NewDispatcher(app)}
}

func (server Server) Serve(input io.Reader, output io.Writer) error {
	writer := lockedWriter{output: output}
	subscription := server.app.SubscribeJobUpdates()
	defer server.app.UnsubscribeJobUpdates(subscription.ID)

	done := make(chan struct{})
	var eventsDone sync.WaitGroup
	eventsDone.Add(1)
	go func() {
		defer eventsDone.Done()
		for {
			select {
			case snapshot, ok := <-subscription.Receiver:
				if !ok {
					return
				}
				_ = writer.writeJSON(Event{Event: "job-update", Payload: snapshot})
			case <-done:
				return
			}
		}
	}()
	defer func() {
		close(done)
		eventsDone.Wait()
	}()

	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 0, 64*1024), maxIPCLineBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytesTrimSpace(line)) == 0 {
			continue
		}
		var request Request
		if err := json.Unmarshal(line, &request); err != nil {
			backendErr := contracts.InvalidInputError(fmt.Sprintf("invalid IPC request: %v", err))
			if writeErr := writer.writeJSON(Response{OK: false, Error: &backendErr}); writeErr != nil {
				return writeErr
			}
			continue
		}
		if len(request.ID) == 0 {
			request.ID = []byte("null")
		}
		result, err := server.dispatcher.Dispatch(request.Command, request.Payload)
		if err != nil {
			backendErr := toBackendError(err)
			if writeErr := writer.writeJSON(Response{ID: request.ID, OK: false, Error: &backendErr}); writeErr != nil {
				return writeErr
			}
			continue
		}
		if err := writer.writeJSON(Response{ID: request.ID, OK: true, Result: result}); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read IPC request: %w", err)
	}
	return nil
}

type lockedWriter struct {
	mu     sync.Mutex
	output io.Writer
}

func (writer *lockedWriter) writeJSON(value any) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	encoder := json.NewEncoder(writer.output)
	return encoder.Encode(value)
}

func toBackendError(err error) contracts.BackendError {
	if backendErr, ok := err.(contracts.BackendError); ok {
		return backendErr
	}
	return contracts.InternalError(err.Error())
}

func bytesTrimSpace(value []byte) []byte {
	start := 0
	end := len(value)
	for start < end && isSpace(value[start]) {
		start++
	}
	for end > start && isSpace(value[end-1]) {
		end--
	}
	return value[start:end]
}

func isSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r'
}
