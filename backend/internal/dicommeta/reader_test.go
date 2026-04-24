package dicommeta

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func TestReadFileSampleDentalRadiographMetadata(t *testing.T) {
	metadata, err := ReadFile(sampleDicomPath(t))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if got, want := metadata.Rows, uint16(2); got != want {
		t.Fatalf("Rows = %d, want %d", got, want)
	}
	if got, want := metadata.Columns, uint16(4); got != want {
		t.Fatalf("Columns = %d, want %d", got, want)
	}
	if got, want := metadata.PhotometricInterpretation, "MONOCHROME2"; got != want {
		t.Fatalf("PhotometricInterpretation = %q, want %q", got, want)
	}
	if got, want := metadata.TransferSyntaxUID, "1.2.840.10008.1.2.1"; got != want {
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
	if got, want := metadata.NumberOfFrames, uint32(1); got != want {
		t.Fatalf("NumberOfFrames = %d, want %d", got, want)
	}
	if got, want := metadata.PixelDataEncoding, PixelDataEncodingNative; got != want {
		t.Fatalf("PixelDataEncoding = %q, want %q", got, want)
	}
	if got, want := floatValue(metadata.WindowCenter), 127.5; got != want {
		t.Fatalf("WindowCenter = %v, want %v", got, want)
	}
	if got, want := floatValue(metadata.WindowWidth), 255.0; got != want {
		t.Fatalf("WindowWidth = %v, want %v", got, want)
	}
	if metadata.PixelSpacing != nil {
		t.Fatalf("PixelSpacing = %+v, want nil", metadata.PixelSpacing)
	}
	if metadata.ImagerPixelSpacing != nil {
		t.Fatalf("ImagerPixelSpacing = %+v, want nil", metadata.ImagerPixelSpacing)
	}
	if metadata.NominalScannedPixelSpacing != nil {
		t.Fatalf("NominalScannedPixelSpacing = %+v, want nil", metadata.NominalScannedPixelSpacing)
	}
	if metadata.MeasurementScale() != nil {
		t.Fatalf("MeasurementScale = %+v, want nil", metadata.MeasurementScale())
	}
}

func TestReadFileProcessedSampleTracksNativeDecodeMetadata(t *testing.T) {
	metadata, err := ReadFile(processedSampleDicomPath(t))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if got, want := metadata.Rows, uint16(2); got != want {
		t.Fatalf("Rows = %d, want %d", got, want)
	}
	if got, want := metadata.Columns, uint16(4); got != want {
		t.Fatalf("Columns = %d, want %d", got, want)
	}
	if got, want := metadata.SamplesPerPixel, uint16(1); got != want {
		t.Fatalf("SamplesPerPixel = %d, want %d", got, want)
	}
	if got, want := metadata.PhotometricInterpretation, "MONOCHROME2"; got != want {
		t.Fatalf("PhotometricInterpretation = %q, want %q", got, want)
	}
	if got, want := metadata.PlanarConfiguration, uint16(0); got != want {
		t.Fatalf("PlanarConfiguration = %d, want %d", got, want)
	}
	if got, want := metadata.BitsAllocated, uint16(8); got != want {
		t.Fatalf("BitsAllocated = %d, want %d", got, want)
	}
	if got, want := metadata.BitsStored, uint16(8); got != want {
		t.Fatalf("BitsStored = %d, want %d", got, want)
	}
	if got, want := metadata.PixelDataEncoding, PixelDataEncodingNative; got != want {
		t.Fatalf("PixelDataEncoding = %q, want %q", got, want)
	}
}

func TestReadUsesPixelSpacingMeasurementScalePrecedence(t *testing.T) {
	metadata, err := Read(bytes.NewReader(buildTestDicom(buildOptions{
		withPart10:                 true,
		transferSyntaxUID:          "1.2.840.10008.1.2.1",
		datasetSyntax:              transferSyntax{byteOrder: binary.LittleEndian, explicit: true},
		rows:                       512,
		columns:                    1024,
		photometricInterpretation:  "MONOCHROME1",
		pixelSpacing:               "0.20\\0.30",
		imagerPixelSpacing:         "0.40\\0.50",
		nominalScannedPixelSpacing: "0.60\\0.70",
		windowCenter:               "1200.5",
		windowWidth:                "2401.25",
	})))
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}

	if got, want := metadata.Rows, uint16(512); got != want {
		t.Fatalf("Rows = %d, want %d", got, want)
	}
	if got, want := metadata.Columns, uint16(1024); got != want {
		t.Fatalf("Columns = %d, want %d", got, want)
	}
	assertSpacingPair(t, metadata.PixelSpacing, 0.20, 0.30)
	assertSpacingPair(t, metadata.ImagerPixelSpacing, 0.40, 0.50)
	assertSpacingPair(t, metadata.NominalScannedPixelSpacing, 0.60, 0.70)
	if metadata.WindowCenter == nil || *metadata.WindowCenter != 1200.5 {
		t.Fatalf("WindowCenter = %v, want 1200.5", metadata.WindowCenter)
	}
	if metadata.WindowWidth == nil || *metadata.WindowWidth != 2401.25 {
		t.Fatalf("WindowWidth = %v, want 2401.25", metadata.WindowWidth)
	}
	if got, want := metadata.PhotometricInterpretation, "MONOCHROME1"; got != want {
		t.Fatalf("PhotometricInterpretation = %q, want %q", got, want)
	}
	if got, want := metadata.TransferSyntaxUID, "1.2.840.10008.1.2.1"; got != want {
		t.Fatalf("TransferSyntaxUID = %q, want %q", got, want)
	}

	scale := metadata.MeasurementScale()
	if scale == nil {
		t.Fatal("MeasurementScale = nil, want pixel spacing scale")
	}
	if got, want := scale.RowSpacingMM, 0.20; got != want {
		t.Fatalf("MeasurementScale.RowSpacingMM = %v, want %v", got, want)
	}
	if got, want := scale.ColumnSpacingMM, 0.30; got != want {
		t.Fatalf("MeasurementScale.ColumnSpacingMM = %v, want %v", got, want)
	}
	if got, want := scale.Source, "PixelSpacing"; got != want {
		t.Fatalf("MeasurementScale.Source = %q, want %q", got, want)
	}
}

func TestReadSupportsExplicitBigEndianDatasetSyntax(t *testing.T) {
	metadata, err := Read(bytes.NewReader(buildTestDicom(buildOptions{
		withPart10:                true,
		transferSyntaxUID:         explicitBigEndianTransferSyntax,
		datasetSyntax:             transferSyntax{byteOrder: binary.BigEndian, explicit: true},
		rows:                      321,
		columns:                   654,
		photometricInterpretation: "MONOCHROME2",
		windowCenter:              "512",
		windowWidth:               "1024",
	})))
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}

	if got, want := metadata.Rows, uint16(321); got != want {
		t.Fatalf("Rows = %d, want %d", got, want)
	}
	if got, want := metadata.Columns, uint16(654); got != want {
		t.Fatalf("Columns = %d, want %d", got, want)
	}
	if got, want := metadata.PhotometricInterpretation, "MONOCHROME2"; got != want {
		t.Fatalf("PhotometricInterpretation = %q, want %q", got, want)
	}
	if got, want := metadata.TransferSyntaxUID, explicitBigEndianTransferSyntax; got != want {
		t.Fatalf("TransferSyntaxUID = %q, want %q", got, want)
	}
}

func TestReadFallsBackToRawImplicitLittleEndianDataset(t *testing.T) {
	metadata, err := Read(bytes.NewReader(buildTestDicom(buildOptions{
		withPart10:                false,
		datasetSyntax:             transferSyntax{byteOrder: binary.LittleEndian, explicit: false},
		rows:                      128,
		columns:                   256,
		photometricInterpretation: "MONOCHROME2",
	})))
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}

	if got, want := metadata.Rows, uint16(128); got != want {
		t.Fatalf("Rows = %d, want %d", got, want)
	}
	if got, want := metadata.Columns, uint16(256); got != want {
		t.Fatalf("Columns = %d, want %d", got, want)
	}
	if got, want := metadata.TransferSyntaxUID, implicitLittleEndianTransferSyntax; got != want {
		t.Fatalf("TransferSyntaxUID = %q, want %q", got, want)
	}
	if got, want := metadata.PixelDataEncoding, PixelDataEncodingNative; got != want {
		t.Fatalf("PixelDataEncoding = %q, want %q", got, want)
	}
}

func TestReadTracksDecodeRelevantFieldsForEncapsulatedPixelData(t *testing.T) {
	metadata, err := Read(bytes.NewReader(buildTestDicom(buildOptions{
		withPart10:                true,
		transferSyntaxUID:         "1.2.840.10008.1.2.4.50",
		datasetSyntax:             transferSyntax{byteOrder: binary.LittleEndian, explicit: true},
		rows:                      640,
		columns:                   480,
		samplesPerPixel:           3,
		photometricInterpretation: "RGB",
		numberOfFrames:            2,
		planarConfiguration:       0,
		bitsAllocated:             8,
		bitsStored:                8,
		pixelRepresentation:       0,
		pixelDataEncapsulated:     true,
	})))
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}

	if got, want := metadata.PixelDataEncoding, PixelDataEncodingEncapsulated; got != want {
		t.Fatalf("PixelDataEncoding = %q, want %q", got, want)
	}
	if got, want := metadata.TransferSyntaxUID, "1.2.840.10008.1.2.4.50"; got != want {
		t.Fatalf("TransferSyntaxUID = %q, want %q", got, want)
	}
	if got, want := metadata.SamplesPerPixel, uint16(3); got != want {
		t.Fatalf("SamplesPerPixel = %d, want %d", got, want)
	}
	if got, want := metadata.BitsAllocated, uint16(8); got != want {
		t.Fatalf("BitsAllocated = %d, want %d", got, want)
	}
	if got, want := metadata.BitsStored, uint16(8); got != want {
		t.Fatalf("BitsStored = %d, want %d", got, want)
	}
	if got, want := metadata.NumberOfFrames, uint32(2); got != want {
		t.Fatalf("NumberOfFrames = %d, want %d", got, want)
	}
}

type buildOptions struct {
	withPart10                 bool
	transferSyntaxUID          string
	datasetSyntax              transferSyntax
	rows                       uint16
	columns                    uint16
	samplesPerPixel            uint16
	photometricInterpretation  string
	numberOfFrames             uint32
	planarConfiguration        uint16
	bitsAllocated              uint16
	bitsStored                 uint16
	pixelRepresentation        uint16
	pixelSpacing               string
	imagerPixelSpacing         string
	nominalScannedPixelSpacing string
	windowCenter               string
	windowWidth                string
	pixelDataEncapsulated      bool
}

func buildTestDicom(options buildOptions) []byte {
	var payload bytes.Buffer

	if options.withPart10 {
		payload.Write(make([]byte, part10PreambleLength))
		payload.WriteString(part10Magic)
		writeElement(
			&payload,
			fileMetaTransferSyntax,
			tagTransferSyntaxUID,
			"UI",
			encodeUI(options.transferSyntaxUID),
		)
	}

	writeElement(
		&payload,
		options.datasetSyntax,
		tagSamplesPerPixel,
		"US",
		encodeUint16(options.datasetSyntax.byteOrder, defaultUint16(options.samplesPerPixel, 1)),
	)
	if options.numberOfFrames != 0 {
		writeElement(
			&payload,
			options.datasetSyntax,
			tagNumberOfFrames,
			"IS",
			encodeString(strconv.FormatUint(uint64(options.numberOfFrames), 10), ' '),
		)
	}
	writeElement(
		&payload,
		options.datasetSyntax,
		tagRows,
		"US",
		encodeUint16(options.datasetSyntax.byteOrder, options.rows),
	)
	writeElement(
		&payload,
		options.datasetSyntax,
		tagColumns,
		"US",
		encodeUint16(options.datasetSyntax.byteOrder, options.columns),
	)
	writeElement(
		&payload,
		options.datasetSyntax,
		tagPhotometricInterpretation,
		"CS",
		encodeString(options.photometricInterpretation, ' '),
	)
	if options.planarConfiguration != 0 || defaultUint16(options.samplesPerPixel, 1) > 1 {
		writeElement(
			&payload,
			options.datasetSyntax,
			tagPlanarConfiguration,
			"US",
			encodeUint16(options.datasetSyntax.byteOrder, options.planarConfiguration),
		)
	}
	writeElement(
		&payload,
		options.datasetSyntax,
		tagBitsAllocated,
		"US",
		encodeUint16(options.datasetSyntax.byteOrder, defaultUint16(options.bitsAllocated, 8)),
	)
	writeElement(
		&payload,
		options.datasetSyntax,
		tagBitsStored,
		"US",
		encodeUint16(options.datasetSyntax.byteOrder, defaultUint16(options.bitsStored, defaultUint16(options.bitsAllocated, 8))),
	)
	writeElement(
		&payload,
		options.datasetSyntax,
		tagPixelRepresentation,
		"US",
		encodeUint16(options.datasetSyntax.byteOrder, options.pixelRepresentation),
	)
	if options.pixelSpacing != "" {
		writeElement(
			&payload,
			options.datasetSyntax,
			tagPixelSpacing,
			"DS",
			encodeString(options.pixelSpacing, ' '),
		)
	}
	if options.imagerPixelSpacing != "" {
		writeElement(
			&payload,
			options.datasetSyntax,
			tagImagerPixelSpacing,
			"DS",
			encodeString(options.imagerPixelSpacing, ' '),
		)
	}
	if options.nominalScannedPixelSpacing != "" {
		writeElement(
			&payload,
			options.datasetSyntax,
			tagNominalScannedPixelSpacing,
			"DS",
			encodeString(options.nominalScannedPixelSpacing, ' '),
		)
	}
	if options.windowCenter != "" {
		writeElement(
			&payload,
			options.datasetSyntax,
			tagWindowCenter,
			"DS",
			encodeString(options.windowCenter, ' '),
		)
	}
	if options.windowWidth != "" {
		writeElement(
			&payload,
			options.datasetSyntax,
			tagWindowWidth,
			"DS",
			encodeString(options.windowWidth, ' '),
		)
	}

	if options.pixelDataEncapsulated {
		writeUndefinedLengthElementHeader(&payload, options.datasetSyntax, tagPixelData, "OB")
	} else {
		writeElement(
			&payload,
			options.datasetSyntax,
			tagPixelData,
			"OB",
			nil,
		)
	}

	return payload.Bytes()
}

func writeElement(
	payload *bytes.Buffer,
	syntax transferSyntax,
	field tag,
	vr string,
	value []byte,
) {
	writeUint16(payload, syntax.byteOrder, field.group)
	writeUint16(payload, syntax.byteOrder, field.element)

	if syntax.explicit {
		payload.WriteString(vr)
		if uses32BitLength(vr) {
			payload.Write([]byte{0x00, 0x00})
			writeUint32(payload, syntax.byteOrder, uint32(len(value)))
		} else {
			writeUint16(payload, syntax.byteOrder, uint16(len(value)))
		}
	} else {
		writeUint32(payload, syntax.byteOrder, uint32(len(value)))
	}

	payload.Write(value)
}

func writeUndefinedLengthElementHeader(
	payload *bytes.Buffer,
	syntax transferSyntax,
	field tag,
	vr string,
) {
	writeUint16(payload, syntax.byteOrder, field.group)
	writeUint16(payload, syntax.byteOrder, field.element)
	payload.WriteString(vr)
	payload.Write([]byte{0x00, 0x00})
	writeUint32(payload, syntax.byteOrder, undefinedLength)
}

func writeUint16(payload *bytes.Buffer, byteOrder binary.ByteOrder, value uint16) {
	var raw [2]byte
	byteOrder.PutUint16(raw[:], value)
	payload.Write(raw[:])
}

func writeUint32(payload *bytes.Buffer, byteOrder binary.ByteOrder, value uint32) {
	var raw [4]byte
	byteOrder.PutUint32(raw[:], value)
	payload.Write(raw[:])
}

func encodeUint16(byteOrder binary.ByteOrder, value uint16) []byte {
	var raw [2]byte
	byteOrder.PutUint16(raw[:], value)
	return raw[:]
}

func defaultUint16(value uint16, fallback uint16) uint16 {
	if value == 0 {
		return fallback
	}

	return value
}

func encodeUI(value string) []byte {
	return encodeString(value, 0x00)
}

func encodeString(value string, padding byte) []byte {
	raw := []byte(value)
	if len(raw)%2 != 0 {
		raw = append(raw, padding)
	}
	return raw
}

func assertSpacingPair(t *testing.T, pair *SpacingPair, row float64, column float64) {
	t.Helper()

	if pair == nil {
		t.Fatalf("SpacingPair = nil, want %v\\%v", row, column)
	}
	if got, want := pair.RowSpacingMM, row; got != want {
		t.Fatalf("RowSpacingMM = %v, want %v", got, want)
	}
	if got, want := pair.ColumnSpacingMM, column; got != want {
		t.Fatalf("ColumnSpacingMM = %v, want %v", got, want)
	}
}

func floatValue(value *float64) any {
	if value == nil {
		return nil
	}

	return *value
}

func BenchmarkReadFile(b *testing.B) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		b.Fatal("runtime.Caller returned no file path")
	}
	path := filepath.Clean(
		filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "images", "sample-dental-radiograph.dcm"),
	)

	info, err := os.Stat(path)
	if err != nil {
		b.Skipf("sample DICOM not found at %s: %v", path, err)
	}

	b.SetBytes(info.Size())
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		meta, err := ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		_ = meta
	}
}

func sampleDicomPath(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "sample-dental-radiograph.dcm")
	if err := os.WriteFile(path, buildSampleMetadataFixture(), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	return path
}

func processedSampleDicomPath(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "sample-dental-radiograph_processed.dcm")
	if err := os.WriteFile(path, buildSampleMetadataFixture(), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	return path
}

func buildSampleMetadataFixture() []byte {
	return buildTestDicom(buildOptions{
		withPart10:                true,
		transferSyntaxUID:         "1.2.840.10008.1.2.1",
		datasetSyntax:             transferSyntax{byteOrder: binary.LittleEndian, explicit: true},
		rows:                      2,
		columns:                   4,
		photometricInterpretation: "MONOCHROME2",
		windowCenter:              "127.5",
		windowWidth:               "255",
	})
}
