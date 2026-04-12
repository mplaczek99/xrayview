//go:build unix

package dicommeta

import (
	"bytes"
	"os"
	"syscall"
)

const mmapThreshold = 1 << 20 // 1 MB

// tryMmapFile attempts to memory-map file for reading. For files smaller than
// mmapThreshold or when mmap fails, it returns (nil, nil, nil) so the caller
// falls back to reading via *os.File. On success the returned closer calls
// Munmap and file.Close; the fd must remain open for the life of the mapping.
func tryMmapFile(file *os.File, size int64) (readerAtSeeker, func() error, error) {
	if size < mmapThreshold {
		return nil, nil, nil
	}
	data, err := syscall.Mmap(int(file.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, nil, nil
	}
	r := bytes.NewReader(data)
	closer := func() error {
		munmapErr := syscall.Munmap(data)
		closeErr := file.Close()
		if munmapErr != nil {
			return munmapErr
		}
		return closeErr
	}
	return r, closer, nil
}
