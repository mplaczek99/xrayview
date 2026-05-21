package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	backendapi "xrayview/contracts/contractv1"
)

const serveAssetBenchmarkPayloadSize = 512 * 1024
const sidecarTransportBurstSize = 16

func BenchmarkServeAssetFromDisk(b *testing.B) {
	previewPath := filepath.Join(b.TempDir(), "preview.png")
	payload := benchmarkPreviewPayload()
	if err := os.WriteFile(previewPath, payload, 0o644); err != nil {
		b.Fatalf("WriteFile returned error: %v", err)
	}

	app := &DesktopApp{}
	request := httptest.NewRequest(
		http.MethodGet,
		previewEndpointPath+"?path="+previewPath,
		nil,
	)

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		recorder := httptest.NewRecorder()
		app.ServeAsset(recorder, request)
		if recorder.Code != http.StatusOK {
			b.Fatalf("ServeAsset() status = %d, want %d", recorder.Code, http.StatusOK)
		}
		if recorder.Body.Len() != len(payload) {
			b.Fatalf("ServeAsset() body len = %d, want %d", recorder.Body.Len(), len(payload))
		}
	}
}

// BenchmarkServeAssetCacheHit measures the 304 Not Modified path (warm browser
// cache). Contrasts with BenchmarkServeAssetFromDisk (full 200 path).
func BenchmarkServeAssetCacheHit(b *testing.B) {
	previewPath := filepath.Join(b.TempDir(), "preview.png")
	payload := benchmarkPreviewPayload()
	if err := os.WriteFile(previewPath, payload, 0o644); err != nil {
		b.Fatalf("WriteFile returned error: %v", err)
	}

	app := &DesktopApp{}

	// Capture ETag from a cold request.
	cold := httptest.NewRecorder()
	app.ServeAsset(cold, httptest.NewRequest(
		http.MethodGet,
		previewEndpointPath+"?path="+previewPath,
		nil,
	))
	etag := cold.Header().Get("ETag")
	if etag == "" {
		b.Fatal("cold request returned no ETag")
	}

	b.ReportAllocs()
	b.SetBytes(0) // 304 body is empty
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		req := httptest.NewRequest(http.MethodGet, previewEndpointPath+"?path="+previewPath, nil)
		req.Header.Set("If-None-Match", etag)
		recorder := httptest.NewRecorder()
		app.ServeAsset(recorder, req)
		if recorder.Code != http.StatusNotModified {
			b.Fatalf("ServeAsset() cache hit status = %d, want %d", recorder.Code, http.StatusNotModified)
		}
	}
}

func BenchmarkServeAssetFromMemoryCandidate(b *testing.B) {
	payload := benchmarkPreviewPayload()

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		recorder := httptest.NewRecorder()
		servePreviewBytes(recorder, payload)
		if recorder.Code != http.StatusOK {
			b.Fatalf("servePreviewBytes() status = %d, want %d", recorder.Code, http.StatusOK)
		}
		if recorder.Body.Len() != len(payload) {
			b.Fatalf("servePreviewBytes() body len = %d, want %d", recorder.Body.Len(), len(payload))
		}
	}
}

func benchmarkPreviewPayload() []byte {
	pattern := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	repeatCount := serveAssetBenchmarkPayloadSize / len(pattern)
	payload := bytes.Repeat(pattern, repeatCount)
	if remainder := serveAssetBenchmarkPayloadSize % len(pattern); remainder > 0 {
		payload = append(payload, pattern[:remainder]...)
	}

	return payload
}

func servePreviewBytes(writer http.ResponseWriter, payload []byte) {
	writer.Header().Set("content-type", "image/png")
	writer.Header().Set("content-length", strconv.Itoa(len(payload)))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(payload)
}

// BenchmarkInvokeViaHTTP measures the full sidecar round-trip for a
// GetJobSnapshot call: marshal command → HTTP POST → decode JSON response.
func BenchmarkInvokeViaHTTP(b *testing.B) {
	snapshot := backendapi.JobSnapshot{JobID: "bench-job-1"}
	snapshotBody, err := json.Marshal(snapshot)
	if err != nil {
		b.Fatal(err)
	}

	health := backendHealth{
		Status:    "ok",
		Service:   expectedBackendService,
		Transport: expectedTransportKind,
	}
	healthBody, err := json.Marshal(health)
	if err != nil {
		b.Fatal(err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(healthBody)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(snapshotBody)
	}))
	defer ts.Close()

	sidecar := &SidecarController{
		mode:    runtimeModeDesktop,
		baseURL: ts.URL,
		probeClient: &http.Client{
			Timeout:   sidecarProbeTimeout,
			Transport: newSidecarTransport(),
		},
		httpClient: &http.Client{
			Timeout:   sidecarRequestTimeout,
			Transport: newSidecarTransport(),
		},
	}
	app := &DesktopApp{sidecar: sidecar}
	command := backendapi.JobCommand{JobID: "bench-job-1"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.GetJobSnapshot(command)
	}
}

// BenchmarkSidecarTransportBurstReuse measures bursty sidecar traffic where
// more concurrent requests finish idle than the transport pool can retain.
func BenchmarkSidecarTransportBurstReuse(b *testing.B) {
	var newConnCount atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConnCount.Add(1)
		}
	}
	server.Start()
	defer server.Close()

	transport := newSidecarTransport()
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Timeout:   sidecarRequestTimeout,
		Transport: transport,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var firstErr error
		var errOnce sync.Once
		var wg sync.WaitGroup
		wg.Add(sidecarTransportBurstSize)
		for requestIndex := 0; requestIndex < sidecarTransportBurstSize; requestIndex++ {
			go func() {
				defer wg.Done()

				response, err := client.Get(server.URL)
				if err != nil {
					errOnce.Do(func() {
						firstErr = err
					})
					return
				}
				defer response.Body.Close()
				if _, err := io.Copy(io.Discard, response.Body); err != nil {
					errOnce.Do(func() {
						firstErr = err
					})
					return
				}
				if response.StatusCode != http.StatusOK {
					errOnce.Do(func() {
						firstErr = errUnexpectedBenchmarkStatus(response.StatusCode)
					})
				}
			}()
		}
		wg.Wait()
		if firstErr != nil {
			b.Fatal(firstErr)
		}
	}

	b.ReportMetric(float64(newConnCount.Load())/float64(b.N), "new_conns/op")
}

func errUnexpectedBenchmarkStatus(statusCode int) error {
	return &unexpectedBenchmarkStatusError{statusCode: statusCode}
}

type unexpectedBenchmarkStatusError struct {
	statusCode int
}

func (err *unexpectedBenchmarkStatusError) Error() string {
	return "unexpected benchmark status: " + strconv.Itoa(err.statusCode)
}
