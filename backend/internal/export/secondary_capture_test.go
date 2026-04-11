package export

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"xrayview/backend/internal/dicommeta"
	"xrayview/backend/internal/imaging"
)

func TestEncodeSecondaryCapturePreservesMetadataAndEncodesGrayscalePixels(t *testing.T) {
	payload, err := encodeSecondaryCapture(
		imaging.GrayPreview(3, 1, []uint8{0, 127, 255}),
		dicommeta.SourceMetadata{
			StudyInstanceUID: "1.2.3.4.5",
			PreservedElements: []dicommeta.PreservedElement{
				{
					TagGroup:   0x0010,
					TagElement: 0x0010,
					VR:         "PN",
					Values:     []string{"Test^Patient"},
				},
				{
					TagGroup:   0x0028,
					TagElement: 0x0030,
					VR:         "DS",
					Values:     []string{"0.25", "0.40"},
				},
			},
		},
		time.Date(2026, time.April, 8, 12, 34, 56, 0, time.UTC),
		fixedUIDs("2.25.100", "2.25.200"),
	)
	if err != nil {
		t.Fatalf("encodeSecondaryCapture returned error: %v", err)
	}

	outputPath := writePayload(t, payload)
	metadata, err := dicommeta.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if got, want := metadata.Rows, uint16(1); got != want {
		t.Fatalf("Rows = %d, want %d", got, want)
	}
	if got, want := metadata.Columns, uint16(3); got != want {
		t.Fatalf("Columns = %d, want %d", got, want)
	}
	if got, want := metadata.PhotometricInterpretation, "MONOCHROME2"; got != want {
		t.Fatalf("PhotometricInterpretation = %q, want %q", got, want)
	}
	if got, want := metadata.TransferSyntaxUID, explicitVRLittleEndianUID; got != want {
		t.Fatalf("TransferSyntaxUID = %q, want %q", got, want)
	}
	if got, want := metadata.SamplesPerPixel, uint16(1); got != want {
		t.Fatalf("SamplesPerPixel = %d, want %d", got, want)
	}
	if got, want := metadata.BitsAllocated, uint16(8); got != want {
		t.Fatalf("BitsAllocated = %d, want %d", got, want)
	}
	if got, want := metadata.BitsStored, uint16(8); got != want {
		t.Fatalf("BitsStored = %d, want %d", got, want)
	}
	if got, want := metadata.PixelRepresentation, uint16(0); got != want {
		t.Fatalf("PixelRepresentation = %d, want %d", got, want)
	}
	if got, want := floatValue(metadata.WindowCenter), 127.5; got != want {
		t.Fatalf("WindowCenter = %v, want %v", got, want)
	}
	if got, want := floatValue(metadata.WindowWidth), 255.0; got != want {
		t.Fatalf("WindowWidth = %v, want %v", got, want)
	}
	if metadata.MeasurementScale() == nil {
		t.Fatal("MeasurementScale = nil, want preserved PixelSpacing scale")
	}

	elements := readAllElements(t, payload)
	assertElementString(t, elements, 0x00020013, implementationVersionName)
	assertElementString(t, elements, 0x00080016, secondaryCaptureSOPClassUID)
	assertElementString(t, elements, 0x00080060, "OT")
	assertElementString(t, elements, 0x00080012, "20260408")
	assertElementString(t, elements, 0x00080013, "123456")
	assertElementString(t, elements, 0x00080023, "20260408")
	assertElementString(t, elements, 0x00080033, "123456")
	assertElementString(t, elements, 0x00100010, "Test^Patient")
	assertElementString(t, elements, 0x0020000d, "1.2.3.4.5")
	assertElementString(t, elements, 0x0020000e, "2.25.200")
	assertElementString(t, elements, 0x00080018, "2.25.100")

	pixelData := elementBytes(t, elements, 0x7fe00010)
	if got, want := pixelData, []byte{0, 127, 255, 0}; !bytes.Equal(got, want) {
		t.Fatalf("pixel data = %v, want %v", got, want)
	}
}

func TestEncodeSecondaryCaptureEncodesRGBAAsRGB(t *testing.T) {
	payload, err := encodeSecondaryCapture(
		imaging.RGBAPreview(2, 1, []uint8{10, 20, 30, 255, 40, 50, 60, 128}),
		dicommeta.SourceMetadata{StudyInstanceUID: "1.2.3.4.5"},
		time.Date(2026, time.April, 8, 12, 34, 56, 0, time.UTC),
		fixedUIDs("2.25.300", "2.25.400"),
	)
	if err != nil {
		t.Fatalf("encodeSecondaryCapture returned error: %v", err)
	}

	outputPath := writePayload(t, payload)
	metadata, err := dicommeta.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if got, want := metadata.Rows, uint16(1); got != want {
		t.Fatalf("Rows = %d, want %d", got, want)
	}
	if got, want := metadata.Columns, uint16(2); got != want {
		t.Fatalf("Columns = %d, want %d", got, want)
	}
	if got, want := metadata.PhotometricInterpretation, "RGB"; got != want {
		t.Fatalf("PhotometricInterpretation = %q, want %q", got, want)
	}
	if got, want := metadata.SamplesPerPixel, uint16(3); got != want {
		t.Fatalf("SamplesPerPixel = %d, want %d", got, want)
	}
	if got, want := metadata.PlanarConfiguration, uint16(0); got != want {
		t.Fatalf("PlanarConfiguration = %d, want %d", got, want)
	}
	if metadata.WindowCenter != nil {
		t.Fatalf("WindowCenter = %v, want nil for color secondary capture", metadata.WindowCenter)
	}
	if metadata.WindowWidth != nil {
		t.Fatalf("WindowWidth = %v, want nil for color secondary capture", metadata.WindowWidth)
	}

	elements := readAllElements(t, payload)
	pixelData := elementBytes(t, elements, 0x7fe00010)
	if got, want := pixelData, []byte{10, 20, 30, 40, 50, 60}; !bytes.Equal(got, want) {
		t.Fatalf("pixel data = %v, want %v", got, want)
	}
}

func TestEncodeSecondaryCaptureRejectsUnsupportedPreservedVR(t *testing.T) {
	_, err := encodeSecondaryCapture(
		imaging.GrayPreview(1, 1, []uint8{128}),
		dicommeta.SourceMetadata{
			StudyInstanceUID: "1.2.3.4.5",
			PreservedElements: []dicommeta.PreservedElement{
				{
					TagGroup:   0x0028,
					TagElement: 0x0100,
					VR:         "US",
					Values:     []string{"8"},
				},
			},
		},
		time.Date(2026, time.April, 8, 12, 34, 56, 0, time.UTC),
		fixedUIDs("2.25.500", "2.25.600"),
	)
	if err == nil {
		t.Fatal("encodeSecondaryCapture returned nil error for unsupported preserved VR")
	}
	if !strings.Contains(err.Error(), "unsupported preserved element VR") {
		t.Fatalf("error = %q, want unsupported preserved VR message", err)
	}
}

func TestEncodeSecondaryCaptureRoundTripsThroughGoDecode(t *testing.T) {
	payload, err := encodeSecondaryCapture(
		imaging.GrayPreview(2, 2, []uint8{0, 64, 128, 255}),
		dicommeta.SourceMetadata{
			StudyInstanceUID: "1.2.3.4.5",
			PreservedElements: []dicommeta.PreservedElement{
				{
					TagGroup:   0x0010,
					TagElement: 0x0010,
					VR:         "PN",
					Values:     []string{"Roundtrip^Patient"},
				},
				{
					TagGroup:   0x0028,
					TagElement: 0x0030,
					VR:         "DS",
					Values:     []string{"0.20", "0.30"},
				},
			},
		},
		time.Date(2026, time.April, 8, 12, 34, 56, 0, time.UTC),
		fixedUIDs("2.25.700", "2.25.800"),
	)
	if err != nil {
		t.Fatalf("encodeSecondaryCapture returned error: %v", err)
	}

	outputPath := writePayload(t, payload)
	study, err := dicommeta.DecodeFile(outputPath)
	if err != nil {
		t.Fatalf("DecodeFile returned error: %v", err)
	}

	if got, want := study.Image.Width, uint32(2); got != want {
		t.Fatalf("Width = %d, want %d", got, want)
	}
	if got, want := study.Image.Height, uint32(2); got != want {
		t.Fatalf("Height = %d, want %d", got, want)
	}
	if got, want := study.Metadata.StudyInstanceUID, "1.2.3.4.5"; got != want {
		t.Fatalf("StudyInstanceUID = %q, want %q", got, want)
	}
	if study.MeasurementScale == nil {
		t.Fatal("MeasurementScale = nil, want preserved PixelSpacing-derived scale")
	}
	if got, want := study.MeasurementScale.RowSpacingMM, 0.20; got != want {
		t.Fatalf("MeasurementScale.RowSpacingMM = %v, want %v", got, want)
	}
	if got, want := study.MeasurementScale.ColumnSpacingMM, 0.30; got != want {
		t.Fatalf("MeasurementScale.ColumnSpacingMM = %v, want %v", got, want)
	}
	if got, want := len(study.Metadata.PreservedElements), 2; got != want {
		t.Fatalf("len(PreservedElements) = %d, want %d", got, want)
	}
}

func fixedUIDs(values ...string) uidGenerator {
	index := 0
	return func() (string, error) {
		if index >= len(values) {
			return "", fmt.Errorf("no fixed uid available")
		}

		value := values[index]
		index += 1
		return value, nil
	}
}

func writePayload(t *testing.T, payload []byte) string {
	t.Helper()

	outputPath := t.TempDir() + "/secondary-capture.dcm"
	if err := os.WriteFile(outputPath, payload, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	return outputPath
}

type parsedElement struct {
	vr    string
	value []byte
}

func readAllElements(t *testing.T, payload []byte) map[uint32]parsedElement {
	t.Helper()

	reader := bytes.NewReader(payload)
	if _, err := reader.Seek(128, io.SeekStart); err != nil {
		t.Fatalf("seek preamble returned error: %v", err)
	}

	var magic [4]byte
	if _, err := io.ReadFull(reader, magic[:]); err != nil {
		t.Fatalf("read magic returned error: %v", err)
	}
	if string(magic[:]) != "DICM" {
		t.Fatalf("magic = %q, want DICM", string(magic[:]))
	}

	elements := make(map[uint32]parsedElement)
	for {
		var rawTag [4]byte
		_, err := io.ReadFull(reader, rawTag[:])
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tag returned error: %v", err)
		}

		tag := tagValue(
			binary.LittleEndian.Uint16(rawTag[0:2]),
			binary.LittleEndian.Uint16(rawTag[2:4]),
		)

		var rawVR [2]byte
		if _, err := io.ReadFull(reader, rawVR[:]); err != nil {
			t.Fatalf("read VR returned error: %v", err)
		}

		vr := string(rawVR[:])
		length, err := readValueLength(reader, vr)
		if err != nil {
			t.Fatalf("read value length returned error: %v", err)
		}

		value := make([]byte, length)
		if _, err := io.ReadFull(reader, value); err != nil {
			t.Fatalf("read element value returned error: %v", err)
		}

		elements[tag] = parsedElement{vr: vr, value: value}
	}

	return elements
}

func readValueLength(reader io.Reader, vr string) (int, error) {
	switch vr {
	case "OB", "OD", "OF", "OL", "OW", "SQ", "UC", "UR", "UT", "UN":
		var reserved [2]byte
		if _, err := io.ReadFull(reader, reserved[:]); err != nil {
			return 0, err
		}

		var rawLength [4]byte
		if _, err := io.ReadFull(reader, rawLength[:]); err != nil {
			return 0, err
		}
		return int(binary.LittleEndian.Uint32(rawLength[:])), nil
	default:
		var rawLength [2]byte
		if _, err := io.ReadFull(reader, rawLength[:]); err != nil {
			return 0, err
		}
		return int(binary.LittleEndian.Uint16(rawLength[:])), nil
	}
}

func assertElementString(
	t *testing.T,
	elements map[uint32]parsedElement,
	tag uint32,
	want string,
) {
	t.Helper()

	if got := elementString(t, elements, tag); got != want {
		t.Fatalf(
			"element (%04x,%04x) = %q, want %q",
			elementGroup(tag),
			elementNumber(tag),
			got,
			want,
		)
	}
}

func elementString(t *testing.T, elements map[uint32]parsedElement, tag uint32) string {
	t.Helper()

	encoded, ok := elements[tag]
	if !ok {
		t.Fatalf("element (%04x,%04x) was not written", elementGroup(tag), elementNumber(tag))
	}

	value := string(encoded.value)
	if encoded.vr == "UI" {
		return strings.TrimRight(value, "\x00 ")
	}

	return strings.TrimRight(value, " ")
}

func elementBytes(t *testing.T, elements map[uint32]parsedElement, tag uint32) []byte {
	t.Helper()

	encoded, ok := elements[tag]
	if !ok {
		t.Fatalf("element (%04x,%04x) was not written", elementGroup(tag), elementNumber(tag))
	}

	return encoded.value
}

func BenchmarkEncodeSecondaryCapture(b *testing.B) {
	fixedTime := time.Date(2026, time.April, 8, 12, 34, 56, 0, time.UTC)
	uidGen := func() (string, error) { return "2.25.12345", nil }
	meta := dicommeta.SourceMetadata{StudyInstanceUID: "1.2.3.4.5"}

	b.Run("Gray8_2048x1536", func(b *testing.B) {
		pixels := make([]uint8, 2048*1536)
		for i := range pixels {
			pixels[i] = uint8(i % 256)
		}
		preview := imaging.GrayPreview(2048, 1536, pixels)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			result, err := encodeSecondaryCapture(preview, meta, fixedTime, uidGen)
			if err != nil {
				b.Fatal(err)
			}
			_ = result
		}
	})

	b.Run("RGBA8_2048x1536", func(b *testing.B) {
		pixels := make([]uint8, 2048*1536*4)
		for i := range pixels {
			pixels[i] = uint8(i % 256)
		}
		preview := imaging.RGBAPreview(2048, 1536, pixels)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			result, err := encodeSecondaryCapture(preview, meta, fixedTime, uidGen)
			if err != nil {
				b.Fatal(err)
			}
			_ = result
		}
	})
}

func floatValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
