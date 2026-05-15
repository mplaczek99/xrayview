package httpapi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xrayview/backend/internal/contracts"
)

func TestSSEHubBroadcastDeliveredWithinLatencyBudget(t *testing.T) {
	hub := newSSEHub(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.serveSSE(w, r)
	}))
	defer server.Close()

	// Connect a real SSE client using http.Get (no timeout on read).
	resp, err := http.Get(server.URL) //nolint:noctx
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	// Body is closed after reading the data frame so server.Close does not block.
	defer resp.Body.Close() //nolint:gocritic — closed again below for clarity

	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	// Fire the broadcast after the client is connected.
	want := contracts.JobSnapshot{
		JobID:   "sse-test-job-1",
		JobKind: contracts.JobKindRenderStudy,
		State:   contracts.JobStateRunning,
		Progress: contracts.JobProgress{
			Percent: 42,
			Stage:   "rendering",
			Message: "Rendering preview",
		},
	}

	start := time.Now()
	hub.broadcast(want)

	// Read the first data frame from the SSE stream.
	scanner := bufio.NewScanner(resp.Body)
	var dataLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			dataLine = line[6:]
			break
		}
	}
	if scanner.Err() != nil {
		t.Fatalf("scan error: %v", scanner.Err())
	}
	// Close early so server.Close() does not block.
	resp.Body.Close()

	elapsed := time.Since(start)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("event delivery latency = %v, want < 100ms", elapsed)
	}

	var got contracts.JobSnapshot
	if err := json.Unmarshal([]byte(dataLine), &got); err != nil {
		t.Fatalf("unmarshal SSE frame: %v", err)
	}
	if got.JobID != want.JobID {
		t.Fatalf("jobId = %q, want %q", got.JobID, want.JobID)
	}
	if got.State != want.State {
		t.Fatalf("state = %q, want %q", got.State, want.State)
	}
	if got.Progress.Percent != want.Progress.Percent {
		t.Fatalf("progress.percent = %d, want %d", got.Progress.Percent, want.Progress.Percent)
	}
}

func TestSSEHubMultipleClientsAllReceiveBroadcast(t *testing.T) {
	hub := newSSEHub(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.serveSSE(w, r)
	}))
	defer server.Close()

	const numClients = 3
	type clientConn struct {
		resp    *http.Response
		scanner *bufio.Scanner
	}
	clients := make([]clientConn, numClients)
	for i := range clients {
		resp, err := http.Get(server.URL) //nolint:noctx
		if err != nil {
			t.Fatalf("client %d GET: %v", i, err)
		}
		clients[i] = clientConn{resp: resp, scanner: bufio.NewScanner(resp.Body)}
	}

	hub.broadcast(contracts.JobSnapshot{
		JobID: "multi-client-job",
		State: contracts.JobStateCompleted,
	})

	for i, c := range clients {
		var dataLine string
		for c.scanner.Scan() {
			line := c.scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				dataLine = line[6:]
				break
			}
		}
		// Close connection immediately after reading the frame so server.Close
		// does not block waiting for the long-lived SSE connection.
		c.resp.Body.Close()

		var snap contracts.JobSnapshot
		if err := json.Unmarshal([]byte(dataLine), &snap); err != nil {
			t.Fatalf("client %d unmarshal: %v", i, err)
		}
		if snap.JobID != "multi-client-job" {
			t.Fatalf("client %d jobId = %q, want %q", i, snap.JobID, "multi-client-job")
		}
	}
}

func TestSSEHubClearsServerWriteTimeoutForLongLivedStream(t *testing.T) {
	hub := newSSEHub(nil)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.serveSSE(w, r)
	}))
	server.Config.WriteTimeout = 25 * time.Millisecond
	server.Start()
	defer server.Close()

	resp, err := http.Get(server.URL) //nolint:noctx
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}

	dataCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				dataCh <- line[6:]
				return
			}
		}
		if err := scanner.Err(); err != nil {
			errCh <- err
			return
		}
		errCh <- io.EOF
	}()

	time.Sleep(3 * server.Config.WriteTimeout)
	hub.broadcast(contracts.JobSnapshot{JobID: "after-write-timeout"})

	select {
	case dataLine := <-dataCh:
		var got contracts.JobSnapshot
		if err := json.Unmarshal([]byte(dataLine), &got); err != nil {
			t.Fatalf("unmarshal SSE frame: %v", err)
		}
		if got.JobID != "after-write-timeout" {
			t.Fatalf("jobId = %q, want %q", got.JobID, "after-write-timeout")
		}
	case err := <-errCh:
		t.Fatalf("read SSE frame: %v", err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for SSE frame after server write timeout elapsed")
	}
}

func TestSSEHubClientDisconnectDoesNotBlockBroadcast(t *testing.T) {
	hub := newSSEHub(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.serveSSE(w, r)
	}))
	defer server.Close()

	// Connect then immediately disconnect.
	resp, err := http.Get(server.URL) //nolint:noctx
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	// Give the hub a moment to process the disconnect.
	time.Sleep(10 * time.Millisecond)

	// Broadcast must complete quickly with no connected clients.
	done := make(chan struct{})
	go func() {
		hub.broadcast(contracts.JobSnapshot{JobID: "orphan-job"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("broadcast blocked after client disconnected")
	}
}

func TestSSEHubCountsAndLogsDroppedFramesForSlowConsumer(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	hub := newSSEHub(logger)
	hub.subscribe()

	for i := range sseClientBufferSize {
		hub.broadcast(contracts.JobSnapshot{
			JobID:   "buffer-fill-job",
			JobKind: contracts.JobKindRenderStudy,
			State:   contracts.JobStateRunning,
			Progress: contracts.JobProgress{
				Percent: i,
				Stage:   "rendering",
				Message: "Rendering preview",
			},
		})
	}

	if got := hub.droppedFrameCount(); got != 0 {
		t.Fatalf("droppedFrameCount after filling buffer = %d, want 0", got)
	}

	hub.broadcast(contracts.JobSnapshot{
		JobID:   "dropped-frame-job",
		JobKind: contracts.JobKindRenderStudy,
		State:   contracts.JobStateRunning,
	})

	if got := hub.droppedFrameCount(); got != 1 {
		t.Fatalf("droppedFrameCount after overflow = %d, want 1", got)
	}

	logOutput := logs.String()
	if !strings.Contains(logOutput, "xrayview_sse_dropped_frames_total=1") {
		t.Fatalf("log output missing cumulative dropped-frame counter: %q", logOutput)
	}
	if !strings.Contains(logOutput, "sse_client_buffer_size=16") {
		t.Fatalf("log output missing SSE client buffer size: %q", logOutput)
	}
}

func TestSSERouteRegisteredOnRouter(t *testing.T) {
	handler := testRouter(t)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, EventsPath, nil)
	// We only check the response does not 404; the test router has no service
	// that implements jobUpdateSubscriber so no events will arrive, but the
	// endpoint should exist and stream.
	// Close the body channel by cancelling the request context immediately —
	// use a recorder so we don't block.
	handler.ServeHTTP(recorder, request)

	// httptest.ResponseRecorder does not implement http.Flusher, so serveSSE
	// returns "streaming not supported" (500).  That is acceptable; what we're
	// verifying is that the path is registered (not 404 or 405).
	if recorder.Code == http.StatusNotFound || recorder.Code == http.StatusMethodNotAllowed {
		t.Fatalf("SSE endpoint not registered: status = %d", recorder.Code)
	}
}
