package analysis

import (
	"image"
	"image/color"
	_ "image/png"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	_ "golang.org/x/image/bmp"

	"xrayview/backend/internal/imaging"
)

const minimumFixtureDice = 0.95
const minimumBoneFixtureDice = 0.95

func TestDetectedToothMaskUsesDynamicMaskForFixtures(t *testing.T) {
	for _, name := range coloredFixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			bmpPath := fixturePath(t, "images", "BMP", name+".bmp")
			pngPath := fixturePath(t, "images", "PNG", "Colored", name+".png")
			if _, err := os.Stat(bmpPath); err != nil {
				t.Skipf("missing BMP fixture: %v", err)
			}
			if _, err := os.Stat(pngPath); err != nil {
				t.Skipf("missing colored PNG fixture: %v", err)
			}

			preview := decodeGrayFixture(t, bmpPath)
			gray, err := grayPixels(preview)
			if err != nil {
				t.Fatalf("grayPixels returned error: %v", err)
			}
			gotMask := detectToothMask(gray, int(preview.Width), int(preview.Height))

			wantMask := greenMaskFromImage(t, pngPath)
			dice := diceCoefficient(gotMask, wantMask)
			if dice < minimumFixtureDice {
				t.Fatalf("green mask Dice = %.3f, want >= %.2f", dice, minimumFixtureDice)
			}
			if name == "1" && dice < 0.95 {
				t.Fatalf("1.bmp green mask Dice = %.3f, want >= 0.95", dice)
			}
			coverage := float64(countMaskPixels(gotMask)) / float64(maxInt(len(gotMask), 1))
			if name == "1" && coverage > 0.70 {
				t.Fatalf("1.bmp coverage = %.3f, want <= 0.70 to avoid green flood", coverage)
			}
		})
	}
}

func TestDetectedBoneLevelMaskUsesDynamicMaskForFixtures(t *testing.T) {
	for _, name := range coloredFixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			bmpPath := fixturePath(t, "images", "BMP", name+".bmp")
			pngPath := fixturePath(t, "images", "PNG", "Colored", name+".png")
			if _, err := os.Stat(bmpPath); err != nil {
				t.Skipf("missing BMP fixture: %v", err)
			}
			if _, err := os.Stat(pngPath); err != nil {
				t.Skipf("missing colored PNG fixture: %v", err)
			}

			preview := decodeGrayFixture(t, bmpPath)
			gray, err := grayPixels(preview)
			if err != nil {
				t.Fatalf("grayPixels returned error: %v", err)
			}
			gotMask := detectBoneLevelMask(gray, int(preview.Width), int(preview.Height))

			wantMask := redMaskFromImage(t, pngPath)
			dice := diceCoefficient(gotMask, wantMask)
			t.Logf("red mask Dice = %.3f", dice)
			if dice < minimumBoneFixtureDice {
				t.Fatalf("red mask Dice = %.3f, want >= %.2f", dice, minimumBoneFixtureDice)
			}
		})
	}
}

func TestGenerateToothOverlayDrawsBoneLevelRedOutlineForFixtures(t *testing.T) {
	for _, name := range coloredFixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			bmpPath := fixturePath(t, "images", "BMP", name+".bmp")
			if _, err := os.Stat(bmpPath); err != nil {
				t.Skipf("missing BMP fixture: %v", err)
			}

			preview := decodeGrayFixture(t, bmpPath)
			result, err := GenerateToothOverlay(preview)
			if err != nil {
				t.Fatalf("GenerateToothOverlay returned error: %v", err)
			}

			gray, err := grayPixels(preview)
			if err != nil {
				t.Fatalf("grayPixels returned error: %v", err)
			}
			toothMask := detectToothMask(gray, int(preview.Width), int(preview.Height))
			boneMask := detectBoneLevelMask(gray, int(preview.Width), int(preview.Height))
			wantMask := dilateBinaryMask(
				boneOutlineMask(toothMask, boneMask, int(preview.Width), int(preview.Height)),
				int(preview.Width),
				int(preview.Height),
				3,
			)
			gotMask := redDominantMaskFromRGBA(result.Preview)
			nearby := maskContainmentRatio(gotMask, wantMask)
			if nearby < 0.80 {
				t.Fatalf("red contour nearby ratio = %.3f, want >= 0.80", nearby)
			}
		})
	}
}

func TestGenerateToothOverlayDrawsToothGreenOutlineAndBlocksInternalBoneRedForFixtures(t *testing.T) {
	for _, name := range coloredFixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			bmpPath := fixturePath(t, "images", "BMP", name+".bmp")
			if _, err := os.Stat(bmpPath); err != nil {
				t.Skipf("missing BMP fixture: %v", err)
			}

			preview := decodeGrayFixture(t, bmpPath)
			result, err := GenerateToothOverlay(preview)
			if err != nil {
				t.Fatalf("GenerateToothOverlay returned error: %v", err)
			}

			gray, err := grayPixels(preview)
			if err != nil {
				t.Fatalf("grayPixels returned error: %v", err)
			}
			toothMask := detectToothMask(gray, int(preview.Width), int(preview.Height))
			redMask := redDominantMaskFromRGBA(result.Preview)
			greenMask := greenDominantMaskFromRGBA(result.Preview)
			toothOutlineMask := innerOutlineMask(toothMask, int(preview.Width), int(preview.Height), toothOutlineThicknessPixels)
			nearbyMask := dilateBinaryMask(toothOutlineMask, int(preview.Width), int(preview.Height), 3)
			nearby := maskContainmentRatio(greenMask, nearbyMask)
			if nearby < 0.70 {
				t.Fatalf("green contour nearby ratio = %.3f, want >= 0.70", nearby)
			}
			for index, value := range toothMask {
				if value != 0 && toothOutlineMask[index] == 0 && redMask[index] != 0 {
					t.Fatalf("red bone overlay was drawn inside filled tooth at index %d", index)
				}
			}
		})
	}
}

func TestOverlayMasksOutlinesToothAndSuppressesBoneInsideTooth(t *testing.T) {
	const width = 9
	const height = 9

	gray := make([]uint8, width*height)
	toothMask := make([]uint8, width*height)
	boneMask := make([]uint8, width*height)
	fillMaskRect(toothMask, width, 1, 1, 7, 7)
	boneMask[4*width+4] = 1
	boneMask[0] = 1

	preview := overlayMasks(gray, toothMask, boneMask, width, height)
	redMask := redDominantMaskFromRGBA(preview)
	greenMask := greenDominantMaskFromRGBA(preview)
	if redMask[0] != 0 {
		t.Fatal("small isolated bone speckle was outlined red")
	}
	if redMask[4*width+4] != 0 {
		t.Fatal("bone pixel inside tooth was drawn red")
	}
	if greenMask[1*width+1] == 0 || greenMask[7*width+7] == 0 {
		t.Fatal("tooth outline was not drawn green")
	}
	if greenMask[4*width+4] != 0 {
		t.Fatal("tooth interior was filled green")
	}
}

func TestOverlayMasksDrawsOneCleanBoneOutlineWithoutInternalLoops(t *testing.T) {
	const width = 9
	const height = 9

	gray := make([]uint8, width*height)
	toothMask := make([]uint8, width*height)
	boneMask := make([]uint8, width*height)
	fillMaskRect(boneMask, width, 2, 2, 5, 5)
	fillMaskRect(toothMask, width, 3, 3, 3, 3)
	boneMask[4*width+4] = 0
	boneMask[0] = 1

	preview := overlayMasks(gray, toothMask, boneMask, width, height)
	redMask := redDominantMaskFromRGBA(preview)
	greenMask := greenDominantMaskFromRGBA(preview)
	if redMask[0] != 0 {
		t.Fatal("small isolated bone component was outlined red")
	}
	if countMaskPixels(redMask) == 0 {
		t.Fatal("main bone level boundary was not outlined red")
	}
	if redMask[4*width+4] != 0 {
		t.Fatal("internal bone hole was outlined red")
	}
	if redMask[4*width+3] != 0 || redMask[4*width+5] != 0 {
		t.Fatal("red bone overlay was drawn inside tooth interior")
	}
	if greenMask[3*width+3] == 0 || greenMask[5*width+5] == 0 {
		t.Fatal("tooth boundary was not outlined green")
	}
}

func TestOverlayMasksDrawsAntialiasedSmoothedContours(t *testing.T) {
	const width = 24
	const height = 24

	gray := make([]uint8, width*height)
	toothMask := make([]uint8, width*height)
	fillMaskRect(toothMask, width, 6, 6, 12, 12)

	preview := overlayMasks(gray, toothMask, nil, width, height)
	partialGreenPixels := 0
	for index := 0; index < len(preview.Pixels)/4; index++ {
		base := index * 4
		if isPartialGreenOverlayPixel(preview.Pixels[base], preview.Pixels[base+1], preview.Pixels[base+2]) {
			partialGreenPixels++
		}
	}
	if partialGreenPixels == 0 {
		t.Fatal("smoothed contour did not produce antialiased green edge pixels")
	}
}

func TestSmoothClosedContourReducesHighFrequencyJitter(t *testing.T) {
	points := make([]overlayPoint, 0, 48)
	for y := 0; y <= 24; y += 2 {
		x := 4.0
		if y%4 == 0 {
			x = 7.0
		}
		points = append(points, overlayPoint{x: x, y: float64(y)})
	}
	for y := 24; y >= 0; y -= 2 {
		points = append(points, overlayPoint{x: 22, y: float64(y)})
	}

	before := contourJitterScore(points)
	after := contourJitterScore(smoothClosedContour(
		lowPassClosedContour(points, overlayContourLowPassIterations),
		overlayContourSmoothingIterations,
	))
	if after >= before*0.18 {
		t.Fatalf("contour jitter score after smoothing = %.3f, want < %.3f", after, before*0.18)
	}
}

func TestGenerateToothOverlayDrawsInnerOutline(t *testing.T) {
	preview := imaging.GrayPreview(32, 32, make([]uint8, 32*32))
	result, err := GenerateToothOverlay(preview)
	if err != nil {
		t.Fatalf("GenerateToothOverlay returned error: %v", err)
	}
	if result.Preview.Format != imaging.FormatRGBA8 {
		t.Fatalf("Preview.Format = %q, want %q", result.Preview.Format, imaging.FormatRGBA8)
	}
	if !strings.Contains(result.Mode, "tooth and bone level") {
		t.Fatalf("Mode = %q, want tooth and bone level mode", result.Mode)
	}
}

func TestColoredFixturesCoverAllBMPInputs(t *testing.T) {
	bmpNames := fixtureNamesByExt(t, "images", "BMP", ".bmp")
	pngNames := fixtureNamesByExt(t, "images", "PNG", "Colored", ".png")
	for name := range bmpNames {
		if !pngNames[name] {
			t.Fatalf("missing colored PNG fixture for BMP %s.bmp", name)
		}
	}
	for name := range pngNames {
		if !bmpNames[name] {
			t.Fatalf("missing BMP fixture for colored PNG %s.png", name)
		}
	}
}

func TestRemoveSmallMaskComponentsKeepsRegionsBiggerThanFiftyPixels(t *testing.T) {
	const width = 16
	const height = 12

	mask := make([]uint8, width*height)
	fillMaskRect(mask, width, 0, 0, 5, 10)
	fillMaskRect(mask, width, 9, 0, 6, 9)

	filtered := removeSmallMaskComponents(mask, width, height, minimumToothAreaFloorPixels)
	if got := countMaskPixels(filtered); got != 54 {
		t.Fatalf("countMaskPixels(filtered) = %d, want 54", got)
	}
	for y := 0; y < 10; y++ {
		for x := 0; x < 5; x++ {
			if filtered[y*width+x] != 0 {
				t.Fatalf("50-pixel component was kept at (%d, %d)", x, y)
			}
		}
	}
	for y := 0; y < 9; y++ {
		for x := 9; x < 15; x++ {
			if filtered[y*width+x] == 0 {
				t.Fatalf("54-pixel component was removed at (%d, %d)", x, y)
			}
		}
	}
}

func TestNineBMPFindsFourLargeToothComponents(t *testing.T) {
	preview := decodeGrayFixture(t, fixturePath(t, "images", "BMP", "9.bmp"))
	gray, err := grayPixels(preview)
	if err != nil {
		t.Fatalf("grayPixels returned error: %v", err)
	}
	mask := detectToothMask(gray, int(preview.Width), int(preview.Height))
	teeth := collectComponents(mask, mask, int(preview.Width), int(preview.Height), minimumToothAreaPixels(int(preview.Width), int(preview.Height)))
	if len(teeth) != 4 {
		t.Fatalf("len(teeth) = %d, want 4", len(teeth))
	}
	for index, tooth := range teeth {
		if tooth.area < minimumToothAreaPixels(int(preview.Width), int(preview.Height)) {
			t.Fatalf("tooth %d area = %d, want >= scaled minimum", index+1, tooth.area)
		}
	}
}

func TestInnerOutlineMaskStaysInsideFilledMask(t *testing.T) {
	const width = 9
	const height = 9

	mask := make([]uint8, width*height)
	fillMaskRect(mask, width, 2, 2, 5, 5)

	outline := innerOutlineMask(mask, width, height, toothOutlineThicknessPixels)
	if got := countMaskPixels(outline); got != 24 {
		t.Fatalf("countMaskPixels(outline) = %d, want 24", got)
	}
	if outline[4*width+4] != 0 {
		t.Fatal("inner center pixel was outlined")
	}
	if outline[2*width+2] == 0 || outline[6*width+6] == 0 {
		t.Fatal("inside edge pixels were not outlined")
	}
	for index, value := range outline {
		if value != 0 && mask[index] == 0 {
			t.Fatalf("outline pixel at index %d was drawn outside the tooth mask", index)
		}
	}
}

func TestFillHolesBinaryMaskFillsOnlyEnclosedGaps(t *testing.T) {
	const width = 8
	const height = 8

	mask := make([]uint8, width*height)
	fillMaskRect(mask, width, 1, 1, 5, 5)
	fillMaskRect(mask, width, 6, 1, 1, 5)
	fillMaskRect(mask, width, 1, 6, 6, 1)
	mask[3*width+3] = 0
	mask[0*width+7] = 0
	mask[1*width+7] = 0
	mask[2*width+7] = 0

	filled := fillHolesBinaryMask(mask, width, height)
	if filled[3*width+3] == 0 {
		t.Fatal("enclosed tooth gap was not filled")
	}
	if filled[0*width+7] != 0 || filled[1*width+7] != 0 || filled[2*width+7] != 0 {
		t.Fatal("background connected to image border was filled")
	}
}

func TestBinaryMorphologyMatchesReference(t *testing.T) {
	for _, tt := range []struct {
		name         string
		width        int
		height       int
		radius       int
		fillEveryNth int
	}{
		{name: "empty", width: 8, height: 7, radius: 2, fillEveryNth: 0},
		{name: "radius_one", width: 9, height: 6, radius: 1, fillEveryNth: 5},
		{name: "radius_two", width: 11, height: 10, radius: 2, fillEveryNth: 7},
		{name: "radius_larger_than_edge", width: 5, height: 4, radius: 3, fillEveryNth: 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mask := make([]uint8, tt.width*tt.height)
			for index := range mask {
				if tt.fillEveryNth > 0 && (index*17+3)%tt.fillEveryNth == 0 {
					mask[index] = 1
				}
			}

			assertMasksEqual(t, dilateBinaryMask(mask, tt.width, tt.height, tt.radius), referenceDilateBinaryMask(mask, tt.width, tt.height, tt.radius))
			assertMasksEqual(t, erodeBinaryMask(mask, tt.width, tt.height, tt.radius), referenceErodeBinaryMask(mask, tt.width, tt.height, tt.radius))
		})
	}
}

func TestRuntimeAnalysisDoesNotReadColoredFixtures(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller returned no file path")
	}

	analysisDir := filepath.Dir(currentFile)
	entries, err := os.ReadDir(analysisDir)
	if err != nil {
		t.Fatalf("read analysis dir: %v", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasSuffix(name, "_test.go") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(analysisDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		contents := string(data)
		for _, forbidden := range []string{"PNG/Colored", "matchingReference", "decodeReference", "referenceMask"} {
			if strings.Contains(contents, forbidden) {
				t.Fatalf("runtime analysis file %s contains forbidden label lookup marker %q", name, forbidden)
			}
		}
	}
}

func BenchmarkBinaryMorphology(b *testing.B) {
	const width = 2048
	const height = 1536
	const radius = 2

	mask := benchmarkMask(width, height)
	b.SetBytes(int64(width * height))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		closed := closeBinaryMask(mask, width, height, radius)
		opened := openBinaryMask(closed, width, height, radius)
		if len(opened) == 0 {
			b.Fatal("empty output")
		}
	}
}

func referenceDilateBinaryMask(mask []uint8, width, height, radius int) []uint8 {
	if radius <= 0 || len(mask) == 0 {
		return append([]uint8(nil), mask...)
	}

	out := make([]uint8, len(mask))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			filled := false
			for ny := maxInt(y-radius, 0); ny <= minInt(y+radius, height-1) && !filled; ny++ {
				for nx := maxInt(x-radius, 0); nx <= minInt(x+radius, width-1); nx++ {
					if mask[ny*width+nx] != 0 {
						filled = true
						break
					}
				}
			}
			if filled {
				out[y*width+x] = 1
			}
		}
	}
	return out
}

func referenceErodeBinaryMask(mask []uint8, width, height, radius int) []uint8 {
	if radius <= 0 || len(mask) == 0 {
		return append([]uint8(nil), mask...)
	}

	out := make([]uint8, len(mask))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			filled := true
			for ny := y - radius; ny <= y+radius && filled; ny++ {
				for nx := x - radius; nx <= x+radius; nx++ {
					if nx < 0 || nx >= width || ny < 0 || ny >= height || mask[ny*width+nx] == 0 {
						filled = false
						break
					}
				}
			}
			if filled {
				out[y*width+x] = 1
			}
		}
	}
	return out
}

func assertMasksEqual(t *testing.T, got []uint8, want []uint8) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for index := range got {
		if got[index] != want[index] {
			t.Fatalf("mask[%d] = %d, want %d", index, got[index], want[index])
		}
	}
}

func benchmarkMask(width, height int) []uint8 {
	rng := rand.New(rand.NewSource(42))
	mask := make([]uint8, width*height)
	for index := range mask {
		if rng.Intn(100) < 18 {
			mask[index] = 1
		}
	}
	return mask
}

func coloredFixtureNames(t *testing.T) []string {
	t.Helper()

	pattern := fixturePath(t, "images", "PNG", "Colored", "*.png")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob colored fixtures: %v", err)
	}
	if len(matches) == 0 {
		t.Skip("missing colored PNG fixtures")
	}

	names := make([]string, 0, len(matches))
	for _, match := range matches {
		name := strings.TrimSuffix(filepath.Base(match), filepath.Ext(match))
		if _, err := os.Stat(fixturePath(t, "images", "BMP", name+".bmp")); err == nil {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func fixtureNamesByExt(t *testing.T, parts ...string) map[string]bool {
	t.Helper()
	extension := parts[len(parts)-1]
	dir := fixturePath(t, parts[:len(parts)-1]...)
	matches, err := filepath.Glob(filepath.Join(dir, "*"+extension))
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	names := make(map[string]bool, len(matches))
	for _, match := range matches {
		name := strings.TrimSuffix(filepath.Base(match), filepath.Ext(match))
		names[name] = true
	}
	return names
}

func fixturePath(t *testing.T, parts ...string) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller returned no file path")
	}

	root := filepath.Join(filepath.Dir(currentFile), "..", "..", "..")
	return filepath.Join(append([]string{root}, parts...)...)
}

func decodeGrayFixture(t *testing.T, path string) imaging.PreviewImage {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer file.Close()

	decoded, _, err := image.Decode(file)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	bounds := decoded.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	pixels := make([]uint8, width*height)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gray := color.GrayModel.Convert(decoded.At(x, y)).(color.Gray)
			pixels[(y-bounds.Min.Y)*width+x-bounds.Min.X] = gray.Y
		}
	}

	return imaging.GrayPreview(uint32(width), uint32(height), pixels)
}

func greenMaskFromRGBA(preview imaging.PreviewImage) []uint8 {
	mask := make([]uint8, len(preview.Pixels)/4)
	for index := range mask {
		base := index * 4
		if preview.Pixels[base] == toothOverlayGreen[0] &&
			preview.Pixels[base+1] == toothOverlayGreen[1] &&
			preview.Pixels[base+2] == toothOverlayGreen[2] {
			mask[index] = 1
		}
	}
	return mask
}

func redMaskFromRGBA(preview imaging.PreviewImage) []uint8 {
	mask := make([]uint8, len(preview.Pixels)/4)
	for index := range mask {
		base := index * 4
		if preview.Pixels[base] == boneOverlayRed[0] &&
			preview.Pixels[base+1] == boneOverlayRed[1] &&
			preview.Pixels[base+2] == boneOverlayRed[2] {
			mask[index] = 1
		}
	}
	return mask
}

func greenDominantMaskFromRGBA(preview imaging.PreviewImage) []uint8 {
	mask := make([]uint8, len(preview.Pixels)/4)
	for index := range mask {
		base := index * 4
		if isGreenOverlayPixel(preview.Pixels[base], preview.Pixels[base+1], preview.Pixels[base+2]) {
			mask[index] = 1
		}
	}
	return mask
}

func redDominantMaskFromRGBA(preview imaging.PreviewImage) []uint8 {
	mask := make([]uint8, len(preview.Pixels)/4)
	for index := range mask {
		base := index * 4
		if isRedOverlayPixel(preview.Pixels[base], preview.Pixels[base+1], preview.Pixels[base+2]) {
			mask[index] = 1
		}
	}
	return mask
}

func isGreenOverlayPixel(red, green, blue uint8) bool {
	return green >= 150 && int(green) > int(red)+35 && int(green) > int(blue)+35
}

func isPartialGreenOverlayPixel(red, green, blue uint8) bool {
	return green > red && green > blue &&
		(red != toothOverlayGreen[0] ||
			green != toothOverlayGreen[1] ||
			blue != toothOverlayGreen[2])
}

func isRedOverlayPixel(red, green, blue uint8) bool {
	return red >= 150 && int(red) > int(green)+35 && int(red) > int(blue)+35
}

func clearMaskPixels(mask []uint8, clear []uint8) {
	for index := range mask {
		if clear[index] != 0 {
			mask[index] = 0
		}
	}
}

func fillMaskRect(mask []uint8, width, x, y, rectWidth, rectHeight int) {
	for dy := 0; dy < rectHeight; dy++ {
		for dx := 0; dx < rectWidth; dx++ {
			mask[(y+dy)*width+x+dx] = 1
		}
	}
}

func greenMaskFromImage(t *testing.T, path string) []uint8 {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open colored fixture: %v", err)
	}
	defer file.Close()

	decoded, _, err := image.Decode(file)
	if err != nil {
		t.Fatalf("decode colored fixture: %v", err)
	}

	bounds := decoded.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	mask := make([]uint8, width*height)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := decoded.At(x, y).RGBA()
			if r>>8 <= 150 && g>>8 >= 220 && b>>8 <= 80 {
				mask[(y-bounds.Min.Y)*width+x-bounds.Min.X] = 1
			}
		}
	}
	return mask
}

func redMaskFromImage(t *testing.T, path string) []uint8 {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open colored fixture: %v", err)
	}
	defer file.Close()

	decoded, _, err := image.Decode(file)
	if err != nil {
		t.Fatalf("decode colored fixture: %v", err)
	}

	bounds := decoded.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	mask := make([]uint8, width*height)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := decoded.At(x, y).RGBA()
			if r>>8 >= 220 && g>>8 <= 80 && b>>8 <= 80 {
				mask[(y-bounds.Min.Y)*width+x-bounds.Min.X] = 1
			}
		}
	}
	return mask
}

func maskContainmentRatio(subject, allowed []uint8) float64 {
	if len(subject) != len(allowed) || len(subject) == 0 {
		return 0
	}

	subjectCount := 0
	containedCount := 0
	for index := range subject {
		if subject[index] == 0 {
			continue
		}
		subjectCount++
		if allowed[index] != 0 {
			containedCount++
		}
	}
	if subjectCount == 0 {
		return 0
	}
	return float64(containedCount) / float64(subjectCount)
}

func contourJitterScore(points []overlayPoint) float64 {
	if len(points) < 3 {
		return 0
	}

	score := 0.0
	for index, point := range points {
		previous := points[(index+len(points)-1)%len(points)]
		next := points[(index+1)%len(points)]
		expected := overlayPoint{
			x: (previous.x + next.x) / 2,
			y: (previous.y + next.y) / 2,
		}
		score += math.Hypot(point.x-expected.x, point.y-expected.y)
	}
	return score / float64(len(points))
}

func diceCoefficient(left, right []uint8) float64 {
	if len(left) != len(right) || len(left) == 0 {
		return 0
	}

	intersection := 0
	leftCount := 0
	rightCount := 0
	for index := range left {
		if left[index] != 0 {
			leftCount++
		}
		if right[index] != 0 {
			rightCount++
		}
		if left[index] != 0 && right[index] != 0 {
			intersection++
		}
	}
	if leftCount+rightCount == 0 {
		return 1
	}
	return float64(2*intersection) / float64(leftCount+rightCount)
}
