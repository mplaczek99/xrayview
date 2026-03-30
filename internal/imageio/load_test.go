package imageio

import (
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/suyashkumar/dicom"
	"github.com/suyashkumar/dicom/pkg/frame"
	"github.com/suyashkumar/dicom/pkg/tag"
	"github.com/suyashkumar/dicom/pkg/uid"
)

func TestLoadDICOM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.dcm")
	if err := writeTestDICOM(path, []uint16{0, 2048, 4095}, 1, 3, "MONOCHROME2"); err != nil {
		t.Fatalf("write test dicom: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load dicom: %v", err)
	}

	if loaded.Format != dicomFormat {
		t.Fatalf("format = %q, want %q", loaded.Format, dicomFormat)
	}
	if loaded.DICOM == nil {
		t.Fatal("expected DICOM dataset to be populated")
	}

	gray, ok := loaded.Image.(*image.Gray)
	if !ok {
		t.Fatalf("image type = %T, want *image.Gray", loaded.Image)
	}

	if got := gray.GrayAt(0, 0).Y; got != 0 {
		t.Fatalf("pixel(0,0) = %d, want 0", got)
	}
	if got := gray.GrayAt(1, 0).Y; got < 127 || got > 128 {
		t.Fatalf("pixel(1,0) = %d, want around 128", got)
	}
	if got := gray.GrayAt(2, 0).Y; got != 255 {
		t.Fatalf("pixel(2,0) = %d, want 255", got)
	}
}

func TestLoadDICOMAppliesStringWindowing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "windowed.dcm")
	if err := writeCustomTestDICOM(
		path,
		&frame.NativeFrame[uint16]{
			InternalBitsPerSample:   16,
			InternalRows:            1,
			InternalCols:            4,
			InternalSamplesPerPixel: 1,
			RawData:                 []uint16{0, 500, 1000, 1500},
		},
		1,
		4,
		"MONOCHROME2",
		16,
		16,
		15,
		0,
		mustNewElement(t, tag.WindowCenter, []string{"1000.0", "1100.0"}),
		mustNewElement(t, tag.WindowWidth, []string{"1000.0", "900.0"}),
	); err != nil {
		t.Fatalf("write windowed dicom: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load dicom: %v", err)
	}

	gray, ok := loaded.Image.(*image.Gray)
	if !ok {
		t.Fatalf("image type = %T, want *image.Gray", loaded.Image)
	}

	if got := gray.GrayAt(0, 0).Y; got != 0 {
		t.Fatalf("pixel(0,0) = %d, want 0", got)
	}
	if got := gray.GrayAt(1, 0).Y; got != 0 {
		t.Fatalf("pixel(1,0) = %d, want 0", got)
	}
	if got := gray.GrayAt(2, 0).Y; got < 127 || got > 128 {
		t.Fatalf("pixel(2,0) = %d, want around 128", got)
	}
	if got := gray.GrayAt(3, 0).Y; got != 255 {
		t.Fatalf("pixel(3,0) = %d, want 255", got)
	}
}

func TestRenderNativeFrameSupportsSignedNativeSamples(t *testing.T) {
	gray, err := renderNativeFrame(&frame.NativeFrame[int16]{
		InternalBitsPerSample:   16,
		InternalRows:            1,
		InternalCols:            3,
		InternalSamplesPerPixel: 1,
		RawData:                 []int16{-1024, 0, 1024},
	}, &dicom.Dataset{Elements: []*dicom.Element{
		mustNewElement(t, tag.BitsStored, []int{16}),
		mustNewElement(t, tag.PixelRepresentation, []int{1}),
		mustNewElement(t, tag.PhotometricInterpretation, []string{"MONOCHROME2"}),
	}})
	if err != nil {
		t.Fatalf("render native frame: %v", err)
	}

	if got := gray.GrayAt(0, 0).Y; got != 0 {
		t.Fatalf("pixel(0,0) = %d, want 0", got)
	}
	if got := gray.GrayAt(1, 0).Y; got < 127 || got > 128 {
		t.Fatalf("pixel(1,0) = %d, want around 128", got)
	}
	if got := gray.GrayAt(2, 0).Y; got != 255 {
		t.Fatalf("pixel(2,0) = %d, want 255", got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("does-not-exist.dcm")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func writeTestDICOM(path string, pixels []uint16, rows, cols int, photometric string) error {
	return writeCustomTestDICOM(
		path,
		&frame.NativeFrame[uint16]{
			InternalBitsPerSample:   16,
			InternalRows:            rows,
			InternalCols:            cols,
			InternalSamplesPerPixel: 1,
			RawData:                 pixels,
		},
		rows,
		cols,
		photometric,
		16,
		12,
		11,
		0,
	)
}

func writeCustomTestDICOM(
	path string,
	nativeFrame frame.INativeFrame,
	rows, cols int,
	photometric string,
	bitsAllocated, bitsStored, highBit, pixelRepresentation int,
	extraElements ...*dicom.Element,
) error {
	dataset := dicom.Dataset{Elements: []*dicom.Element{
		mustNewElement(nil, tag.MediaStorageSOPClassUID, []string{secondaryCaptureSOPClassUID}),
		mustNewElement(nil, tag.MediaStorageSOPInstanceUID, []string{"1.2.3.4.5.6.7.8.1"}),
		mustNewElement(nil, tag.TransferSyntaxUID, []string{uid.ExplicitVRLittleEndian}),
		mustNewElement(nil, tag.SOPClassUID, []string{secondaryCaptureSOPClassUID}),
		mustNewElement(nil, tag.SOPInstanceUID, []string{"1.2.3.4.5.6.7.8.1"}),
		mustNewElement(nil, tag.PatientID, []string{"PID-123"}),
		mustNewElement(nil, tag.StudyInstanceUID, []string{"1.2.3.4.5.6.7.8.2"}),
		mustNewElement(nil, tag.Rows, []int{rows}),
		mustNewElement(nil, tag.Columns, []int{cols}),
		mustNewElement(nil, tag.SamplesPerPixel, []int{nativeFrame.SamplesPerPixel()}),
		mustNewElement(nil, tag.PhotometricInterpretation, []string{photometric}),
	}}
	dataset.Elements = append(dataset.Elements,
		mustNewElement(nil, tag.BitsAllocated, []int{bitsAllocated}),
		mustNewElement(nil, tag.BitsStored, []int{bitsStored}),
		mustNewElement(nil, tag.HighBit, []int{highBit}),
		mustNewElement(nil, tag.PixelRepresentation, []int{pixelRepresentation}),
	)
	dataset.Elements = append(dataset.Elements, extraElements...)
	dataset.Elements = append(dataset.Elements, mustNewElement(nil, tag.PixelData, dicom.PixelDataInfo{IsEncapsulated: false, Frames: []*frame.Frame{{
		Encapsulated: false,
		NativeData:   nativeFrame,
	}}}))

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	return dicom.Write(file, dataset)
}

func mustNewElement(t *testing.T, dicomTag tag.Tag, data any) *dicom.Element {
	if t != nil {
		t.Helper()
	}
	elem, err := dicom.NewElement(dicomTag, data)
	if err != nil {
		if t != nil {
			t.Fatalf("new element %s: %v", dicomTag, err)
		}
		panic(err)
	}
	return elem
}
