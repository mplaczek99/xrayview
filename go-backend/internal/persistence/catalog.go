package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xrayview/go-backend/internal/contracts"
)

const recentStudyLimit = 10

type RecentStudyEntry struct {
	InputPath        string                     `json:"inputPath"`
	InputName        string                     `json:"inputName"`
	MeasurementScale *PersistedMeasurementScale `json:"measurementScale,omitempty"`
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
}

func New(rootDir string) *Catalog {
	cleanRoot := filepath.Clean(rootDir)

	return &Catalog{
		rootDir: cleanRoot,
		path:    filepath.Join(cleanRoot, "catalog.json"),
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (catalog *Catalog) RootDir() string {
	return catalog.rootDir
}

func (catalog *Catalog) Ensure() error {
	return os.MkdirAll(catalog.rootDir, 0o755)
}

func (catalog *Catalog) Load() (StudyCatalog, error) {
	contents, err := os.ReadFile(catalog.path)
	if err != nil {
		return StudyCatalog{}, nil
	}

	var value StudyCatalog
	if err := json.Unmarshal(contents, &value); err != nil {
		_ = os.Rename(catalog.path, catalog.corruptPath())
		return StudyCatalog{}, contracts.CacheCorrupted(
			fmt.Sprintf("study catalog at %s is invalid JSON: %v", catalog.path, err),
		)
	}

	return value, nil
}

func (catalog *Catalog) RecordOpenedStudy(study contracts.StudyRecord) error {
	value := catalog.loadOrDefault()
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

func (catalog *Catalog) loadOrDefault() StudyCatalog {
	value, err := catalog.Load()
	if err != nil {
		return StudyCatalog{}
	}

	return value
}

func (catalog *Catalog) save(value StudyCatalog) error {
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
