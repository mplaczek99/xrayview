package studies

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sync"

	"xrayview/go-backend/internal/contracts"
)

type idGenerator func() (string, error)

type Registry struct {
	mu      sync.RWMutex
	newID   idGenerator
	studies map[string]contracts.StudyRecord
}

func New() *Registry {
	return newRegistryWithIDGenerator(generateStudyID)
}

func newRegistryWithIDGenerator(generator idGenerator) *Registry {
	return &Registry{
		newID:   generator,
		studies: make(map[string]contracts.StudyRecord),
	}
}

func (registry *Registry) Register(
	inputPath string,
	measurementScale *contracts.MeasurementScale,
) (contracts.StudyRecord, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	studyID, err := registry.newID()
	if err != nil {
		return contracts.StudyRecord{}, err
	}

	record := contracts.StudyRecord{
		StudyID:          studyID,
		InputPath:        inputPath,
		InputName:        inputNameFromPath(inputPath),
		MeasurementScale: measurementScale,
	}
	registry.studies[record.StudyID] = record

	return record, nil
}

func (registry *Registry) Get(studyID string) (contracts.StudyRecord, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	record, ok := registry.studies[studyID]
	return record, ok
}

func (registry *Registry) Count() int {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	return len(registry.studies)
}

func inputNameFromPath(inputPath string) string {
	baseName := filepath.Base(filepath.Clean(inputPath))
	if baseName == "." || baseName == string(filepath.Separator) || baseName == "" {
		return inputPath
	}

	return baseName
}

func generateStudyID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate study id: %w", err)
	}

	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80

	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], raw[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], raw[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], raw[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], raw[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], raw[10:16])

	return string(encoded), nil
}
