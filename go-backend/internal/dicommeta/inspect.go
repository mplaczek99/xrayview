package dicommeta

import "sort"

type DecodeProfile struct {
	Path                      string `json:"path"`
	Rows                      uint16 `json:"rows"`
	Columns                   uint16 `json:"columns"`
	SamplesPerPixel           uint16 `json:"samplesPerPixel"`
	BitsAllocated             uint16 `json:"bitsAllocated"`
	BitsStored                uint16 `json:"bitsStored"`
	PixelRepresentation       uint16 `json:"pixelRepresentation"`
	PlanarConfiguration       uint16 `json:"planarConfiguration"`
	NumberOfFrames            uint32 `json:"numberOfFrames"`
	PixelDataEncoding         string `json:"pixelDataEncoding"`
	PhotometricInterpretation string `json:"photometricInterpretation"`
	TransferSyntaxUID         string `json:"transferSyntaxUid"`
}

type DecodeCorpusSummary struct {
	StudyCount                         int      `json:"studyCount"`
	TransferSyntaxUIDs                 []string `json:"transferSyntaxUids"`
	PixelDataEncodings                 []string `json:"pixelDataEncodings"`
	PhotometricInterpretations         []string `json:"photometricInterpretations"`
	SamplesPerPixelValues              []uint16 `json:"samplesPerPixelValues"`
	BitsAllocatedValues                []uint16 `json:"bitsAllocatedValues"`
	NumberOfFramesValues               []uint32 `json:"numberOfFramesValues"`
	EncapsulatedStudyCount             int      `json:"encapsulatedStudyCount"`
	CompressedTransferSyntaxStudyCount int      `json:"compressedTransferSyntaxStudyCount"`
	ColorStudyCount                    int      `json:"colorStudyCount"`
	MultiFrameStudyCount               int      `json:"multiFrameStudyCount"`
	Warnings                           []string `json:"warnings,omitempty"`
}

type DecodeInspectionReport struct {
	Studies []DecodeProfile     `json:"studies"`
	Summary DecodeCorpusSummary `json:"summary"`
}

func InspectFile(path string) (DecodeProfile, error) {
	metadata, err := ReadFile(path)
	if err != nil {
		return DecodeProfile{}, err
	}

	return metadata.DecodeProfile(path), nil
}

func InspectFiles(paths []string) (DecodeInspectionReport, error) {
	profiles := make([]DecodeProfile, 0, len(paths))
	for _, path := range paths {
		profile, err := InspectFile(path)
		if err != nil {
			return DecodeInspectionReport{}, err
		}
		profiles = append(profiles, profile)
	}

	return DecodeInspectionReport{
		Studies: profiles,
		Summary: SummarizeProfiles(profiles),
	}, nil
}

func (metadata Metadata) DecodeProfile(path string) DecodeProfile {
	return DecodeProfile{
		Path:                      path,
		Rows:                      metadata.Rows,
		Columns:                   metadata.Columns,
		SamplesPerPixel:           metadata.SamplesPerPixel,
		BitsAllocated:             metadata.BitsAllocated,
		BitsStored:                metadata.BitsStored,
		PixelRepresentation:       metadata.PixelRepresentation,
		PlanarConfiguration:       metadata.PlanarConfiguration,
		NumberOfFrames:            metadata.NumberOfFrames,
		PixelDataEncoding:         metadata.PixelDataEncoding,
		PhotometricInterpretation: metadata.PhotometricInterpretation,
		TransferSyntaxUID:         metadata.TransferSyntaxUID,
	}
}

func SummarizeProfiles(profiles []DecodeProfile) DecodeCorpusSummary {
	summary := DecodeCorpusSummary{
		StudyCount: profilesCount(profiles),
	}
	if len(profiles) == 0 {
		summary.Warnings = []string{"no studies inspected"}
		return summary
	}

	transferSyntaxUIDs := make(map[string]struct{}, len(profiles))
	pixelDataEncodings := make(map[string]struct{}, len(profiles))
	photometricInterpretations := make(map[string]struct{}, len(profiles))
	samplesPerPixelValues := make(map[uint16]struct{}, len(profiles))
	bitsAllocatedValues := make(map[uint16]struct{}, len(profiles))
	numberOfFramesValues := make(map[uint32]struct{}, len(profiles))

	for _, profile := range profiles {
		transferSyntaxUIDs[profile.TransferSyntaxUID] = struct{}{}
		pixelDataEncodings[profile.PixelDataEncoding] = struct{}{}
		photometricInterpretations[profile.PhotometricInterpretation] = struct{}{}
		samplesPerPixelValues[profile.SamplesPerPixel] = struct{}{}
		bitsAllocatedValues[profile.BitsAllocated] = struct{}{}
		numberOfFramesValues[profile.NumberOfFrames] = struct{}{}

		if profile.PixelDataEncoding == PixelDataEncodingEncapsulated {
			summary.EncapsulatedStudyCount++
		}
		if !isUncompressedTransferSyntax(profile.TransferSyntaxUID) {
			summary.CompressedTransferSyntaxStudyCount++
		}
		if profile.SamplesPerPixel > 1 {
			summary.ColorStudyCount++
		}
		if profile.NumberOfFrames > 1 {
			summary.MultiFrameStudyCount++
		}
	}

	summary.TransferSyntaxUIDs = sortedKeys(transferSyntaxUIDs)
	summary.PixelDataEncodings = sortedKeys(pixelDataEncodings)
	summary.PhotometricInterpretations = sortedKeys(photometricInterpretations)
	summary.SamplesPerPixelValues = sortedUint16Keys(samplesPerPixelValues)
	summary.BitsAllocatedValues = sortedUint16Keys(bitsAllocatedValues)
	summary.NumberOfFramesValues = sortedUint32Keys(numberOfFramesValues)

	if len(summary.TransferSyntaxUIDs) == 1 {
		summary.Warnings = append(summary.Warnings, "sample set contains one transfer syntax")
	}
	if summary.EncapsulatedStudyCount == 0 {
		summary.Warnings = append(summary.Warnings, "sample set contains no encapsulated pixel data")
	}
	if summary.CompressedTransferSyntaxStudyCount == 0 {
		summary.Warnings = append(summary.Warnings, "sample set contains no compressed transfer syntaxes")
	}
	if summary.MultiFrameStudyCount == 0 {
		summary.Warnings = append(summary.Warnings, "sample set contains no multi-frame studies")
	}

	return summary
}

func isUncompressedTransferSyntax(uid string) bool {
	switch uid {
	case "",
		implicitLittleEndianTransferSyntax,
		"1.2.840.10008.1.2.1",
		explicitBigEndianTransferSyntax,
		deflatedTransferSyntax:
		return true
	default:
		return false
	}
}

func profilesCount(profiles []DecodeProfile) int {
	return len(profiles)
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}

func sortedUint16Keys(values map[uint16]struct{}) []uint16 {
	keys := make([]uint16, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Slice(keys, func(i int, j int) bool {
		return keys[i] < keys[j]
	})
	return keys
}

func sortedUint32Keys(values map[uint32]struct{}) []uint32 {
	keys := make([]uint32, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Slice(keys, func(i int, j int) bool {
		return keys[i] < keys[j]
	})
	return keys
}
