package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/mplaczek99/xrayview/internal/imageio"
)

func TestWriteStudyDescriptionIncludesMeasurementScale(t *testing.T) {
	var output bytes.Buffer
	err := writeStudyDescription(&output, imageio.LoadedImage{
		MeasurementScale: &imageio.MeasurementScale{
			RowSpacingMM:    0.4,
			ColumnSpacingMM: 0.6,
			Source:          "PixelSpacing",
		},
	})
	if err != nil {
		t.Fatalf("writeStudyDescription returned error: %v", err)
	}

	var description studyDescription
	if err := json.Unmarshal(output.Bytes(), &description); err != nil {
		t.Fatalf("unmarshal study description: %v", err)
	}

	if description.MeasurementScale == nil {
		t.Fatal("expected measurement scale in study description")
	}
	if description.MeasurementScale.RowSpacingMM != 0.4 {
		t.Fatalf("row spacing = %g, want %g", description.MeasurementScale.RowSpacingMM, 0.4)
	}
	if description.MeasurementScale.ColumnSpacingMM != 0.6 {
		t.Fatalf("column spacing = %g, want %g", description.MeasurementScale.ColumnSpacingMM, 0.6)
	}
	if description.MeasurementScale.Source != "PixelSpacing" {
		t.Fatalf("source = %q, want %q", description.MeasurementScale.Source, "PixelSpacing")
	}
}

func TestValidateConfigAllowsDescribeStudyWithoutOutput(t *testing.T) {
	err := validateConfig(config{
		inputPath:     "input.dcm",
		describeStudy: true,
	})
	if err != nil {
		t.Fatalf("validateConfig returned error: %v", err)
	}
}
