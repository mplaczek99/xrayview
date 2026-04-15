package dicommeta

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"xrayview/backend/internal/contracts"
)

const (
	part10PreambleLength               = 128
	part10Magic                        = "DICM"
	implicitLittleEndianTransferSyntax = "1.2.840.10008.1.2"
	explicitBigEndianTransferSyntax    = "1.2.840.10008.1.2.2"
	deflatedTransferSyntax             = "1.2.840.10008.1.2.1.99"
	undefinedLength                    = ^uint32(0)
)

type readerAtSeeker interface {
	io.Reader
	io.ReaderAt
	io.Seeker
}

type transferSyntax struct {
	byteOrder binary.ByteOrder
	explicit  bool
}

type tag struct {
	group   uint16
	element uint16
}

type elementHeader struct {
	tag    tag
	vr     string
	length uint32
}

type SpacingPair struct {
	RowSpacingMM    float64
	ColumnSpacingMM float64
}

type Metadata struct {
	Rows                       uint16
	Columns                    uint16
	SamplesPerPixel            uint16
	BitsAllocated              uint16
	BitsStored                 uint16
	PixelRepresentation        uint16
	PlanarConfiguration        uint16
	NumberOfFrames             uint32
	PixelDataEncoding          string
	PixelSpacing               *SpacingPair
	ImagerPixelSpacing         *SpacingPair
	NominalScannedPixelSpacing *SpacingPair
	WindowCenter               *float64
	WindowWidth                *float64
	PhotometricInterpretation  string
	TransferSyntaxUID          string
}

var (
	tagTransferSyntaxUID          = tag{group: 0x0002, element: 0x0010}
	tagSamplesPerPixel            = tag{group: 0x0028, element: 0x0002}
	tagRows                       = tag{group: 0x0028, element: 0x0010}
	tagColumns                    = tag{group: 0x0028, element: 0x0011}
	tagPhotometricInterpretation  = tag{group: 0x0028, element: 0x0004}
	tagPixelSpacing               = tag{group: 0x0028, element: 0x0030}
	tagNumberOfFrames             = tag{group: 0x0028, element: 0x0008}
	tagPlanarConfiguration        = tag{group: 0x0028, element: 0x0006}
	tagBitsAllocated              = tag{group: 0x0028, element: 0x0100}
	tagBitsStored                 = tag{group: 0x0028, element: 0x0101}
	tagPixelRepresentation        = tag{group: 0x0028, element: 0x0103}
	tagImagerPixelSpacing         = tag{group: 0x0018, element: 0x1164}
	tagNominalScannedPixelSpacing = tag{group: 0x0018, element: 0x2010}
	tagWindowCenter               = tag{group: 0x0028, element: 0x1050}
	tagWindowWidth                = tag{group: 0x0028, element: 0x1051}
	tagPixelData                  = tag{group: 0x7fe0, element: 0x0010}
	tagItemDelimitation           = tag{group: 0xfffe, element: 0xe00d}
	tagSequenceDelimitation       = tag{group: 0xfffe, element: 0xe0dd}
	fileMetaTransferSyntax        = transferSyntax{byteOrder: binary.LittleEndian, explicit: true}
)

const (
	PixelDataEncodingMissing      = "missing"
	PixelDataEncodingNative       = "native"
	PixelDataEncodingEncapsulated = "encapsulated"
)

func ReadFile(path string) (Metadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return Metadata{}, fmt.Errorf("open source file: %w", err)
	}
	defer file.Close()

	metadata, err := Read(file)
	if err != nil {
		if supportsStandaloneImagePath(path) {
			if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
				return Metadata{}, fmt.Errorf("seek source input: %w", seekErr)
			}
			if imageMetadata, imageErr := tryReadImageMetadata(file); imageErr == nil {
				return imageMetadata, nil
			}
		}
		return Metadata{}, fmt.Errorf("read source metadata from %s: %w", path, err)
	}

	return metadata, nil
}

func Read(source readerAtSeeker) (Metadata, error) {
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return Metadata{}, fmt.Errorf("seek source input: %w", err)
	}

	transferSyntaxUID, err := loadTransferSyntaxUID(source)
	if err != nil {
		return Metadata{}, err
	}

	syntax, err := syntaxFromUID(transferSyntaxUID)
	if err != nil {
		return Metadata{}, err
	}

	metadata := Metadata{
		TransferSyntaxUID: transferSyntaxUID,
	}
	if err := parseDataset(source, syntax, &metadata); err != nil {
		return Metadata{}, err
	}
	metadata.applyDecodeDefaults()

	if metadata.Rows == 0 {
		return Metadata{}, fmt.Errorf("invalid DICOM metadata: missing Rows (0028,0010)")
	}
	if metadata.Columns == 0 {
		return Metadata{}, fmt.Errorf("invalid DICOM metadata: missing Columns (0028,0011)")
	}

	return metadata, nil
}

func (metadata Metadata) MeasurementScale() *contracts.MeasurementScale {
	candidates := []struct {
		pair   *SpacingPair
		source string
	}{
		{pair: metadata.PixelSpacing, source: "PixelSpacing"},
		{pair: metadata.ImagerPixelSpacing, source: "ImagerPixelSpacing"},
		{pair: metadata.NominalScannedPixelSpacing, source: "NominalScannedPixelSpacing"},
	}

	for _, candidate := range candidates {
		if candidate.pair == nil {
			continue
		}
		if candidate.pair.RowSpacingMM <= 0 || candidate.pair.ColumnSpacingMM <= 0 {
			continue
		}

		return &contracts.MeasurementScale{
			RowSpacingMM:    candidate.pair.RowSpacingMM,
			ColumnSpacingMM: candidate.pair.ColumnSpacingMM,
			Source:          candidate.source,
		}
	}

	return nil
}

func loadTransferSyntaxUID(source readerAtSeeker) (string, error) {
	hasPart10, err := hasPart10Magic(source)
	if err != nil {
		return "", err
	}

	if !hasPart10 {
		if _, err := source.Seek(0, io.SeekStart); err != nil {
			return "", fmt.Errorf("seek raw DICOM dataset: %w", err)
		}
		return implicitLittleEndianTransferSyntax, nil
	}

	if _, err := source.Seek(int64(part10PreambleLength+len(part10Magic)), io.SeekStart); err != nil {
		return "", fmt.Errorf("seek file meta: %w", err)
	}

	transferSyntaxUID := ""
	for {
		nextGroup, err := peekGroup(source, binary.LittleEndian)
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("peek file meta tag: %w", err)
		}
		if nextGroup != 0x0002 {
			break
		}

		header, err := readElementHeader(source, fileMetaTransferSyntax)
		if err != nil {
			return "", fmt.Errorf("read file meta element: %w", err)
		}
		if header.length == undefinedLength {
			return "", fmt.Errorf("invalid DICOM file meta: undefined length on %s", header.tag)
		}

		value, err := readValue(source, header.length)
		if err != nil {
			return "", fmt.Errorf("read file meta value for %s: %w", header.tag, err)
		}
		if header.tag == tagTransferSyntaxUID {
			transferSyntaxUID = trimStringValue(value)
		}
	}

	if transferSyntaxUID == "" {
		return "", fmt.Errorf("invalid DICOM file meta: missing TransferSyntaxUID (0002,0010)")
	}

	return transferSyntaxUID, nil
}

func parseDataset(source readerAtSeeker, syntax transferSyntax, metadata *Metadata) error {
	// Reused for element values <= 4 bytes (US, SS, UL, SL). Escapes once per
	// call rather than one heap allocation per small element (the common case).
	var smallBuf [4]byte

	for {
		header, err := readElementHeader(source, syntax)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read dataset element: %w", err)
		}

		if header.tag == tagPixelData {
			metadata.PixelDataEncoding = pixelDataEncodingForHeader(header)
			return nil
		}

		if header.length == undefinedLength {
			if err := skipUndefinedValue(source, syntax); err != nil {
				return fmt.Errorf("skip undefined-length %s: %w", header.tag, err)
			}
			continue
		}

		if !isTrackedTag(header.tag) {
			if _, err := source.Seek(int64(header.length), io.SeekCurrent); err != nil {
				return fmt.Errorf("skip %s payload: %w", header.tag, err)
			}
			continue
		}

		if header.length <= 4 {
			if _, err := io.ReadFull(source, smallBuf[:header.length]); err != nil {
				return fmt.Errorf("read value for %s: %w", header.tag, err)
			}
			applyValue(metadata, syntax, header, smallBuf[:header.length])
			continue
		}

		value, err := readValue(source, header.length)
		if err != nil {
			return fmt.Errorf("read value for %s: %w", header.tag, err)
		}
		applyValue(metadata, syntax, header, value)
	}
}

func applyValue(metadata *Metadata, syntax transferSyntax, header elementHeader, value []byte) {
	switch header.tag {
	case tagSamplesPerPixel:
		if parsed, ok := parseUint16Value(value, syntax.byteOrder); ok {
			metadata.SamplesPerPixel = parsed
		}
	case tagRows:
		if parsed, ok := parseUint16Value(value, syntax.byteOrder); ok {
			metadata.Rows = parsed
		}
	case tagColumns:
		if parsed, ok := parseUint16Value(value, syntax.byteOrder); ok {
			metadata.Columns = parsed
		}
	case tagPixelSpacing:
		metadata.PixelSpacing = parseSpacingPair(value)
	case tagNumberOfFrames:
		if parsed, ok := parseIntStringValue(value, 32); ok {
			metadata.NumberOfFrames = parsed
		}
	case tagPlanarConfiguration:
		if parsed, ok := parseUint16Value(value, syntax.byteOrder); ok {
			metadata.PlanarConfiguration = parsed
		}
	case tagBitsAllocated:
		if parsed, ok := parseUint16Value(value, syntax.byteOrder); ok {
			metadata.BitsAllocated = parsed
		}
	case tagBitsStored:
		if parsed, ok := parseUint16Value(value, syntax.byteOrder); ok {
			metadata.BitsStored = parsed
		}
	case tagPixelRepresentation:
		if parsed, ok := parseUint16Value(value, syntax.byteOrder); ok {
			metadata.PixelRepresentation = parsed
		}
	case tagImagerPixelSpacing:
		metadata.ImagerPixelSpacing = parseSpacingPair(value)
	case tagNominalScannedPixelSpacing:
		metadata.NominalScannedPixelSpacing = parseSpacingPair(value)
	case tagWindowCenter:
		metadata.WindowCenter = parseFloatValue(value)
	case tagWindowWidth:
		metadata.WindowWidth = parseFloatValue(value)
	case tagPhotometricInterpretation:
		metadata.PhotometricInterpretation = trimStringValue(value)
	}
}

func (metadata *Metadata) applyDecodeDefaults() {
	if metadata.SamplesPerPixel == 0 {
		metadata.SamplesPerPixel = 1
	}
	if metadata.NumberOfFrames == 0 {
		metadata.NumberOfFrames = 1
	}
	if metadata.BitsStored == 0 && metadata.BitsAllocated > 0 {
		metadata.BitsStored = metadata.BitsAllocated
	}
	if metadata.PixelDataEncoding == "" {
		metadata.PixelDataEncoding = PixelDataEncodingMissing
	}
}

func pixelDataEncodingForHeader(header elementHeader) string {
	if header.length == undefinedLength {
		return PixelDataEncodingEncapsulated
	}

	return PixelDataEncodingNative
}

func syntaxFromUID(uid string) (transferSyntax, error) {
	switch uid {
	case implicitLittleEndianTransferSyntax:
		return transferSyntax{byteOrder: binary.LittleEndian, explicit: false}, nil
	case explicitBigEndianTransferSyntax:
		return transferSyntax{byteOrder: binary.BigEndian, explicit: true}, nil
	case deflatedTransferSyntax:
		return transferSyntax{}, fmt.Errorf("unsupported deflated transfer syntax for metadata reader: %s", uid)
	case "":
		return transferSyntax{}, fmt.Errorf("invalid DICOM metadata: empty transfer syntax UID")
	default:
		// Compressed transfer syntaxes still encode the dataset as explicit VR little endian.
		return transferSyntax{byteOrder: binary.LittleEndian, explicit: true}, nil
	}
}

func hasPart10Magic(source readerAtSeeker) (bool, error) {
	header := make([]byte, part10PreambleLength+len(part10Magic))
	n, err := source.ReadAt(header, 0)
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read DICOM header: %w", err)
	}
	if n < len(header) {
		return false, nil
	}

	return string(header[part10PreambleLength:]) == part10Magic, nil
}

func peekGroup(source readerAtSeeker, byteOrder binary.ByteOrder) (uint16, error) {
	offset, err := source.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, fmt.Errorf("resolve current offset: %w", err)
	}

	raw := make([]byte, 4)
	n, readErr := source.ReadAt(raw, offset)
	if readErr != nil && readErr != io.EOF {
		return 0, readErr
	}
	if n < len(raw) {
		return 0, io.EOF
	}

	return byteOrder.Uint16(raw[:2]), nil
}

func readElementHeader(source readerAtSeeker, syntax transferSyntax) (elementHeader, error) {
	// Single 4-byte scratch buffer reused for all small reads in this call.
	// Reduces per-element header allocations from 3-4 (readValue calls) to 1.
	var buf [4]byte

	if _, err := io.ReadFull(source, buf[:4]); err != nil {
		return elementHeader{}, err
	}

	header := elementHeader{
		tag: tag{
			group:   syntax.byteOrder.Uint16(buf[:2]),
			element: syntax.byteOrder.Uint16(buf[2:]),
		},
	}

	if header.tag.group == 0xfffe {
		if _, err := io.ReadFull(source, buf[:4]); err != nil {
			return elementHeader{}, err
		}
		header.length = syntax.byteOrder.Uint32(buf[:4])
		return header, nil
	}

	if syntax.explicit {
		if _, err := io.ReadFull(source, buf[:2]); err != nil {
			return elementHeader{}, err
		}
		header.vr = string(buf[:2])

		if uses32BitLength(header.vr) {
			if _, err := io.ReadFull(source, buf[:2]); err != nil { // skip reserved 2 bytes
				return elementHeader{}, err
			}
			if _, err := io.ReadFull(source, buf[:4]); err != nil {
				return elementHeader{}, err
			}
			header.length = syntax.byteOrder.Uint32(buf[:4])
			return header, nil
		}

		if _, err := io.ReadFull(source, buf[:2]); err != nil {
			return elementHeader{}, err
		}
		header.length = uint32(syntax.byteOrder.Uint16(buf[:2]))
		return header, nil
	}

	if _, err := io.ReadFull(source, buf[:4]); err != nil {
		return elementHeader{}, err
	}
	header.length = syntax.byteOrder.Uint32(buf[:4])
	return header, nil
}

func readValue(source readerAtSeeker, length uint32) ([]byte, error) {
	if length == 0 {
		return []byte{}, nil
	}

	value := make([]byte, length)
	if _, err := io.ReadFull(source, value); err != nil {
		return nil, err
	}
	return value, nil
}

func skipUndefinedValue(source readerAtSeeker, syntax transferSyntax) error {
	depth := 1
	for depth > 0 {
		header, err := readElementHeader(source, syntax)
		if err != nil {
			return err
		}

		switch header.tag {
		case tagItemDelimitation, tagSequenceDelimitation:
			if header.length > 0 {
				if _, err := source.Seek(int64(header.length), io.SeekCurrent); err != nil {
					return err
				}
			}
			depth--
		default:
			if header.length == undefinedLength {
				depth++
				continue
			}
			if _, err := source.Seek(int64(header.length), io.SeekCurrent); err != nil {
				return err
			}
		}
	}

	return nil
}

func uses32BitLength(vr string) bool {
	switch vr {
	case "OB", "OD", "OF", "OL", "OV", "OW", "SQ", "UC", "UR", "UT", "UN":
		return true
	default:
		return false
	}
}

func isTrackedTag(value tag) bool {
	switch value {
	case tagSamplesPerPixel,
		tagRows,
		tagColumns,
		tagPhotometricInterpretation,
		tagPixelSpacing,
		tagNumberOfFrames,
		tagPlanarConfiguration,
		tagBitsAllocated,
		tagBitsStored,
		tagPixelRepresentation,
		tagImagerPixelSpacing,
		tagNominalScannedPixelSpacing,
		tagWindowCenter,
		tagWindowWidth:
		return true
	default:
		return false
	}
}

func parseUint16Value(
	value []byte,
	byteOrder binary.ByteOrder,
) (uint16, bool) {
	if len(value) == 2 {
		return byteOrder.Uint16(value), true
	}

	trimmed := trimStringValue(value)
	if trimmed == "" {
		return 0, false
	}

	parsed, err := strconv.ParseUint(firstComponent(trimmed), 10, 16)
	if err != nil {
		return 0, false
	}

	return uint16(parsed), true
}

func parseIntStringValue(value []byte, bits int) (uint32, bool) {
	trimmed := trimStringValue(value)
	if trimmed == "" {
		return 0, false
	}

	parsed, err := strconv.ParseUint(firstComponent(trimmed), 10, bits)
	if err != nil {
		return 0, false
	}

	return uint32(parsed), true
}

func parseSpacingPair(value []byte) *SpacingPair {
	row, column, ok := parseFloatPair(trimStringValue(value))
	if !ok {
		return nil
	}

	return &SpacingPair{
		RowSpacingMM:    row,
		ColumnSpacingMM: column,
	}
}

func parseFloatValue(value []byte) *float64 {
	raw := firstComponent(trimStringValue(value))
	if raw == "" {
		return nil
	}

	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}

	return &parsed
}

func parseFloatPair(raw string) (float64, float64, bool) {
	parts := strings.Split(raw, `\`)
	if len(parts) < 2 {
		return 0, 0, false
	}

	first, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return 0, 0, false
	}
	second, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return 0, 0, false
	}

	return first, second, true
}

func trimStringValue(value []byte) string {
	return strings.TrimRight(string(value), " \x00")
}

func firstComponent(raw string) string {
	return strings.TrimSpace(strings.Split(raw, `\`)[0])
}

func (value tag) String() string {
	return fmt.Sprintf("(%04x,%04x)", value.group, value.element)
}
