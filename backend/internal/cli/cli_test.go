package cli

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVersionPrintsContractMetadata(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if err := Run([]string{"version"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}

	if stdout.String() != "xrayview-backend contract-v2\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestHelpFlagPrintsUsageAndExitsSuccessfully(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if err := Run([]string{"--help"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Usage:") || !strings.Contains(stdout.String(), "xrayview-backend") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestProcessPreviewRejectsZeroContrast(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "study.bmp")
	output := filepath.Join(root, "process.bmp")
	if err := os.WriteFile(input, buildBMP32(4, 2, grayscaleRamp()), 0o666); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	err := Run([]string{"process-preview", "--contrast", "0", input, output}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error")
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !strings.Contains(err.Error(), "contrast must be >= 0.1") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Fatalf("output exists or stat failed unexpectedly: %v", statErr)
	}
}

func TestRenderAndProcessPreviewWriteBMPs(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "study.bmp")
	renderOutput := filepath.Join(root, "render.bmp")
	processOutput := filepath.Join(root, "process.bmp")
	if err := os.WriteFile(input, buildBMP32(4, 2, grayscaleRamp()), 0o666); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	if err := Run([]string{"render-preview", input, renderOutput}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if bytesRead, err := os.ReadFile(renderOutput); err != nil || !bytes.HasPrefix(bytesRead, []byte("BM")) {
		t.Fatalf("render output invalid: %v", err)
	}

	stdout.Reset()
	if err := Run([]string{"process-preview", "--invert", "--palette", " HoT ", input, processOutput}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if bytesRead, err := os.ReadFile(processOutput); err != nil || !bytes.HasPrefix(bytesRead, []byte("BM")) {
		t.Fatalf("process output invalid: %v", err)
	}

	var summary map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary["palette"] != "hot" {
		t.Fatalf("palette = %v", summary["palette"])
	}
}

func TestRenderPreviewFullRangePreservesSourceValues(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "study.bmp")
	defaultOutput := filepath.Join(root, "default.bmp")
	fullRangeOutput := filepath.Join(root, "full-range.bmp")
	if err := os.WriteFile(input, buildBMP32(2, 1, []rgb{{10, 10, 10}, {40, 40, 40}}), 0o666); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	if err := Run([]string{"render-preview", input, defaultOutput}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := Run([]string{"render-preview", "--full-range", input, fullRangeOutput}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}

	defaultPixels := decodeBMPPixelsForTest(t, defaultOutput)
	fullRangePixels := decodeBMPPixelsForTest(t, fullRangeOutput)
	if string(defaultPixels) != string([]byte{0, 255}) {
		t.Fatalf("default pixels = %v", defaultPixels)
	}
	if string(fullRangePixels) != string([]byte{10, 40}) {
		t.Fatalf("full range pixels = %v", fullRangePixels)
	}

	var summary map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary["windowMode"] != "full-range" {
		t.Fatalf("windowMode = %v", summary["windowMode"])
	}
}

func TestDecodeSourceReportsBMPMetadataAndRange(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "study.bmp")
	if err := os.WriteFile(input, buildBMP32(2, 1, []rgb{{10, 10, 10}, {40, 40, 40}}), 0o666); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	if err := Run([]string{"decode-source", input}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}

	var summary map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary["width"] != float64(2) || summary["height"] != float64(1) {
		t.Fatalf("summary = %+v", summary)
	}
	if summary["minValue"] != float64(10) || summary["maxValue"] != float64(40) {
		t.Fatalf("range = %v..%v", summary["minValue"], summary["maxValue"])
	}
	if _, ok := summary["measurementScale"]; ok {
		t.Fatalf("measurementScale should be omitted: %+v", summary)
	}
}

func TestInspectionSubcommandsReturnManifestAndStudyMetadata(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "study.bmp")
	if err := os.WriteFile(input, buildBMP32(4, 2, grayscaleRamp()), 0o666); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	if err := Run([]string{"processing-manifest"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["defaultPresetId"] != "default" || len(manifest["presets"].([]any)) != 3 {
		t.Fatalf("manifest = %+v", manifest)
	}

	stdout.Reset()
	if err := Run([]string{"describe-study", input}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	var study map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &study); err != nil {
		t.Fatal(err)
	}
	if study["width"] != float64(4) || study["height"] != float64(2) ||
		study["colorChannelCount"] != float64(3) || study["bitsPerChannel"] != float64(8) ||
		study["colorModel"] != "rgb" {
		t.Fatalf("study = %+v", study)
	}
}

func TestTopLevelWorkflowFlagsAreRejected(t *testing.T) {
	var stdout, stderr bytes.Buffer

	err := Run([]string{"--input", "study.bmp"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestAnalyzePreviewWritesBMPAndSummary(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "analysis.bmp")
	output := filepath.Join(root, "analysis-overlay.bmp")
	if err := os.WriteFile(input, buildBMP32(20, 20, analysisFixturePixels()), 0o666); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	if err := Run([]string{"analyze-preview", "--filled", input, output}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if bytesRead, err := os.ReadFile(output); err != nil || !bytes.HasPrefix(bytesRead, []byte("BM")) {
		t.Fatalf("analysis output invalid: %v", err)
	}
	var summary map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary["filled"] != true || summary["loadedWidth"] != float64(20) || summary["loadedHeight"] != float64(20) {
		t.Fatalf("summary = %+v", summary)
	}
	if !strings.HasPrefix(summary["mode"].(string), "dynamic tooth and bone level overlay") {
		t.Fatalf("mode = %v", summary["mode"])
	}
}

type rgb struct {
	red   byte
	green byte
	blue  byte
}

func grayscaleRamp() []rgb {
	return []rgb{
		{0, 0, 0},
		{36, 36, 36},
		{72, 72, 72},
		{108, 108, 108},
		{144, 144, 144},
		{180, 180, 180},
		{216, 216, 216},
		{255, 255, 255},
	}
}

func analysisFixturePixels() []rgb {
	pixels := make([]rgb, 20*20)
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			value := byte(24)
			if x >= 5 && x < 15 && y >= 4 && y < 14 {
				value = 220
			}
			if y == 15 {
				value = 80
			}
			pixels[y*20+x] = rgb{value, value, value}
		}
	}
	return pixels
}

func buildBMP32(width, height uint32, pixels []rgb) []byte {
	rowStride := int(width) * 4
	pixelBytes := rowStride * int(height)
	fileSize := 54 + pixelBytes
	bmp := make([]byte, 0, fileSize)
	bmp = append(bmp, 'B', 'M')
	bmp = binary.LittleEndian.AppendUint32(bmp, uint32(fileSize))
	bmp = append(bmp, 0, 0, 0, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, 54)
	bmp = binary.LittleEndian.AppendUint32(bmp, 40)
	bmp = binary.LittleEndian.AppendUint32(bmp, width)
	bmp = binary.LittleEndian.AppendUint32(bmp, height)
	bmp = binary.LittleEndian.AppendUint16(bmp, 1)
	bmp = binary.LittleEndian.AppendUint16(bmp, 32)
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, uint32(pixelBytes))
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	bmp = binary.LittleEndian.AppendUint32(bmp, 0)
	for outputY := int(height) - 1; outputY >= 0; outputY-- {
		row := pixels[outputY*int(width) : (outputY+1)*int(width)]
		for _, pixel := range row {
			bmp = append(bmp, pixel.blue, pixel.green, pixel.red, 255)
		}
	}
	return bmp
}

func decodeBMPPixelsForTest(t *testing.T, path string) []byte {
	t.Helper()
	bytesRead, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pixelOffset := int(binary.LittleEndian.Uint32(bytesRead[10:14]))
	width := int(int32(binary.LittleEndian.Uint32(bytesRead[18:22])))
	height := int(int32(binary.LittleEndian.Uint32(bytesRead[22:26])))
	stride := (width + 3) / 4 * 4
	pixels := make([]byte, width*height)
	for outputY := 0; outputY < height; outputY++ {
		sourceY := height - 1 - outputY
		rowStart := pixelOffset + sourceY*stride
		copy(pixels[outputY*width:(outputY+1)*width], bytesRead[rowStart:rowStart+width])
	}
	return pixels
}
