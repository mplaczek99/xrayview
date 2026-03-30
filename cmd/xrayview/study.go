package main

import (
	"encoding/json"
	"io"

	"github.com/mplaczek99/xrayview/internal/imageio"
)

type studyDescription struct {
	MeasurementScale *imageio.MeasurementScale `json:"measurementScale,omitempty"`
}

func writeStudyDescription(w io.Writer, loaded imageio.LoadedImage) error {
	encoder := json.NewEncoder(w)
	return encoder.Encode(studyDescription{
		MeasurementScale: loaded.MeasurementScale,
	})
}
