package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"xrayview/backend/internal/contracts"
)

const recentStudyLimit = 10

type RecentStudyEntry struct {
	InputPath        string                     `json:"inputPath"`
	InputName        string                     `json:"inputName"`
	MeasurementScale *PersistedMeasurementScale `json:"measurementScale"`
	LastOpenedAt     string                     `json:"lastOpenedAt"`
}

type StudyCatalog struct {
	RecentStudies []RecentStudyEntry `json:"recentStudies"`
}

type PersistedMeasurementScale struct {
	RowSpacingMM    float64 `json:"rowSpacingMm"`
	ColumnSpacingMM float64 `json:"columnSpacingMm"`
	Source          string  `json:"source"`
}

type Catalog struct {
	rootDir string
	path    string
	now     func() time.Time

	mu     sync.Mutex
	loaded bool
	cache  StudyCatalog
}

func New(rootDir string) *Catalog {
	return NewAtPath(filepath.Join(filepath.Clean(rootDir), "catalog.json"))
}

func NewAtPath(path string) *Catalog {
	cleanPath := filepath.Clean(path)

	return &Catalog{
		rootDir: filepath.Dir(cleanPath),
		path:    cleanPath,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (catalog *Catalog) RootDir() string {
	return catalog.rootDir
}

func (catalog *Catalog) Path() string {
	return catalog.path
}

func (catalog *Catalog) Ensure() error {
	return os.MkdirAll(catalog.rootDir, 0o755)
}

// Load reads the persisted study catalog. A missing file is the expected
// first-run state — we swallow the error and return an empty catalog so
// nothing upstream has to special-case it. Corrupt JSON is different:
// we rename the bad file to *.corrupt first, then surface the error.
// That keeps the next save() from silently overwriting the evidence.
func (catalog *Catalog) Load() (StudyCatalog, error) {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()

	value, err := catalog.loadFromDisk()
	if err != nil {
		catalog.loaded = false
		catalog.cache = emptyStudyCatalog()
		return value, err
	}

	catalog.cache = cloneStudyCatalog(value)
	catalog.loaded = true

	return cloneStudyCatalog(value), nil
}

func (catalog *Catalog) loadFromDisk() (StudyCatalog, error) {
	contents, err := os.ReadFile(catalog.path)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyStudyCatalog(), nil
		}

		return emptyStudyCatalog(), contracts.Internal(
			fmt.Sprintf("failed to read study catalog %s: %v", catalog.path, err),
		)
	}

	var value StudyCatalog
	if err := json.Unmarshal(contents, &value); err != nil {
		_ = os.Rename(catalog.path, catalog.corruptPath())
		return emptyStudyCatalog(), contracts.CacheCorrupted(
			fmt.Sprintf("study catalog at %s is invalid JSON: %v", catalog.path, err),
		)
	}
	if value.RecentStudies == nil {
		value.RecentStudies = []RecentStudyEntry{}
	}

	return value, nil
}

func (catalog *Catalog) RecordOpenedStudy(study contracts.StudyRecord) error {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()

	value, err := catalog.loadOrDefaultLocked()
	if err != nil {
		return err
	}

	filtered := make([]RecentStudyEntry, 0, len(value.RecentStudies))
	for _, entry := range value.RecentStudies {
		if entry.InputPath != study.InputPath {
			filtered = append(filtered, entry)
		}
	}

	entry := RecentStudyEntry{
		InputPath:        study.InputPath,
		InputName:        study.InputName,
		MeasurementScale: persistedMeasurementScale(study.MeasurementScale),
		LastOpenedAt:     catalog.now().Format(time.RFC3339),
	}
	value.RecentStudies = append([]RecentStudyEntry{entry}, filtered...)
	if len(value.RecentStudies) > recentStudyLimit {
		value.RecentStudies = value.RecentStudies[:recentStudyLimit]
	}

	return catalog.save(value)
}

func (catalog *Catalog) loadOrDefaultLocked() (StudyCatalog, error) {
	if catalog.loaded {
		return cloneStudyCatalog(catalog.cache), nil
	}

	value, err := catalog.loadFromDisk()
	if err != nil {
		if backendErr, ok := err.(contracts.BackendError); ok &&
			backendErr.Code == contracts.BackendErrorCodeCacheCorrupted {
			return emptyStudyCatalog(), nil
		}

		return emptyStudyCatalog(), err
	}

	catalog.cache = cloneStudyCatalog(value)
	catalog.loaded = true

	return cloneStudyCatalog(value), nil
}

func (catalog *Catalog) save(value StudyCatalog) error {
	if value.RecentStudies == nil {
		value.RecentStudies = []RecentStudyEntry{}
	}

	if err := os.MkdirAll(catalog.rootDir, 0o755); err != nil {
		return contracts.Internal(
			fmt.Sprintf("failed to create catalog directory %s: %v", catalog.rootDir, err),
		)
	}

	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return contracts.Internal(fmt.Sprintf("serialize study catalog: %v", err))
	}

	if err := os.WriteFile(catalog.path, payload, 0o644); err != nil {
		return contracts.Internal(
			fmt.Sprintf("failed to write study catalog %s: %v", catalog.path, err),
		)
	}

	catalog.cache = cloneStudyCatalog(value)
	catalog.loaded = true

	return nil
}

func (catalog *Catalog) corruptPath() string {
	extension := filepath.Ext(catalog.path)
	if extension == "" {
		return catalog.path + ".corrupt"
	}

	return strings.TrimSuffix(catalog.path, extension) + ".corrupt" + extension
}

func persistedMeasurementScale(
	scale *contracts.MeasurementScale,
) *PersistedMeasurementScale {
	if scale == nil {
		return nil
	}

	return &PersistedMeasurementScale{
		RowSpacingMM:    scale.RowSpacingMM,
		ColumnSpacingMM: scale.ColumnSpacingMM,
		Source:          scale.Source,
	}
}

func emptyStudyCatalog() StudyCatalog {
	return StudyCatalog{
		RecentStudies: []RecentStudyEntry{},
	}
}

func cloneStudyCatalog(value StudyCatalog) StudyCatalog {
	recentStudies := make([]RecentStudyEntry, len(value.RecentStudies))
	for index, entry := range value.RecentStudies {
		recentStudies[index] = entry
		if entry.MeasurementScale != nil {
			scale := *entry.MeasurementScale
			recentStudies[index].MeasurementScale = &scale
		}
	}

	return StudyCatalog{
		RecentStudies: recentStudies,
	}
}
