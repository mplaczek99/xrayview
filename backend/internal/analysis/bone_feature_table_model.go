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
	boneTableXBins                = 160
	boneTableYBins                = 224
	boneTableNormalizedBins       = 32
	boneTableGradientBins         = 16
	boneTableProbabilityThreshold = 96
	boneTableMinimumTrainingHits  = 2
	boneTableMinimumPositiveHits  = 1
)

//go:embed bone_feature_table_model.bin.gz
var compressedBoneFeatureTable []byte

var boneFeatureTable = struct {
	sync.Once
	keys          []uint32
	probabilities []uint8
	err           error
}{}

func boneFeatureTableMask(normalized []uint8, gradient []uint8, width, height int) []uint8 {
	mask := make([]uint8, len(normalized))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			index := y*width + x
			probability, ok := boneFeatureTableProbability(boneFeatureTableKey(x, y, width, height, normalized[index], gradient[index]))
			if ok && probability >= boneTableProbabilityThreshold {
				mask[index] = 1
			}
		}
	}
	return mask
}

func boneFeatureTableProbability(key uint32) (uint8, bool) {
	boneFeatureTable.Once.Do(loadBoneFeatureTable)
	if boneFeatureTable.err != nil {
		return 0, false
	}
	index := sort.Search(len(boneFeatureTable.keys), func(index int) bool {
		return boneFeatureTable.keys[index] >= key
	})
	if index >= len(boneFeatureTable.keys) || boneFeatureTable.keys[index] != key {
		return 0, false
	}
	return boneFeatureTable.probabilities[index], true
}

func boneFeatureTableKey(x, y, width, height int, normalized uint8, gradient uint8) uint32 {
	xb := x * boneTableXBins / maxInt(width, 1)
	yb := y * boneTableYBins / maxInt(height, 1)
	nb := int(normalized) * boneTableNormalizedBins / 256
	gb := int(gradient) * boneTableGradientBins / 256
	if xb >= boneTableXBins {
		xb = boneTableXBins - 1
	}
	if yb >= boneTableYBins {
		yb = boneTableYBins - 1
	}
	if nb >= boneTableNormalizedBins {
		nb = boneTableNormalizedBins - 1
	}
	if gb >= boneTableGradientBins {
		gb = boneTableGradientBins - 1
	}
	return uint32(xb) | uint32(yb)<<8 | uint32(nb)<<16 | uint32(gb)<<21
}

func loadBoneFeatureTable() {
	reader, err := gzip.NewReader(bytes.NewReader(compressedBoneFeatureTable))
	if err != nil {
		boneFeatureTable.err = err
		return
	}
	defer reader.Close()

	magic := make([]byte, 5)
	if _, err := io.ReadFull(reader, magic); err != nil {
		boneFeatureTable.err = err
		return
	}
	if string(magic) != "XVBL1" {
		boneFeatureTable.err = fmt.Errorf("invalid bone feature table magic %q", string(magic))
		return
	}

	var count uint32
	if err := binary.Read(reader, binary.LittleEndian, &count); err != nil {
		boneFeatureTable.err = err
		return
	}
	keys := make([]uint32, count)
	probabilities := make([]uint8, count)
	var key uint32
	var probability [1]byte
	for index := uint32(0); index < count; index++ {
		if err := binary.Read(reader, binary.LittleEndian, &key); err != nil {
			boneFeatureTable.err = err
			return
		}
		if _, err := io.ReadFull(reader, probability[:]); err != nil {
			boneFeatureTable.err = err
			return
		}
		keys[index] = key
		probabilities[index] = probability[0]
	}

	boneFeatureTable.keys = keys
	boneFeatureTable.probabilities = probabilities
}
