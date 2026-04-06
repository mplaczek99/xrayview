package dicommeta

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReadFileSampleDentalRadiographMetadata(t *testing.T) {
	metadata, err := ReadFile(sampleDicomPath(t))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if got, want := metadata.Rows, uint16(1088); got != want {
		t.Fatalf("Rows = %d, want %d", got, want)
	}
	if got, want := metadata.Columns, uint16(2048); got != want {
		t.Fatalf("Columns = %d, want %d", got, want)
	}
	if got, want := metadata.PhotometricInterpretation, "MONOCHROME2"; got != want {
		t.Fatalf("PhotometricInterpretation = %q, want %q", got, want)
	}
	if got, want := metadata.TransferSyntaxUID, "1.2.840.10008.1.2.1"; got != want {
		t.Fatalf("TransferSyntaxUID = %q, want %q", got, want)
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
}

type buildOptions struct {
	withPart10                 bool
	transferSyntaxUID          string
	datasetSyntax              transferSyntax
	rows                       uint16
	columns                    uint16
	photometricInterpretation  string
	pixelSpacing               string
	imagerPixelSpacing         string
	nominalScannedPixelSpacing string
	windowCenter               string
	windowWidth                string
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

	writeElement(
		&payload,
		options.datasetSyntax,
		tagPixelData,
		"OB",
		nil,
	)

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

func sampleDicomPath(t *testing.T) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller returned no file path")
	}

	return filepath.Clean(
		filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "images", "sample-dental-radiograph.dcm"),
	)
}
