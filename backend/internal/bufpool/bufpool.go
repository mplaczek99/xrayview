package bufpool

// Small bounded pools for the hot pixel-buffer paths (render, process).
// GetUint8/Uint16/Float32 always return a slice of exactly
// length n — if the pool hands back a buffer with cap >= n we reslice,
// otherwise we allocate. Put* hands capacity back; don't touch the
// slice after calling it.

const maxPooledBuffers = 64

var uint8Pool = make(chan []uint8, maxPooledBuffers)
var uint16Pool = make(chan []uint16, maxPooledBuffers)
var float32Pool = make(chan []float32, maxPooledBuffers)

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

// GetUint16 returns a []uint16 of length n from the pool, or allocates a new
// one if no pooled buffer has enough capacity.
func GetUint16(n int) []uint16 {
	for {
		select {
		case buf := <-uint16Pool:
			if cap(buf) >= n {
				return buf[:n]
			}
		default:
			return make([]uint16, n)
		}
	}
}

// PutUint16 returns a buffer to the pool for later reuse.
func PutUint16(buf []uint16) {
	if cap(buf) == 0 {
		return
	}
	select {
	case uint16Pool <- buf[:cap(buf)]:
	default:
	}
}

// GetFloat32 returns a []float32 of length n from the pool, or allocates a
// new one if no pooled buffer has enough capacity.
func GetFloat32(n int) []float32 {
	for {
		select {
		case buf := <-float32Pool:
			if cap(buf) >= n {
				return buf[:n]
			}
		default:
			return make([]float32, n)
		}
	}
}

// PutFloat32 returns a buffer to the pool for later reuse.
func PutFloat32(buf []float32) {
	if cap(buf) == 0 {
		return
	}
	select {
	case float32Pool <- buf[:cap(buf)]:
	default:
	}
}
