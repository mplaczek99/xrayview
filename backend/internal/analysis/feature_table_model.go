package analysis

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

const (
	featureTableXBins                = 256
	featureTableYBins                = 512
	featureTableNormalizedBins       = 64
	featureTableScoreBins            = 32
	featureTableProbabilityThreshold = 192
)

//go:embed feature_table_model.bin.gz
var compressedFeatureTable []byte

var featureTable = struct {
	sync.Once
	probabilities map[uint32]uint8
	err           error
}{}

func featureTableProbability(key uint32) (uint8, bool) {
	featureTable.Once.Do(loadFeatureTable)
	return loadedFeatureTableProbability(key)
}

func loadedFeatureTableProbability(key uint32) (uint8, bool) {
	if featureTable.err != nil {
		return 0, false
	}
	probability, ok := featureTable.probabilities[key]
	return probability, ok
}

func loadFeatureTable() {
	reader, err := gzip.NewReader(bytes.NewReader(compressedFeatureTable))
	if err != nil {
		featureTable.err = err
		return
	}
	defer reader.Close()

	magic := make([]byte, 5)
	if _, err := io.ReadFull(reader, magic); err != nil {
		featureTable.err = err
		return
	}
	if string(magic) != "XVFT1" {
		featureTable.err = fmt.Errorf("invalid feature table magic %q", string(magic))
		return
	}

	var count uint32
	if err := binary.Read(reader, binary.LittleEndian, &count); err != nil {
		featureTable.err = err
		return
	}
	probabilities := make(map[uint32]uint8, count)
	var key uint32
	var probability [1]byte
	for index := uint32(0); index < count; index++ {
		if err := binary.Read(reader, binary.LittleEndian, &key); err != nil {
			featureTable.err = err
			return
		}
		if _, err := io.ReadFull(reader, probability[:]); err != nil {
			featureTable.err = err
			return
		}
		probabilities[key] = probability[0]
	}

	featureTable.probabilities = probabilities
}
