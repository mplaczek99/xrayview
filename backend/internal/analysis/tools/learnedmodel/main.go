package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"strconv"
)

type learnedNode struct {
	feature   int32
	threshold float64
	left      int32
	right     int32
	value     float64
}

func main() {
	source := flag.String("source", "", "Go source containing learnedModelTrees")
	output := flag.String("out", "internal/analysis/learned_model.bin", "binary model output path")
	flag.Parse()
	if *source == "" {
		fatal(errors.New("-source is required"))
	}

	trees, err := readLearnedModelTrees(*source)
	if err != nil {
		fatal(err)
	}
	if err := writeLearnedModel(*output, trees); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "learnedmodel: %v\n", err)
	os.Exit(1)
}

func readLearnedModelTrees(path string) ([][]learnedNode, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return nil, err
	}

	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, spec := range general.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, name := range value.Names {
				if name.Name != "learnedModelTrees" {
					continue
				}
				if index >= len(value.Values) {
					return nil, errors.New("learnedModelTrees has no initializer")
				}
				return parseTrees(value.Values[index])
			}
		}
	}

	return nil, errors.New("learnedModelTrees variable not found")
}

func parseTrees(expression ast.Expr) ([][]learnedNode, error) {
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		return nil, errors.New("learnedModelTrees initializer is not a composite literal")
	}
	trees := make([][]learnedNode, 0, len(literal.Elts))
	for _, treeExpression := range literal.Elts {
		treeLiteral, ok := treeExpression.(*ast.CompositeLit)
		if !ok {
			return nil, errors.New("tree entry is not a composite literal")
		}
		tree := make([]learnedNode, 0, len(treeLiteral.Elts))
		for _, nodeExpression := range treeLiteral.Elts {
			node, err := parseNode(nodeExpression)
			if err != nil {
				return nil, err
			}
			tree = append(tree, node)
		}
		trees = append(trees, tree)
	}
	return trees, nil
}

func parseNode(expression ast.Expr) (learnedNode, error) {
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		return learnedNode{}, errors.New("node entry is not a composite literal")
	}

	node := learnedNode{}
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			return learnedNode{}, errors.New("node entry must use keyed fields")
		}
		key, ok := pair.Key.(*ast.Ident)
		if !ok {
			return learnedNode{}, errors.New("node key is not an identifier")
		}
		switch key.Name {
		case "feature":
			value, err := parseInt32(pair.Value)
			if err != nil {
				return learnedNode{}, fmt.Errorf("feature: %w", err)
			}
			node.feature = value
		case "threshold":
			value, err := parseFloat64(pair.Value)
			if err != nil {
				return learnedNode{}, fmt.Errorf("threshold: %w", err)
			}
			node.threshold = value
		case "left":
			value, err := parseInt32(pair.Value)
			if err != nil {
				return learnedNode{}, fmt.Errorf("left: %w", err)
			}
			node.left = value
		case "right":
			value, err := parseInt32(pair.Value)
			if err != nil {
				return learnedNode{}, fmt.Errorf("right: %w", err)
			}
			node.right = value
		case "value":
			value, err := parseFloat64(pair.Value)
			if err != nil {
				return learnedNode{}, fmt.Errorf("value: %w", err)
			}
			node.value = value
		default:
			return learnedNode{}, fmt.Errorf("unknown node field %q", key.Name)
		}
	}
	return node, nil
}

func parseInt32(expression ast.Expr) (int32, error) {
	value, err := parseFloat64(expression)
	if err != nil {
		return 0, err
	}
	if value != math.Trunc(value) || value < math.MinInt32 || value > math.MaxInt32 {
		return 0, fmt.Errorf("%v is not an int32", value)
	}
	return int32(value), nil
}

func parseFloat64(expression ast.Expr) (float64, error) {
	switch value := expression.(type) {
	case *ast.BasicLit:
		if value.Kind != token.INT && value.Kind != token.FLOAT {
			return 0, fmt.Errorf("unsupported literal kind %s", value.Kind)
		}
		return strconv.ParseFloat(value.Value, 64)
	case *ast.UnaryExpr:
		if value.Op != token.SUB {
			return 0, fmt.Errorf("unsupported unary operator %s", value.Op)
		}
		number, err := parseFloat64(value.X)
		if err != nil {
			return 0, err
		}
		return -number, nil
	default:
		return 0, fmt.Errorf("unsupported expression %T", expression)
	}
}

func writeLearnedModel(path string, trees [][]learnedNode) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := file.Write([]byte("XVLM1")); err != nil {
		return err
	}
	if err := binary.Write(file, binary.LittleEndian, uint32(len(trees))); err != nil {
		return err
	}
	for _, tree := range trees {
		if err := binary.Write(file, binary.LittleEndian, uint32(len(tree))); err != nil {
			return err
		}
		for _, node := range tree {
			if err := binary.Write(file, binary.LittleEndian, node.feature); err != nil {
				return err
			}
			if err := binary.Write(file, binary.LittleEndian, node.threshold); err != nil {
				return err
			}
			if err := binary.Write(file, binary.LittleEndian, node.left); err != nil {
				return err
			}
			if err := binary.Write(file, binary.LittleEndian, node.right); err != nil {
				return err
			}
			if err := binary.Write(file, binary.LittleEndian, node.value); err != nil {
				return err
			}
		}
	}
	return nil
}
