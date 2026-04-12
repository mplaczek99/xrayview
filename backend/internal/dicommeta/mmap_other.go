//go:build !unix

package dicommeta

import "os"

// tryMmapFile is not supported on this platform; always returns (nil, nil, nil)
// so the caller falls back to reading via *os.File.
func tryMmapFile(_ *os.File, _ int64) (readerAtSeeker, func() error, error) {
	return nil, nil, nil
}
