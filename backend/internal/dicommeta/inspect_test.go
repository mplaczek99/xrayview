package dicommeta

import "testing"

func TestMetadataDecodeProfileCopiesDecodeFields(t *testing.T) {
	metadata := Metadata{
		Rows:                      1088,
		Columns:                   2048,
		SamplesPerPixel:           3,
		BitsAllocated:             8,
		BitsStored:                8,
		PixelRepresentation:       0,
		PlanarConfiguration:       0,
		NumberOfFrames:            1,
		PixelDataEncoding:         PixelDataEncodingNative,
		PhotometricInterpretation: "RGB",
		TransferSyntaxUID:         "1.2.840.10008.1.2.1",
	}

	profile := metadata.DecodeProfile("/tmp/study.dcm")

	if got, want := profile.Path, "/tmp/study.dcm"; got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
	if got, want := profile.SamplesPerPixel, uint16(3); got != want {
		t.Fatalf("SamplesPerPixel = %d, want %d", got, want)
	}
	if got, want := profile.PixelDataEncoding, PixelDataEncodingNative; got != want {
		t.Fatalf("PixelDataEncoding = %q, want %q", got, want)
	}
}

func TestSummarizeProfilesFlagsNarrowUncompressedCorpus(t *testing.T) {
	summary := SummarizeProfiles([]DecodeProfile{
		{
			TransferSyntaxUID:         "1.2.840.10008.1.2.1",
			PixelDataEncoding:         PixelDataEncodingNative,
			PhotometricInterpretation: "MONOCHROME2",
			SamplesPerPixel:           1,
			BitsAllocated:             8,
			NumberOfFrames:            1,
		},
		{
			TransferSyntaxUID:         "1.2.840.10008.1.2.1",
			PixelDataEncoding:         PixelDataEncodingNative,
			PhotometricInterpretation: "RGB",
			SamplesPerPixel:           3,
			BitsAllocated:             8,
			NumberOfFrames:            1,
		},
	})

	if got, want := summary.StudyCount, 2; got != want {
		t.Fatalf("StudyCount = %d, want %d", got, want)
	}
	if got, want := summary.EncapsulatedStudyCount, 0; got != want {
		t.Fatalf("EncapsulatedStudyCount = %d, want %d", got, want)
	}
	if got, want := summary.CompressedTransferSyntaxStudyCount, 0; got != want {
		t.Fatalf("CompressedTransferSyntaxStudyCount = %d, want %d", got, want)
	}
	if got, want := summary.ColorStudyCount, 1; got != want {
		t.Fatalf("ColorStudyCount = %d, want %d", got, want)
	}
	if got, want := summary.MultiFrameStudyCount, 0; got != want {
		t.Fatalf("MultiFrameStudyCount = %d, want %d", got, want)
	}
	assertStringSliceEqual(
		t,
		summary.Warnings,
		[]string{
			"sample set contains one transfer syntax",
			"sample set contains no encapsulated pixel data",
			"sample set contains no compressed transfer syntaxes",
			"sample set contains no multi-frame studies",
		},
	)
}

func TestSummarizeProfilesCapturesEncapsulatedCompressedCoverage(t *testing.T) {
	summary := SummarizeProfiles([]DecodeProfile{
		{
			TransferSyntaxUID:         "1.2.840.10008.1.2.4.50",
			PixelDataEncoding:         PixelDataEncodingEncapsulated,
			PhotometricInterpretation: "MONOCHROME2",
			SamplesPerPixel:           1,
			BitsAllocated:             16,
			NumberOfFrames:            4,
		},
	})

	if got, want := summary.EncapsulatedStudyCount, 1; got != want {
		t.Fatalf("EncapsulatedStudyCount = %d, want %d", got, want)
	}
	if got, want := summary.CompressedTransferSyntaxStudyCount, 1; got != want {
		t.Fatalf("CompressedTransferSyntaxStudyCount = %d, want %d", got, want)
	}
	if got, want := summary.MultiFrameStudyCount, 1; got != want {
		t.Fatalf("MultiFrameStudyCount = %d, want %d", got, want)
	}
	assertStringSliceEqual(t, summary.Warnings, []string{"sample set contains one transfer syntax"})
}

func assertStringSliceEqual(t *testing.T, got []string, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("len(slice) = %d, want %d; got=%v want=%v", len(got), len(want), got, want)
	}

	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("slice[%d] = %q, want %q; got=%v want=%v", index, got[index], want[index], got, want)
		}
	}
}
