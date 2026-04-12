package dicommeta

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"strconv"
	"strings"
	"testing"

	"xrayview/backend/internal/imaging"
)

type decodeDicomOptions struct {
	buildOptions
	studyInstanceUID string
	patientName      string
	rescaleSlope     string
	rescaleIntercept string
}

func TestDecodeBuildsScaledMonochromeStudyFromNativePixelData(t *testing.T) {
	study, err := Decode(bytes.NewReader(buildDecodeTestDicom(
		decodeDicomOptions{
			buildOptions: buildOptions{
				withPart10:                true,
				transferSyntaxUID:         "1.2.840.10008.1.2.1",
				datasetSyntax:             transferSyntax{byteOrder: binary.LittleEndian, explicit: true},
				rows:                      1,
				columns:                   2,
				photometricInterpretation: "MONOCHROME1",
				bitsAllocated:             8,
				bitsStored:                8,
				pixelSpacing:              "0.25\\0.50",
				windowCenter:              "100",
				windowWidth:               "200",
			},
			studyInstanceUID: "1.2.3.4",
			patientName:      "Test^Patient",
			rescaleSlope:     "2",
			rescaleIntercept: "-10",
		},
		[]byte{0, 100},
		true,
	)))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}

	if got, want := study.Image.Width, uint32(2); got != want {
		t.Fatalf("Image.Width = %d, want %d", got, want)
	}
	if got, want := study.Image.Height, uint32(1); got != want {
		t.Fatalf("Image.Height = %d, want %d", got, want)
	}
	if got, want := study.Image.Format, imaging.FormatGrayFloat32; got != want {
		t.Fatalf("Image.Format = %q, want %q", got, want)
	}
	if got, want := len(study.Image.Pixels), 2; got != want {
		t.Fatalf("len(Image.Pixels) = %d, want %d", got, want)
	}
	if got, want := study.Image.Pixels[0], float32(-10); got != want {
		t.Fatalf("Image.Pixels[0] = %v, want %v", got, want)
	}
	if got, want := study.Image.Pixels[1], float32(190); got != want {
		t.Fatalf("Image.Pixels[1] = %v, want %v", got, want)
	}
	if got, want := study.Image.MinValue, float32(-10); got != want {
		t.Fatalf("Image.MinValue = %v, want %v", got, want)
	}
	if got, want := study.Image.MaxValue, float32(190); got != want {
		t.Fatalf("Image.MaxValue = %v, want %v", got, want)
	}
	if !study.Image.Invert {
		t.Fatal("Image.Invert = false, want true for MONOCHROME1")
	}
	if study.Image.DefaultWindow == nil {
		t.Fatal("Image.DefaultWindow = nil, want window metadata")
	}
	if got, want := study.Image.DefaultWindow.Center, float32(100); got != want {
		t.Fatalf("DefaultWindow.Center = %v, want %v", got, want)
	}
	if got, want := study.Image.DefaultWindow.Width, float32(200); got != want {
		t.Fatalf("DefaultWindow.Width = %v, want %v", got, want)
	}

	if got, want := study.Metadata.StudyInstanceUID, "1.2.3.4"; got != want {
		t.Fatalf("StudyInstanceUID = %q, want %q", got, want)
	}
	if got, want := len(study.Metadata.PreservedElements), 2; got != want {
		t.Fatalf("len(PreservedElements) = %d, want %d", got, want)
	}
	patientName, ok := findPreservedElement(study.Metadata.PreservedElements, 0x0010, 0x0010)
	if !ok {
		t.Fatal("patient name preserved element missing")
	}
	if got, want := patientName.Values, []string{"Test^Patient"}; !stringSlicesEqual(got, want) {
		t.Fatalf("patientName.Values = %v, want %v", got, want)
	}
	pixelSpacing, ok := findPreservedElement(study.Metadata.PreservedElements, 0x0028, 0x0030)
	if !ok {
		t.Fatal("pixel spacing preserved element missing")
	}
	if got, want := pixelSpacing.Values, []string{"0.25", "0.50"}; !stringSlicesEqual(got, want) {
		t.Fatalf("pixelSpacing.Values = %v, want %v", got, want)
	}

	if study.MeasurementScale == nil {
		t.Fatal("MeasurementScale = nil, want pixel spacing scale")
	}
	if got, want := study.MeasurementScale.RowSpacingMM, 0.25; got != want {
		t.Fatalf("MeasurementScale.RowSpacingMM = %v, want %v", got, want)
	}
	if got, want := study.MeasurementScale.ColumnSpacingMM, 0.50; got != want {
		t.Fatalf("MeasurementScale.ColumnSpacingMM = %v, want %v", got, want)
	}
	if got, want := study.MeasurementScale.Source, "PixelSpacing"; got != want {
		t.Fatalf("MeasurementScale.Source = %q, want %q", got, want)
	}
}

func TestDecodeRejectsMalformedSourceStudies(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "unsupported deflated transfer syntax",
			data: buildDecodeTestDicom(
				decodeDicomOptions{
					buildOptions: buildOptions{
						withPart10:                true,
						transferSyntaxUID:         deflatedTransferSyntax,
						datasetSyntax:             transferSyntax{byteOrder: binary.LittleEndian, explicit: true},
						rows:                      1,
						columns:                   1,
						photometricInterpretation: "MONOCHROME2",
					},
				},
				[]byte{0},
				true,
			),
			want: "unsupported deflated transfer syntax",
		},
		{
			name: "missing rows",
			data: buildDecodeTestDicom(
				decodeDicomOptions{
					buildOptions: buildOptions{
						withPart10:                true,
						transferSyntaxUID:         "1.2.840.10008.1.2.1",
						datasetSyntax:             transferSyntax{byteOrder: binary.LittleEndian, explicit: true},
						rows:                      0,
						columns:                   1,
						photometricInterpretation: "MONOCHROME2",
					},
				},
				[]byte{0},
				true,
			),
			want: "missing Rows",
		},
		{
			name: "missing columns",
			data: buildDecodeTestDicom(
				decodeDicomOptions{
					buildOptions: buildOptions{
						withPart10:                true,
						transferSyntaxUID:         "1.2.840.10008.1.2.1",
						datasetSyntax:             transferSyntax{byteOrder: binary.LittleEndian, explicit: true},
						rows:                      1,
						columns:                   0,
						photometricInterpretation: "MONOCHROME2",
					},
				},
				[]byte{0},
				true,
			),
			want: "missing Columns",
		},
		{
			name: "missing pixel data",
			data: buildDecodeTestDicom(
				decodeDicomOptions{
					buildOptions: buildOptions{
						withPart10:                true,
						transferSyntaxUID:         "1.2.840.10008.1.2.1",
						datasetSyntax:             transferSyntax{byteOrder: binary.LittleEndian, explicit: true},
						rows:                      1,
						columns:                   1,
						photometricInterpretation: "MONOCHROME2",
					},
				},
				nil,
				false,
			),
			want: "missing PixelData",
		},
		{
			name: "short native pixel data",
			data: buildDecodeTestDicom(
				decodeDicomOptions{
					buildOptions: buildOptions{
						withPart10:                true,
						transferSyntaxUID:         "1.2.840.10008.1.2.1",
						datasetSyntax:             transferSyntax{byteOrder: binary.LittleEndian, explicit: true},
						rows:                      1,
						columns:                   2,
						photometricInterpretation: "MONOCHROME2",
					},
				},
				[]byte{0},
				true,
			),
			want: "dicom frame sample count 1 does not match image size 2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode(bytes.NewReader(test.data))
			if err == nil {
				t.Fatal("Decode returned nil error, want malformed-source failure")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Decode error = %q, want substring %q", err, test.want)
			}
		})
	}
}

func TestDecodeNativePixelDataRejectsUnsupportedLayouts(t *testing.T) {
	tests := []struct {
		name     string
		metadata Metadata
		raw      []byte
		want     string
	}{
		{
			name: "unsupported bits allocated",
			metadata: Metadata{
				Rows:                      1,
				Columns:                   1,
				SamplesPerPixel:           1,
				BitsAllocated:             12,
				BitsStored:                12,
				PhotometricInterpretation: "MONOCHROME2",
			},
			raw:  []byte{0x00, 0x00},
			want: "unsupported BitsAllocated for source decode: 12",
		},
		{
			name: "short monochrome frame",
			metadata: Metadata{
				Rows:                      1,
				Columns:                   2,
				SamplesPerPixel:           1,
				BitsAllocated:             8,
				BitsStored:                8,
				PhotometricInterpretation: "MONOCHROME2",
			},
			raw:  []byte{0x00},
			want: "dicom frame sample count 1 does not match image size 2",
		},
		{
			name: "unsupported 16-bit color",
			metadata: Metadata{
				Rows:                      1,
				Columns:                   1,
				SamplesPerPixel:           3,
				BitsAllocated:             16,
				BitsStored:                16,
				PhotometricInterpretation: "RGB",
			},
			raw:  make([]byte, 6),
			want: "16-bit color DICOM source decode is not supported yet",
		},
		{
			name: "unsupported color photometric interpretation",
			metadata: Metadata{
				Rows:                      1,
				Columns:                   2,
				SamplesPerPixel:           3,
				BitsAllocated:             8,
				BitsStored:                8,
				PhotometricInterpretation: "YBR_FULL",
			},
			raw:  []byte{0, 0, 0, 255, 255, 255},
			want: "unsupported color photometric interpretation: YBR_FULL",
		},
		{
			name: "unsupported planar configuration",
			metadata: Metadata{
				Rows:                      1,
				Columns:                   2,
				SamplesPerPixel:           3,
				BitsAllocated:             8,
				BitsStored:                8,
				PhotometricInterpretation: "RGB",
				PlanarConfiguration:       2,
			},
			raw:  []byte{0, 0, 0, 255, 255, 255},
			want: "unsupported planar configuration: 2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := &sourceStudyState{metadata: test.metadata}
			_, err := state.decodeNativePixelData(test.raw, binary.LittleEndian)
			if err == nil {
				t.Fatal("decodeNativePixelData returned nil error, want rejection")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("decodeNativePixelData error = %q, want substring %q", err, test.want)
			}
		})
	}
}

func TestDecodeEncapsulatedPixelDataRejectsMalformedFragments(t *testing.T) {
	syntax := transferSyntax{byteOrder: binary.LittleEndian, explicit: true}

	tests := []struct {
		name     string
		metadata Metadata
		build    func(*bytes.Buffer)
		want     string
	}{
		{
			name: "multi-frame encapsulated study",
			metadata: Metadata{
				NumberOfFrames: 2,
			},
			build: func(*bytes.Buffer) {},
			want:  "unsupported multi-frame encapsulated source decode: 2 frames",
		},
		{
			name:     "missing basic offset item",
			metadata: Metadata{},
			build: func(payload *bytes.Buffer) {
				writeSpecialHeader(payload, tagSequenceDelimitation, 0)
			},
			want: "invalid encapsulated pixel data: expected item header",
		},
		{
			name:     "undefined basic offset length",
			metadata: Metadata{},
			build: func(payload *bytes.Buffer) {
				writeSpecialHeader(payload, tagItem, undefinedLength)
			},
			want: "invalid encapsulated pixel data: undefined basic offset table length",
		},
		{
			name:     "undefined fragment length",
			metadata: Metadata{},
			build: func(payload *bytes.Buffer) {
				writeSpecialHeader(payload, tagItem, 0)
				writeSpecialHeader(payload, tagItem, undefinedLength)
			},
			want: "invalid encapsulated pixel data: undefined fragment length",
		},
		{
			name:     "unexpected fragment tag",
			metadata: Metadata{},
			build: func(payload *bytes.Buffer) {
				writeSpecialHeader(payload, tagItem, 0)
				writeElement(payload, syntax, tagRows, "US", encodeUint16(binary.LittleEndian, 1))
			},
			want: "invalid encapsulated pixel data: unexpected item (0028,0010)",
		},
		{
			name:     "empty payload",
			metadata: Metadata{},
			build: func(payload *bytes.Buffer) {
				writeSpecialHeader(payload, tagItem, 0)
				writeSpecialHeader(payload, tagSequenceDelimitation, 0)
			},
			want: "encapsulated pixel data did not contain any frame bytes",
		},
		{
			name:     "invalid compressed payload",
			metadata: Metadata{},
			build: func(payload *bytes.Buffer) {
				writeSpecialHeader(payload, tagItem, 0)
				writeSpecialHeader(payload, tagItem, 4)
				payload.WriteString("nope")
				writeSpecialHeader(payload, tagSequenceDelimitation, 0)
			},
			want: "decode encapsulated pixel data",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var payload bytes.Buffer
			test.build(&payload)

			state := &sourceStudyState{metadata: test.metadata}
			_, err := state.decodeEncapsulatedPixelData(bytes.NewReader(payload.Bytes()), syntax)
			if err == nil {
				t.Fatal("decodeEncapsulatedPixelData returned nil error, want rejection")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("decodeEncapsulatedPixelData error = %q, want substring %q", err, test.want)
			}
		})
	}
}

func TestDecodeCompressedImageBuildsSourceImageFromJPEGPayload(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 2, 1))
	img.SetGray(0, 0, color.Gray{Y: 0})
	img.SetGray(1, 0, color.Gray{Y: 255})

	var payload bytes.Buffer
	if err := jpeg.Encode(&payload, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatalf("jpeg.Encode returned error: %v", err)
	}

	windowCenter := 128.0
	windowWidth := 256.0
	state := &sourceStudyState{
		metadata: Metadata{
			PhotometricInterpretation: "MONOCHROME1",
			WindowCenter:              &windowCenter,
			WindowWidth:               &windowWidth,
		},
	}

	sourceImage, err := state.decodeCompressedImage(payload.Bytes())
	if err != nil {
		t.Fatalf("decodeCompressedImage returned error: %v", err)
	}

	if got, want := sourceImage.Width, uint32(2); got != want {
		t.Fatalf("Width = %d, want %d", got, want)
	}
	if got, want := sourceImage.Height, uint32(1); got != want {
		t.Fatalf("Height = %d, want %d", got, want)
	}
	if got, want := sourceImage.Format, imaging.FormatGrayFloat32; got != want {
		t.Fatalf("Format = %q, want %q", got, want)
	}
	if got, want := len(sourceImage.Pixels), 2; got != want {
		t.Fatalf("len(Pixels) = %d, want %d", got, want)
	}
	if sourceImage.DefaultWindow == nil {
		t.Fatal("DefaultWindow = nil, want resolved window metadata")
	}
	if got, want := sourceImage.DefaultWindow.Center, float32(128); got != want {
		t.Fatalf("DefaultWindow.Center = %v, want %v", got, want)
	}
	if got, want := sourceImage.DefaultWindow.Width, float32(256); got != want {
		t.Fatalf("DefaultWindow.Width = %v, want %v", got, want)
	}
	if !sourceImage.Invert {
		t.Fatal("Invert = false, want true for MONOCHROME1")
	}
	if sourceImage.MinValue >= sourceImage.MaxValue {
		t.Fatalf("MinValue = %v, MaxValue = %v, want increasing intensity range", sourceImage.MinValue, sourceImage.MaxValue)
	}
}

func BenchmarkReadU16Samples(b *testing.B) {
	const width, height = 2048, 1536
	raw := make([]byte, width*height*2)
	for i := range raw {
		raw[i] = byte(i*7 + 13)
	}
	b.Run("LittleEndian", func(b *testing.B) {
		b.SetBytes(int64(len(raw)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = readU16Samples(raw, binary.LittleEndian)
		}
	})
	b.Run("BigEndian", func(b *testing.B) {
		b.SetBytes(int64(len(raw)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = readU16Samples(raw, binary.BigEndian)
		}
	})
}

func BenchmarkReadU32Samples(b *testing.B) {
	const width, height = 2048, 1536
	raw := make([]byte, width*height*4)
	for i := range raw {
		raw[i] = byte(i*7 + 13)
	}
	b.Run("LittleEndian", func(b *testing.B) {
		b.SetBytes(int64(len(raw)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = readU32Samples(raw, binary.LittleEndian)
		}
	})
	b.Run("BigEndian", func(b *testing.B) {
		b.SetBytes(int64(len(raw)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = readU32Samples(raw, binary.BigEndian)
		}
	})
}

func BenchmarkDecodeNativePixelData(b *testing.B) {
	const width, height = 2048, 1536
	raw := make([]byte, width*height*2)
	for i := range raw {
		raw[i] = byte(i*7 + 13)
	}
	state := &sourceStudyState{
		metadata: Metadata{
			Rows:                      height,
			Columns:                   width,
			SamplesPerPixel:           1,
			BitsAllocated:             16,
			BitsStored:                16,
			PixelRepresentation:       0,
			PhotometricInterpretation: "MONOCHROME2",
		},
	}
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := state.decodeNativePixelData(raw, binary.LittleEndian)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func buildDecodeTestDicom(options decodeDicomOptions, pixelData []byte, includePixelData bool) []byte {
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
	if options.studyInstanceUID != "" {
		writeElement(
			&payload,
			options.datasetSyntax,
			tagStudyInstanceUID,
			"UI",
			encodeUI(options.studyInstanceUID),
		)
	}
	if options.patientName != "" {
		writeElement(
			&payload,
			options.datasetSyntax,
			tag{group: 0x0010, element: 0x0010},
			"PN",
			encodeString(options.patientName, ' '),
		)
	}
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
	if options.rescaleSlope != "" {
		writeElement(
			&payload,
			options.datasetSyntax,
			tagRescaleSlope,
			"DS",
			encodeString(options.rescaleSlope, ' '),
		)
	}
	if options.rescaleIntercept != "" {
		writeElement(
			&payload,
			options.datasetSyntax,
			tagRescaleIntercept,
			"DS",
			encodeString(options.rescaleIntercept, ' '),
		)
	}
	if includePixelData {
		pixelVR := "OB"
		if defaultUint16(options.bitsAllocated, 8) > 8 {
			pixelVR = "OW"
		}
		writeElement(
			&payload,
			options.datasetSyntax,
			tagPixelData,
			pixelVR,
			pixelData,
		)
	}

	return payload.Bytes()
}

func writeSpecialHeader(payload *bytes.Buffer, field tag, length uint32) {
	writeUint16(payload, binary.LittleEndian, field.group)
	writeUint16(payload, binary.LittleEndian, field.element)
	writeUint32(payload, binary.LittleEndian, length)
}

func stringSlicesEqual(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}

	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}

	return true
}

func findPreservedElement(
	elements []PreservedElement,
	group uint16,
	element uint16,
) (PreservedElement, bool) {
	for _, candidate := range elements {
		if candidate.TagGroup == group && candidate.TagElement == element {
			return candidate, true
		}
	}

	return PreservedElement{}, false
}
