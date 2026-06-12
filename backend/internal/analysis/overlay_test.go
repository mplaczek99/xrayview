package analysis

import (
	"strings"
	"testing"

	"xrayview/backend/internal/render"
)

// Smoke test: a 32×32 image with a bright smooth square (tooth-like — bright and
// low-texture) confirms the pipeline runs end-to-end and returns valid RGBA
// previews. We assert the tooth side fires on the obvious bright blob; we do NOT
// assert bone, since the bone forest keys on trabecular texture a synthetic toy
// image cannot reproduce.
func TestGenerateToothOverlayReturnsOverlayImages(t *testing.T) {
	result, err := GenerateToothOverlay(render.Gray(32, 32, analysisFixtureGray()))
	if err != nil {
		t.Fatal(err)
	}
	if result.Preview.Width != 32 || result.Preview.Height != 32 || result.Preview.Format != render.RGBA8 {
		t.Fatalf("preview = %+v", result.Preview)
	}
	if result.FilledPreview.Width != 32 || result.FilledPreview.Height != 32 || result.FilledPreview.Format != render.RGBA8 {
		t.Fatalf("filled preview = %+v", result.FilledPreview)
	}
	if len(result.Preview.Pixels) != 32*32*4 || len(result.FilledPreview.Pixels) != 32*32*4 {
		t.Fatalf("preview lengths = %d/%d", len(result.Preview.Pixels), len(result.FilledPreview.Pixels))
	}
	if result.ToothPixels <= 0 {
		t.Fatalf("tooth pixels = %d", result.ToothPixels)
	}
	if result.Coverage <= 0 {
		t.Fatalf("coverage = %f", result.Coverage)
	}
	if result.CandidateCount <= 0 {
		t.Fatalf("candidate count = %d", result.CandidateCount)
	}
	if !strings.HasPrefix(result.Mode, "dynamic tooth and bone level overlay") {
		t.Fatalf("mode = %q", result.Mode)
	}
}

func TestGenerateToothOverlayRejectsInvalidInputs(t *testing.T) {
	if _, err := GenerateToothOverlay(render.RGBA(8, 8, make([]byte, 8*8*4))); err == nil || !strings.Contains(err.Error(), "Gray8") {
		t.Fatalf("wrong format error = %v", err)
	}
	if _, err := GenerateToothOverlay(render.Gray(7, 8, make([]byte, 7*8))); err == nil || !strings.Contains(err.Error(), "too small") {
		t.Fatalf("small image error = %v", err)
	}
	if _, err := GenerateToothOverlay(render.Gray(8, 8, make([]byte, 63))); err == nil || !strings.Contains(err.Error(), "preview pixel length") {
		t.Fatalf("length error = %v", err)
	}
}

func TestOverlayPreviewSuppressesBoneInsideTooth(t *testing.T) {
	const width = 8
	const height = 8
	gray := make([]byte, width*height)
	for index := range gray {
		gray[index] = 64
	}
	toothMask := make([]bool, width*height)
	boneMask := make([]bool, width*height)
	fillMaskRect(toothMask, width, 1, 1, 6, 6)
	boneMask[4*width+4] = true

	preview := overlayPreview(gray, width, height, toothMask, boneMask, false, newMaskBuffers(width*height))

	if got := rgbaAt(preview.Pixels, width, 4, 4); got != [4]byte{64, 64, 64, 255} {
		t.Fatalf("bone inside tooth pixel = %v", got)
	}
	if got := rgbaAt(preview.Pixels, width, 1, 4); got != [4]byte{toothGreen, 255, 0, 255} {
		t.Fatalf("tooth outline pixel = %v", got)
	}
}

func TestFilledOverlayDrawsOutlinesOverBlendedSections(t *testing.T) {
	const width = 24
	const height = 30
	gray := make([]byte, width*height)
	for index := range gray {
		gray[index] = 100
	}
	toothMask := make([]bool, width*height)
	boneMask := make([]bool, width*height)
	fillMaskRect(toothMask, width, 5, 5, 10, 10)
	fillMaskRect(boneMask, width, 5, 20, 10, 6)

	preview := overlayPreview(gray, width, height, toothMask, boneMask, true, newMaskBuffers(width*height))

	if got := rgbaAt(preview.Pixels, width, 10, 10); got != [4]byte{109, 170, 55, 255} {
		t.Fatalf("tooth fill pixel = %v", got)
	}
	if got := rgbaAt(preview.Pixels, width, 10, 23); got != [4]byte{170, 55, 55, 255} {
		t.Fatalf("bone fill pixel = %v", got)
	}
	if got := rgbaAt(preview.Pixels, width, 5, 20); got != [4]byte{boneRed, 0, 0, 255} {
		t.Fatalf("bone outline pixel = %v", got)
	}
	if got := rgbaAt(preview.Pixels, width, 5, 5); got != [4]byte{toothGreen, 255, 0, 255} {
		t.Fatalf("tooth outline pixel = %v", got)
	}
}

func TestClipToothToBoneLevelKeepsCrownReclassifiesRoot(t *testing.T) {
	const width = 9
	const height = 10
	tooth := make([]bool, width*height)
	bone := make([]bool, width*height)
	fillMaskRect(tooth, width, 3, 0, 3, height) // one vertical tooth, crown to root
	fillMaskRect(bone, width, 0, 5, 3, 5)       // interdental bone, lower-left
	fillMaskRect(bone, width, 6, 5, 3, 5)       // interdental bone, lower-right

	clipToothToBoneLevel(tooth, bone, width, height)

	// Crown (top): open background is nearer than bone, so it stays tooth.
	if !tooth[1*width+4] {
		t.Fatalf("crown pixel was clipped")
	}
	// Root (bottom): bone flanks both sides, so it is reclassified as bone.
	if tooth[8*width+4] || !bone[8*width+4] {
		t.Fatalf("root pixel not reclassified to bone: tooth=%v bone=%v", tooth[8*width+4], bone[8*width+4])
	}
}

func TestClipToothToBoneLevelKeepsAllWhenNoBone(t *testing.T) {
	const width = 8
	const height = 8
	tooth := make([]bool, width*height)
	bone := make([]bool, width*height)
	fillMaskRect(tooth, width, 2, 2, 4, 4)
	before := countMask(tooth)

	clipToothToBoneLevel(tooth, bone, width, height)

	if got := countMask(tooth); got != before {
		t.Fatalf("tooth changed without bone: before=%d after=%d", before, got)
	}
}

func analysisFixtureGray() []byte {
	pixels := make([]byte, 32*32)
	for index := range pixels {
		pixels[index] = 24
	}
	for y := 8; y < 24; y++ {
		for x := 9; x < 23; x++ {
			pixels[y*32+x] = 210
		}
	}
	return pixels
}

func fillMaskRect(mask []bool, width, x, y, rectWidth, rectHeight int) {
	for yy := y; yy < y+rectHeight; yy++ {
		for xx := x; xx < x+rectWidth; xx++ {
			mask[yy*width+xx] = true
		}
	}
}

func rgbaAt(pixels []byte, width, x, y int) [4]byte {
	offset := (y*width + x) * 4
	return [4]byte{pixels[offset], pixels[offset+1], pixels[offset+2], pixels[offset+3]}
}
