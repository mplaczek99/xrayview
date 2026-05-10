package processing

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"xrayview/backend/internal/bufpool"
	"xrayview/backend/internal/imaging"
)

const (
	minParallelComparePixels  = 64 * 1024
	maxParallelCompareWorkers = 8
)

var compareGrayLookup = buildCompareGrayLookup()

func CombineComparison(
	left imaging.PreviewImage,
	right imaging.PreviewImage,
) (imaging.PreviewImage, error) {
	if err := left.Validate(); err != nil {
		return imaging.PreviewImage{}, fmt.Errorf("validate left preview image: %w", err)
	}
	if err := right.Validate(); err != nil {
		return imaging.PreviewImage{}, fmt.Errorf("validate right preview image: %w", err)
	}
	if left.Format != imaging.FormatGray8 {
		return imaging.PreviewImage{}, fmt.Errorf(
			"compare preview requires %q source on the left side",
			imaging.FormatGray8,
		)
	}
	if left.Width != right.Width || left.Height != right.Height {
		return imaging.PreviewImage{}, fmt.Errorf("compare preview requires matching image dimensions")
	}
	// Guard so combinedWidth = left.Width * 2 below doesn't wrap past
	// uint32. Rare in practice, but a "cleanup" pass might be tempted
	// to strip this — don't.
	if left.Width > ^uint32(0)/2 {
		return imaging.PreviewImage{}, fmt.Errorf("compare preview width overflow")
	}

	width := int(left.Width)
	height := int(left.Height)
	combinedWidth := left.Width * 2
	pixels := bufpool.GetUint8(int(uint64(combinedWidth) * uint64(left.Height) * 4))

	switch right.Format {
	case imaging.FormatGray8:
		combineComparisonGrayRows(left.Pixels, right.Pixels, pixels, width, int(combinedWidth), height)
	case imaging.FormatRGBA8:
		combineComparisonRGBARows(left.Pixels, right.Pixels, pixels, width, int(combinedWidth), height)
	default:
		bufpool.PutUint8(pixels)
		return imaging.PreviewImage{}, fmt.Errorf("unsupported preview image format %q", right.Format)
	}

	return imaging.RGBAPreviewWithRelease(combinedWidth, left.Height, pixels, bufpool.PutUint8), nil
}

func combineComparisonGrayRows(
	leftPixels []uint8,
	rightPixels []uint8,
	pixels []uint8,
	width int,
	combinedWidth int,
	height int,
) {
	workers := compareWorkerCount(width, height)
	if workers <= 1 {
		combineComparisonGrayRowRange(leftPixels, rightPixels, pixels, width, combinedWidth, 0, height)
		return
	}

	rowsPerWorker := (height + workers - 1) / workers
	var wg sync.WaitGroup
	for startY := 0; startY < height; startY += rowsPerWorker {
		endY := startY + rowsPerWorker
		if endY > height {
			endY = height
		}
		wg.Add(1)
		go func(startY, endY int, leftPixels, rightPixels, pixels []uint8) {
			defer wg.Done()
			combineComparisonGrayRowRange(leftPixels, rightPixels, pixels, width, combinedWidth, startY, endY)
		}(startY, endY, leftPixels, rightPixels, pixels)
	}
	wg.Wait()
}

func combineComparisonRGBARows(
	leftPixels []uint8,
	rightPixels []uint8,
	pixels []uint8,
	width int,
	combinedWidth int,
	height int,
) {
	workers := compareWorkerCount(width, height)
	if workers <= 1 {
		combineComparisonRGBARowRange(leftPixels, rightPixels, pixels, width, combinedWidth, 0, height)
		return
	}

	rowsPerWorker := (height + workers - 1) / workers
	var wg sync.WaitGroup
	for startY := 0; startY < height; startY += rowsPerWorker {
		endY := startY + rowsPerWorker
		if endY > height {
			endY = height
		}
		wg.Add(1)
		go func(startY, endY int, leftPixels, rightPixels, pixels []uint8) {
			defer wg.Done()
			combineComparisonRGBARowRange(leftPixels, rightPixels, pixels, width, combinedWidth, startY, endY)
		}(startY, endY, leftPixels, rightPixels, pixels)
	}
	wg.Wait()
}

func combineComparisonGrayRowRange(
	leftPixels []uint8,
	rightPixels []uint8,
	pixels []uint8,
	width int,
	combinedWidth int,
	startY int,
	endY int,
) {
	for row := startY; row < endY; row++ {
		dstRow := comparisonOutputRow(pixels, combinedWidth, row)
		leftRow := leftPixels[row*width : (row+1)*width]
		expandGrayRowToRGBA(dstRow[:width*4], leftRow)

		rightRow := rightPixels[row*width : (row+1)*width]
		expandGrayRowToRGBA(dstRow[width*4:width*8], rightRow)
	}
}

func combineComparisonRGBARowRange(
	leftPixels []uint8,
	rightPixels []uint8,
	pixels []uint8,
	width int,
	combinedWidth int,
	startY int,
	endY int,
) {
	for row := startY; row < endY; row++ {
		dstRow := comparisonOutputRow(pixels, combinedWidth, row)
		leftRow := leftPixels[row*width : (row+1)*width]
		expandGrayRowToRGBA(dstRow[:width*4], leftRow)

		srcRowStart := row * width * 4
		srcRow := rightPixels[srcRowStart : srcRowStart+width*4]
		copy(dstRow[width*4:width*8], srcRow)
	}
}

func buildCompareGrayLookup() [256]uint32 {
	var lookup [256]uint32
	for index := range lookup {
		value := uint8(index)
		lookup[index] = rgbaUint32([4]uint8{value, value, value, 255})
	}

	return lookup
}

func comparisonOutputRow(pixels []uint8, width, row int) []uint8 {
	rowStart := row * width * 4
	return pixels[rowStart : rowStart+width*4]
}

func expandGrayRowToRGBA(dst []uint8, src []uint8) {
	if len(src) == 0 {
		return
	}

	rgba := unsafe.Slice((*uint32)(unsafe.Pointer(&dst[0])), len(src))
	for index, value := range src {
		rgba[index] = compareGrayLookup[value]
	}
}

func compareWorkerCount(width, height int) int {
	if uint64(width)*uint64(height) < minParallelComparePixels {
		return 1
	}

	workers := runtime.NumCPU()
	if workers > maxParallelCompareWorkers {
		workers = maxParallelCompareWorkers
	}
	if workers > height {
		workers = height
	}
	if workers <= 1 {
		return 1
	}

	return workers
}
