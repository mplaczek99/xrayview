package export

// Writes a DICOM Secondary Capture Image SOP instance (Class
// 1.2.840.10008.5.1.4.1.1.7) carrying the processed preview as its Pixel
// Data. Output is always explicit VR little endian with the Part 10 layout:
//
//	128-byte preamble | "DICM" | file meta group (prefixed by its group-length
//	element) | dataset in ascending tag order
//
// Patient/study/series tags extracted by dicommeta on the decode side are
// round-tripped through sourceMeta.PreservedElements so the derived file
// stays linked to its source. Pixel data is written in native form — Gray8
// into a MONOCHROME2 element, RGBA8 packed down to interleaved RGB — no
// compression, no multi-frame encapsulation.

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"xrayview/backend/internal/dicommeta"
	"xrayview/backend/internal/imaging"
)

const (
	secondaryCaptureSOPClassUID = "1.2.840.10008.5.1.4.1.1.7"
	explicitVRLittleEndianUID   = "1.2.840.10008.1.2.1"
	// Writer identity — stamped into (0002,0012) and (0002,0013) on every
	// emitted file. These identify the xrayview build that produced the
	// output, not the study; treat changes as release-level metadata.
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

	datasetElements := make([]element, 0, 32)
	insertElement(&datasetElements, stringElement(0x00080016, "UI", secondaryCaptureSOPClassUID))           // SOP Class UID
	insertElement(&datasetElements, stringElement(0x00080018, "UI", sopInstanceUID))                        // SOP Instance UID
	insertElement(&datasetElements, stringElement(0x00080060, "CS", "OT"))                                  // Modality — "OT" (Other)
	insertElement(&datasetElements, multiStringElement(0x00080008, "CS", []string{"DERIVED", "SECONDARY"})) // Image Type
	insertElement(&datasetElements, stringElement(0x00080064, "CS", "WSD"))                                 // Conversion Type — workstation
	insertElement(&datasetElements, stringElement(0x00080012, "DA", dateValue))                             // Instance Creation Date
	insertElement(&datasetElements, stringElement(0x00080013, "TM", timeValue))                             // Instance Creation Time
	insertElement(&datasetElements, stringElement(0x00080023, "DA", dateValue))                             // Content Date
	insertElement(&datasetElements, stringElement(0x00080033, "TM", timeValue))                             // Content Time
	insertElement(&datasetElements, stringElement(0x0008103e, "LO", defaultProcessedSeriesDesc))            // Series Description
	insertElement(&datasetElements, stringElement(0x00082111, "ST", defaultDerivationDescription))          // Derivation Description
	insertElement(&datasetElements, stringElement(0x00080070, "LO", defaultManufacturer))                   // Manufacturer
	insertElement(&datasetElements, stringElement(0x00081090, "LO", defaultManufacturerModelName))          // Manufacturer's Model Name
	insertElement(&datasetElements, stringElement(0x00181020, "LO", defaultSoftwareVersions))               // Software Versions
	insertElement(&datasetElements, stringElement(0x0020000d, "UI", studyInstanceUID))                      // Study Instance UID
	insertElement(&datasetElements, stringElement(0x0020000e, "UI", seriesInstanceUID))                     // Series Instance UID
	insertElement(&datasetElements, stringElement(0x00200011, "IS", "999"))                                 // Series Number
	insertElement(&datasetElements, stringElement(0x00200013, "IS", "1"))                                   // Instance Number

	for _, preserved := range sourceMeta.PreservedElements {
		encoded, err := preservedElement(preserved)
		if err != nil {
			return nil, err
		}
		insertElement(&datasetElements, encoded)
	}

	for _, encoded := range pixelElements(preview) {
		insertElement(&datasetElements, encoded)
	}

	// File meta group (group 0002). Listed in ascending tag order so we can
	// skip insertElement; the (0002,0000) group-length element is written
	// separately at the head of the group with groupLength as its value.
	metaElements := []element{
		binaryElement(0x00020001, "OB", []byte{0x00, 0x01}),          // File Meta Information Version
		stringElement(0x00020002, "UI", secondaryCaptureSOPClassUID), // Media Storage SOP Class UID
		stringElement(0x00020003, "UI", sopInstanceUID),              // Media Storage SOP Instance UID
		stringElement(0x00020010, "UI", explicitVRLittleEndianUID),   // Transfer Syntax UID
		stringElement(0x00020012, "UI", implementationClassUID),      // Implementation Class UID
		stringElement(0x00020013, "SH", implementationVersionName),   // Implementation Version Name
	}

	// (0002,0000) carries the byte total of every meta element that follows
	// it — sum the encoded lengths up-front so the header can be written
	// before the elements themselves.
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

	// datasetElements is maintained in sorted tag order by insertElement.
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

// pixelElements emits Pixel Data plus the Image Pixel module attributes the
// Secondary Capture IOD requires (PS3.3 C.8.6.1 / C.7.6.3). Bits Allocated =
// Bits Stored = 8 and High Bit = 7 are fixed at this point because we are
// writing previews, not original acquisition. Grayscale uses MONOCHROME2
// plus a centered default window over 0..255 so a viewer without its own
// window defaults still renders the image; color uses interleaved RGB
// (PlanarConfiguration = 0).
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

// insertElement inserts e into elements maintaining ascending tag order.
// If an element with the same tag already exists, it is replaced.
func insertElement(elements *[]element, e element) {
	lo, hi := 0, len(*elements)
	for lo < hi {
		mid := (lo + hi) / 2
		if (*elements)[mid].tag < e.tag {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(*elements) && (*elements)[lo].tag == e.tag {
		(*elements)[lo] = e
		return
	}
	*elements = append(*elements, element{})
	copy((*elements)[lo+1:], (*elements)[lo:])
	(*elements)[lo] = e
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

// encodeStringValues joins a multi-valued VR with the DICOM "\" separator
// and pads to even byte length. Padding byte is NUL for UI (per PS3.5
// Table 6.2-1) and space for every other string VR; either is stripped by
// the reader on the way back in.
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

// encodedElementLength computes the on-wire byte count of an element under
// explicit VR little endian. The VR list below is the 32-bit-length class
// (large binary blobs + sequences): tag(4) + VR(2) + reserved(2) + length(4)
// + value. Every other VR uses the short form: tag(4) + VR(2) + length(2) +
// value. writeElement branches on the same list — keep it in sync with
// uses32BitLength in dicommeta/reader.go so round-trip decode/encode agrees.
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
