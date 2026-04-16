package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

const serveAssetBenchmarkPayloadSize = 512 * 1024

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
