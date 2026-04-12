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
	"math/bits"
	"os"
	"strings"
	"unsafe"

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
	source, closeSource, err := openFileSource(path)
	if err != nil {
		return SourceStudy{}, fmt.Errorf("open source file: %w", err)
	}
	defer closeSource()

	study, err := Decode(source)
	if err != nil {
		if supportsStandaloneImagePath(path) {
			if _, seekErr := source.Seek(0, io.SeekStart); seekErr != nil {
				return SourceStudy{}, fmt.Errorf("seek source input: %w", seekErr)
			}
			if imageStudy, imageErr := tryDecodeImageStudy(source); imageErr == nil {
				return imageStudy, nil
			}
		}
		return SourceStudy{}, fmt.Errorf("decode source study from %s: %w", path, err)
	}

	return study, nil
}

// openFileSource opens path for reading. For large files on supported platforms
// it memory-maps the file to avoid userspace buffering overhead; on failure or
// small files it falls back to the regular *os.File. The caller must invoke the
// returned close function to release resources.
func openFileSource(path string) (readerAtSeeker, func() error, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}

	if source, closer, err := tryMmapFile(file, info.Size()); source != nil {
		return source, closer, err
	}

	return file, file.Close, nil
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
			pixels, minVal, maxVal := decodeU8Monochrome(samples, cfg)
			return buildSourceImage(
				uint32(width),
				uint32(height),
				pixels,
				minVal,
				maxVal,
				cfg.defaultWindow,
				cfg.invert,
			)
		}

		pixels, minVal, maxVal, err := state.decodeU8Color(samples, framePixels, samplesPerPixel)
		if err != nil {
			return imaging.SourceImage{}, err
		}
		return buildSourceImage(uint32(width), uint32(height), pixels, minVal, maxVal, cfg.defaultWindow, cfg.invert)
	case 16:
		if samplesPerPixel != 1 {
			return imaging.SourceImage{}, fmt.Errorf("16-bit color DICOM source decode is not supported yet")
		}
		if err := ensureFrameLen(len(raw), frameSampleCount*2); err != nil {
			return imaging.SourceImage{}, err
		}

		samples := readU16Samples(raw[:frameSampleCount*2], byteOrder)
		pixels, minVal, maxVal := decodeU16Monochrome(samples, cfg)
		return buildSourceImage(
			uint32(width),
			uint32(height),
			pixels,
			minVal,
			maxVal,
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
		pixels, minVal, maxVal := decodeU32Monochrome(samples, cfg)
		return buildSourceImage(
			uint32(width),
			uint32(height),
			pixels,
			minVal,
			maxVal,
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
) ([]float32, float32, float32, error) {
	if samplesPerPixel != 3 {
		return nil, 0, 0, fmt.Errorf(
			"unsupported SamplesPerPixel for color source decode: %d",
			samplesPerPixel,
		)
	}

	photometric := strings.ToUpper(strings.TrimSpace(state.metadata.PhotometricInterpretation))
	if photometric != "RGB" {
		return nil, 0, 0, fmt.Errorf("unsupported color photometric interpretation: %s", photometric)
	}

	if framePixels == 0 {
		return nil, 0, 0, nil
	}

	pixels := make([]float32, framePixels)
	var minVal, maxVal float32
	switch state.metadata.PlanarConfiguration {
	case 0:
		if chunkOffset := 0; chunkOffset+2 < len(samples) {
			first := float32(grayFromRGB8(samples[chunkOffset], samples[chunkOffset+1], samples[chunkOffset+2]))
			pixels[0] = first
			minVal, maxVal = first, first
		}
		for index, chunkOffset := 1, 3; chunkOffset+2 < len(samples); index, chunkOffset = index+1, chunkOffset+3 {
			v := float32(grayFromRGB8(samples[chunkOffset], samples[chunkOffset+1], samples[chunkOffset+2]))
			pixels[index] = v
			if v < minVal {
				minVal = v
			}
			if v > maxVal {
				maxVal = v
			}
		}
	case 1:
		if len(samples) < framePixels*3 {
			return nil, 0, 0, fmt.Errorf(
				"dicom frame sample count %d does not match image size %d",
				len(samples),
				framePixels*3,
			)
		}

		red := samples[:framePixels]
		green := samples[framePixels : framePixels*2]
		blue := samples[framePixels*2 : framePixels*3]
		first := float32(grayFromRGB8(red[0], green[0], blue[0]))
		pixels[0] = first
		minVal, maxVal = first, first
		for index := 1; index < framePixels; index++ {
			v := float32(grayFromRGB8(red[index], green[index], blue[index]))
			pixels[index] = v
			if v < minVal {
				minVal = v
			}
			if v > maxVal {
				maxVal = v
			}
		}
	default:
		return nil, 0, 0, fmt.Errorf("unsupported planar configuration: %d", state.metadata.PlanarConfiguration)
	}

	return pixels, minVal, maxVal, nil
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
	minValue float32,
	maxValue float32,
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
	n := width * height
	if n == 0 {
		return buildSourceImage(uint32(width), uint32(height), nil, 0, 0, defaultWindow, invert)
	}
	pixels := make([]float32, 0, n)

	switch imageValue := decoded.(type) {
	case *image.Gray:
		rowStart := imageValue.PixOffset(bounds.Min.X, bounds.Min.Y)
		row := imageValue.Pix[rowStart : rowStart+width]
		first := float32(row[0])
		pixels = append(pixels, first)
		minVal, maxVal := first, first
		for _, value := range row[1:] {
			v := float32(value)
			pixels = append(pixels, v)
			if v < minVal {
				minVal = v
			}
			if v > maxVal {
				maxVal = v
			}
		}
		for y := bounds.Min.Y + 1; y < bounds.Max.Y; y++ {
			rowStart = imageValue.PixOffset(bounds.Min.X, y)
			row = imageValue.Pix[rowStart : rowStart+width]
			for _, value := range row {
				v := float32(value)
				pixels = append(pixels, v)
				if v < minVal {
					minVal = v
				}
				if v > maxVal {
					maxVal = v
				}
			}
		}
		return buildSourceImage(uint32(width), uint32(height), pixels, minVal, maxVal, defaultWindow, invert)
	case *image.Gray16:
		first := float32(imageValue.Gray16At(bounds.Min.X, bounds.Min.Y).Y)
		pixels = append(pixels, first)
		minVal, maxVal := first, first
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			startX := bounds.Min.X
			if y == bounds.Min.Y {
				startX++
			}
			for x := startX; x < bounds.Max.X; x++ {
				v := float32(imageValue.Gray16At(x, y).Y)
				pixels = append(pixels, v)
				if v < minVal {
					minVal = v
				}
				if v > maxVal {
					maxVal = v
				}
			}
		}
		return buildSourceImage(uint32(width), uint32(height), pixels, minVal, maxVal, defaultWindow, invert)
	default:
		firstRed, firstGreen, firstBlue, _ := decoded.At(bounds.Min.X, bounds.Min.Y).RGBA()
		first := float32(grayFromRGB8(uint8(firstRed>>8), uint8(firstGreen>>8), uint8(firstBlue>>8)))
		pixels = append(pixels, first)
		minVal, maxVal := first, first
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			startX := bounds.Min.X
			if y == bounds.Min.Y {
				startX++
			}
			for x := startX; x < bounds.Max.X; x++ {
				red, green, blue, _ := decoded.At(x, y).RGBA()
				v := float32(grayFromRGB8(uint8(red>>8), uint8(green>>8), uint8(blue>>8)))
				pixels = append(pixels, v)
				if v < minVal {
					minVal = v
				}
				if v > maxVal {
					maxVal = v
				}
			}
		}
		return buildSourceImage(uint32(width), uint32(height), pixels, minVal, maxVal, nil, false)
	}
}

func decodeU8Monochrome(samples []byte, cfg sourceDecodeConfig) ([]float32, float32, float32) {
	n := len(samples)
	if n == 0 {
		return nil, 0, 0
	}
	pixels := make([]float32, n)
	first := scaledStoredPixelValue(uint32(samples[0]), cfg)
	pixels[0] = first
	minVal, maxVal := first, first
	for i := 1; i < n; i++ {
		v := scaledStoredPixelValue(uint32(samples[i]), cfg)
		pixels[i] = v
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}
	return pixels, minVal, maxVal
}

func decodeU16Monochrome(samples []uint16, cfg sourceDecodeConfig) ([]float32, float32, float32) {
	n := len(samples)
	if n == 0 {
		return nil, 0, 0
	}
	pixels := make([]float32, n)
	first := scaledStoredPixelValue(uint32(samples[0]), cfg)
	pixels[0] = first
	minVal, maxVal := first, first
	for i := 1; i < n; i++ {
		v := scaledStoredPixelValue(uint32(samples[i]), cfg)
		pixels[i] = v
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}
	return pixels, minVal, maxVal
}

func decodeU32Monochrome(samples []uint32, cfg sourceDecodeConfig) ([]float32, float32, float32) {
	n := len(samples)
	if n == 0 {
		return nil, 0, 0
	}
	pixels := make([]float32, n)
	first := scaledStoredPixelValue(samples[0], cfg)
	pixels[0] = first
	minVal, maxVal := first, first
	for i := 1; i < n; i++ {
		v := scaledStoredPixelValue(samples[i], cfg)
		pixels[i] = v
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}
	return pixels, minVal, maxVal
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
	n := len(raw) / 2
	if n == 0 {
		return nil
	}
	if byteOrder == binary.LittleEndian {
		// Zero-copy: reinterpret the raw byte slice as []uint16.
		// Safe because decodeU16Monochrome only reads from this slice
		// and the SourceImage it builds holds no reference to it.
		return unsafe.Slice((*uint16)(unsafe.Pointer(&raw[0])), n)
	}
	// Big-endian: allocate, bulk-copy bytes, then swap each element.
	samples := make([]uint16, n)
	dst := unsafe.Slice((*byte)(unsafe.Pointer(&samples[0])), n*2)
	copy(dst, raw[:n*2])
	for i, v := range samples {
		samples[i] = bits.ReverseBytes16(v)
	}
	return samples
}

func readU32Samples(raw []byte, byteOrder binary.ByteOrder) []uint32 {
	n := len(raw) / 4
	if n == 0 {
		return nil
	}
	if byteOrder == binary.LittleEndian {
		// Zero-copy: reinterpret the raw byte slice as []uint32.
		return unsafe.Slice((*uint32)(unsafe.Pointer(&raw[0])), n)
	}
	// Big-endian: allocate, bulk-copy bytes, then swap each element.
	samples := make([]uint32, n)
	dst := unsafe.Slice((*byte)(unsafe.Pointer(&samples[0])), n*4)
	copy(dst, raw[:n*4])
	for i, v := range samples {
		samples[i] = bits.ReverseBytes32(v)
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
