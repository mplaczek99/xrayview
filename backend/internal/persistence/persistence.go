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

const RecentStudyLimit = 10

// RecentStudyEntry is one row in the recent-studies catalog. Missing JSON
// fields decode to zero values so older files remain readable.
type RecentStudyEntry struct {
	InputPath        string                      `json:"inputPath"`
	InputName        string                      `json:"inputName"`
	MeasurementScale *contracts.MeasurementScale `json:"measurementScale,omitempty"`
	LastOpenedAt     string                      `json:"lastOpenedAt"`
}

// StudyCatalog is the top-level on-disk JSON shape. Unknown fields are
// intentionally ignored by encoding/json for forward compatibility.
type StudyCatalog struct {
	RecentStudies []RecentStudyEntry `json:"recentStudies"`
}

// Catalog serializes recent-study load/update/save operations and caches a
// successful read so repeated record operations do not hit disk unnecessarily.
type Catalog struct {
	rootDir string
	path    string

	operationMu sync.Mutex
	stateMu     sync.Mutex
	state       catalogState

	nowMu sync.Mutex
	now   func() time.Time
}

type catalogState struct {
	loaded bool
	cache  StudyCatalog
}

func NewCatalog(rootDir string) *Catalog {
	return &Catalog{
		rootDir: rootDir,
		path:    filepath.Join(rootDir, "catalog.json"),
		now:     time.Now,
	}
}

func (catalog *Catalog) RootDir() string {
	return catalog.rootDir
}

func (catalog *Catalog) Path() string {
	return catalog.path
}

// SetNow injects a deterministic clock for tests and parity fixtures.
func (catalog *Catalog) SetNow(now func() time.Time) {
	catalog.nowMu.Lock()
	defer catalog.nowMu.Unlock()
	catalog.now = now
}

func (catalog *Catalog) Ensure() error {
	if err := os.MkdirAll(catalog.rootDir, 0o777); err != nil {
		backendErr := contracts.InternalError(fmt.Sprintf("failed to create catalog directory %s: %v", catalog.rootDir, err))
		return backendErr
	}
	return nil
}

// Load forces a disk read and updates the in-memory cache only after a
// successful parse.
func (catalog *Catalog) Load() (StudyCatalog, error) {
	catalog.operationMu.Lock()
	defer catalog.operationMu.Unlock()

	value, err := catalog.loadFromDisk()
	if err != nil {
		catalog.stateMu.Lock()
		catalog.state.loaded = false
		catalog.state.cache = StudyCatalog{}
		catalog.stateMu.Unlock()
		return StudyCatalog{}, err
	}

	catalog.stateMu.Lock()
	catalog.state.loaded = true
	catalog.state.cache = cloneStudyCatalog(value)
	catalog.stateMu.Unlock()
	return value, nil
}

// RecordOpenedStudy moves a study to the front of the recent list, dedupes by
// input path, truncates to the UI limit, and persists the catalog.
func (catalog *Catalog) RecordOpenedStudy(study contracts.StudyRecord) error {
	catalog.operationMu.Lock()
	defer catalog.operationMu.Unlock()

	value, err := catalog.loadOrDefault()
	if err != nil {
		return err
	}

	recent := value.RecentStudies[:0]
	for _, entry := range value.RecentStudies {
		if entry.InputPath != study.InputPath {
			recent = append(recent, entry)
		}
	}
	value.RecentStudies = recent

	now := catalog.currentTime().UTC().Format(time.RFC3339)
	entry := RecentStudyEntry{
		InputPath:        study.InputPath,
		InputName:        study.InputName,
		MeasurementScale: cloneMeasurementScale(study.MeasurementScale),
		LastOpenedAt:     now,
	}
	value.RecentStudies = append([]RecentStudyEntry{entry}, value.RecentStudies...)
	if len(value.RecentStudies) > RecentStudyLimit {
		value.RecentStudies = value.RecentStudies[:RecentStudyLimit]
	}

	return catalog.save(value)
}

func (catalog *Catalog) loadFromDisk() (StudyCatalog, error) {
	contents, err := os.ReadFile(catalog.path)
	if err != nil {
		if os.IsNotExist(err) {
			return StudyCatalog{}, nil
		}
		backendErr := contracts.InternalError(fmt.Sprintf("failed to read study catalog %s: %v", catalog.path, err))
		return StudyCatalog{}, backendErr
	}

	var value StudyCatalog
	if err := json.Unmarshal(contents, &value); err != nil {
		_ = os.Rename(catalog.path, catalog.corruptPath())
		backendErr := contracts.NewBackendError(
			contracts.CacheCorrupted,
			fmt.Sprintf("study catalog at %s is invalid JSON: %v", catalog.path, err),
		)
		return StudyCatalog{}, backendErr
	}
	return value, nil
}

func (catalog *Catalog) loadOrDefault() (StudyCatalog, error) {
	catalog.stateMu.Lock()
	if catalog.state.loaded {
		value := cloneStudyCatalog(catalog.state.cache)
		catalog.stateMu.Unlock()
		return value, nil
	}
	catalog.stateMu.Unlock()

	value, err := catalog.loadFromDisk()
	if err == nil {
		catalog.stateMu.Lock()
		catalog.state.loaded = true
		catalog.state.cache = cloneStudyCatalog(value)
		catalog.stateMu.Unlock()
		return value, nil
	}
	if backendErr, ok := err.(contracts.BackendError); ok && backendErr.Code == contracts.CacheCorrupted {
		return StudyCatalog{}, nil
	}
	return StudyCatalog{}, err
}

func (catalog *Catalog) save(value StudyCatalog) error {
	if err := catalog.Ensure(); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		backendErr := contracts.InternalError(fmt.Sprintf("serialize study catalog: %v", err))
		return backendErr
	}
	if err := os.WriteFile(catalog.path, payload, 0o666); err != nil {
		backendErr := contracts.InternalError(fmt.Sprintf("failed to write study catalog %s: %v", catalog.path, err))
		return backendErr
	}

	catalog.stateMu.Lock()
	catalog.state.loaded = true
	catalog.state.cache = cloneStudyCatalog(value)
	catalog.stateMu.Unlock()
	return nil
}

func (catalog *Catalog) corruptPath() string {
	dir := filepath.Dir(catalog.path)
	base := filepath.Base(catalog.path)
	extension := filepath.Ext(base)
	if extension == "" {
		return catalog.path + ".corrupt"
	}
	stem := strings.TrimSuffix(base, extension)
	return filepath.Join(dir, stem+".corrupt"+extension)
}

func (catalog *Catalog) currentTime() time.Time {
	catalog.nowMu.Lock()
	defer catalog.nowMu.Unlock()
	return catalog.now()
}

func cloneStudyCatalog(value StudyCatalog) StudyCatalog {
	if value.RecentStudies == nil {
		return StudyCatalog{}
	}
	entries := make([]RecentStudyEntry, len(value.RecentStudies))
	for index, entry := range value.RecentStudies {
		entries[index] = entry
		entries[index].MeasurementScale = cloneMeasurementScale(entry.MeasurementScale)
	}
	return StudyCatalog{RecentStudies: entries}
}

func cloneMeasurementScale(scale *contracts.MeasurementScale) *contracts.MeasurementScale {
	if scale == nil {
		return nil
	}
	copy := *scale
	return &copy
}
