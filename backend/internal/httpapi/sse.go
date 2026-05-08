package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync"

	"xrayview/backend/internal/contracts"
)

var sseFrameBufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

// sseHub fans out job-update events to all connected SSE clients.
// Each subscriber receives a buffered channel; frames are dropped for slow
// clients rather than blocking the broadcasting goroutine.
type sseHub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func newSSEHub() *sseHub {
	return &sseHub{clients: make(map[chan []byte]struct{})}
}

func (h *sseHub) subscribe() chan []byte {
	ch := make(chan []byte, 16)
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
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- frame:
		default:
		}
	}
}

// serveSSE upgrades the HTTP connection to a text/event-stream and streams
// job-update events until the client disconnects.
func (h *sseHub) serveSSE(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming not supported", http.StatusInternalServerError)
		return
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
