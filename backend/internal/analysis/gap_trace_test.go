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
	if len(traces) == 0 {
		t.Fatal("len(traces) = 0, want at least 1")
	}

	closedCount := 0
	edgeTouchCount := 0
	for _, trace := range traces {
		if trace.Closed {
			closedCount++
		}
		for _, point := range trace.Points {
			if point.X == 0 || point.Y == 0 || point.X == width || point.Y == height {
				edgeTouchCount++
				break
			}
		}
	}
	if closedCount < 1 {
		t.Fatalf("closedCount = %d, want at least 1", closedCount)
	}
	if edgeTouchCount < 1 {
		t.Fatalf("edgeTouchCount = %d, want at least 1", edgeTouchCount)
	}
}
