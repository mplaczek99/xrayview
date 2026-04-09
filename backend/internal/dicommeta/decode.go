package dicommeta

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math/big"
	"os"
	"strings"

	"xrayview/backend/internal/contracts"
	"xrayview/backend/internal/imaging"
)

type SourceStudy struct {
	Image            imaging.SourceImage         `json:"image"`
	Metadata         SourceMetadata              `json:"metadata"`
	MeasurementScale *contracts.MeasurementScale `json:"measurementScale,omitempty"`
}

type SourceMetadata struct {
	StudyInstanceUID  string             `json:"studyInstanceUid"`
	PreservedElements []PreservedElement `json:"preservedElements"`
}

type PreservedElement struct {
	TagGroup   uint16   `json:"tagGroup"`
	TagElement uint16   `json:"tagElement"`
	VR         string   `json:"vr"`
	Values     []string `json:"values"`
}

type Decoder struct{}

type sourceDecodeConfig struct {
	bitsStored          uint16
	pixelRepresentation uint16
	slope               float32
	intercept           float32
	defaultWindow       *imaging.WindowLevel
	invert              bool
}

type sourceStudyState struct {
	metadata         Metadata
	studyInstanceUID string
	preserved        map[tag]PreservedElement
	rescaleSlope     float32
	rescaleIntercept float32
	image            imaging.SourceImage
	pixelDataFound   bool
}

var (
	tagStudyInstanceUID     = tag{group: 0x0020, element: 0x000d}
	tagRescaleIntercept     = tag{group: 0x0028, element: 0x1052}
	tagRescaleSlope         = tag{group: 0x0028, element: 0x1053}
	tagItem                 = tag{group: 0xfffe, element: 0xe000}
	preservedSourceTagOrder = []tag{
		{group: 0x0010, element: 0x0010},
		{group: 0x0010, element: 0x0020},
		{group: 0x0010, element: 0x0030},
		{group: 0x0010, element: 0x0040},
		{group: 0x0020, element: 0x0010},
		{group: 0x0008, element: 0x0020},
		{group: 0x0008, element: 0x0030},
		{group: 0x0008, element: 0x0050},
		{group: 0x0008, element: 0x1030},
		{group: 0x0008, element: 0x0090},
		{group: 0x0008, element: 0x0080},
		{group: 0x0028, element: 0x0030},
		{group: 0x0018, element: 0x1164},
		{group: 0x0018, element: 0x2010},
		{group: 0x0028, element: 0x0a04},
		{group: 0x0028, element: 0x0a02},
	}
	preservedSourceTagVRs = map[tag]string{
		{group: 0x0010, element: 0x0010}: "PN",
		{group: 0x0010, element: 0x0020}: "LO",
		{group: 0x0010, element: 0x0030}: "DA",
		{group: 0x0010, element: 0x0040}: "CS",
		{group: 0x0020, element: 0x0010}: "SH",
		{group: 0x0008, element: 0x0020}: "DA",
		{group: 0x0008, element: 0x0030}: "TM",
		{group: 0x0008, element: 0x0050}: "SH",
		{group: 0x0008, element: 0x1030}: "LO",
		{group: 0x0008, element: 0x0090}: "PN",
		{group: 0x0008, element: 0x0080}: "LO",
		{group: 0x0028, element: 0x0030}: "DS",
		{group: 0x0018, element: 0x1164}: "DS",
		{group: 0x0018, element: 0x2010}: "DS",
		{group: 0x0028, element: 0x0a04}: "CS",
		{group: 0x0028, element: 0x0a02}: "LO",
	}
)

func NewDecoder() Decoder {
	return Decoder{}
}

func (Decoder) DecodeStudy(ctx context.Context, path string) (SourceStudy, error) {
	if err := ctx.Err(); err != nil {
		return SourceStudy{}, err
	}

	study, err := DecodeFile(path)
	if err != nil {
		return SourceStudy{}, err
	}

	if err := ctx.Err(); err != nil {
		return SourceStudy{}, err
	}

	return study, nil
}

func DecodeFile(path string) (SourceStudy, error) {
	file, err := os.Open(path)
	if err != nil {
		return SourceStudy{}, fmt.Errorf("open source file: %w", err)
	}
	defer file.Close()

	study, err := Decode(file)
	if err != nil {
		if supportsStandaloneImagePath(path) {
			if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
				return SourceStudy{}, fmt.Errorf("seek source input: %w", seekErr)
			}
			if imageStudy, imageErr := tryDecodeImageStudy(file); imageErr == nil {
				return imageStudy, nil
			}
		}
		return SourceStudy{}, fmt.Errorf("decode source study from %s: %w", path, err)
	}

	return study, nil
}

func Decode(source readerAtSeeker) (SourceStudy, error) {
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return SourceStudy{}, fmt.Errorf("seek source input: %w", err)
	}

	transferSyntaxUID, err := loadTransferSyntaxUID(source)
	if err != nil {
		return SourceStudy{}, err
	}

	syntax, err := syntaxFromUID(transferSyntaxUID)
	if err != nil {
		return SourceStudy{}, err
	}

	state := sourceStudyState{
		metadata: Metadata{
			TransferSyntaxUID: transferSyntaxUID,
		},
		preserved:        make(map[tag]PreservedElement, len(preservedSourceTagOrder)),
		rescaleSlope:     1.0,
		rescaleIntercept: 0.0,
	}

	if err := parseSourceDataset(source, syntax, &state); err != nil {
		return SourceStudy{}, err
	}

	state.metadata.applyDecodeDefaults()
	if state.metadata.Rows == 0 {
		return SourceStudy{}, fmt.Errorf("invalid DICOM source: missing Rows (0028,0010)")
	}
	if state.metadata.Columns == 0 {
		return SourceStudy{}, fmt.Errorf("invalid DICOM source: missing Columns (0028,0011)")
	}
	if !state.pixelDataFound {
		return SourceStudy{}, fmt.Errorf("invalid DICOM source: missing PixelData (7fe0,0010)")
	}

	metadata := state.sourceMetadata()
	study := SourceStudy{
		Image:            state.image,
		Metadata:         metadata,
		MeasurementScale: state.metadata.MeasurementScale(),
	}
	if err := study.Image.Validate(); err != nil {
		return SourceStudy{}, fmt.Errorf("decoded source image is invalid: %w", err)
	}

	return study, nil
}

func parseSourceDataset(
	source readerAtSeeker,
	syntax transferSyntax,
	state *sourceStudyState,
) error {
	for {
		header, err := readElementHeader(source, syntax)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read dataset element: %w", err)
		}

		if header.tag == tagPixelData {
			if err := state.decodePixelData(source, syntax, header); err != nil {
				return err
			}
			return nil
		}

		if header.length == undefinedLength {
			if err := skipUndefinedValue(source, syntax); err != nil {
				return fmt.Errorf("skip undefined-length %s: %w", header.tag, err)
			}
			continue
		}

		if !tracksSourceStudyValue(header.tag) {
			if _, err := source.Seek(int64(header.length), io.SeekCurrent); err != nil {
				return fmt.Errorf("skip %s payload: %w", header.tag, err)
			}
			continue
		}

		value, err := readValue(source, header.length)
		if err != nil {
			return fmt.Errorf("read value for %s: %w", header.tag, err)
		}
		state.applyValue(syntax, header, value)
	}
}

func tracksSourceStudyValue(value tag) bool {
	if isTrackedTag(value) {
		return true
	}
	if value == tagStudyInstanceUID || value == tagRescaleIntercept || value == tagRescaleSlope {
		return true
	}
	_, ok := preservedSourceTagVRs[value]
	return ok
}

func (state *sourceStudyState) applyValue(
	syntax transferSyntax,
	header elementHeader,
	value []byte,
) {
	applyValue(&state.metadata, syntax, header, value)

	switch header.tag {
	case tagStudyInstanceUID:
		state.studyInstanceUID = trimStringValue(value)
	case tagRescaleSlope:
		if parsed := parseFloatValue(value); parsed != nil {
			state.rescaleSlope = float32(*parsed)
		}
	case tagRescaleIntercept:
		if parsed := parseFloatValue(value); parsed != nil {
			state.rescaleIntercept = float32(*parsed)
		}
	}

	vr, ok := preservedSourceTagVRs[header.tag]
	if !ok {
		return
	}
	if header.vr != "" {
		vr = header.vr
	}

	state.preserved[header.tag] = PreservedElement{
		TagGroup:   header.tag.group,
		TagElement: header.tag.element,
		VR:         strings.ToUpper(strings.TrimSpace(vr)),
		Values:     parseStringValues(value),
	}
}

func (state *sourceStudyState) sourceMetadata() SourceMetadata {
	preserved := make([]PreservedElement, 0, len(state.preserved))
	for _, candidate := range preservedSourceTagOrder {
		element, ok := state.preserved[candidate]
		if ok {
			preserved = append(preserved, element)
		}
	}

	studyInstanceUID := strings.TrimSpace(state.studyInstanceUID)
	if studyInstanceUID == "" {
		studyInstanceUID = generateSourceStudyUID()
	}

	return SourceMetadata{
		StudyInstanceUID:  studyInstanceUID,
		PreservedElements: preserved,
	}
}

func (state *sourceStudyState) decodePixelData(
	source readerAtSeeker,
	syntax transferSyntax,
	header elementHeader,
) error {
	state.metadata.PixelDataEncoding = pixelDataEncodingForHeader(header)

	var (
		image imaging.SourceImage
		err   error
	)
	if header.length == undefinedLength {
		image, err = state.decodeEncapsulatedPixelData(source, syntax)
	} else {
		raw, readErr := readValue(source, header.length)
		if readErr != nil {
			return fmt.Errorf("read value for %s: %w", header.tag, readErr)
		}
		image, err = state.decodeNativePixelData(raw, syntax.byteOrder)
	}
	if err != nil {
		return err
	}

	state.image = image
	state.pixelDataFound = true
	return nil
}

func (state *sourceStudyState) decodeNativePixelData(
	raw []byte,
	byteOrder binary.ByteOrder,
) (imaging.SourceImage, error) {
	width := int(state.metadata.Columns)
	height := int(state.metadata.Rows)
	framePixels := width * height
	samplesPerPixel := int(state.metadata.SamplesPerPixel)
	frameSampleCount := framePixels * samplesPerPixel
	cfg := state.resolveDecodeConfig()

	switch state.metadata.BitsAllocated {
	case 8:
		if err := ensureFrameLen(len(raw), frameSampleCount); err != nil {
			return imaging.SourceImage{}, err
		}
		samples := raw[:frameSampleCount]
		if samplesPerPixel == 1 {
			return buildSourceImage(
				uint32(width),
				uint32(height),
				decodeU8Monochrome(samples, cfg),
				cfg.defaultWindow,
				cfg.invert,
			)
		}

		pixels, err := state.decodeU8Color(samples, framePixels, samplesPerPixel)
		if err != nil {
			return imaging.SourceImage{}, err
		}
		return buildSourceImage(uint32(width), uint32(height), pixels, cfg.defaultWindow, cfg.invert)
	case 16:
		if samplesPerPixel != 1 {
			return imaging.SourceImage{}, fmt.Errorf("16-bit color DICOM source decode is not supported yet")
		}
		if err := ensureFrameLen(len(raw), frameSampleCount*2); err != nil {
			return imaging.SourceImage{}, err
		}

		samples := readU16Samples(raw[:frameSampleCount*2], byteOrder)
		return buildSourceImage(
			uint32(width),
			uint32(height),
			decodeU16Monochrome(samples, cfg),
			cfg.defaultWindow,
			cfg.invert,
		)
	case 32:
		if samplesPerPixel != 1 {
			return imaging.SourceImage{}, fmt.Errorf("32-bit color DICOM source decode is not supported yet")
		}
		if err := ensureFrameLen(len(raw), frameSampleCount*4); err != nil {
			return imaging.SourceImage{}, err
		}

		samples := readU32Samples(raw[:frameSampleCount*4], byteOrder)
		return buildSourceImage(
			uint32(width),
			uint32(height),
			decodeU32Monochrome(samples, cfg),
			cfg.defaultWindow,
			cfg.invert,
		)
	default:
		return imaging.SourceImage{}, fmt.Errorf(
			"unsupported BitsAllocated for source decode: %d",
			state.metadata.BitsAllocated,
		)
	}
}

func (state *sourceStudyState) decodeEncapsulatedPixelData(
	source readerAtSeeker,
	syntax transferSyntax,
) (imaging.SourceImage, error) {
	if state.metadata.NumberOfFrames > 1 {
		return imaging.SourceImage{}, fmt.Errorf(
			"unsupported multi-frame encapsulated source decode: %d frames",
			state.metadata.NumberOfFrames,
		)
	}

	header, err := readElementHeader(source, syntax)
	if err != nil {
		return imaging.SourceImage{}, fmt.Errorf("read encapsulated basic offset table: %w", err)
	}
	if header.tag != tagItem {
		return imaging.SourceImage{}, fmt.Errorf(
			"invalid encapsulated pixel data: expected item header, found %s",
			header.tag,
		)
	}
	if header.length == undefinedLength {
		return imaging.SourceImage{}, fmt.Errorf("invalid encapsulated pixel data: undefined basic offset table length")
	}
	if _, err := source.Seek(int64(header.length), io.SeekCurrent); err != nil {
		return imaging.SourceImage{}, fmt.Errorf("skip basic offset table: %w", err)
	}

	var payload bytes.Buffer
	for {
		item, err := readElementHeader(source, syntax)
		if err != nil {
			return imaging.SourceImage{}, fmt.Errorf("read encapsulated pixel fragment: %w", err)
		}

		switch item.tag {
		case tagItem:
			if item.length == undefinedLength {
				return imaging.SourceImage{}, fmt.Errorf("invalid encapsulated pixel data: undefined fragment length")
			}
			fragment, err := readValue(source, item.length)
			if err != nil {
				return imaging.SourceImage{}, fmt.Errorf("read encapsulated pixel fragment payload: %w", err)
			}
			payload.Write(fragment)
		case tagSequenceDelimitation:
			if item.length > 0 {
				if _, err := source.Seek(int64(item.length), io.SeekCurrent); err != nil {
					return imaging.SourceImage{}, fmt.Errorf("skip encapsulated sequence delimiter: %w", err)
				}
			}
			return state.decodeCompressedImage(payload.Bytes())
		default:
			return imaging.SourceImage{}, fmt.Errorf(
				"invalid encapsulated pixel data: unexpected item %s",
				item.tag,
			)
		}
	}
}

func (state *sourceStudyState) decodeCompressedImage(payload []byte) (imaging.SourceImage, error) {
	if len(payload) == 0 {
		return imaging.SourceImage{}, fmt.Errorf("encapsulated pixel data did not contain any frame bytes")
	}

	decoded, _, err := image.Decode(bytes.NewReader(payload))
	if err != nil {
		return imaging.SourceImage{}, fmt.Errorf("decode encapsulated pixel data: %w", err)
	}

	cfg := state.resolveDecodeConfig()
	return sourceImageFromImage(decoded, cfg.defaultWindow, cfg.invert)
}

func (state *sourceStudyState) decodeU8Color(
	samples []byte,
	framePixels int,
	samplesPerPixel int,
) ([]float32, error) {
	if samplesPerPixel != 3 {
		return nil, fmt.Errorf(
			"unsupported SamplesPerPixel for color source decode: %d",
			samplesPerPixel,
		)
	}

	photometric := strings.ToUpper(strings.TrimSpace(state.metadata.PhotometricInterpretation))
	if photometric != "RGB" {
		return nil, fmt.Errorf("unsupported color photometric interpretation: %s", photometric)
	}

	pixels := make([]float32, framePixels)
	switch state.metadata.PlanarConfiguration {
	case 0:
		for index, chunkOffset := 0, 0; chunkOffset+2 < len(samples); index, chunkOffset = index+1, chunkOffset+3 {
			pixels[index] = float32(
				grayFromRGB8(samples[chunkOffset], samples[chunkOffset+1], samples[chunkOffset+2]),
			)
		}
	case 1:
		if len(samples) < framePixels*3 {
			return nil, fmt.Errorf(
				"dicom frame sample count %d does not match image size %d",
				len(samples),
				framePixels*3,
			)
		}

		red := samples[:framePixels]
		green := samples[framePixels : framePixels*2]
		blue := samples[framePixels*2 : framePixels*3]
		for index := 0; index < framePixels; index += 1 {
			pixels[index] = float32(grayFromRGB8(red[index], green[index], blue[index]))
		}
	default:
		return nil, fmt.Errorf("unsupported planar configuration: %d", state.metadata.PlanarConfiguration)
	}

	return pixels, nil
}

func (state *sourceStudyState) resolveDecodeConfig() sourceDecodeConfig {
	bitsStored := state.metadata.BitsStored
	if bitsStored == 0 {
		bitsStored = state.metadata.BitsAllocated
	}

	var defaultWindow *imaging.WindowLevel
	if state.metadata.WindowCenter != nil && state.metadata.WindowWidth != nil && *state.metadata.WindowWidth > 1.0 {
		defaultWindow = &imaging.WindowLevel{
			Center: float32(*state.metadata.WindowCenter),
			Width:  float32(*state.metadata.WindowWidth),
		}
	}

	return sourceDecodeConfig{
		bitsStored:          bitsStored,
		pixelRepresentation: state.metadata.PixelRepresentation,
		slope:               state.rescaleSlope,
		intercept:           state.rescaleIntercept,
		defaultWindow:       defaultWindow,
		invert:              strings.EqualFold(state.metadata.PhotometricInterpretation, "MONOCHROME1"),
	}
}

func buildSourceImage(
	width uint32,
	height uint32,
	pixels []float32,
	defaultWindow *imaging.WindowLevel,
	invert bool,
) (imaging.SourceImage, error) {
	expected := int(width) * int(height)
	if len(pixels) != expected {
		return imaging.SourceImage{}, fmt.Errorf(
			"decoded source pixel count %d does not match dimensions %dx%d",
			len(pixels),
			width,
			height,
		)
	}

	minValue := float32(0)
	maxValue := float32(0)
	if len(pixels) > 0 {
		minValue = pixels[0]
		maxValue = pixels[0]
		for _, value := range pixels[1:] {
			if value < minValue {
				minValue = value
			}
			if value > maxValue {
				maxValue = value
			}
		}
	}

	return imaging.SourceImage{
		Width:         width,
		Height:        height,
		Format:        imaging.FormatGrayFloat32,
		Pixels:        pixels,
		MinValue:      minValue,
		MaxValue:      maxValue,
		DefaultWindow: defaultWindow,
		Invert:        invert,
	}, nil
}

func sourceImageFromImage(
	decoded image.Image,
	defaultWindow *imaging.WindowLevel,
	invert bool,
) (imaging.SourceImage, error) {
	bounds := decoded.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	pixels := make([]float32, 0, width*height)

	switch imageValue := decoded.(type) {
	case *image.Gray:
		for y := bounds.Min.Y; y < bounds.Max.Y; y += 1 {
			rowStart := imageValue.PixOffset(bounds.Min.X, y)
			row := imageValue.Pix[rowStart : rowStart+width]
			for _, value := range row {
				pixels = append(pixels, float32(value))
			}
		}
		return buildSourceImage(uint32(width), uint32(height), pixels, defaultWindow, invert)
	case *image.Gray16:
		for y := bounds.Min.Y; y < bounds.Max.Y; y += 1 {
			for x := bounds.Min.X; x < bounds.Max.X; x += 1 {
				pixels = append(pixels, float32(imageValue.Gray16At(x, y).Y))
			}
		}
		return buildSourceImage(uint32(width), uint32(height), pixels, defaultWindow, invert)
	default:
		for y := bounds.Min.Y; y < bounds.Max.Y; y += 1 {
			for x := bounds.Min.X; x < bounds.Max.X; x += 1 {
				red, green, blue, _ := decoded.At(x, y).RGBA()
				pixels = append(
					pixels,
					float32(
						grayFromRGB8(
							uint8(red>>8),
							uint8(green>>8),
							uint8(blue>>8),
						),
					),
				)
			}
		}
		return buildSourceImage(uint32(width), uint32(height), pixels, nil, false)
	}
}

func decodeU8Monochrome(samples []byte, cfg sourceDecodeConfig) []float32 {
	pixels := make([]float32, len(samples))
	for index, value := range samples {
		pixels[index] = scaledStoredPixelValue(uint32(value), cfg)
	}
	return pixels
}

func decodeU16Monochrome(samples []uint16, cfg sourceDecodeConfig) []float32 {
	pixels := make([]float32, len(samples))
	for index, value := range samples {
		pixels[index] = scaledStoredPixelValue(uint32(value), cfg)
	}
	return pixels
}

func decodeU32Monochrome(samples []uint32, cfg sourceDecodeConfig) []float32 {
	pixels := make([]float32, len(samples))
	for index, value := range samples {
		pixels[index] = scaledStoredPixelValue(value, cfg)
	}
	return pixels
}

func ensureFrameLen(actual int, expected int) error {
	if actual < expected {
		return fmt.Errorf(
			"dicom frame sample count %d does not match image size %d",
			actual,
			expected,
		)
	}

	return nil
}

func readU16Samples(raw []byte, byteOrder binary.ByteOrder) []uint16 {
	samples := make([]uint16, 0, len(raw)/2)
	for offset := 0; offset+1 < len(raw); offset += 2 {
		samples = append(samples, byteOrder.Uint16(raw[offset:offset+2]))
	}
	return samples
}

func readU32Samples(raw []byte, byteOrder binary.ByteOrder) []uint32 {
	samples := make([]uint32, 0, len(raw)/4)
	for offset := 0; offset+3 < len(raw); offset += 4 {
		samples = append(samples, byteOrder.Uint32(raw[offset:offset+4]))
	}
	return samples
}

func grayFromRGB8(red uint8, green uint8, blue uint8) uint8 {
	redValue := uint32(red)
	greenValue := uint32(green)
	blueValue := uint32(blue)
	redValue = redValue | (redValue << 8)
	greenValue = greenValue | (greenValue << 8)
	blueValue = blueValue | (blueValue << 8)

	return uint8((19595*redValue + 38470*greenValue + 7471*blueValue + (1 << 15)) >> 24)
}

func decodeStoredPixelValue(rawValue uint32, bitsStored uint16, pixelRepresentation uint16) int32 {
	if bitsStored == 0 || bitsStored > 32 {
		bitsStored = 32
	}

	masked := rawValue
	if bitsStored < 32 {
		mask := uint32(1<<bitsStored) - 1
		masked = rawValue & mask
	}

	if pixelRepresentation == 0 || bitsStored == 32 {
		return int32(masked)
	}

	signBit := uint32(1) << (bitsStored - 1)
	if masked&signBit == 0 {
		return int32(masked)
	}

	mask := uint32(1<<bitsStored) - 1
	return int32(masked | ^mask)
}

func scaledStoredPixelValue(rawValue uint32, cfg sourceDecodeConfig) float32 {
	return float32(decodeStoredPixelValue(rawValue, cfg.bitsStored, cfg.pixelRepresentation))*cfg.slope +
		cfg.intercept
}

func parseStringValues(value []byte) []string {
	raw := trimStringValue(value)
	if raw == "" {
		return []string{}
	}

	parts := strings.Split(raw, `\`)
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		values = append(values, strings.TrimSpace(part))
	}
	return values
}

func generateSourceStudyUID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "2.25.0"
	}

	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80

	return "2.25." + new(big.Int).SetBytes(raw[:]).String()
}
