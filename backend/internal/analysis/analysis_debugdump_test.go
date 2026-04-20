//go:build debugdump

package analysis

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"xrayview/backend/internal/contracts"
)

func TestDumpAnalysisStages(t *testing.T) {
	preview := loadAnalyzePreviewFixture(t)

	var debug analysisDebugSnapshot
	analysis, err := analyzeGrayscalePixelsWithDebug(preview.Width, preview.Height, preview.Pixels, nil, &debug)
	if err != nil {
		t.Fatalf("analyzeGrayscalePixelsWithDebug returned error: %v", err)
	}
	if analysis.Tooth == nil {
		t.Fatal("analysis.Tooth = nil, want primary candidate in debug dump run")
	}

	outputDir := repoPathFromHere(t, "backend", "internal", "analysis", "_debug")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	if err := writeGrayPNG(filepath.Join(outputDir, "normalized.png"), preview.Width, preview.Height, debug.Normalized); err != nil {
		t.Fatalf("write normalized.png: %v", err)
	}
	if err := writeSignedPNG(filepath.Join(outputDir, "small-minus-large.png"), preview.Width, preview.Height, debug.SmallMinusLarge); err != nil {
		t.Fatalf("write small-minus-large.png: %v", err)
	}
	if err := writeGrayPNG(filepath.Join(outputDir, "local-contrast-prebias-legacy.png"), preview.Width, preview.Height, debug.LocalContrastLegacy); err != nil {
		t.Fatalf("write local-contrast-prebias-legacy.png: %v", err)
	}
	if err := writeGrayPNG(filepath.Join(outputDir, "local-contrast-structural.png"), preview.Width, preview.Height, debug.LocalContrast); err != nil {
		t.Fatalf("write local-contrast-structural.png: %v", err)
	}
	if err := writeGrayPNG(filepath.Join(outputDir, "gradient.png"), preview.Width, preview.Height, debug.Gradient); err != nil {
		t.Fatalf("write gradient.png: %v", err)
	}
	if err := writeGrayPNG(filepath.Join(outputDir, "toothness.png"), preview.Width, preview.Height, debug.Toothness); err != nil {
		t.Fatalf("write toothness.png: %v", err)
	}
	if err := writeBinaryMaskPNG(filepath.Join(outputDir, "mask-threshold.png"), preview.Width, preview.Height, debug.PostThresholdMask); err != nil {
		t.Fatalf("write mask-threshold.png: %v", err)
	}
	if err := writeBinaryMaskPNG(filepath.Join(outputDir, "mask-close.png"), preview.Width, preview.Height, debug.PostCloseMask); err != nil {
		t.Fatalf("write mask-close.png: %v", err)
	}
	if err := writeBinaryMaskPNG(filepath.Join(outputDir, "mask-open.png"), preview.Width, preview.Height, debug.PostOpenMask); err != nil {
		t.Fatalf("write mask-open.png: %v", err)
	}
	if err := writeSearchRegionOverlay(filepath.Join(outputDir, "search-region-overlay.png"), preview.Width, preview.Height, debug.Normalized, debug.SearchRegion); err != nil {
		t.Fatalf("write search-region-overlay.png: %v", err)
	}

	components := labelDebugComponents(debug.PostOpenMask, preview.Width, preview.Height, debug.SearchRegion)
	if err := writeConnectedComponentOverlay(filepath.Join(outputDir, "connected-components-overlay.png"), preview.Width, preview.Height, debug.Normalized, components); err != nil {
		t.Fatalf("write connected-components-overlay.png: %v", err)
	}
	if err := writeDetectedTeethOverlay(filepath.Join(outputDir, "detected-teeth-overlay.png"), preview.Width, preview.Height, debug.Normalized, analysis.Teeth); err != nil {
		t.Fatalf("write detected-teeth-overlay.png: %v", err)
	}

	thresholdReport := struct {
		ImageWidth         uint32            `json:"imageWidth"`
		ImageHeight        uint32            `json:"imageHeight"`
		SearchRegion       debugSearchRegion `json:"searchRegion"`
		ToothnessThreshold uint8             `json:"toothnessThreshold"`
		IntensityThreshold uint8             `json:"intensityThreshold"`
		MaskCoverageRatio  float64           `json:"maskCoverageRatio"`
		ComponentCount     int               `json:"componentCount"`
		DetectedCount      int               `json:"detectedCount"`
		PrimaryMaskArea    uint32            `json:"primaryMaskArea"`
		PrimaryBoundingBox interface{}       `json:"primaryBoundingBox"`
		SmallMinusLargeMin int16             `json:"smallMinusLargeMin"`
		SmallMinusLargeMax int16             `json:"smallMinusLargeMax"`
	}{
		ImageWidth:  preview.Width,
		ImageHeight: preview.Height,
		SearchRegion: debugSearchRegion{
			X:      debug.SearchRegion.x,
			Y:      debug.SearchRegion.y,
			Width:  debug.SearchRegion.width,
			Height: debug.SearchRegion.height,
		},
		ToothnessThreshold: debug.ToothnessThreshold,
		IntensityThreshold: debug.IntensityThreshold,
		MaskCoverageRatio:  debug.MaskCoverageRatio,
		ComponentCount:     len(components),
		DetectedCount:      len(debug.DetectedCandidates),
		PrimaryMaskArea:    analysis.Tooth.MaskAreaPixels,
		PrimaryBoundingBox: analysis.Tooth.Geometry.BoundingBox,
	}
	thresholdReport.SmallMinusLargeMin, thresholdReport.SmallMinusLargeMax = minMaxInt16(debug.SmallMinusLarge)
	if err := writeJSON(filepath.Join(outputDir, "thresholds.json"), thresholdReport); err != nil {
		t.Fatalf("write thresholds.json: %v", err)
	}

	candidateAudit := make([]debugCandidateAudit, 0, len(debug.AllCandidates))
	for index, candidate := range debug.AllCandidates {
		candidateAudit = append(candidateAudit, buildDebugCandidateAudit(index, candidate, debug.SearchRegion))
	}
	if err := writeJSON(filepath.Join(outputDir, "candidates.json"), candidateAudit); err != nil {
		t.Fatalf("write candidates.json: %v", err)
	}

	t.Logf("wrote debug analysis artifacts to %s", outputDir)
}

type debugComponent struct {
	pixels []int
	bbox   contracts.BoundingBox
	area   uint32
}

type debugCandidateAudit struct {
	Index        int                   `json:"index"`
	BoundingBox  contracts.BoundingBox `json:"boundingBox"`
	Area         uint32                `json:"area"`
	AreaRatio    float64               `json:"areaRatio"`
	WidthRatio   float64               `json:"widthRatio"`
	HeightRatio  float64               `json:"heightRatio"`
	AspectRatio  float64               `json:"aspectRatio"`
	FillRatio    float64               `json:"fillRatio"`
	Strict       bool                  `json:"strict"`
	Score        float64               `json:"score"`
	CenterX      float64               `json:"centerX"`
	CenterY      float64               `json:"centerY"`
	SpansMidline bool                  `json:"spansMidline"`
}

type debugSearchRegion struct {
	X      uint32 `json:"x"`
	Y      uint32 `json:"y"`
	Width  uint32 `json:"width"`
	Height uint32 `json:"height"`
}

func buildDebugCandidateAudit(index int, candidate componentCandidate, search searchRegion) debugCandidateAudit {
	fillRatio := 0.0
	if candidate.bbox.Width > 0 && candidate.bbox.Height > 0 {
		fillRatio = float64(candidate.area) / float64(candidate.bbox.Width*candidate.bbox.Height)
	}
	centerX := float64(candidate.bbox.X) + float64(candidate.bbox.Width)/2.0
	centerY := float64(candidate.bbox.Y) + float64(candidate.bbox.Height)/2.0
	searchMidY := search.y + search.height/2
	bboxBottom := candidate.bbox.Y + candidate.bbox.Height - 1

	return debugCandidateAudit{
		Index:        index,
		BoundingBox:  candidate.bbox,
		Area:         candidate.area,
		AreaRatio:    float64(candidate.area) / float64(maxUint32(search.area(), 1)),
		WidthRatio:   float64(candidate.bbox.Width) / float64(maxUint32(search.width, 1)),
		HeightRatio:  float64(candidate.bbox.Height) / float64(maxUint32(search.height, 1)),
		AspectRatio:  float64(candidate.bbox.Height) / float64(maxUint32(candidate.bbox.Width, 1)),
		FillRatio:    fillRatio,
		Strict:       candidate.strict,
		Score:        candidate.score,
		CenterX:      centerX,
		CenterY:      centerY,
		SpansMidline: candidate.bbox.Y <= searchMidY && bboxBottom >= searchMidY,
	}
}

func writeGrayPNG(path string, width, height uint32, pixels []uint8) error {
	imageGray := &image.Gray{
		Pix:    append([]uint8(nil), pixels...),
		Stride: int(width),
		Rect:   image.Rect(0, 0, int(width), int(height)),
	}
	return writePNG(path, imageGray)
}

func writeBinaryMaskPNG(path string, width, height uint32, mask []uint8) error {
	pixels := make([]uint8, len(mask))
	for index, value := range mask {
		if value != 0 {
			pixels[index] = 255
		}
	}
	return writeGrayPNG(path, width, height, pixels)
}

func writeSignedPNG(path string, width, height uint32, values []int16) error {
	minValue, maxValue := minMaxInt16(values)
	pixels := make([]uint8, len(values))
	if maxValue == minValue {
		return writeGrayPNG(path, width, height, pixels)
	}

	rangeValue := int(maxValue) - int(minValue)
	for index, value := range values {
		pixels[index] = uint8((int(value-minValue) * 255) / rangeValue)
	}
	return writeGrayPNG(path, width, height, pixels)
}

func writeSearchRegionOverlay(path string, width, height uint32, normalized []uint8, search searchRegion) error {
	overlay := grayToRGBA(normalized, width, height)
	drawRectOutline(overlay, search, color.RGBA{R: 255, G: 64, B: 64, A: 255})
	return writePNG(path, overlay)
}

func writeConnectedComponentOverlay(path string, width, height uint32, normalized []uint8, components []debugComponent) error {
	overlay := grayToRGBA(normalized, width, height)
	for index, component := range components {
		componentColor := debugPaletteColor(index)
		for _, pixelIndex := range component.pixels {
			base := pixelIndex * 4
			overlay.Pix[base+0] = mixChannel(overlay.Pix[base+0], componentColor.R)
			overlay.Pix[base+1] = mixChannel(overlay.Pix[base+1], componentColor.G)
			overlay.Pix[base+2] = mixChannel(overlay.Pix[base+2], componentColor.B)
			overlay.Pix[base+3] = 255
		}
		drawRectOutline(overlay, searchRegion{
			x:      component.bbox.X,
			y:      component.bbox.Y,
			width:  component.bbox.Width,
			height: component.bbox.Height,
		}, componentColor)
	}
	return writePNG(path, overlay)
}

func writeDetectedTeethOverlay(path string, width, height uint32, normalized []uint8, teeth []contracts.ToothCandidate) error {
	overlay := grayToRGBA(normalized, width, height)
	for index, tooth := range teeth {
		componentColor := debugPaletteColor(index)
		drawRectOutline(overlay, searchRegion{
			x:      tooth.Geometry.BoundingBox.X,
			y:      tooth.Geometry.BoundingBox.Y,
			width:  tooth.Geometry.BoundingBox.Width,
			height: tooth.Geometry.BoundingBox.Height,
		}, componentColor)
	}
	return writePNG(path, overlay)
}

func grayToRGBA(normalized []uint8, width, height uint32) *image.RGBA {
	rgba := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	for index, value := range normalized {
		base := index * 4
		rgba.Pix[base+0] = value
		rgba.Pix[base+1] = value
		rgba.Pix[base+2] = value
		rgba.Pix[base+3] = 255
	}
	return rgba
}

func drawRectOutline(imageRGBA *image.RGBA, rect searchRegion, stroke color.RGBA) {
	if rect.width == 0 || rect.height == 0 {
		return
	}

	left := int(rect.x)
	top := int(rect.y)
	right := int(rect.x + rect.width - 1)
	bottom := int(rect.y + rect.height - 1)

	for x := left; x <= right; x++ {
		setRGBA(imageRGBA, x, top, stroke)
		setRGBA(imageRGBA, x, bottom, stroke)
	}
	for y := top; y <= bottom; y++ {
		setRGBA(imageRGBA, left, y, stroke)
		setRGBA(imageRGBA, right, y, stroke)
	}
}

func setRGBA(imageRGBA *image.RGBA, x, y int, value color.RGBA) {
	if !(image.Point{X: x, Y: y}).In(imageRGBA.Rect) {
		return
	}
	offset := imageRGBA.PixOffset(x, y)
	imageRGBA.Pix[offset+0] = value.R
	imageRGBA.Pix[offset+1] = value.G
	imageRGBA.Pix[offset+2] = value.B
	imageRGBA.Pix[offset+3] = value.A
}

func mixChannel(base, tint uint8) uint8 {
	return uint8((uint16(base)*2 + uint16(tint)) / 3)
}

func debugPaletteColor(index int) color.RGBA {
	palette := [...]color.RGBA{
		{R: 255, G: 99, B: 71, A: 255},
		{R: 64, G: 224, B: 208, A: 255},
		{R: 255, G: 215, B: 0, A: 255},
		{R: 135, G: 206, B: 250, A: 255},
		{R: 255, G: 105, B: 180, A: 255},
		{R: 144, G: 238, B: 144, A: 255},
		{R: 255, G: 160, B: 122, A: 255},
		{R: 173, G: 216, B: 230, A: 255},
	}
	return palette[index%len(palette)]
}

func labelDebugComponents(mask []uint8, width, height uint32, search searchRegion) []debugComponent {
	widthInt := int(width)
	heightInt := int(height)
	visited := make([]bool, len(mask))
	queue := make([]int, 0, 1024)
	components := make([]debugComponent, 0)

	for y := int(search.y); y < int(search.y+search.height); y++ {
		for x := int(search.x); x < int(search.x+search.width); x++ {
			startIndex := y*widthInt + x
			if visited[startIndex] || mask[startIndex] == 0 {
				continue
			}

			visited[startIndex] = true
			queue = append(queue[:0], startIndex)
			head := 0

			minX := uint32(x)
			maxX := uint32(x)
			minY := uint32(y)
			maxY := uint32(y)

			for head < len(queue) {
				index := queue[head]
				head++

				px := uint32(index % widthInt)
				py := uint32(index / widthInt)
				minX = minUint32(minX, px)
				maxX = maxUint32(maxX, px)
				minY = minUint32(minY, py)
				maxY = maxUint32(maxY, py)

				for ny := maxInt(int(py)-1, 0); ny <= minInt(int(py)+1, heightInt-1); ny++ {
					for nx := maxInt(int(px)-1, 0); nx <= minInt(int(px)+1, widthInt-1); nx++ {
						neighbor := ny*widthInt + nx
						if !visited[neighbor] && mask[neighbor] != 0 {
							visited[neighbor] = true
							queue = append(queue, neighbor)
						}
					}
				}
			}

			pixels := append([]int(nil), queue...)
			components = append(components, debugComponent{
				pixels: pixels,
				bbox: contracts.BoundingBox{
					X:      minX,
					Y:      minY,
					Width:  maxX - minX + 1,
					Height: maxY - minY + 1,
				},
				area: uint32(len(queue)),
			})
		}
	}

	return components
}

func minMaxInt16(values []int16) (int16, int16) {
	if len(values) == 0 {
		return 0, 0
	}
	minValue := values[0]
	maxValue := values[0]
	for _, value := range values[1:] {
		if value < minValue {
			minValue = value
		}
		if value > maxValue {
			maxValue = value
		}
	}
	return minValue, maxValue
}

func writeJSON(path string, payload interface{}) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func writePNG(path string, imageData image.Image) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	return png.Encode(file, imageData)
}
