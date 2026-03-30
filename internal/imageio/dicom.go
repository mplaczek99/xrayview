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
	values := make([]float64, rows*cols)

	bitsStored := nativeFrame.BitsPerSample()
	if value, ok := lookupInt(ds, tag.BitsStored); ok && value > 0 {
		bitsStored = value
	}
	pixelRepresentation, _ := lookupInt(ds, tag.PixelRepresentation)
	slope, ok := lookupFloat(ds, tag.RescaleSlope)
	if !ok {
		slope = 1
	}
	intercept, ok := lookupFloat(ds, tag.RescaleIntercept)
	if !ok {
		intercept = 0
	}
	windowCenter, hasWindowCenter := lookupFloat(ds, tag.WindowCenter)
	windowWidth, hasWindowWidth := lookupFloat(ds, tag.WindowWidth)
	useWindow := hasWindowCenter && hasWindowWidth && windowWidth > 1

	minValue := math.Inf(1)
	maxValue := math.Inf(-1)
	rawSamples, err := rawFrameSamples(nativeFrame)
	if err != nil {
		return nil, err
	}

	for idx, rawValue := range rawSamples {
		storedValue := decodeStoredPixelValue(rawValue, bitsStored, pixelRepresentation)
		value := float64(storedValue)*slope + intercept
		values[idx] = value
		if value < minValue {
			minValue = value
		}
		if value > maxValue {
			maxValue = value
		}
	}

	if math.IsInf(minValue, 1) || math.IsInf(maxValue, -1) {
		return nil, fmt.Errorf("dicom frame contained no samples")
	}

	for idx, value := range values {
		mapped := mapToDisplayRange(value, minValue, maxValue, windowCenter, windowWidth, useWindow)
		if isMonochromeOne(ds) {
			mapped = 255 - mapped
		}
		gray.Pix[idx] = mapped
	}

	return gray, nil
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

	elements := make([]*dicom.Element, 0, 32)
	appendElement := func(t tag.Tag, data any) error {
		elem, err := dicom.NewElement(t, data)
		if err != nil {
			return err
		}
		elements = append(elements, elem)
		return nil
	}

	if err := appendElement(tag.MediaStorageSOPClassUID, []string{secondaryCaptureSOPClassUID}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.MediaStorageSOPInstanceUID, []string{sopInstanceUID}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.TransferSyntaxUID, []string{uid.ExplicitVRLittleEndian}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.ImplementationClassUID, []string{implementationClassUID}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.ImplementationVersionName, []string{implementationVersionName}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.SOPClassUID, []string{secondaryCaptureSOPClassUID}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.SOPInstanceUID, []string{sopInstanceUID}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.Modality, []string{"OT"}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.ImageType, []string{"DERIVED", "SECONDARY"}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.ConversionType, []string{"WSD"}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.InstanceCreationDate, []string{now.Format("20060102")}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.InstanceCreationTime, []string{now.Format("150405")}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.ContentDate, []string{now.Format("20060102")}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.ContentTime, []string{now.Format("150405")}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.SeriesDescription, []string{defaultProcessedSeriesDescription}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.DerivationDescription, []string{"Processed by XRayView"}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.Manufacturer, []string{"XRayView"}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.ManufacturerModelName, []string{"xrayview"}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.SoftwareVersions, []string{"xrayview"}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.StudyInstanceUID, []string{studyInstanceUID}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.SeriesInstanceUID, []string{seriesInstanceUID}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.SeriesNumber, []string{"999"}); err != nil {
		return dicom.Dataset{}, err
	}
	if err := appendElement(tag.InstanceNumber, []string{"1"}); err != nil {
		return dicom.Dataset{}, err
	}

	if source != nil {
		for _, preservedTag := range preservedSourceTags {
			if preservedTag == tag.StudyInstanceUID {
				continue
			}
			elem, err := source.FindElementByTag(preservedTag)
			if err == nil {
				elements = append(elements, elem)
			}
		}
	}

	pixelData, imageElements, err := dicomPixelDataFromImage(img)
	if err != nil {
		return dicom.Dataset{}, err
	}
	elements = append(elements, imageElements...)
	if err := appendElement(tag.PixelData, pixelData); err != nil {
		return dicom.Dataset{}, err
	}

	return dicom.Dataset{Elements: elements}, nil
}

func dicomPixelDataFromImage(img image.Image) (dicom.PixelDataInfo, []*dicom.Element, error) {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return dicom.PixelDataInfo{}, nil, fmt.Errorf("output image has invalid bounds")
	}

	elements := make([]*dicom.Element, 0, 10)
	appendElement := func(t tag.Tag, data any) error {
		elem, err := dicom.NewElement(t, data)
		if err != nil {
			return err
		}
		elements = append(elements, elem)
		return nil
	}

	if isGrayLikeImage(img) {
		gray := convertToGray(img)
		raw := append([]uint8(nil), gray.Pix...)
		frameData := &frame.NativeFrame[uint8]{
			InternalBitsPerSample:   8,
			InternalRows:            height,
			InternalCols:            width,
			InternalSamplesPerPixel: 1,
			RawData:                 raw,
		}
		pixelData := dicom.PixelDataInfo{
			IsEncapsulated: false,
			Frames: []*frame.Frame{{
				Encapsulated: false,
				NativeData:   frameData,
			}},
		}

		for _, spec := range []struct {
			tag  tag.Tag
			data any
		}{
			{tag.Rows, []int{height}},
			{tag.Columns, []int{width}},
			{tag.SamplesPerPixel, []int{1}},
			{tag.PhotometricInterpretation, []string{"MONOCHROME2"}},
			{tag.BitsAllocated, []int{8}},
			{tag.BitsStored, []int{8}},
			{tag.HighBit, []int{7}},
			{tag.PixelRepresentation, []int{0}},
			{tag.WindowCenter, []string{"127.5"}},
			{tag.WindowWidth, []string{"255"}},
		} {
			if err := appendElement(spec.tag, spec.data); err != nil {
				return dicom.PixelDataInfo{}, nil, err
			}
		}

		return pixelData, elements, nil
	}

	raw := make([]uint8, 0, width*height*3)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			raw = append(raw, uint8(r>>8), uint8(g>>8), uint8(b>>8))
		}
	}

	frameData := &frame.NativeFrame[uint8]{
		InternalBitsPerSample:   8,
		InternalRows:            height,
		InternalCols:            width,
		InternalSamplesPerPixel: 3,
		RawData:                 raw,
	}
	pixelData := dicom.PixelDataInfo{
		IsEncapsulated: false,
		Frames: []*frame.Frame{{
			Encapsulated: false,
			NativeData:   frameData,
		}},
	}

	for _, spec := range []struct {
		tag  tag.Tag
		data any
	}{
		{tag.Rows, []int{height}},
		{tag.Columns, []int{width}},
		{tag.SamplesPerPixel, []int{3}},
		{tag.PhotometricInterpretation, []string{"RGB"}},
		{tag.PlanarConfiguration, []int{0}},
		{tag.BitsAllocated, []int{8}},
		{tag.BitsStored, []int{8}},
		{tag.HighBit, []int{7}},
		{tag.PixelRepresentation, []int{0}},
	} {
		if err := appendElement(spec.tag, spec.data); err != nil {
			return dicom.PixelDataInfo{}, nil, err
		}
	}

	return pixelData, elements, nil
}

func rawFrameSamples(nativeFrame frame.INativeFrame) ([]uint32, error) {
	switch raw := nativeFrame.RawDataSlice().(type) {
	case []uint8:
		values := make([]uint32, len(raw))
		for i, value := range raw {
			values[i] = uint32(value)
		}
		return values, nil
	case []uint16:
		values := make([]uint32, len(raw))
		for i, value := range raw {
			values[i] = uint32(value)
		}
		return values, nil
	case []uint32:
		return append([]uint32(nil), raw...), nil
	case []int:
		values := make([]uint32, len(raw))
		for i, value := range raw {
			values[i] = uint32(value)
		}
		return values, nil
	default:
		return nil, fmt.Errorf("unsupported DICOM sample type: %T", nativeFrame.RawDataSlice())
	}
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

func mapToDisplayRange(value, minValue, maxValue, windowCenter, windowWidth float64, useWindow bool) uint8 {
	if useWindow {
		lower := windowCenter - 0.5 - (windowWidth-1)/2
		upper := windowCenter - 0.5 + (windowWidth-1)/2
		switch {
		case value <= lower:
			return 0
		case value > upper:
			return 255
		default:
			normalized := ((value - (windowCenter - 0.5)) / (windowWidth - 1)) + 0.5
			return clampToByte(normalized * 255)
		}
	}

	if maxValue <= minValue {
		return 0
	}

	normalized := (value - minValue) / (maxValue - minValue)
	return clampToByte(normalized * 255)
}

func clampToByte(value float64) uint8 {
	if value <= 0 {
		return 0
	}
	if value >= 255 {
		return 255
	}
	return uint8(math.Round(value))
}

func convertToGray(img image.Image) *image.Gray {
	bounds := img.Bounds()
	gray := image.NewGray(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gray.SetGray(x, y, color.GrayModel.Convert(img.At(x, y)).(color.Gray))
		}
	}
	return gray
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

	switch elem.Value.ValueType() {
	case dicom.Ints:
		values := dicom.MustGetInts(elem.Value)
		if len(values) == 0 {
			return 0, false
		}
		return values[0], true
	case dicom.Strings:
		values := dicom.MustGetStrings(elem.Value)
		if len(values) == 0 {
			return 0, false
		}
		parsed, err := strconv.Atoi(values[0])
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
	default:
		return 0, false
	}
}

func lookupString(ds *dicom.Dataset, t tag.Tag) (string, bool) {
	elem, err := ds.FindElementByTag(t)
	if err != nil {
		return "", false
	}
	if elem.Value.ValueType() != dicom.Strings {
		return "", false
	}
	values := dicom.MustGetStrings(elem.Value)
	if len(values) == 0 {
		return "", false
	}
	return values[0], true
}

func generateUID() (string, error) {
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	value := new(big.Int).SetBytes(randomBytes)
	return "2.25." + value.String(), nil
}
