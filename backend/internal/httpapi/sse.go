package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"xrayview/backend/internal/contracts"
)

const sseClientBufferSize = 16

var sseFrameBufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

// sseHub fans out job-update events to all connected SSE clients.
// Each subscriber receives a buffered channel; frames are dropped for slow
// clients rather than blocking the broadcasting goroutine.
type sseHub struct {
	mu            sync.Mutex
	clients       map[chan []byte]struct{}
	logger        *slog.Logger
	droppedFrames atomic.Uint64
}

func newSSEHub(logger *slog.Logger) *sseHub {
	return &sseHub{
		clients: make(map[chan []byte]struct{}),
		logger:  logger,
	}
}

func (h *sseHub) subscribe() chan []byte {
	ch := make(chan []byte, sseClientBufferSize)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *sseHub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

// broadcast serialises snapshot and enqueues a SSE data frame to every client.
// Non-blocking: frames are dropped for clients whose buffer is full.
func (h *sseHub) broadcast(snapshot contracts.JobSnapshot) {
	h.mu.Lock()
	if len(h.clients) == 0 {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()

	buf := sseFrameBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	buf.WriteString("data: ")
	if err := json.NewEncoder(buf).Encode(snapshot); err != nil {
		sseFrameBufferPool.Put(buf)
		return
	}
	buf.WriteByte('\n')

	// SSE clients receive frames asynchronously, so copy the bytes before
	// returning the scratch buffer to the pool.
	frame := bytes.Clone(buf.Bytes())
	sseFrameBufferPool.Put(buf)

	h.mu.Lock()
	droppedFrames := 0
	clientCount := len(h.clients)
	for ch := range h.clients {
		select {
		case ch <- frame:
		default:
			droppedFrames++
		}
	}
	var droppedFramesTotal uint64
	if droppedFrames > 0 {
		droppedFramesTotal = h.droppedFrames.Add(uint64(droppedFrames))
	}
	logger := h.logger
	h.mu.Unlock()

	if droppedFrames > 0 && logger != nil {
		logger.Warn(
			"sse client frame buffer full; dropped job update frames",
			slog.Int("sse_dropped_frames", droppedFrames),
			slog.Uint64("xrayview_sse_dropped_frames_total", droppedFramesTotal),
			slog.Int("sse_client_count", clientCount),
			slog.Int("sse_client_buffer_size", sseClientBufferSize),
		)
	}
}

func (h *sseHub) droppedFrameCount() uint64 {
	return h.droppedFrames.Load()
}

// serveSSE upgrades the HTTP connection to a text/event-stream and streams
// job-update events until the client disconnects.
func (h *sseHub) serveSSE(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming not supported", http.StatusInternalServerError)
		return
	}
	// SSE streams are intentionally long-lived. Clear the server write timeout
	// for this request so an idle stream can still deliver a later job update.
	if err := http.NewResponseController(writer).SetWriteDeadline(time.Time{}); err != nil && h.logger != nil {
		h.logger.Debug("sse write deadline not adjustable", slog.String("error", err.Error()))
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	writer.Header().Set("X-Accel-Buffering", "no")
	writer.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := h.subscribe()
	defer h.unsubscribe(ch)

	ctx := request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-ch:
			if _, err := writer.Write(frame); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
