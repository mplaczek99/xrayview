package bufpool

// Small bounded pools for the hot pixel-buffer paths (render, process).
// GetUint8 always returns a slice of exactly length n; if the pool hands back
// a buffer with cap >= n we reslice, otherwise we allocate. PutUint8 hands
// capacity back; don't touch the slice after calling it.

const maxPooledBuffers = 64

var uint8Pool = make(chan []uint8, maxPooledBuffers)

// GetUint8 returns a []uint8 of length n from the pool, or allocates a new
// one if no pooled buffer has enough capacity.
func GetUint8(n int) []uint8 {
	for {
		select {
		case buf := <-uint8Pool:
			if cap(buf) >= n {
				return buf[:n]
			}
		default:
			return make([]uint8, n)
		}
	}
}

// PutUint8 returns a buffer to the pool for later reuse.
func PutUint8(buf []uint8) {
	if cap(buf) == 0 {
		return
	}
	select {
	case uint8Pool <- buf[:cap(buf)]:
	default:
	}
}
