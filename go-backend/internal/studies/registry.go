package studies

import (
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type Record struct {
	ID           string    `json:"id"`
	InputPath    string    `json:"inputPath"`
	InputName    string    `json:"inputName"`
	RegisteredAt time.Time `json:"registeredAt"`
}

type Registry struct {
	mu       sync.RWMutex
	sequence int
	studies  map[string]Record
}

func New() *Registry {
	return &Registry{
		studies: make(map[string]Record),
	}
}

func (registry *Registry) Register(inputPath string) Record {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	registry.sequence++
	id := "go-study-" + strconv.Itoa(registry.sequence)
	record := Record{
		ID:           id,
		InputPath:    inputPath,
		InputName:    filepath.Base(inputPath),
		RegisteredAt: time.Now().UTC(),
	}
	registry.studies[id] = record

	return record
}

func (registry *Registry) Count() int {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	return len(registry.studies)
}
