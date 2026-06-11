package analysis

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Shared tooth/bone-segmentation model: feature extraction + the gradient-boosted
// forest. Ported from the Rust tooth_model: BOTH detectors (tooth via
// learned_model, bone via bone_model) score the same position-free texture
// features, so a tooth/bone is classified the same wherever it sits in the frame
// — the property the old absolute-(x,y) models lacked.
//
// All window statistics come from two integral images (sum and sum-of-squares)
// so a mean/variance at any radius is an O(1) lookup.

// windowRadii are the pixel radii for the multi-scale neighbourhood statistics.
// r2/r4 catch trabecular speckle (bone); r16/r32 approximate the local
// background and the coarse texture where teeth stay smooth but bone does not.
var windowRadii = [5]int{2, 4, 8, 16, 32}

// toothFeatureCount is the number of per-pixel features the forest consumes.
// Locked into the model header so a mismatched asset is rejected at load.
const toothFeatureCount = 16

// featureEpsilon avoids divide-by-zero in the normalized-contrast features.
const featureEpsilon = 1.0 / 255.0

var modelMagic = [5]byte{'X', 'V', 'L', 'M', '2'}

// treeNode is one regression-tree node. A negative Feature marks a leaf whose
// prediction is Value; otherwise Left/Right index sibling nodes and the split
// sends features[Feature] <= Threshold left.
type treeNode struct {
	Feature   int32
	Threshold float64
	Left      int32
	Right     int32
	Value     float64
}

// toothForest is a gradient-boosted forest. Prediction is
// bias + learningRate * Σ tree.
type toothForest struct {
	LearningRate float64
	Bias         float64
	Trees        [][]treeNode
}

// score returns the forest output for one feature vector (a probability-like
// score in roughly [0, 1]; threshold at ~0.5 for a hard mask).
func (forest *toothForest) score(features *[toothFeatureCount]float64) float64 {
	sum := forest.Bias
	for i := range forest.Trees {
		sum += forest.LearningRate * evalForestTree(forest.Trees[i], features)
	}
	return sum
}

// decodeToothForest parses and validates an XVLM2 forest asset. It rejects a
// wrong magic, a feature-count mismatch, non-finite numbers, out-of-range child
// indices, and cycles, so a corrupt model is caught at load rather than
// producing garbage masks.
func decodeToothForest(data []byte) (*toothForest, error) {
	reader := &leReader{data: data}
	magic, err := reader.bytes(5)
	if err != nil {
		return nil, fmt.Errorf("read tooth model magic: %w", err)
	}
	if [5]byte{magic[0], magic[1], magic[2], magic[3], magic[4]} != modelMagic {
		return nil, fmt.Errorf("invalid tooth model magic %q", string(magic))
	}
	featureCount, err := reader.u32()
	if err != nil {
		return nil, err
	}
	if int(featureCount) != toothFeatureCount {
		return nil, fmt.Errorf("tooth model feature count %d != expected %d", featureCount, toothFeatureCount)
	}
	learningRate, err := reader.f64()
	if err != nil {
		return nil, err
	}
	bias, err := reader.f64()
	if err != nil {
		return nil, err
	}
	if !finiteFloat(learningRate) || !finiteFloat(bias) {
		return nil, fmt.Errorf("tooth model has non-finite learning rate or bias")
	}

	treeCount, err := reader.u32()
	if err != nil {
		return nil, err
	}
	trees := make([][]treeNode, 0, treeCount)
	for range treeCount {
		nodeCount, err := reader.u32()
		if err != nil {
			return nil, err
		}
		tree := make([]treeNode, 0, nodeCount)
		for range nodeCount {
			feature, err := reader.i32()
			if err != nil {
				return nil, err
			}
			threshold, err := reader.f64()
			if err != nil {
				return nil, err
			}
			left, err := reader.i32()
			if err != nil {
				return nil, err
			}
			right, err := reader.i32()
			if err != nil {
				return nil, err
			}
			value, err := reader.f64()
			if err != nil {
				return nil, err
			}
			tree = append(tree, treeNode{Feature: feature, Threshold: threshold, Left: left, Right: right, Value: value})
		}
		if err := validateForestTree(len(trees), tree); err != nil {
			return nil, err
		}
		trees = append(trees, tree)
	}
	if reader.pos != len(data) {
		return nil, fmt.Errorf("tooth model has %d trailing bytes", len(data)-reader.pos)
	}
	return &toothForest{LearningRate: learningRate, Bias: bias, Trees: trees}, nil
}

func evalForestTree(tree []treeNode, features *[toothFeatureCount]float64) float64 {
	index := 0
	// Bounded by node count: a validated tree is acyclic, so this terminates at
	// a leaf well within len(tree) steps.
	for range len(tree) + 1 {
		if index < 0 || index >= len(tree) {
			return 0
		}
		node := tree[index]
		if node.Feature < 0 {
			return node.Value
		}
		if int(node.Feature) >= len(features) {
			return 0
		}
		if features[node.Feature] <= node.Threshold {
			index = int(node.Left)
		} else {
			index = int(node.Right)
		}
	}
	return 0
}

func validateForestTree(treeIndex int, tree []treeNode) error {
	if len(tree) == 0 {
		return fmt.Errorf("tooth model tree %d is empty", treeIndex)
	}
	for nodeIndex, node := range tree {
		if !finiteFloat(node.Threshold) || !finiteFloat(node.Value) {
			return fmt.Errorf("tooth model tree %d node %d has non-finite field", treeIndex, nodeIndex)
		}
		if node.Feature < 0 {
			continue
		}
		if int(node.Feature) >= toothFeatureCount {
			return fmt.Errorf("tooth model tree %d node %d references feature %d", treeIndex, nodeIndex, node.Feature)
		}
		for _, child := range [2]int32{node.Left, node.Right} {
			if child < 0 || int(child) >= len(tree) {
				return fmt.Errorf("tooth model tree %d node %d has invalid child %d", treeIndex, nodeIndex, child)
			}
		}
	}
	visiting := make([]bool, len(tree))
	visited := make([]bool, len(tree))
	return validateForestNode(treeIndex, tree, 0, visiting, visited)
}

func validateForestNode(treeIndex int, tree []treeNode, index int, visiting, visited []bool) error {
	if visited[index] {
		return nil
	}
	if visiting[index] {
		return fmt.Errorf("tooth model tree %d contains a cycle at node %d", treeIndex, index)
	}
	visiting[index] = true
	node := tree[index]
	if node.Feature >= 0 {
		if err := validateForestNode(treeIndex, tree, int(node.Left), visiting, visited); err != nil {
			return err
		}
		if err := validateForestNode(treeIndex, tree, int(node.Right), visiting, visited); err != nil {
			return err
		}
	}
	visiting[index] = false
	visited[index] = true
	return nil
}

// featurePlanes is the per-image precomputation that makes per-pixel feature
// extraction O(1): integral images of the normalized intensity and its square,
// plus a gradient plane. Build once per image, then call features per pixel.
type featurePlanes struct {
	width      int
	height     int
	normalized []byte
	sum        []uint64
	sumSq      []uint64
	gradient   []byte
}

// buildFeaturePlanes builds the integral images and gradient plane from a
// normalized (contrast-stretched) grayscale buffer.
func buildFeaturePlanes(normalized []byte, width, height int) *featurePlanes {
	stride := width + 1
	sum := make([]uint64, stride*(height+1))
	sumSq := make([]uint64, stride*(height+1))
	for y := 0; y < height; y++ {
		var rowSum, rowSumSq uint64
		for x := 0; x < width; x++ {
			value := uint64(normalized[y*width+x])
			rowSum += value
			rowSumSq += value * value
			here := (y+1)*stride + (x + 1)
			above := y*stride + (x + 1)
			sum[here] = sum[above] + rowSum
			sumSq[here] = sumSq[above] + rowSumSq
		}
	}
	return &featurePlanes{
		width:      width,
		height:     height,
		normalized: append([]byte(nil), normalized...),
		sum:        sum,
		sumSq:      sumSq,
		gradient:   gradientGray(normalized, width, height),
	}
}

// windowMeanStd is the mean and standard deviation of the (clamped) window of
// radius r around (x, y), both in raw 0..255 units. Integral-image lookup → O(1).
func (planes *featurePlanes) windowMeanStd(x, y, r int) (float64, float64) {
	stride := planes.width + 1
	x0 := x - r
	if x0 < 0 {
		x0 = 0
	}
	y0 := y - r
	if y0 < 0 {
		y0 = 0
	}
	x1 := x + r + 1
	if x1 > planes.width {
		x1 = planes.width
	}
	y1 := y + r + 1
	if y1 > planes.height {
		y1 = planes.height
	}
	area := float64((x1 - x0) * (y1 - y0))
	region := func(table []uint64) float64 {
		a := table[y1*stride+x1]
		b := table[y0*stride+x1]
		c := table[y1*stride+x0]
		d := table[y0*stride+x0]
		return float64(a + d - b - c)
	}
	mean := region(planes.sum) / area
	variance := region(planes.sumSq)/area - mean*mean
	if variance < 0 {
		variance = 0
	}
	return mean, math.Sqrt(variance)
}

// features returns the 16-feature vector for pixel (x, y). Position-free by
// construction — only neighbourhood statistics enter, never x or y themselves.
func (planes *featurePlanes) features(x, y int) [toothFeatureCount]float64 {
	n := float64(planes.normalized[y*planes.width+x]) / 255.0
	_, s2 := planes.windowMeanStd(x, y, windowRadii[0])
	m4, s4 := planes.windowMeanStd(x, y, windowRadii[1])
	m8, s8 := planes.windowMeanStd(x, y, windowRadii[2])
	m16, s16 := planes.windowMeanStd(x, y, windowRadii[3])
	m32, s32 := planes.windowMeanStd(x, y, windowRadii[4])
	grad := float64(planes.gradient[y*planes.width+x]) / 255.0
	m4, m8, m16, m32 = m4/255.0, m8/255.0, m16/255.0, m32/255.0
	s2, s4, s8, s16, s32 = s2/255.0, s4/255.0, s8/255.0, s16/255.0, s32/255.0
	return [toothFeatureCount]float64{
		n,                                 // raw brightness
		m4,                                // local mean (fine)
		m8,                                // local mean (mid)
		m16,                               // local mean (background)
		m32,                               // local mean (broad background)
		s2,                                // fine texture — high on trabecular bone
		s4,                                // texture
		s8,                                // texture (coarser)
		s16,                               // coarse texture
		s32,                               // broad texture — teeth stay smooth here
		grad,                              // edge response
		n - m32,                           // local contrast vs broad background
		m4 - m32,                          // band-pass: fine vs coarse mean
		s2 - s16,                          // texture-scale contrast (speckle vs smooth)
		(n - m16) / (s16 + featureEpsilon), // z-scored contrast (exposure-invariant)
		s16 / (m16 + featureEpsilon),       // coefficient of variation
	}
}

func finiteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// leReader is a minimal little-endian cursor over a byte slice.
type leReader struct {
	data []byte
	pos  int
}

func (reader *leReader) bytes(n int) ([]byte, error) {
	if reader.pos+n > len(reader.data) {
		return nil, fmt.Errorf("unexpected end of tooth model data")
	}
	out := reader.data[reader.pos : reader.pos+n]
	reader.pos += n
	return out, nil
}

func (reader *leReader) u32() (uint32, error) {
	b, err := reader.bytes(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

func (reader *leReader) i32() (int32, error) {
	value, err := reader.u32()
	return int32(value), err
}

func (reader *leReader) f64() (float64, error) {
	b, err := reader.bytes(8)
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(b)), nil
}
