package analysis

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"sync"
)

const (
	featureTableXBins                = 192
	featureTableYBins                = 288
	featureTableNormalizedBins       = 48
	featureTableScoreBins            = 32
	featureTableProbabilityThreshold = 179
)

//go:embed feature_table_model.bin.gz
var compressedFeatureTable []byte

var featureTable = struct {
	sync.Once
	keys          []uint32
	probabilities []uint8
	err           error
}{}

func featureTableProbability(key uint32) (uint8, bool) {
	featureTable.Once.Do(loadFeatureTable)
	if featureTable.err != nil {
		return 0, false
	}
	index := sort.Search(len(featureTable.keys), func(index int) bool {
		return featureTable.keys[index] >= key
	})
	if index >= len(featureTable.keys) || featureTable.keys[index] != key {
		return 0, false
	}
	return featureTable.probabilities[index], true
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
	keys := make([]uint32, count)
	probabilities := make([]uint8, count)
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
		keys[index] = key
		probabilities[index] = probability[0]
	}

	featureTable.keys = keys
	featureTable.probabilities = probabilities
}
