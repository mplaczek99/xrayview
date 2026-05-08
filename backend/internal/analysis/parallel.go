package analysis

import (
	"runtime"
	"sync"
)

const minParallelPixels = 64 * 1024

func parallelRows(width, height int, work func(startY, endY int)) {
	if height <= 0 {
		return
	}
	if width*height < minParallelPixels {
		work(0, height)
		return
	}

	workers := runtime.NumCPU()
	if workers > height {
		workers = height
	}
	if workers <= 1 {
		work(0, height)
		return
	}

	rowsPerWorker := (height + workers - 1) / workers
	var wg sync.WaitGroup
	for startY := 0; startY < height; startY += rowsPerWorker {
		endY := minInt(startY+rowsPerWorker, height)
		wg.Add(1)
		go func(startY, endY int) {
			defer wg.Done()
			work(startY, endY)
		}(startY, endY)
	}
	wg.Wait()
}
