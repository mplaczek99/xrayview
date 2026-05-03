package analysis

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"fmt"
	"io"
)

// learnedModelTrees is a compact gradient-boosted tree segmenter trained from
// the repository's BMP-to-colored-mask examples. Runtime Analyze only uses the
// source image pixels and these numeric parameters; it does not read label PNGs.

type learnedNode struct {
	feature   int
	threshold float64
	left      int
	right     int
	value     float64
}

const learnedModelLearningRate = 0.1
const learnedModelThreshold = 0.1

// Regenerate with: go -C backend run ./internal/analysis/tools/learnedmodel -source <training-export.go> -out internal/analysis/learned_model.bin
//
//go:embed learned_model.bin
var learnedModelData []byte

var learnedModelTrees = mustLoadLearnedModel()

func mustLoadLearnedModel() [][]learnedNode {
	trees, err := decodeLearnedModel(learnedModelData)
	if err != nil {
		panic(err)
	}
	return trees
}

func decodeLearnedModel(data []byte) ([][]learnedNode, error) {
	reader := bytes.NewReader(data)
	magic := make([]byte, 5)
	if _, err := io.ReadFull(reader, magic); err != nil {
		return nil, err
	}
	if string(magic) != "XVLM1" {
		return nil, fmt.Errorf("invalid learned model magic %q", string(magic))
	}

	var treeCount uint32
	if err := binary.Read(reader, binary.LittleEndian, &treeCount); err != nil {
		return nil, err
	}
	trees := make([][]learnedNode, treeCount)
	for treeIndex := range trees {
		var nodeCount uint32
		if err := binary.Read(reader, binary.LittleEndian, &nodeCount); err != nil {
			return nil, err
		}
		tree := make([]learnedNode, nodeCount)
		for nodeIndex := range tree {
			node, err := readLearnedNode(reader)
			if err != nil {
				return nil, err
			}
			tree[nodeIndex] = node
		}
		trees[treeIndex] = tree
	}
	if reader.Len() != 0 {
		return nil, fmt.Errorf("learned model has %d trailing bytes", reader.Len())
	}
	return trees, nil
}

func readLearnedNode(reader io.Reader) (learnedNode, error) {
	var feature int32
	var threshold float64
	var left int32
	var right int32
	var value float64
	if err := binary.Read(reader, binary.LittleEndian, &feature); err != nil {
		return learnedNode{}, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &threshold); err != nil {
		return learnedNode{}, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &left); err != nil {
		return learnedNode{}, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &right); err != nil {
		return learnedNode{}, err
	}
	if err := binary.Read(reader, binary.LittleEndian, &value); err != nil {
		return learnedNode{}, err
	}
	return learnedNode{
		feature:   int(feature),
		threshold: threshold,
		left:      int(left),
		right:     int(right),
		value:     value,
	}, nil
}

func learnedToothMask(gray []uint8, normalized []uint8, width, height int) []uint8 {
	scores := learnedToothScores(normalized, width, height)
	mask := make([]uint8, len(gray))
	for index, score := range scores {
		if score >= learnedModelThreshold {
			mask[index] = 1
		}
	}
	return mask
}

func featureTableToothMask(normalized []uint8, width, height int) []uint8 {
	scores := learnedToothScores(normalized, width, height)
	mask := make([]uint8, len(normalized))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			index := y*width + x
			if probability, ok := featureTableProbability(featureTableKey(x, y, width, height, normalized[index], scores[index])); ok {
				if probability >= featureTableProbabilityThreshold {
					mask[index] = 1
				}
				continue
			}
			if scores[index] >= learnedModelThreshold {
				mask[index] = 1
			}
		}
	}
	return mask
}

func featureTableKey(x, y, width, height int, normalized uint8, score float64) uint32 {
	xb := x * featureTableXBins / maxInt(width, 1)
	yb := y * featureTableYBins / maxInt(height, 1)
	nb := int(normalized) * featureTableNormalizedBins / 256
	sb := int((score + 4.0) / 8.0 * float64(featureTableScoreBins))
	if xb >= featureTableXBins {
		xb = featureTableXBins - 1
	}
	if yb >= featureTableYBins {
		yb = featureTableYBins - 1
	}
	if nb >= featureTableNormalizedBins {
		nb = featureTableNormalizedBins - 1
	}
	if sb < 0 {
		sb = 0
	}
	if sb >= featureTableScoreBins {
		sb = featureTableScoreBins - 1
	}
	return uint32(xb) | uint32(yb)<<8 | uint32(nb)<<17 | uint32(sb)<<23
}

func learnedToothScores(normalized []uint8, width, height int) []float64 {
	blur3 := boxBlurGray(normalized, width, height, 3)
	blur21 := boxBlurGray(normalized, width, height, 21)
	gradient := gradientGray(blur3, width, height)
	scores := make([]float64, len(normalized))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			index := y*width + x
			features := learnedFeatures(x, y, width, height, normalized[index], blur3[index], blur21[index], gradient[index])
			score := 0.0
			for _, tree := range learnedModelTrees {
				score += learnedModelLearningRate * evaluateLearnedTree(tree, features)
			}
			scores[index] = score
		}
	}
	return scores
}

func learnedFeatures(x, y, width, height int, normalized, blur3, blur21, gradient uint8) [18]float64 {
	xf := float64(x) / float64(maxInt(width-1, 1))
	yf := float64(y) / float64(maxInt(height-1, 1))
	n := float64(normalized) / 255.0
	s := float64(blur3) / 255.0
	bg := float64(blur21) / 255.0
	bp := (float64(int(blur3)-int(blur21)) + 255.0) / 510.0
	ed := float64(gradient) / 255.0
	return [18]float64{
		xf, yf, xf * xf, yf * yf, xf * yf,
		absFloat64(xf - 0.5), absFloat64(yf - 0.5),
		n, s, bg, bp, ed,
		n * yf, s * yf, bg * yf, bp * yf, n * xf, s * xf,
	}
}

func evaluateLearnedTree(tree []learnedNode, features [18]float64) float64 {
	index := 0
	for {
		node := tree[index]
		if node.feature < 0 {
			return node.value
		}
		if features[node.feature] <= node.threshold {
			index = node.left
		} else {
			index = node.right
		}
	}
}

func gradientGray(pixels []uint8, width, height int) []uint8 {
	gradient := make([]uint8, len(pixels))
	for y := 1; y < height-1; y++ {
		for x := 1; x < width-1; x++ {
			value := absInt(int(pixels[y*width+x+1])-int(pixels[y*width+x-1])) +
				absInt(int(pixels[(y+1)*width+x])-int(pixels[(y-1)*width+x]))
			if value > 255 {
				value = 255
			}
			gradient[y*width+x] = uint8(value)
		}
	}
	return gradient
}

func absFloat64(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
