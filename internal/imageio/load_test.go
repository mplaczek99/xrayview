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

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("does-not-exist.dcm")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func writeTestDICOM(path string, pixels []uint16, rows, cols int, photometric string) error {
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
		mustNewElement(nil, tag.SamplesPerPixel, []int{1}),
		mustNewElement(nil, tag.PhotometricInterpretation, []string{photometric}),
		mustNewElement(nil, tag.BitsAllocated, []int{16}),
		mustNewElement(nil, tag.BitsStored, []int{12}),
		mustNewElement(nil, tag.HighBit, []int{11}),
		mustNewElement(nil, tag.PixelRepresentation, []int{0}),
		mustNewElement(nil, tag.PixelData, dicom.PixelDataInfo{IsEncapsulated: false, Frames: []*frame.Frame{{
			Encapsulated: false,
			NativeData: &frame.NativeFrame[uint16]{
				InternalBitsPerSample:   16,
				InternalRows:            rows,
				InternalCols:            cols,
				InternalSamplesPerPixel: 1,
				RawData:                 pixels,
			},
		}}}),
	}}

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
