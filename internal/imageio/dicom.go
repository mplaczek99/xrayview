package imageio

import (
	"crypto/rand"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/suyashkumar/dicom"
	"github.com/suyashkumar/dicom/pkg/frame"
	"github.com/suyashkumar/dicom/pkg/tag"
	"github.com/suyashkumar/dicom/pkg/uid"
)

const (
	dicomFormat                       = "dicom"
	secondaryCaptureSOPClassUID       = "1.2.840.10008.5.1.4.1.1.7"
	defaultProcessedSeriesDescription = "XRayView Processed"
	implementationClassUID            = "2.25.302043790172249692526321623266752743501"
	implementationVersionName         = "XRAYVIEW_1_0"
)

var preservedSourceTags = []tag.Tag{
	tag.PatientName,
	tag.PatientID,
	tag.PatientBirthDate,
	tag.PatientSex,
	tag.StudyInstanceUID,
	tag.StudyID,
	tag.StudyDate,
	tag.StudyTime,
	tag.AccessionNumber,
	tag.StudyDescription,
	tag.ReferringPhysicianName,
	tag.InstitutionName,
}

type elementSpec struct {
	tag  tag.Tag
	data any
}

type elementBuilder struct {
	elements []*dicom.Element
}

func newElementBuilder(capacity int) *elementBuilder {
	return &elementBuilder{elements: make([]*dicom.Element, 0, capacity)}
}

func (b *elementBuilder) Append(t tag.Tag, data any) error {
	elem, err := dicom.NewElement(t, data)
	if err != nil {
		return err
	}
	b.elements = append(b.elements, elem)
	return nil
}

func (b *elementBuilder) AppendExisting(elements ...*dicom.Element) {
	b.elements = append(b.elements, elements...)
}

func (b *elementBuilder) Elements() []*dicom.Element {
	return b.elements
}

func appendElementSpecs(appendElement func(tag.Tag, any) error, specs ...elementSpec) error {
	for _, spec := range specs {
		if err := appendElement(spec.tag, spec.data); err != nil {
			return err
		}
	}

	return nil
}

func nativePixelData(raw []uint8, width, height, samplesPerPixel int) dicom.PixelDataInfo {
	frameData := &frame.NativeFrame[uint8]{
		InternalBitsPerSample:   8,
		InternalRows:            height,
		InternalCols:            width,
		InternalSamplesPerPixel: samplesPerPixel,
		RawData:                 raw,
	}

	return dicom.PixelDataInfo{
		IsEncapsulated: false,
		Frames: []*frame.Frame{{
			Encapsulated: false,
			NativeData:   frameData,
		}},
	}
}

func loadDICOM(path string) (LoadedImage, error) {
	ds, err := dicom.ParseFile(path, nil, dicom.AllowMissingMetaElementGroupLength(), dicom.AllowUnknownSpecificCharacterSet())
	if err != nil {
		return LoadedImage{}, fmt.Errorf("decode DICOM: %w", err)
	}

	img, err := renderDICOM(&ds)
	if err != nil {
		return LoadedImage{}, fmt.Errorf("render DICOM: %w", err)
	}

	return LoadedImage{
		Image:  img,
		Format: dicomFormat,
		DICOM:  &ds,
	}, nil
}

func renderDICOM(ds *dicom.Dataset) (image.Image, error) {
	pixelDataElement, err := ds.FindElementByTag(tag.PixelData)
	if err != nil {
		return nil, fmt.Errorf("find PixelData: %w", err)
	}

	pixelData := dicom.MustGetPixelDataInfo(pixelDataElement.Value)
	if len(pixelData.Frames) == 0 {
		return nil, fmt.Errorf("dicom contains no image frames")
	}

	firstFrame := pixelData.Frames[0]
	nativeFrame, err := firstFrame.GetNativeFrame()
	if err == nil {
		return renderNativeFrame(nativeFrame, ds)
	}

	frameImage, err := firstFrame.GetImage()
	if err != nil {
		return nil, fmt.Errorf("decode DICOM frame: %w", err)
	}

	gray := convertToGray(frameImage)
	if isMonochromeOne(ds) {
		invertGray(gray)
	}

	return gray, nil
}

type nativeSample interface {
	~uint8 | ~int8 | ~uint16 | ~int16 | ~uint32 | ~int32 | ~int
}

type nativeRenderConfig struct {
	bitsStored          int
	pixelRepresentation int
	slope               float64
	intercept           float64
	windowCenter        float64
	windowWidth         float64
	useWindow           bool
	invert              bool
}

func renderNativeFrame(nativeFrame frame.INativeFrame, ds *dicom.Dataset) (*image.Gray, error) {
	if nativeFrame.SamplesPerPixel() != 1 {
		frameImage, err := nativeFrame.GetImage()
		if err != nil {
			return nil, fmt.Errorf("unsupported native DICOM frame: %w", err)
		}
		gray := convertToGray(frameImage)
		if isMonochromeOne(ds) {
			invertGray(gray)
		}
		return gray, nil
	}

	rows := nativeFrame.Rows()
	cols := nativeFrame.Cols()
	gray := image.NewGray(image.Rect(0, 0, cols, rows))
	cfg := resolveNativeRenderConfig(ds, nativeFrame)

	switch raw := nativeFrame.RawDataSlice().(type) {
	case []uint8:
		if err := renderNativeSamples(gray.Pix, raw, cfg); err != nil {
			return nil, err
		}
	case []int8:
		if err := renderNativeSamples(gray.Pix, raw, cfg); err != nil {
			return nil, err
		}
	case []uint16:
		if cfg.pixelRepresentation == 0 {
			if err := renderUnsignedUint16Samples(gray.Pix, raw, cfg); err != nil {
				return nil, err
			}
			break
		}
		if err := renderNativeSamples(gray.Pix, raw, cfg); err != nil {
			return nil, err
		}
	case []int16:
		if err := renderNativeSamples(gray.Pix, raw, cfg); err != nil {
			return nil, err
		}
	case []uint32:
		if err := renderNativeSamples(gray.Pix, raw, cfg); err != nil {
			return nil, err
		}
	case []int32:
		if err := renderNativeSamples(gray.Pix, raw, cfg); err != nil {
			return nil, err
		}
	case []int:
		if err := renderNativeSamples(gray.Pix, raw, cfg); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported DICOM sample type: %T", nativeFrame.RawDataSlice())
	}

	return gray, nil
}

func resolveNativeRenderConfig(ds *dicom.Dataset, nativeFrame frame.INativeFrame) nativeRenderConfig {
	cfg := nativeRenderConfig{
		bitsStored: nativeFrame.BitsPerSample(),
		slope:      1,
	}

	var hasWindowCenter bool
	var hasWindowWidth bool
	for _, elem := range ds.Elements {
		switch elem.Tag {
		case tag.BitsStored:
			if value, ok := intValueFromElement(elem); ok && value > 0 {
				cfg.bitsStored = value
			}
		case tag.PixelRepresentation:
			if value, ok := intValueFromElement(elem); ok {
				cfg.pixelRepresentation = value
			}
		case tag.RescaleSlope:
			if value, ok := floatValueFromElement(elem); ok {
				cfg.slope = value
			}
		case tag.RescaleIntercept:
			if value, ok := floatValueFromElement(elem); ok {
				cfg.intercept = value
			}
		case tag.WindowCenter:
			if value, ok := floatValueFromElement(elem); ok {
				cfg.windowCenter = value
				hasWindowCenter = true
			}
		case tag.WindowWidth:
			if value, ok := floatValueFromElement(elem); ok {
				cfg.windowWidth = value
				hasWindowWidth = true
			}
		case tag.PhotometricInterpretation:
			if value, ok := stringValueFromElement(elem); ok {
				cfg.invert = strings.EqualFold(value, "MONOCHROME1")
			}
		}
	}

	cfg.useWindow = hasWindowCenter && hasWindowWidth && cfg.windowWidth > 1
	return cfg
}

func renderUnsignedUint16Samples(dst []uint8, raw []uint16, cfg nativeRenderConfig) error {
	if len(raw) == 0 {
		return fmt.Errorf("dicom frame contained no samples")
	}

	if len(raw) < len(dst) {
		return fmt.Errorf("dicom frame sample count %d does not match image size %d", len(raw), len(dst))
	}
	if len(raw) > len(dst) {
		raw = raw[:len(dst)]
	}

	bitsStored := cfg.bitsStored
	if bitsStored <= 0 || bitsStored > 16 {
		bitsStored = 16
	}

	mask := ^uint16(0)
	if bitsStored < 16 {
		mask = uint16((1 << bitsStored) - 1)
	}

	if cfg.slope == 1 && cfg.intercept == 0 {
		return renderUnsignedUint16NoRescale(dst, raw, mask, cfg)
	}

	return renderUnsignedUint16Rescaled(dst, raw, mask, cfg)
}

func renderUnsignedUint16NoRescale(dst []uint8, raw []uint16, mask uint16, cfg nativeRenderConfig) error {
	if cfg.useWindow {
		windowScale := 255 / (cfg.windowWidth - 1)
		windowOffset := 127.5 - (cfg.windowCenter-0.5)*windowScale
		lower := cfg.windowCenter - 0.5 - (cfg.windowWidth-1)/2
		upper := cfg.windowCenter - 0.5 + (cfg.windowWidth-1)/2

		if cfg.invert {
			for idx, rawValue := range raw {
				dst[idx] = 255 - mapWindowValue(float64(rawValue&mask), lower, upper, windowScale, windowOffset)
			}
			return nil
		}

		for idx, rawValue := range raw {
			dst[idx] = mapWindowValue(float64(rawValue&mask), lower, upper, windowScale, windowOffset)
		}
		return nil
	}

	minValue := float64(raw[0] & mask)
	maxValue := minValue
	for _, rawValue := range raw[1:] {
		value := float64(rawValue & mask)
		if value < minValue {
			minValue = value
		}
		if value > maxValue {
			maxValue = value
		}
	}

	return renderLinearUint16(dst, raw, mask, cfg.invert, minValue, maxValue, 1, 0)
}

func renderUnsignedUint16Rescaled(dst []uint8, raw []uint16, mask uint16, cfg nativeRenderConfig) error {
	if cfg.useWindow {
		windowScale := 255 / (cfg.windowWidth - 1)
		windowOffset := 127.5 - (cfg.windowCenter-0.5)*windowScale
		lower := cfg.windowCenter - 0.5 - (cfg.windowWidth-1)/2
		upper := cfg.windowCenter - 0.5 + (cfg.windowWidth-1)/2

		if cfg.invert {
			for idx, rawValue := range raw {
				value := float64(rawValue&mask)*cfg.slope + cfg.intercept
				dst[idx] = 255 - mapWindowValue(value, lower, upper, windowScale, windowOffset)
			}
			return nil
		}

		for idx, rawValue := range raw {
			value := float64(rawValue&mask)*cfg.slope + cfg.intercept
			dst[idx] = mapWindowValue(value, lower, upper, windowScale, windowOffset)
		}
		return nil
	}

	minValue := float64(raw[0]&mask)*cfg.slope + cfg.intercept
	maxValue := minValue
	for _, rawValue := range raw[1:] {
		value := float64(rawValue&mask)*cfg.slope + cfg.intercept
		if value < minValue {
			minValue = value
		}
		if value > maxValue {
			maxValue = value
		}
	}

	return renderLinearUint16(dst, raw, mask, cfg.invert, minValue, maxValue, cfg.slope, cfg.intercept)
}

func renderLinearUint16(dst []uint8, raw []uint16, mask uint16, invert bool, minValue, maxValue, slope, intercept float64) error {
	if maxValue <= minValue {
		fill := uint8(0)
		if invert {
			fill = 255
		}
		for idx := range dst {
			dst[idx] = fill
		}
		return nil
	}

	linearScale := 255 / (maxValue - minValue)
	linearOffset := -minValue * linearScale

	if invert {
		for idx, rawValue := range raw {
			value := (float64(rawValue&mask)*slope + intercept) * linearScale
			dst[idx] = 255 - clampToByte(value+linearOffset)
		}
		return nil
	}

	for idx, rawValue := range raw {
		value := (float64(rawValue&mask)*slope + intercept) * linearScale
		dst[idx] = clampToByte(value + linearOffset)
	}

	return nil
}

func renderNativeSamples[T nativeSample](dst []uint8, raw []T, cfg nativeRenderConfig) error {
	if len(raw) == 0 {
		return fmt.Errorf("dicom frame contained no samples")
	}

	if len(raw) < len(dst) {
		return fmt.Errorf("dicom frame sample count %d does not match image size %d", len(raw), len(dst))
	}
	if len(raw) > len(dst) {
		raw = raw[:len(dst)]
	}

	if cfg.useWindow {
		windowScale := 255 / (cfg.windowWidth - 1)
		windowOffset := 127.5 - (cfg.windowCenter-0.5)*windowScale
		lower := cfg.windowCenter - 0.5 - (cfg.windowWidth-1)/2
		upper := cfg.windowCenter - 0.5 + (cfg.windowWidth-1)/2

		if cfg.invert {
			for idx, rawValue := range raw {
				dst[idx] = 255 - mapWindowValue(scaledStoredPixelValue(uint32(rawValue), cfg.bitsStored, cfg.pixelRepresentation, cfg.slope, cfg.intercept), lower, upper, windowScale, windowOffset)
			}
			return nil
		}

		for idx, rawValue := range raw {
			dst[idx] = mapWindowValue(scaledStoredPixelValue(uint32(rawValue), cfg.bitsStored, cfg.pixelRepresentation, cfg.slope, cfg.intercept), lower, upper, windowScale, windowOffset)
		}
		return nil
	}

	minValue := math.Inf(1)
	maxValue := math.Inf(-1)
	for _, rawValue := range raw {
		value := scaledStoredPixelValue(uint32(rawValue), cfg.bitsStored, cfg.pixelRepresentation, cfg.slope, cfg.intercept)
		if value < minValue {
			minValue = value
		}
		if value > maxValue {
			maxValue = value
		}
	}
	if maxValue <= minValue {
		fill := uint8(0)
		if cfg.invert {
			fill = 255
		}
		for idx := range dst {
			dst[idx] = fill
		}
		return nil
	}

	linearScale := 255 / (maxValue - minValue)
	linearOffset := -minValue * linearScale

	if cfg.invert {
		for idx, rawValue := range raw {
			dst[idx] = 255 - clampToByte(scaledStoredPixelValue(uint32(rawValue), cfg.bitsStored, cfg.pixelRepresentation, cfg.slope, cfg.intercept)*linearScale+linearOffset)
		}
		return nil
	}

	for idx, rawValue := range raw {
		dst[idx] = clampToByte(scaledStoredPixelValue(uint32(rawValue), cfg.bitsStored, cfg.pixelRepresentation, cfg.slope, cfg.intercept)*linearScale + linearOffset)
	}

	return nil
}

func scaledStoredPixelValue(rawValue uint32, bitsStored, pixelRepresentation int, slope, intercept float64) float64 {
	return float64(decodeStoredPixelValue(rawValue, bitsStored, pixelRepresentation))*slope + intercept
}

func saveDICOM(path string, img image.Image, source LoadedImage) error {
	dataset, err := buildSecondaryCaptureDataset(img, source.DICOM)
	if err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create output image: %w", err)
	}

	if err := dicom.Write(file, dataset); err != nil {
		file.Close()
		return fmt.Errorf("encode output image: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close output image: %w", err)
	}

	return nil
}

func buildSecondaryCaptureDataset(img image.Image, source *dicom.Dataset) (dicom.Dataset, error) {
	now := time.Now().UTC()
	sopInstanceUID, err := generateUID()
	if err != nil {
		return dicom.Dataset{}, fmt.Errorf("generate SOP instance UID: %w", err)
	}
	seriesInstanceUID, err := generateUID()
	if err != nil {
		return dicom.Dataset{}, fmt.Errorf("generate series instance UID: %w", err)
	}

	studyInstanceUID := ""
	if source != nil {
		studyInstanceUID, _ = lookupString(source, tag.StudyInstanceUID)
	}
	if studyInstanceUID == "" {
		studyInstanceUID, err = generateUID()
		if err != nil {
			return dicom.Dataset{}, fmt.Errorf("generate study instance UID: %w", err)
		}
	}

	builder := newElementBuilder(32)
	if err := appendElementSpecs(
		builder.Append,
		elementSpec{tag: tag.MediaStorageSOPClassUID, data: []string{secondaryCaptureSOPClassUID}},
		elementSpec{tag: tag.MediaStorageSOPInstanceUID, data: []string{sopInstanceUID}},
		elementSpec{tag: tag.TransferSyntaxUID, data: []string{uid.ExplicitVRLittleEndian}},
		elementSpec{tag: tag.ImplementationClassUID, data: []string{implementationClassUID}},
		elementSpec{tag: tag.ImplementationVersionName, data: []string{implementationVersionName}},
		elementSpec{tag: tag.SOPClassUID, data: []string{secondaryCaptureSOPClassUID}},
		elementSpec{tag: tag.SOPInstanceUID, data: []string{sopInstanceUID}},
		elementSpec{tag: tag.Modality, data: []string{"OT"}},
		elementSpec{tag: tag.ImageType, data: []string{"DERIVED", "SECONDARY"}},
		elementSpec{tag: tag.ConversionType, data: []string{"WSD"}},
		elementSpec{tag: tag.InstanceCreationDate, data: []string{now.Format("20060102")}},
		elementSpec{tag: tag.InstanceCreationTime, data: []string{now.Format("150405")}},
		elementSpec{tag: tag.ContentDate, data: []string{now.Format("20060102")}},
		elementSpec{tag: tag.ContentTime, data: []string{now.Format("150405")}},
		elementSpec{tag: tag.SeriesDescription, data: []string{defaultProcessedSeriesDescription}},
		elementSpec{tag: tag.DerivationDescription, data: []string{"Processed by XRayView"}},
		elementSpec{tag: tag.Manufacturer, data: []string{"XRayView"}},
		elementSpec{tag: tag.ManufacturerModelName, data: []string{"xrayview"}},
		elementSpec{tag: tag.SoftwareVersions, data: []string{"xrayview"}},
		elementSpec{tag: tag.StudyInstanceUID, data: []string{studyInstanceUID}},
		elementSpec{tag: tag.SeriesInstanceUID, data: []string{seriesInstanceUID}},
		elementSpec{tag: tag.SeriesNumber, data: []string{"999"}},
		elementSpec{tag: tag.InstanceNumber, data: []string{"1"}},
	); err != nil {
		return dicom.Dataset{}, err
	}

	if source != nil {
		for _, preservedTag := range preservedSourceTags {
			if preservedTag == tag.StudyInstanceUID {
				continue
			}
			elem, err := source.FindElementByTag(preservedTag)
			if err == nil {
				builder.AppendExisting(elem)
			}
		}
	}

	pixelData, imageElements, err := dicomPixelDataFromImage(img)
	if err != nil {
		return dicom.Dataset{}, err
	}
	builder.AppendExisting(imageElements...)
	if err := builder.Append(tag.PixelData, pixelData); err != nil {
		return dicom.Dataset{}, err
	}

	return dicom.Dataset{Elements: builder.Elements()}, nil
}

func dicomPixelDataFromImage(img image.Image) (dicom.PixelDataInfo, []*dicom.Element, error) {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return dicom.PixelDataInfo{}, nil, fmt.Errorf("output image has invalid bounds")
	}

	builder := newElementBuilder(10)

	switch src := img.(type) {
	case *image.Gray:
		pixelData, err := grayscalePixelData(src, bounds, width, height, builder.Append)
		return pixelData, builder.Elements(), err
	case *image.Gray16:
		pixelData, err := grayscalePixelData(convertToGray(src), bounds, width, height, builder.Append)
		return pixelData, builder.Elements(), err
	case *image.RGBA:
		pixelData, err := rgbPixelData(rgbaPixels(src, bounds, width, height), width, height, builder.Append)
		return pixelData, builder.Elements(), err
	case *image.NRGBA:
		pixelData, err := rgbPixelData(nrgbaPixels(src, bounds, width, height), width, height, builder.Append)
		return pixelData, builder.Elements(), err
	}

	if isGrayLikeImage(img) {
		pixelData, err := grayscalePixelData(convertToGray(img), bounds, width, height, builder.Append)
		return pixelData, builder.Elements(), err
	}

	pixelData, err := rgbPixelData(imageRGBPixels(img, bounds, width, height), width, height, builder.Append)
	return pixelData, builder.Elements(), err
}

func grayscalePixelData(gray *image.Gray, bounds image.Rectangle, width, height int, appendElement func(tag.Tag, any) error) (dicom.PixelDataInfo, error) {
	raw := grayPixels(gray, bounds, width, height)
	pixelData := nativePixelData(raw, width, height, 1)

	if err := appendElementSpecs(
		appendElement,
		elementSpec{tag: tag.Rows, data: []int{height}},
		elementSpec{tag: tag.Columns, data: []int{width}},
		elementSpec{tag: tag.SamplesPerPixel, data: []int{1}},
		elementSpec{tag: tag.PhotometricInterpretation, data: []string{"MONOCHROME2"}},
		elementSpec{tag: tag.BitsAllocated, data: []int{8}},
		elementSpec{tag: tag.BitsStored, data: []int{8}},
		elementSpec{tag: tag.HighBit, data: []int{7}},
		elementSpec{tag: tag.PixelRepresentation, data: []int{0}},
		elementSpec{tag: tag.WindowCenter, data: []string{"127.5"}},
		elementSpec{tag: tag.WindowWidth, data: []string{"255"}},
	); err != nil {
		return dicom.PixelDataInfo{}, err
	}

	return pixelData, nil
}

func rgbPixelData(raw []uint8, width, height int, appendElement func(tag.Tag, any) error) (dicom.PixelDataInfo, error) {
	pixelData := nativePixelData(raw, width, height, 3)

	if err := appendElementSpecs(
		appendElement,
		elementSpec{tag: tag.Rows, data: []int{height}},
		elementSpec{tag: tag.Columns, data: []int{width}},
		elementSpec{tag: tag.SamplesPerPixel, data: []int{3}},
		elementSpec{tag: tag.PhotometricInterpretation, data: []string{"RGB"}},
		elementSpec{tag: tag.PlanarConfiguration, data: []int{0}},
		elementSpec{tag: tag.BitsAllocated, data: []int{8}},
		elementSpec{tag: tag.BitsStored, data: []int{8}},
		elementSpec{tag: tag.HighBit, data: []int{7}},
		elementSpec{tag: tag.PixelRepresentation, data: []int{0}},
	); err != nil {
		return dicom.PixelDataInfo{}, err
	}

	return pixelData, nil
}

func grayPixels(src *image.Gray, bounds image.Rectangle, width, height int) []uint8 {
	start := src.PixOffset(bounds.Min.X, bounds.Min.Y)
	if start == 0 && src.Stride == width && len(src.Pix) == width*height {
		return src.Pix
	}

	raw := make([]uint8, width*height)
	offset := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		srcStart := src.PixOffset(bounds.Min.X, y)
		copy(raw[offset:offset+width], src.Pix[srcStart:srcStart+width])
		offset += width
	}

	return raw
}

func rgbaPixels(src *image.RGBA, bounds image.Rectangle, width, height int) []uint8 {
	raw := make([]uint8, width*height*3)
	offset := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		srcStart := src.PixOffset(bounds.Min.X, y)
		srcRow := src.Pix[srcStart : srcStart+width*4]
		for i := 0; i < len(srcRow); i += 4 {
			raw[offset] = srcRow[i]
			raw[offset+1] = srcRow[i+1]
			raw[offset+2] = srcRow[i+2]
			offset += 3
		}
	}

	return raw
}

func nrgbaPixels(src *image.NRGBA, bounds image.Rectangle, width, height int) []uint8 {
	raw := make([]uint8, width*height*3)
	offset := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		srcStart := src.PixOffset(bounds.Min.X, y)
		srcRow := src.Pix[srcStart : srcStart+width*4]
		for i := 0; i < len(srcRow); i += 4 {
			a := uint32(srcRow[i+3])
			aa := a | (a << 8)
			r := uint32(srcRow[i])
			r |= r << 8
			g := uint32(srcRow[i+1])
			g |= g << 8
			b := uint32(srcRow[i+2])
			b |= b << 8
			raw[offset] = uint8((r * aa / 0xffff) >> 8)
			raw[offset+1] = uint8((g * aa / 0xffff) >> 8)
			raw[offset+2] = uint8((b * aa / 0xffff) >> 8)
			offset += 3
		}
	}

	return raw
}

func imageRGBPixels(img image.Image, bounds image.Rectangle, width, height int) []uint8 {
	raw := make([]uint8, width*height*3)
	offset := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			raw[offset] = uint8(r >> 8)
			raw[offset+1] = uint8(g >> 8)
			raw[offset+2] = uint8(b >> 8)
			offset += 3
		}
	}

	return raw
}

func decodeStoredPixelValue(rawValue uint32, bitsStored, pixelRepresentation int) int32 {
	if bitsStored <= 0 || bitsStored > 32 {
		bitsStored = 32
	}

	if bitsStored < 32 {
		mask := uint32(1<<bitsStored) - 1
		rawValue &= mask
	}

	if pixelRepresentation == 0 {
		return int32(rawValue)
	}
	if bitsStored == 32 {
		return int32(rawValue)
	}

	signBit := uint32(1) << (bitsStored - 1)
	mask := uint32(1<<bitsStored) - 1
	if rawValue&signBit == 0 {
		return int32(rawValue)
	}

	return int32(rawValue | ^mask)
}

func clampToByte(value float64) uint8 {
	if value <= 0 {
		return 0
	}
	if value >= 255 {
		return 255
	}
	return uint8(value + 0.5)
}

func mapWindowValue(value, lower, upper, scale, offset float64) uint8 {
	switch {
	case value <= lower:
		return 0
	case value > upper:
		return 255
	default:
		return clampToByte(value*scale + offset)
	}
}

func convertToGray(img image.Image) *image.Gray {
	switch src := img.(type) {
	case *image.Gray:
		return cloneGrayImage(src)
	case *image.Gray16:
		return gray16ToGray(src)
	case *image.RGBA:
		return rgbaToGray(src)
	case *image.NRGBA:
		return nrgbaToGray(src)
	case *image.YCbCr:
		return ycbcrToGray(src)
	}

	bounds := img.Bounds()
	gray := image.NewGray(bounds)
	width := bounds.Dx()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		rowStart := gray.PixOffset(bounds.Min.X, y)
		row := gray.Pix[rowStart : rowStart+width]
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			row[x-bounds.Min.X] = color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y
		}
	}
	return gray
}

func cloneGrayImage(src *image.Gray) *image.Gray {
	dst := image.NewGray(src.Bounds())
	copyGrayImageRows(dst, src)
	return dst
}

func copyGrayImageRows(dst, src *image.Gray) {
	bounds := src.Bounds()
	width := bounds.Dx()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		srcStart := src.PixOffset(bounds.Min.X, y)
		dstStart := dst.PixOffset(bounds.Min.X, y)
		copy(dst.Pix[dstStart:dstStart+width], src.Pix[srcStart:srcStart+width])
	}
}

func gray16ToGray(src *image.Gray16) *image.Gray {
	bounds := src.Bounds()
	dst := image.NewGray(bounds)
	width := bounds.Dx()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		srcStart := src.PixOffset(bounds.Min.X, y)
		dstStart := dst.PixOffset(bounds.Min.X, y)
		srcRow := src.Pix[srcStart : srcStart+width*2]
		dstRow := dst.Pix[dstStart : dstStart+width]
		for x := 0; x < width; x++ {
			dstRow[x] = srcRow[x*2]
		}
	}
	return dst
}

func rgbaToGray(src *image.RGBA) *image.Gray {
	bounds := src.Bounds()
	dst := image.NewGray(bounds)
	width := bounds.Dx()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		srcStart := src.PixOffset(bounds.Min.X, y)
		dstStart := dst.PixOffset(bounds.Min.X, y)
		srcRow := src.Pix[srcStart : srcStart+width*4]
		dstRow := dst.Pix[dstStart : dstStart+width]
		for x, i := 0, 0; x < width; x, i = x+1, i+4 {
			r := uint32(srcRow[i])
			r |= r << 8
			g := uint32(srcRow[i+1])
			g |= g << 8
			b := uint32(srcRow[i+2])
			b |= b << 8
			dstRow[x] = grayValueFromRGB16(r, g, b)
		}
	}
	return dst
}

func nrgbaToGray(src *image.NRGBA) *image.Gray {
	bounds := src.Bounds()
	dst := image.NewGray(bounds)
	width := bounds.Dx()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		srcStart := src.PixOffset(bounds.Min.X, y)
		dstStart := dst.PixOffset(bounds.Min.X, y)
		srcRow := src.Pix[srcStart : srcStart+width*4]
		dstRow := dst.Pix[dstStart : dstStart+width]
		for x, i := 0, 0; x < width; x, i = x+1, i+4 {
			r, g, b := premultiplyNRGBA16(srcRow[i], srcRow[i+1], srcRow[i+2], srcRow[i+3])
			dstRow[x] = grayValueFromRGB16(r, g, b)
		}
	}
	return dst
}

func ycbcrToGray(src *image.YCbCr) *image.Gray {
	bounds := src.Bounds()
	dst := image.NewGray(bounds)
	width := bounds.Dx()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		dstStart := dst.PixOffset(bounds.Min.X, y)
		dstRow := dst.Pix[dstStart : dstStart+width]
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			yy := src.Y[src.YOffset(x, y)]
			cbcrOffset := src.COffset(x, y)
			r, g, b := color.YCbCrToRGB(yy, src.Cb[cbcrOffset], src.Cr[cbcrOffset])
			dstRow[x-bounds.Min.X] = grayValueFromRGB16(expandByteToUint16(r), expandByteToUint16(g), expandByteToUint16(b))
		}
	}
	return dst
}

func grayValueFromRGB16(r, g, b uint32) uint8 {
	return uint8((19595*r + 38470*g + 7471*b + 1<<15) >> 24)
}

func expandByteToUint16(value uint8) uint32 {
	v := uint32(value)
	return v | (v << 8)
}

func premultiplyNRGBA16(r, g, b, a uint8) (uint32, uint32, uint32) {
	aa := expandByteToUint16(a)
	rr := expandByteToUint16(r)
	gg := expandByteToUint16(g)
	bb := expandByteToUint16(b)
	return rr * aa / 0xffff, gg * aa / 0xffff, bb * aa / 0xffff
}

func invertGray(img *image.Gray) {
	for i := range img.Pix {
		img.Pix[i] = 255 - img.Pix[i]
	}
}

func isGrayLikeImage(img image.Image) bool {
	switch img.(type) {
	case *image.Gray, *image.Gray16:
		return true
	}

	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r != g || g != b {
				return false
			}
		}
	}
	return true
}

func isMonochromeOne(ds *dicom.Dataset) bool {
	value, ok := lookupString(ds, tag.PhotometricInterpretation)
	if !ok {
		return false
	}
	return strings.EqualFold(value, "MONOCHROME1")
}

func lookupInt(ds *dicom.Dataset, t tag.Tag) (int, bool) {
	elem, err := ds.FindElementByTag(t)
	if err != nil {
		return 0, false
	}
	return intValueFromElement(elem)
}

func intValueFromElement(elem *dicom.Element) (int, bool) {
	switch elem.Value.ValueType() {
	case dicom.Ints:
		values := dicom.MustGetInts(elem.Value)
		if len(values) == 0 {
			return 0, false
		}
		return values[0], true
	case dicom.Strings:
		values := dicom.MustGetStrings(elem.Value)
		firstValue, ok := firstValueString(values)
		if !ok {
			return 0, false
		}
		parsed, err := strconv.Atoi(firstValue)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func lookupFloat(ds *dicom.Dataset, t tag.Tag) (float64, bool) {
	elem, err := ds.FindElementByTag(t)
	if err != nil {
		return 0, false
	}
	return floatValueFromElement(elem)
}

func floatValueFromElement(elem *dicom.Element) (float64, bool) {
	switch elem.Value.ValueType() {
	case dicom.Floats:
		values := dicom.MustGetFloats(elem.Value)
		if len(values) == 0 {
			return 0, false
		}
		return values[0], true
	case dicom.Ints:
		values := dicom.MustGetInts(elem.Value)
		if len(values) == 0 {
			return 0, false
		}
		return float64(values[0]), true
	case dicom.Strings:
		values := dicom.MustGetStrings(elem.Value)
		firstValue, ok := firstValueString(values)
		if !ok {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(firstValue, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func lookupString(ds *dicom.Dataset, t tag.Tag) (string, bool) {
	elem, err := ds.FindElementByTag(t)
	if err != nil {
		return "", false
	}
	return stringValueFromElement(elem)
}

func stringValueFromElement(elem *dicom.Element) (string, bool) {
	if elem.Value.ValueType() != dicom.Strings {
		return "", false
	}
	values := dicom.MustGetStrings(elem.Value)
	return firstValueString(values)
}

func firstValueString(values []string) (string, bool) {
	for _, value := range values {
		start := 0
		for i := 0; i <= len(value); i++ {
			if i != len(value) && value[i] != '\\' {
				continue
			}

			candidate := strings.TrimSpace(value[start:i])
			if candidate != "" {
				return candidate, true
			}
			start = i + 1
		}
	}

	return "", false
}

func generateUID() (string, error) {
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	value := new(big.Int).SetBytes(randomBytes)
	return "2.25." + value.String(), nil
}
