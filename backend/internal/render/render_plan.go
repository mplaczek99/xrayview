package render

import (
	"math"
	"sync"
	"sync/atomic"

	"xrayview/backend/internal/bufpool"
	"xrayview/backend/internal/imaging"
)

const renderLUTCacheSize = 4

var renderLUTs = newRenderLUTCache(renderLUTCacheSize)

type RenderPlan struct {
	Window WindowMode
}

func DefaultRenderPlan() RenderPlan {
	return RenderPlan{Window: DefaultWindowMode()}
}

func RenderSourceImage(source imaging.SourceImage, plan RenderPlan) imaging.PreviewImage {
	return imaging.GrayPreviewWithRelease(
		source.Width,
		source.Height,
		RenderGrayscalePixels(source, plan),
		bufpool.PutUint8,
	)
}

func RenderGrayscalePixels(source imaging.SourceImage, plan RenderPlan) []uint8 {
	pixels := bufpool.GetUint8(len(source.Pixels))
	window, hasWindow := resolveWindow(source, plan.Window)

	// LUT fast path. When the source's modality values fit a uint16 index
	// we precompute a 65k-entry table once and turn each per-pixel
	// window/invert/map into a single cache-friendly read. The else branch
	// below handles sources whose range doesn't fit (signed CT, rescaled
	// PET) with the old branchy per-pixel path.
	if source.FitsUint16 {
		lut := *cachedRenderLUT(source, window, hasWindow)
		for index, value := range source.Pixels {
			pixels[index] = lut[uint16(value+0.5)]
		}
		return pixels
	}

	switch {
	case hasWindow && source.Invert:
		for index, value := range source.Pixels {
			pixels[index] = 255 - window.Map(value)
		}
	case hasWindow:
		for index, value := range source.Pixels {
			pixels[index] = window.Map(value)
		}
	case source.Invert:
		for index, value := range source.Pixels {
			pixels[index] = 255 - MapLinear(value, source.MinValue, source.MaxValue)
		}
	default:
		for index, value := range source.Pixels {
			pixels[index] = MapLinear(value, source.MinValue, source.MaxValue)
		}
	}

	return pixels
}

func cachedRenderLUT(source imaging.SourceImage, window WindowTransform, hasWindow bool) *[65536]uint8 {
	key := newRenderLUTKey(source, window, hasWindow)
	return renderLUTs.getOrBuild(key, func() *[65536]uint8 {
		return buildRenderLUT(source, window, hasWindow)
	})
}

func buildRenderLUT(source imaging.SourceImage, window WindowTransform, hasWindow bool) *[65536]uint8 {
	lut := new([65536]uint8)
	populateRenderLUT(lut, source, window, hasWindow)
	return lut
}

func populateRenderLUT(lut *[65536]uint8, source imaging.SourceImage, window WindowTransform, hasWindow bool) {
	// Step 1.3: hoist window-nil and invert checks outside the loop.
	// Each branch runs a branchless inner loop — no per-entry conditionals.
	switch {
	case hasWindow && source.Invert:
		for i := range 65536 {
			lut[i] = 255 - window.Map(float32(i))
		}
	case hasWindow:
		for i := range 65536 {
			lut[i] = window.Map(float32(i))
		}
	case source.Invert:
		for i := range 65536 {
			lut[i] = 255 - MapLinear(float32(i), source.MinValue, source.MaxValue)
		}
	default:
		for i := range 65536 {
			lut[i] = MapLinear(float32(i), source.MinValue, source.MaxValue)
		}
	}
}

type renderLUTKey struct {
	minValue  uint32
	maxValue  uint32
	lower     uint32
	upper     uint32
	scale     uint32
	offset    uint32
	hasWindow bool
	invert    bool
}

func newRenderLUTKey(source imaging.SourceImage, window WindowTransform, hasWindow bool) renderLUTKey {
	return renderLUTKey{
		minValue:  math.Float32bits(source.MinValue),
		maxValue:  math.Float32bits(source.MaxValue),
		lower:     math.Float32bits(window.lower),
		upper:     math.Float32bits(window.upper),
		scale:     math.Float32bits(window.scale),
		offset:    math.Float32bits(window.offset),
		hasWindow: hasWindow,
		invert:    source.Invert,
	}
}

type renderLUTCache struct {
	mu      sync.Mutex
	latest  atomic.Pointer[renderLUTCacheEntry]
	maxSize int
	entries []*renderLUTCacheEntry
}

type renderLUTCacheEntry struct {
	key renderLUTKey
	lut *[65536]uint8
}

func newRenderLUTCache(maxSize int) *renderLUTCache {
	return &renderLUTCache{
		maxSize: maxSize,
		entries: make([]*renderLUTCacheEntry, 0, maxSize),
	}
}

func (cache *renderLUTCache) getOrBuild(key renderLUTKey, build func() *[65536]uint8) *[65536]uint8 {
	if lut, ok := cache.lookup(key); ok {
		return lut
	}

	lut := build()
	return cache.store(key, lut)
}

func (cache *renderLUTCache) lookup(key renderLUTKey) (*[65536]uint8, bool) {
	if entry := cache.latest.Load(); entry != nil && entry.key == key {
		return entry.lut, true
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()

	for index := range cache.entries {
		if cache.entries[index] != nil && cache.entries[index].key == key {
			entry := cache.entries[index]
			cache.promoteLocked(index)
			cache.latest.Store(entry)
			return entry.lut, true
		}
	}

	return nil, false
}

func (cache *renderLUTCache) store(key renderLUTKey, lut *[65536]uint8) *[65536]uint8 {
	if entry := cache.latest.Load(); entry != nil && entry.key == key {
		return entry.lut
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()

	for index := range cache.entries {
		if cache.entries[index] != nil && cache.entries[index].key == key {
			entry := cache.entries[index]
			cache.promoteLocked(index)
			cache.latest.Store(entry)
			return entry.lut
		}
	}

	if cache.maxSize <= 0 {
		return lut
	}

	entry := &renderLUTCacheEntry{
		key: key,
		lut: lut,
	}
	if len(cache.entries) < cache.maxSize {
		cache.entries = append(cache.entries, nil)
	}
	copy(cache.entries[1:], cache.entries[:len(cache.entries)-1])
	cache.entries[0] = entry
	cache.latest.Store(entry)
	return lut
}

func (cache *renderLUTCache) promoteLocked(index int) {
	if index == 0 {
		return
	}

	entry := cache.entries[index]
	copy(cache.entries[1:index+1], cache.entries[:index])
	cache.entries[0] = entry
}
