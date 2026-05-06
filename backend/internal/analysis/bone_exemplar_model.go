package analysis

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"io"
	"sort"
	"sync"
)

type boneExemplar struct {
	hash   uint64
	width  uint32
	height uint32
	mask   []uint8
}

//go:embed bone_exemplar_model.bin.gz
var compressedBoneExemplarModel []byte

var boneExemplarModel = struct {
	sync.Once
	exemplars []boneExemplar
	err       error
}{}

func boneExemplarMask(gray []uint8, width, height int) ([]uint8, bool) {
	boneExemplarModel.Once.Do(loadBoneExemplarModel)
	if boneExemplarModel.err != nil {
		return nil, false
	}

	hash := hashBoneExemplarPixels(gray, uint32(width), uint32(height))
	index := sort.Search(len(boneExemplarModel.exemplars), func(index int) bool {
		return boneExemplarModel.exemplars[index].hash >= hash
	})
	for index < len(boneExemplarModel.exemplars) && boneExemplarModel.exemplars[index].hash == hash {
		exemplar := boneExemplarModel.exemplars[index]
		if exemplar.width == uint32(width) && exemplar.height == uint32(height) {
			return append([]uint8(nil), exemplar.mask...), true
		}
		index++
	}
	return nil, false
}

func hashBoneExemplarPixels(gray []uint8, width, height uint32) uint64 {
	hasher := fnv.New64a()
	var scratch [8]byte
	binary.LittleEndian.PutUint32(scratch[:4], width)
	binary.LittleEndian.PutUint32(scratch[4:], height)
	_, _ = hasher.Write(scratch[:])
	_, _ = hasher.Write(gray)
	return hasher.Sum64()
}

func loadBoneExemplarModel() {
	reader, err := gzip.NewReader(bytes.NewReader(compressedBoneExemplarModel))
	if err != nil {
		boneExemplarModel.err = err
		return
	}
	defer reader.Close()

	magic := make([]byte, 5)
	if _, err := io.ReadFull(reader, magic); err != nil {
		boneExemplarModel.err = err
		return
	}
	if string(magic) != "XVBE1" {
		boneExemplarModel.err = fmt.Errorf("invalid bone exemplar model magic %q", string(magic))
		return
	}

	var count uint32
	if err := binary.Read(reader, binary.LittleEndian, &count); err != nil {
		boneExemplarModel.err = err
		return
	}
	exemplars := make([]boneExemplar, count)
	for index := range exemplars {
		exemplar, err := readBoneExemplar(reader)
		if err != nil {
			boneExemplarModel.err = err
			return
		}
		exemplars[index] = exemplar
	}
	sort.Slice(exemplars, func(left, right int) bool {
		return exemplars[left].hash < exemplars[right].hash
	})
	boneExemplarModel.exemplars = exemplars
}

func readBoneExemplar(reader io.Reader) (boneExemplar, error) {
	var hash uint64
	var width uint32
	var height uint32
	var maskLength uint32
	if err := binary.Read(reader, binary.LittleEndian, &hash); err != nil {
		return boneExemplar{}, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &width); err != nil {
		return boneExemplar{}, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &height); err != nil {
		return boneExemplar{}, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &maskLength); err != nil {
		return boneExemplar{}, err
	}
	if uint64(width)*uint64(height) != uint64(maskLength) {
		return boneExemplar{}, fmt.Errorf("invalid exemplar dimensions %dx%d for mask length %d", width, height, maskLength)
	}
	mask := make([]uint8, maskLength)
	if _, err := io.ReadFull(reader, mask); err != nil {
		return boneExemplar{}, err
	}
	return boneExemplar{
		hash:   hash,
		width:  width,
		height: height,
		mask:   mask,
	}, nil
}
