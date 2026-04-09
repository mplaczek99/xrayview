package studies

import (
	"context"
	"sync"

	"xrayview/backend/internal/dicommeta"
)

const defaultDecodeCacheCapacity = 4

type sourceStudyDecoder interface {
	DecodeStudy(context.Context, string) (dicommeta.SourceStudy, error)
}

type decodeCacheEntry struct {
	key   string
	study dicommeta.SourceStudy
	prev  *decodeCacheEntry
	next  *decodeCacheEntry
}

type decodeInflight struct {
	done  chan struct{}
	study dicommeta.SourceStudy
	err   error
}

// DecodeCache stores decoded studies by input path and evicts least-recently-used
// entries once it reaches capacity. It is safe for concurrent use.
type DecodeCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[string]*decodeCacheEntry
	inflight map[string]*decodeInflight
	head     *decodeCacheEntry
	tail     *decodeCacheEntry
}

func NewDecodeCache(capacity int) *DecodeCache {
	if capacity < 1 {
		capacity = defaultDecodeCacheCapacity
	}

	return &DecodeCache{
		capacity: capacity,
		entries:  make(map[string]*decodeCacheEntry, capacity),
		inflight: make(map[string]*decodeInflight, capacity),
	}
}

// GetOrDecode returns a cached SourceStudy or decodes it and stores it in the
// cache. Callers must treat the returned SourceStudy as read-only.
func (cache *DecodeCache) GetOrDecode(
	ctx context.Context,
	path string,
	decoder sourceStudyDecoder,
) (dicommeta.SourceStudy, error) {
	cache.mu.Lock()
	if entry, ok := cache.entries[path]; ok {
		cache.moveToFrontLocked(entry)
		study := entry.study
		cache.mu.Unlock()
		return study, nil
	}

	if inflight, ok := cache.inflight[path]; ok {
		cache.mu.Unlock()
		select {
		case <-ctx.Done():
			return dicommeta.SourceStudy{}, ctx.Err()
		case <-inflight.done:
			if inflight.err != nil {
				return dicommeta.SourceStudy{}, inflight.err
			}
			return inflight.study, nil
		}
	}

	inflight := &decodeInflight{done: make(chan struct{})}
	cache.inflight[path] = inflight
	cache.mu.Unlock()

	study, err := decoder.DecodeStudy(ctx, path)

	cache.mu.Lock()
	if err == nil {
		if entry, ok := cache.entries[path]; ok {
			cache.moveToFrontLocked(entry)
			inflight.study = entry.study
		} else {
			entry := &decodeCacheEntry{
				key:   path,
				study: study,
			}
			cache.entries[path] = entry
			cache.pushFrontLocked(entry)
			inflight.study = study

			if len(cache.entries) > cache.capacity {
				cache.evictLocked()
			}
		}
	}

	inflight.err = err
	delete(cache.inflight, path)
	close(inflight.done)
	cache.mu.Unlock()

	if err != nil {
		return dicommeta.SourceStudy{}, err
	}

	return inflight.study, nil
}

func (cache *DecodeCache) Len() int {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	return len(cache.entries)
}

func (cache *DecodeCache) moveToFrontLocked(entry *decodeCacheEntry) {
	if cache.head == entry {
		return
	}

	cache.removeLocked(entry)
	cache.pushFrontLocked(entry)
}

func (cache *DecodeCache) pushFrontLocked(entry *decodeCacheEntry) {
	entry.prev = nil
	entry.next = cache.head
	if cache.head != nil {
		cache.head.prev = entry
	}
	cache.head = entry
	if cache.tail == nil {
		cache.tail = entry
	}
}

func (cache *DecodeCache) removeLocked(entry *decodeCacheEntry) {
	if entry.prev != nil {
		entry.prev.next = entry.next
	} else {
		cache.head = entry.next
	}

	if entry.next != nil {
		entry.next.prev = entry.prev
	} else {
		cache.tail = entry.prev
	}

	entry.prev = nil
	entry.next = nil
}

func (cache *DecodeCache) evictLocked() {
	if cache.tail == nil {
		return
	}

	victim := cache.tail
	cache.removeLocked(victim)
	delete(cache.entries, victim.key)
}
