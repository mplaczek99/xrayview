package analysis

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

// encodeForest mirrors the XVLM2 layout decodeToothForest reads; test-only so
// production code stays decode-only.
func encodeForest(forest *toothForest) []byte {
	buf := new(bytes.Buffer)
	buf.Write(modelMagic[:])
	_ = binary.Write(buf, binary.LittleEndian, uint32(toothFeatureCount))
	_ = binary.Write(buf, binary.LittleEndian, forest.LearningRate)
	_ = binary.Write(buf, binary.LittleEndian, forest.Bias)
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(forest.Trees)))
	for _, tree := range forest.Trees {
		_ = binary.Write(buf, binary.LittleEndian, uint32(len(tree)))
		for _, node := range tree {
			_ = binary.Write(buf, binary.LittleEndian, node.Feature)
			_ = binary.Write(buf, binary.LittleEndian, node.Threshold)
			_ = binary.Write(buf, binary.LittleEndian, node.Left)
			_ = binary.Write(buf, binary.LittleEndian, node.Right)
			_ = binary.Write(buf, binary.LittleEndian, node.Value)
		}
	}
	return buf.Bytes()
}

func TestDecodeToothForestRoundTrips(t *testing.T) {
	forest := &toothForest{
		LearningRate: 0.1,
		Bias:         0.25,
		Trees: [][]treeNode{
			{
				{Feature: 0, Threshold: 0.5, Left: 1, Right: 2, Value: 0},
				{Feature: -1, Threshold: 0, Left: -1, Right: -1, Value: 1},
				{Feature: -1, Threshold: 0, Left: -1, Right: -1, Value: -1},
			},
			{{Feature: -1, Threshold: 0, Left: -1, Right: -1, Value: 0.5}},
		},
	}
	decoded, err := decodeToothForest(encodeForest(forest))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, forest) {
		t.Fatalf("decoded = %+v, want %+v", decoded, forest)
	}
}

func TestDecodeToothForestRejectsFeatureCountMismatch(t *testing.T) {
	bytesData := encodeForest(&toothForest{
		LearningRate: 0.1,
		Trees:        [][]treeNode{{{Feature: -1, Left: -1, Right: -1}}},
	})
	// Corrupt the feature-count u32 that follows the 5-byte magic.
	bytesData[5] = byte(toothFeatureCount + 1)
	if _, err := decodeToothForest(bytesData); err == nil || !bytes.Contains([]byte(err.Error()), []byte("feature count")) {
		t.Fatalf("error = %v", err)
	}
}

func TestToothForestScoreFollowsSplits(t *testing.T) {
	// feature[0] <= 0.5 → leaf 1 (value 2.0); else leaf 2 (value -2.0).
	forest := &toothForest{
		LearningRate: 0.5,
		Bias:         1.0,
		Trees: [][]treeNode{{
			{Feature: 0, Threshold: 0.5, Left: 1, Right: 2, Value: 0},
			{Feature: -1, Left: -1, Right: -1, Value: 2},
			{Feature: -1, Left: -1, Right: -1, Value: -2},
		}},
	}
	var low [toothFeatureCount]float64
	var high [toothFeatureCount]float64
	high[0] = 1.0
	if got := forest.score(&low); got != 1.0+0.5*2.0 {
		t.Fatalf("low score = %f", got)
	}
	if got := forest.score(&high); got != 1.0+0.5*-2.0 {
		t.Fatalf("high score = %f", got)
	}
}

func TestFeaturesArePositionInvariantForUniformImage(t *testing.T) {
	const width, height = 40, 30
	normalized := make([]byte, width*height)
	for i := range normalized {
		normalized[i] = 128
	}
	planes := buildFeaturePlanes(normalized, width, height)
	a := planes.features(5, 5)
	b := planes.features(31, 22)
	if a != b {
		t.Fatalf("features differ by position: %v vs %v", a, b)
	}
}

func TestFineTextureFeatureSeparatesSpeckleFromFlat(t *testing.T) {
	const width, height = 32, 32
	flat := make([]byte, width*height)
	for i := range flat {
		flat[i] = 128
	}
	speckle := make([]byte, width*height)
	for index := range speckle {
		x, y := index%width, index/width
		if (x+y)%2 == 0 {
			speckle[index] = 64
		} else {
			speckle[index] = 192
		}
	}
	// Feature index 5 is s2 (fine-scale local std).
	flatS2 := buildFeaturePlanes(flat, width, height).features(16, 16)[5]
	speckleS2 := buildFeaturePlanes(speckle, width, height).features(16, 16)[5]
	if speckleS2 <= flatS2+0.1 {
		t.Fatalf("speckle s2 %f should exceed flat s2 %f", speckleS2, flatS2)
	}
}

func TestToothForestModelLoads(t *testing.T) {
	forest, ok := loadedToothModel()
	if !ok {
		t.Fatalf("tooth forest should load: %v", toothModelErr)
	}
	if len(forest.Trees) == 0 {
		t.Fatal("tooth forest has no trees")
	}
	for i, tree := range forest.Trees {
		if len(tree) == 0 {
			t.Fatalf("tooth forest tree %d is empty", i)
		}
	}
}

func TestBoneForestModelLoads(t *testing.T) {
	forest, ok := loadedBoneModel()
	if !ok {
		t.Fatalf("bone forest should load: %v", boneModelErr)
	}
	if len(forest.Trees) == 0 {
		t.Fatal("bone forest has no trees")
	}
	for i, tree := range forest.Trees {
		if len(tree) == 0 {
			t.Fatalf("bone forest tree %d is empty", i)
		}
	}
}
