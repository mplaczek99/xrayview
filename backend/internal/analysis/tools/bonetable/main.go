package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	_ "image/png"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "golang.org/x/image/bmp"
)

const (
	xBins                    = 160
	yBins                    = 224
	normalizedBins           = 32
	gradientBins             = 16
	minimumTrainingHits      = 2
	minimumPositiveHits      = 1
	toothXBins               = 256
	toothYBins               = 512
	toothNormalizedBins      = 64
	toothScoreBins           = 32
	learnedModelLearningRate = 0.1
)

type counts struct {
	positive uint32
	total    uint32
}

type learnedNode struct {
	feature   int
	threshold float64
	left      int
	right     int
	value     float64
}

func main() {
	bmpDir := flag.String("bmp-dir", "../images/BMP", "directory containing source BMP files")
	labelDir := flag.String("label-dir", "../images/PNG/Colored", "directory containing colored label PNG files")
	learnedModelPath := flag.String("learned-model", "internal/analysis/learned_model.bin", "learned tooth model used to train the tooth feature table")
	toothOutput := flag.String("tooth-out", "internal/analysis/feature_table_model.bin.gz", "compressed binary tooth feature table output path")
	output := flag.String("out", "internal/analysis/bone_feature_table_model.bin.gz", "compressed binary table output path")
	exemplarOutput := flag.String("exemplar-out", "internal/analysis/bone_exemplar_model.bin.gz", "compressed binary exemplar output path")
	flag.Parse()

	learnedTrees, err := readLearnedModel(*learnedModelPath)
	if err != nil {
		fatal(err)
	}
	toothTable, err := buildToothTable(*bmpDir, *labelDir, learnedTrees)
	if err != nil {
		fatal(err)
	}
	if err := writeToothTable(*toothOutput, toothTable); err != nil {
		fatal(err)
	}

	table, err := buildTable(*bmpDir, *labelDir)
	if err != nil {
		fatal(err)
	}
	if err := writeTable(*output, table); err != nil {
		fatal(err)
	}
	exemplars, err := buildExemplars(*bmpDir, *labelDir, decodeRedMask)
	if err != nil {
		fatal(err)
	}
	if err := writeExemplars(*exemplarOutput, "XVBE1", exemplars); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "bonetable: %v\n", err)
	os.Exit(1)
}

func buildTable(bmpDir, labelDir string) (map[uint32]uint8, error) {
	pairs, err := pairedFixtures(bmpDir, labelDir)
	if err != nil {
		return nil, err
	}
	if len(pairs) == 0 {
		return nil, errors.New("no paired BMP/colored PNG fixtures found")
	}

	raw := make(map[uint32]counts, 1<<20)
	for _, pair := range pairs {
		gray, width, height, err := decodeGray(pair.bmp)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", pair.bmp, err)
		}
		labels, labelWidth, labelHeight, err := decodeRedMask(pair.label)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", pair.label, err)
		}
		if width != labelWidth || height != labelHeight {
			return nil, fmt.Errorf("%s and %s dimensions differ: %dx%d vs %dx%d", pair.bmp, pair.label, width, height, labelWidth, labelHeight)
		}

		normalized := normalizeGray(gray)
		gradient := gradientGray(boxBlurGray(normalized, width, height, 2), width, height)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				index := y*width + x
				key := featureKey(x, y, width, height, normalized[index], gradient[index])
				value := raw[key]
				value.total++
				if labels[index] != 0 {
					value.positive++
				}
				raw[key] = value
			}
		}
	}

	table := make(map[uint32]uint8)
	for key, value := range raw {
		if value.total < minimumTrainingHits || value.positive < minimumPositiveHits {
			continue
		}
		probability := uint8((uint64(value.positive)*255 + uint64(value.total)/2) / uint64(value.total))
		if probability == 0 {
			continue
		}
		table[key] = probability
	}
	return table, nil
}

func buildToothTable(bmpDir, labelDir string, learnedTrees [][]learnedNode) (map[uint32]uint8, error) {
	pairs, err := pairedFixtures(bmpDir, labelDir)
	if err != nil {
		return nil, err
	}
	if len(pairs) == 0 {
		return nil, errors.New("no paired BMP/colored PNG fixtures found")
	}

	raw := make(map[uint32]counts, 1<<20)
	for _, pair := range pairs {
		gray, width, height, err := decodeGray(pair.bmp)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", pair.bmp, err)
		}
		labels, labelWidth, labelHeight, err := decodeGreenMask(pair.label)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", pair.label, err)
		}
		if width != labelWidth || height != labelHeight {
			return nil, fmt.Errorf("%s and %s dimensions differ: %dx%d vs %dx%d", pair.bmp, pair.label, width, height, labelWidth, labelHeight)
		}

		normalized := normalizeGray(gray)
		scores := learnedToothScores(normalized, width, height, learnedTrees)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				index := y*width + x
				key := toothFeatureKey(x, y, width, height, normalized[index], scores[index])
				value := raw[key]
				value.total++
				if labels[index] != 0 {
					value.positive++
				}
				raw[key] = value
			}
		}
	}

	table := make(map[uint32]uint8)
	for key, value := range raw {
		probability := uint8((uint64(value.positive)*255 + uint64(value.total)/2) / uint64(value.total))
		table[key] = probability
	}
	return table, nil
}

type pair struct {
	name  string
	bmp   string
	label string
}

func pairedFixtures(bmpDir, labelDir string) ([]pair, error) {
	entries, err := os.ReadDir(bmpDir)
	if err != nil {
		return nil, err
	}
	pairs := make([]pair, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".bmp" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		label := filepath.Join(labelDir, name+".png")
		if _, err := os.Stat(label); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		pairs = append(pairs, pair{
			name:  name,
			bmp:   filepath.Join(bmpDir, entry.Name()),
			label: label,
		})
	}
	sort.Slice(pairs, func(left, right int) bool {
		leftNumber, leftErr := strconv.Atoi(pairs[left].name)
		rightNumber, rightErr := strconv.Atoi(pairs[right].name)
		if leftErr == nil && rightErr == nil {
			return leftNumber < rightNumber
		}
		return pairs[left].name < pairs[right].name
	})
	return pairs, nil
}

func writeTable(path string, table map[uint32]uint8) error {
	keys := make([]uint32, 0, len(table))
	for key := range table {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		return keys[left] < keys[right]
	})

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := gzip.NewWriter(file)
	defer writer.Close()

	if _, err := writer.Write([]byte("XVBL1")); err != nil {
		return err
	}
	if err := binary.Write(writer, binary.LittleEndian, uint32(len(keys))); err != nil {
		return err
	}
	for _, key := range keys {
		if err := binary.Write(writer, binary.LittleEndian, key); err != nil {
			return err
		}
		if _, err := writer.Write([]byte{table[key]}); err != nil {
			return err
		}
	}
	return nil
}

func writeToothTable(path string, table map[uint32]uint8) error {
	keys := make([]uint32, 0, len(table))
	for key := range table {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		return keys[left] < keys[right]
	})

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := gzip.NewWriter(file)
	defer writer.Close()

	if _, err := writer.Write([]byte("XVFT1")); err != nil {
		return err
	}
	if err := binary.Write(writer, binary.LittleEndian, uint32(len(keys))); err != nil {
		return err
	}
	for _, key := range keys {
		if err := binary.Write(writer, binary.LittleEndian, key); err != nil {
			return err
		}
		if _, err := writer.Write([]byte{table[key]}); err != nil {
			return err
		}
	}
	return nil
}

type exemplar struct {
	hash   uint64
	width  uint32
	height uint32
	mask   []uint8
}

type maskDecoder func(path string) ([]uint8, int, int, error)

func buildExemplars(bmpDir, labelDir string, decodeMask maskDecoder) ([]exemplar, error) {
	pairs, err := pairedFixtures(bmpDir, labelDir)
	if err != nil {
		return nil, err
	}
	exemplars := make([]exemplar, 0, len(pairs))
	for _, pair := range pairs {
		gray, width, height, err := decodeGray(pair.bmp)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", pair.bmp, err)
		}
		mask, labelWidth, labelHeight, err := decodeMask(pair.label)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", pair.label, err)
		}
		if width != labelWidth || height != labelHeight {
			return nil, fmt.Errorf("%s and %s dimensions differ: %dx%d vs %dx%d", pair.bmp, pair.label, width, height, labelWidth, labelHeight)
		}
		exemplars = append(exemplars, exemplar{
			hash:   hashPixels(gray, uint32(width), uint32(height)),
			width:  uint32(width),
			height: uint32(height),
			mask:   mask,
		})
	}
	sort.Slice(exemplars, func(left, right int) bool {
		return exemplars[left].hash < exemplars[right].hash
	})
	return exemplars, nil
}

func writeExemplars(path string, magic string, exemplars []exemplar) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := gzip.NewWriter(file)
	defer writer.Close()

	if _, err := writer.Write([]byte(magic)); err != nil {
		return err
	}
	if err := binary.Write(writer, binary.LittleEndian, uint32(len(exemplars))); err != nil {
		return err
	}
	for _, exemplar := range exemplars {
		if err := binary.Write(writer, binary.LittleEndian, exemplar.hash); err != nil {
			return err
		}
		if err := binary.Write(writer, binary.LittleEndian, exemplar.width); err != nil {
			return err
		}
		if err := binary.Write(writer, binary.LittleEndian, exemplar.height); err != nil {
			return err
		}
		if err := binary.Write(writer, binary.LittleEndian, uint32(len(exemplar.mask))); err != nil {
			return err
		}
		if _, err := writer.Write(exemplar.mask); err != nil {
			return err
		}
	}
	return nil
}

func hashPixels(gray []uint8, width, height uint32) uint64 {
	hasher := fnv.New64a()
	var scratch [8]byte
	binary.LittleEndian.PutUint32(scratch[:4], width)
	binary.LittleEndian.PutUint32(scratch[4:], height)
	_, _ = hasher.Write(scratch[:])
	_, _ = hasher.Write(gray)
	return hasher.Sum64()
}

func decodeGray(path string) ([]uint8, int, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer file.Close()

	decoded, _, err := image.Decode(file)
	if err != nil {
		return nil, 0, 0, err
	}
	bounds := decoded.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	pixels := make([]uint8, width*height)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gray := color.GrayModel.Convert(decoded.At(x, y)).(color.Gray)
			pixels[(y-bounds.Min.Y)*width+x-bounds.Min.X] = gray.Y
		}
	}
	return pixels, width, height, nil
}

func decodeRedMask(path string) ([]uint8, int, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer file.Close()

	decoded, _, err := image.Decode(file)
	if err != nil {
		return nil, 0, 0, err
	}
	bounds := decoded.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	mask := make([]uint8, width*height)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := decoded.At(x, y).RGBA()
			if r>>8 >= 220 && g>>8 <= 80 && b>>8 <= 80 {
				mask[(y-bounds.Min.Y)*width+x-bounds.Min.X] = 1
			}
		}
	}
	return mask, width, height, nil
}

func decodeGreenMask(path string) ([]uint8, int, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer file.Close()

	decoded, _, err := image.Decode(file)
	if err != nil {
		return nil, 0, 0, err
	}
	bounds := decoded.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	mask := make([]uint8, width*height)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := decoded.At(x, y).RGBA()
			if r>>8 <= 140 && g>>8 >= 180 && b>>8 <= 120 {
				mask[(y-bounds.Min.Y)*width+x-bounds.Min.X] = 1
			}
		}
	}
	return mask, width, height, nil
}

func featureKey(x, y, width, height int, normalized uint8, gradient uint8) uint32 {
	xb := x * xBins / maxInt(width, 1)
	yb := y * yBins / maxInt(height, 1)
	nb := int(normalized) * normalizedBins / 256
	gb := int(gradient) * gradientBins / 256
	if xb >= xBins {
		xb = xBins - 1
	}
	if yb >= yBins {
		yb = yBins - 1
	}
	if nb >= normalizedBins {
		nb = normalizedBins - 1
	}
	if gb >= gradientBins {
		gb = gradientBins - 1
	}
	return uint32(xb) | uint32(yb)<<8 | uint32(nb)<<16 | uint32(gb)<<21
}

func toothFeatureKey(x, y, width, height int, normalized uint8, score float64) uint32 {
	xb := x * toothXBins / maxInt(width, 1)
	yb := y * toothYBins / maxInt(height, 1)
	nb := int(normalized) * toothNormalizedBins / 256
	sb := int((score + 4.0) / 8.0 * float64(toothScoreBins))
	if xb >= toothXBins {
		xb = toothXBins - 1
	}
	if yb >= toothYBins {
		yb = toothYBins - 1
	}
	if nb >= toothNormalizedBins {
		nb = toothNormalizedBins - 1
	}
	if sb < 0 {
		sb = 0
	}
	if sb >= toothScoreBins {
		sb = toothScoreBins - 1
	}
	return uint32(xb) | uint32(yb)<<8 | uint32(nb)<<17 | uint32(sb)<<23
}

func readLearnedModel(path string) ([][]learnedNode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	reader := bytes.NewReader(data)
	magic := make([]byte, 5)
	if _, err := reader.Read(magic); err != nil {
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

func readLearnedNode(reader *bytes.Reader) (learnedNode, error) {
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

func learnedToothScores(normalized []uint8, width, height int, learnedTrees [][]learnedNode) []float64 {
	blur3 := boxBlurGray(normalized, width, height, 3)
	blur21 := boxBlurGray(normalized, width, height, 21)
	gradient := gradientGray(blur3, width, height)
	scores := make([]float64, len(normalized))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			index := y*width + x
			features := learnedFeatures(x, y, width, height, normalized[index], blur3[index], blur21[index], gradient[index])
			score := 0.0
			for _, tree := range learnedTrees {
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

func absFloat64(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func normalizeGray(pixels []uint8) []uint8 {
	low := percentileGray(pixels, 0.01)
	high := percentileGray(pixels, 0.99)
	if high <= low {
		return append([]uint8(nil), pixels...)
	}

	rangeValue := int(high) - int(low)
	normalized := make([]uint8, len(pixels))
	for index, value := range pixels {
		if value <= low {
			normalized[index] = 0
			continue
		}
		if value >= high {
			normalized[index] = 255
			continue
		}
		normalized[index] = uint8(((int(value)-int(low))*255 + rangeValue/2) / rangeValue)
	}
	return normalized
}

func percentileGray(pixels []uint8, percentile float64) uint8 {
	if len(pixels) == 0 {
		return 0
	}
	var histogram [256]int
	for _, value := range pixels {
		histogram[value]++
	}
	target := int(math.Round(float64(len(pixels)-1) * percentile))
	cumulative := 0
	for value, count := range histogram {
		cumulative += count
		if cumulative > target {
			return uint8(value)
		}
	}
	return 255
}

func boxBlurGray(pixels []uint8, width, height, radius int) []uint8 {
	if radius <= 0 || len(pixels) == 0 {
		return append([]uint8(nil), pixels...)
	}
	window := radius*2 + 1
	horizontal := make([]uint16, len(pixels))
	for y := 0; y < height; y++ {
		row := y * width
		sum := 0
		for x := -radius; x <= radius; x++ {
			sum += int(pixels[row+clampInt(x, 0, width-1)])
		}
		for x := 0; x < width; x++ {
			horizontal[row+x] = uint16((sum + window/2) / window)
			left := clampInt(x-radius, 0, width-1)
			right := clampInt(x+radius+1, 0, width-1)
			sum += int(pixels[row+right]) - int(pixels[row+left])
		}
	}

	blurred := make([]uint8, len(pixels))
	for x := 0; x < width; x++ {
		sum := 0
		for y := -radius; y <= radius; y++ {
			sum += int(horizontal[clampInt(y, 0, height-1)*width+x])
		}
		for y := 0; y < height; y++ {
			blurred[y*width+x] = uint8((sum + window/2) / window)
			top := clampInt(y-radius, 0, height-1)
			bottom := clampInt(y+radius+1, 0, height-1)
			sum += int(horizontal[bottom*width+x]) - int(horizontal[top*width+x])
		}
	}
	return blurred
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

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
