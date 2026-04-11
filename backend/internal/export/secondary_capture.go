package export

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strings"
	"time"

	"xrayview/backend/internal/dicommeta"
	"xrayview/backend/internal/imaging"
)

const (
	secondaryCaptureSOPClassUID  = "1.2.840.10008.5.1.4.1.1.7"
	explicitVRLittleEndianUID    = "1.2.840.10008.1.2.1"
	implementationClassUID       = "2.25.302043790172249692526321623266752743501"
	implementationVersionName    = "XRAYVIEW_GO_1_0"
	defaultProcessedSeriesDesc   = "XRayView Processed"
	defaultDerivationDescription = "Processed by XRayView"
	defaultManufacturer          = "XRayView"
	defaultManufacturerModelName = "xrayview"
	defaultSoftwareVersions      = "xrayview"
)

type uidGenerator func() (string, error)

type element struct {
	tag   uint32
	vr    string
	value []byte
}

func WriteSecondaryCapture(
	path string,
	preview imaging.PreviewImage,
	sourceMeta dicommeta.SourceMetadata,
) error {
	payload, err := encodeSecondaryCapture(preview, sourceMeta, time.Now().UTC(), generateUID)
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("write secondary capture %s: %w", path, err)
	}

	return nil
}

func encodeSecondaryCapture(
	preview imaging.PreviewImage,
	sourceMeta dicommeta.SourceMetadata,
	now time.Time,
	newUID uidGenerator,
) ([]byte, error) {
	if err := preview.Validate(); err != nil {
		return nil, fmt.Errorf("validate preview image: %w", err)
	}
	if newUID == nil {
		newUID = generateUID
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if preview.Width > 0xffff || preview.Height > 0xffff {
		return nil, fmt.Errorf(
			"secondary capture dimensions must fit DICOM Rows/Columns, got %dx%d",
			preview.Width,
			preview.Height,
		)
	}

	studyInstanceUID := strings.TrimSpace(sourceMeta.StudyInstanceUID)
	if studyInstanceUID == "" {
		var err error
		studyInstanceUID, err = newUID()
		if err != nil {
			return nil, fmt.Errorf("generate study instance uid: %w", err)
		}
	}

	sopInstanceUID, err := newUID()
	if err != nil {
		return nil, fmt.Errorf("generate sop instance uid: %w", err)
	}
	seriesInstanceUID, err := newUID()
	if err != nil {
		return nil, fmt.Errorf("generate series instance uid: %w", err)
	}

	dateValue := now.Format("20060102")
	timeValue := now.Format("150405")

	elements := make(map[uint32]element)
	putElement(elements, stringElement(0x00080016, "UI", secondaryCaptureSOPClassUID))
	putElement(elements, stringElement(0x00080018, "UI", sopInstanceUID))
	putElement(elements, stringElement(0x00080060, "CS", "OT"))
	putElement(elements, multiStringElement(0x00080008, "CS", []string{"DERIVED", "SECONDARY"}))
	putElement(elements, stringElement(0x00080064, "CS", "WSD"))
	putElement(elements, stringElement(0x00080012, "DA", dateValue))
	putElement(elements, stringElement(0x00080013, "TM", timeValue))
	putElement(elements, stringElement(0x00080023, "DA", dateValue))
	putElement(elements, stringElement(0x00080033, "TM", timeValue))
	putElement(elements, stringElement(0x0008103e, "LO", defaultProcessedSeriesDesc))
	putElement(elements, stringElement(0x00082111, "ST", defaultDerivationDescription))
	putElement(elements, stringElement(0x00080070, "LO", defaultManufacturer))
	putElement(elements, stringElement(0x00081090, "LO", defaultManufacturerModelName))
	putElement(elements, stringElement(0x00181020, "LO", defaultSoftwareVersions))
	putElement(elements, stringElement(0x0020000d, "UI", studyInstanceUID))
	putElement(elements, stringElement(0x0020000e, "UI", seriesInstanceUID))
	putElement(elements, stringElement(0x00200011, "IS", "999"))
	putElement(elements, stringElement(0x00200013, "IS", "1"))

	for _, preserved := range sourceMeta.PreservedElements {
		encoded, err := preservedElement(preserved)
		if err != nil {
			return nil, err
		}
		putElement(elements, encoded)
	}

	for _, encoded := range pixelElements(preview) {
		putElement(elements, encoded)
	}

	metaElements := []element{
		binaryElement(0x00020001, "OB", []byte{0x00, 0x01}),
		stringElement(0x00020002, "UI", secondaryCaptureSOPClassUID),
		stringElement(0x00020003, "UI", sopInstanceUID),
		stringElement(0x00020010, "UI", explicitVRLittleEndianUID),
		stringElement(0x00020012, "UI", implementationClassUID),
		stringElement(0x00020013, "SH", implementationVersionName),
	}

	sortElements(metaElements)

	groupLength := 0
	for _, encoded := range metaElements {
		length, err := encodedElementLength(encoded)
		if err != nil {
			return nil, err
		}
		groupLength += length
	}

	// Preallocate buffer: 128 (preamble) + 4 (DICM) + meta overhead +
	// dataset metadata (~2KB generous) + pixel data (dominates size).
	pixelSize := len(preview.Pixels)
	if preview.Format == imaging.FormatRGBA8 {
		pixelSize = pixelSize / 4 * 3 // RGBA→RGB conversion
	}
	estimatedSize := 128 + 4 + groupLength + 2048 + pixelSize
	var payload bytes.Buffer
	payload.Grow(estimatedSize)
	payload.Write(make([]byte, 128))
	payload.WriteString("DICM")
	if err := writeElement(&payload, u32Element(0x00020000, groupLength)); err != nil {
		return nil, err
	}
	for _, encoded := range metaElements {
		if err := writeElement(&payload, encoded); err != nil {
			return nil, err
		}
	}

	datasetElements := make([]element, 0, len(elements))
	for _, encoded := range elements {
		datasetElements = append(datasetElements, encoded)
	}
	sortElements(datasetElements)
	for _, encoded := range datasetElements {
		if err := writeElement(&payload, encoded); err != nil {
			return nil, err
		}
	}

	return payload.Bytes(), nil
}

func preservedElement(source dicommeta.PreservedElement) (element, error) {
	vr := strings.ToUpper(strings.TrimSpace(source.VR))
	if source.TagGroup == 0x0002 {
		return element{}, fmt.Errorf(
			"preserved element (%04x,%04x) cannot target file meta information",
			source.TagGroup,
			source.TagElement,
		)
	}
	if !isSupportedStringVR(vr) {
		return element{}, fmt.Errorf(
			"unsupported preserved element VR %q for (%04x,%04x)",
			vr,
			source.TagGroup,
			source.TagElement,
		)
	}

	return multiStringElement(tagValue(source.TagGroup, source.TagElement), vr, source.Values), nil
}

func pixelElements(preview imaging.PreviewImage) []element {
	width := uint16(preview.Width)
	height := uint16(preview.Height)

	switch preview.Format {
	case imaging.FormatGray8:
		return []element{
			u16Element(0x00280010, height),
			u16Element(0x00280011, width),
			u16Element(0x00280002, 1),
			stringElement(0x00280004, "CS", "MONOCHROME2"),
			u16Element(0x00280100, 8),
			u16Element(0x00280101, 8),
			u16Element(0x00280102, 7),
			u16Element(0x00280103, 0),
			stringElement(0x00281050, "DS", "127.5"),
			stringElement(0x00281051, "DS", "255"),
			binaryElement(0x7fe00010, "OB", evenLengthBytes(preview.Pixels, 0x00)),
		}
	case imaging.FormatRGBA8:
		return []element{
			u16Element(0x00280010, height),
			u16Element(0x00280011, width),
			u16Element(0x00280002, 3),
			stringElement(0x00280004, "CS", "RGB"),
			u16Element(0x00280006, 0),
			u16Element(0x00280100, 8),
			u16Element(0x00280101, 8),
			u16Element(0x00280102, 7),
			u16Element(0x00280103, 0),
			rgbaPixelElement(0x7fe00010, preview.Pixels),
		}
	default:
		return nil
	}
}

func putElement(elements map[uint32]element, encoded element) {
	elements[encoded.tag] = encoded
}

func stringElement(tag uint32, vr string, value string) element {
	return multiStringElement(tag, vr, []string{value})
}

func multiStringElement(tag uint32, vr string, values []string) element {
	return element{
		tag:   tag,
		vr:    vr,
		value: encodeStringValues(vr, values),
	}
}

func u16Element(tag uint32, value uint16) element {
	var raw [2]byte
	binary.LittleEndian.PutUint16(raw[:], value)
	return element{tag: tag, vr: "US", value: raw[:]}
}

func u32Element(tag uint32, value int) element {
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], uint32(value))
	return element{tag: tag, vr: "UL", value: raw[:]}
}

func binaryElement(tag uint32, vr string, value []byte) element {
	return element{
		tag:   tag,
		vr:    vr,
		value: append([]byte(nil), value...),
	}
}

func encodeStringValues(vr string, values []string) []byte {
	joined := strings.Join(values, "\\")
	padding := byte(' ')
	if strings.EqualFold(vr, "UI") {
		padding = 0x00
	}

	return evenLengthBytes([]byte(joined), padding)
}

func evenLengthBytes(value []byte, padding byte) []byte {
	raw := append([]byte(nil), value...)
	if len(raw)%2 != 0 {
		raw = append(raw, padding)
	}
	return raw
}

func isSupportedStringVR(vr string) bool {
	switch strings.ToUpper(vr) {
	case "AE", "AS", "CS", "DA", "DS", "DT", "IS", "LO", "LT", "PN", "SH", "ST", "TM", "UC", "UI", "UR", "UT":
		return true
	default:
		return false
	}
}

func sortElements(elements []element) {
	sort.Slice(elements, func(left, right int) bool {
		if elementGroup(elements[left].tag) != elementGroup(elements[right].tag) {
			return elementGroup(elements[left].tag) < elementGroup(elements[right].tag)
		}
		return elementNumber(elements[left].tag) < elementNumber(elements[right].tag)
	})
}

func encodedElementLength(encoded element) (int, error) {
	switch normalizedVR(encoded.vr) {
	case "OB", "OD", "OF", "OL", "OW", "SQ", "UC", "UR", "UT", "UN":
		return 12 + len(encoded.value), nil
	default:
		if len(encoded.value) > 0xffff {
			return 0, fmt.Errorf(
				"element (%04x,%04x) with VR %s exceeds 16-bit value length",
				elementGroup(encoded.tag),
				elementNumber(encoded.tag),
				encoded.vr,
			)
		}
		return 8 + len(encoded.value), nil
	}
}

func writeElement(buffer *bytes.Buffer, encoded element) error {
	if _, err := encodedElementLength(encoded); err != nil {
		return err
	}

	var rawTag [4]byte
	binary.LittleEndian.PutUint16(rawTag[0:2], elementGroup(encoded.tag))
	binary.LittleEndian.PutUint16(rawTag[2:4], elementNumber(encoded.tag))
	buffer.Write(rawTag[:])

	vr := normalizedVR(encoded.vr)
	if len(vr) != 2 {
		return fmt.Errorf(
			"element (%04x,%04x) has invalid VR %q",
			elementGroup(encoded.tag),
			elementNumber(encoded.tag),
			encoded.vr,
		)
	}

	buffer.WriteString(vr)

	switch vr {
	case "OB", "OD", "OF", "OL", "OW", "SQ", "UC", "UR", "UT", "UN":
		buffer.Write([]byte{0x00, 0x00})
		var rawLength [4]byte
		binary.LittleEndian.PutUint32(rawLength[:], uint32(len(encoded.value)))
		buffer.Write(rawLength[:])
	default:
		var rawLength [2]byte
		binary.LittleEndian.PutUint16(rawLength[:], uint16(len(encoded.value)))
		buffer.Write(rawLength[:])
	}

	buffer.Write(encoded.value)
	return nil
}

// rgbaPixelElement converts RGBA pixel data to RGB directly into a single
// element value buffer, avoiding intermediate allocations from rgbaToRGB,
// evenLengthBytes, and binaryElement's defensive copy.
func rgbaPixelElement(tag uint32, rgba []uint8) element {
	rgbLen := len(rgba) / 4 * 3
	paddedLen := rgbLen
	if paddedLen%2 != 0 {
		paddedLen++ // DICOM even-length padding; pad byte is 0x00 from make
	}
	rgb := make([]byte, paddedLen)
	j := 0
	for offset := 0; offset+3 < len(rgba); offset += 4 {
		rgb[j] = rgba[offset]
		rgb[j+1] = rgba[offset+1]
		rgb[j+2] = rgba[offset+2]
		j += 3
	}
	return element{tag: tag, vr: "OB", value: rgb}
}

func generateUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("read random uid bytes: %w", err)
	}

	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80

	return "2.25." + new(big.Int).SetBytes(raw[:]).String(), nil
}

func normalizedVR(vr string) string {
	return strings.ToUpper(strings.TrimSpace(vr))
}

func tagValue(group uint16, elementNumber uint16) uint32 {
	return uint32(group)<<16 | uint32(elementNumber)
}

func elementGroup(tag uint32) uint16 {
	return uint16(tag >> 16)
}

func elementNumber(tag uint32) uint16 {
	return uint16(tag)
}
