package analysis

import (
	"testing"

	"xrayview/backend/internal/imaging"
)

func TestExtractBlackGapTracesFollowsBlackBandInterfaces(t *testing.T) {
	const width = uint32(80)
	const height = uint32(48)

	pixels := make([]uint8, width*height)
	for index := range pixels {
		pixels[index] = 180
	}
	for y := uint32(20); y < 26; y++ {
		for x := uint32(0); x < width; x++ {
			pixels[y*width+x] = 0
		}
	}
	for y := uint32(28); y < 38; y++ {
		for x := uint32(30); x < 42; x++ {
			pixels[y*width+x] = 0
		}
	}

	traces, err := ExtractBlackGapTraces(imaging.GrayPreview(width, height, pixels))
	if err != nil {
		t.Fatalf("ExtractBlackGapTraces returned error: %v", err)
	}
	if len(traces) < 2 {
		t.Fatalf("len(traces) = %d, want at least 2", len(traces))
	}

	openCount := 0
	for _, trace := range traces {
		if !trace.Closed {
			openCount++
		}
	}
	if openCount < 1 {
		t.Fatalf("openCount = %d, want at least 1", openCount)
	}
}
