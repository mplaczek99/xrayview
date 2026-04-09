package studies

import (
	"context"
	"sync"
	"testing"
	"time"

	"xrayview/backend/internal/dicommeta"
	"xrayview/backend/internal/imaging"
)

type countingDecoder struct {
	mu      sync.Mutex
	study   dicommeta.SourceStudy
	calls   int
	release <-chan struct{}
}

func (decoder *countingDecoder) DecodeStudy(
	ctx context.Context,
	_ string,
) (dicommeta.SourceStudy, error) {
	if err := ctx.Err(); err != nil {
		return dicommeta.SourceStudy{}, err
	}

	decoder.mu.Lock()
	decoder.calls++
	decoder.mu.Unlock()

	if decoder.release != nil {
		select {
		case <-ctx.Done():
			return dicommeta.SourceStudy{}, ctx.Err()
		case <-decoder.release:
		}
	}

	return decoder.study, nil
}

func TestDecodeCacheConcurrentGetOrDecodeCoalescesByPath(t *testing.T) {
	cache := NewDecodeCache(2)
	release := make(chan struct{})
	decoder := &countingDecoder{
		study:   testDecodedStudy(2, 2),
		release: release,
	}

	const workers = 8
	results := make(chan dicommeta.SourceStudy, workers)
	errs := make(chan error, workers)

	for index := 0; index < workers; index++ {
		go func() {
			study, err := cache.GetOrDecode(context.Background(), "/tmp/study.dcm", decoder)
			if err != nil {
				errs <- err
				return
			}
			results <- study
		}()
	}

	close(release)

	for index := 0; index < workers; index++ {
		select {
		case err := <-errs:
			t.Fatalf("GetOrDecode returned error: %v", err)
		case study := <-results:
			if got, want := study.Image.Width, uint32(2); got != want {
				t.Fatalf("study.Image.Width = %d, want %d", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent GetOrDecode did not complete before timeout")
		}
	}

	decoder.mu.Lock()
	defer decoder.mu.Unlock()
	if got, want := decoder.calls, 1; got != want {
		t.Fatalf("DecodeStudy calls = %d, want %d", got, want)
	}
}

func TestDecodeCacheEvictsLeastRecentlyUsedEntry(t *testing.T) {
	cache := NewDecodeCache(2)
	decoder := &countingDecoder{study: testDecodedStudy(2, 2)}

	if _, err := cache.GetOrDecode(context.Background(), "/tmp/a.dcm", decoder); err != nil {
		t.Fatalf("GetOrDecode a returned error: %v", err)
	}
	if _, err := cache.GetOrDecode(context.Background(), "/tmp/b.dcm", decoder); err != nil {
		t.Fatalf("GetOrDecode b returned error: %v", err)
	}
	if _, err := cache.GetOrDecode(context.Background(), "/tmp/a.dcm", decoder); err != nil {
		t.Fatalf("GetOrDecode a refresh returned error: %v", err)
	}
	if _, err := cache.GetOrDecode(context.Background(), "/tmp/c.dcm", decoder); err != nil {
		t.Fatalf("GetOrDecode c returned error: %v", err)
	}
	if got, want := cache.Len(), 2; got != want {
		t.Fatalf("Len = %d, want %d", got, want)
	}

	if _, err := cache.GetOrDecode(context.Background(), "/tmp/b.dcm", decoder); err != nil {
		t.Fatalf("GetOrDecode b after eviction returned error: %v", err)
	}

	decoder.mu.Lock()
	defer decoder.mu.Unlock()
	if got, want := decoder.calls, 4; got != want {
		t.Fatalf("DecodeStudy calls = %d, want %d", got, want)
	}
}

func TestDecodeCachePropagatesContextCancellation(t *testing.T) {
	cache := NewDecodeCache(1)
	decoder := &countingDecoder{release: make(chan struct{})}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := cache.GetOrDecode(ctx, "/tmp/study.dcm", decoder); err == nil {
		t.Fatal("GetOrDecode returned nil error, want context cancellation")
	}

	if got := cache.Len(); got != 0 {
		t.Fatalf("Len = %d, want 0 after cancelled decode", got)
	}
}

func testDecodedStudy(width, height uint32) dicommeta.SourceStudy {
	pixels := make([]float32, int(width*height))
	for index := range pixels {
		pixels[index] = float32(index)
	}

	return dicommeta.SourceStudy{
		Image: imaging.SourceImage{
			Width:    width,
			Height:   height,
			Format:   imaging.FormatGrayFloat32,
			Pixels:   pixels,
			MinValue: 0,
			MaxValue: float32(len(pixels)),
		},
	}
}
