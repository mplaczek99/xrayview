package bufpool

import "sync"

var uint8Pool = sync.Pool{}
var uint16Pool = sync.Pool{}
var float32Pool = sync.Pool{}

// GetUint8 returns a []uint8 of length n from the pool, or allocates a new
// one if no pooled buffer has enough capacity.
func GetUint8(n int) []uint8 {
	if v := uint8Pool.Get(); v != nil {
		if buf := *v.(*[]uint8); cap(buf) >= n {
			return buf[:n]
		}
	}
	return make([]uint8, n)
}

// PutUint8 returns a buffer to the pool for later reuse.
func PutUint8(buf []uint8) {
	if cap(buf) == 0 {
		return
	}
	buf = buf[:cap(buf)]
	uint8Pool.Put(&buf)
}

// GetUint16 returns a []uint16 of length n from the pool, or allocates a new
// one if no pooled buffer has enough capacity.
func GetUint16(n int) []uint16 {
	if v := uint16Pool.Get(); v != nil {
		if buf := *v.(*[]uint16); cap(buf) >= n {
			return buf[:n]
		}
	}
	return make([]uint16, n)
}

// PutUint16 returns a buffer to the pool for later reuse.
func PutUint16(buf []uint16) {
	if cap(buf) == 0 {
		return
	}
	buf = buf[:cap(buf)]
	uint16Pool.Put(&buf)
}

// GetFloat32 returns a []float32 of length n from the pool, or allocates a
// new one if no pooled buffer has enough capacity.
func GetFloat32(n int) []float32 {
	if v := float32Pool.Get(); v != nil {
		if buf := *v.(*[]float32); cap(buf) >= n {
			return buf[:n]
		}
	}
	return make([]float32, n)
}

// PutFloat32 returns a buffer to the pool for later reuse.
func PutFloat32(buf []float32) {
	if cap(buf) == 0 {
		return
	}
	buf = buf[:cap(buf)]
	float32Pool.Put(&buf)
}
