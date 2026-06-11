package persistence

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xrayview/backend/internal/contracts"
)

func TestRecordOpenedStudyKeepsMostRecentEntryFirst(t *testing.T) {
	catalog := NewCatalog(t.TempDir())
	catalog.SetNow(fixedNow)

	if err := catalog.RecordOpenedStudy(study("/tmp/one.bmp", "one.bmp")); err != nil {
		t.Fatal(err)
	}
	if err := catalog.RecordOpenedStudy(study("/tmp/two.bmp", "two.bmp")); err != nil {
		t.Fatal(err)
	}

	value, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(value.RecentStudies) != 2 {
		t.Fatalf("recent count = %d, want 2", len(value.RecentStudies))
	}
	if value.RecentStudies[0].InputName != "two.bmp" || value.RecentStudies[1].InputName != "one.bmp" {
		t.Fatalf("recent studies = %+v", value.RecentStudies)
	}
}

func TestLoadMissingCatalogReturnsEmptyRecentStudiesArray(t *testing.T) {
	catalog := NewCatalog(filepath.Join(t.TempDir(), "missing"))

	value, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(value.RecentStudies) != 0 {
		t.Fatalf("recent studies = %+v, want empty", value.RecentStudies)
	}
}

func TestLoadTreatsInvalidCatalogAsCorruptedCache(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), []byte("{ not json"), 0o666); err != nil {
		t.Fatal(err)
	}
	catalog := NewCatalog(root)

	_, err := catalog.Load()
	if err == nil {
		t.Fatal("expected error")
	}
	var backendErr contracts.BackendError
	if !errors.As(err, &backendErr) {
		t.Fatalf("error = %T, want BackendError", err)
	}
	if backendErr.Code != contracts.CacheCorrupted {
		t.Fatalf("code = %s, want cacheCorrupted", backendErr.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "catalog.corrupt.json")); err != nil {
		t.Fatalf("corrupt sidecar missing: %v", err)
	}
}

func TestLoadToleratesUnknownAndMissingEntryFieldsLikeLenientJSONUnmarshal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), []byte(`{
	  "recentStudies": [
	    {
	      "inputPath": "/tmp/one.bmp",
	      "inputName": "one.bmp",
	      "extra": true
	    }
	  ],
	  "futureField": "ignored"
	}`), 0o666); err != nil {
		t.Fatal(err)
	}
	catalog := NewCatalog(root)

	value, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(value.RecentStudies) != 1 {
		t.Fatalf("recent count = %d, want 1", len(value.RecentStudies))
	}
	entry := value.RecentStudies[0]
	if entry.InputPath != "/tmp/one.bmp" || entry.InputName != "one.bmp" {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.MeasurementScale != nil || entry.LastOpenedAt != "" {
		t.Fatalf("entry defaults = %+v", entry)
	}
}

func TestRecordOpenedStudyRecoversFromCorruptCatalog(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), []byte("{ not json"), 0o666); err != nil {
		t.Fatal(err)
	}
	catalog := NewCatalog(root)

	if err := catalog.RecordOpenedStudy(study("/tmp/recovered.bmp", "recovered.bmp")); err != nil {
		t.Fatal(err)
	}
	value, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(value.RecentStudies) != 1 || value.RecentStudies[0].InputName != "recovered.bmp" {
		t.Fatalf("recent studies = %+v", value.RecentStudies)
	}
	if _, err := os.Stat(filepath.Join(root, "catalog.corrupt.json")); err != nil {
		t.Fatalf("corrupt sidecar missing: %v", err)
	}
}

func TestRecordOpenedStudyReordersExistingStudyWithoutDuplicate(t *testing.T) {
	catalog := NewCatalog(t.TempDir())

	for _, opened := range []contracts.StudyRecord{
		study("/tmp/one.bmp", "one.bmp"),
		study("/tmp/two.bmp", "two.bmp"),
		study("/tmp/one.bmp", "one.bmp"),
	} {
		if err := catalog.RecordOpenedStudy(opened); err != nil {
			t.Fatal(err)
		}
	}
	value, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(value.RecentStudies) != 2 {
		t.Fatalf("recent count = %d, want 2", len(value.RecentStudies))
	}
	if value.RecentStudies[0].InputPath != "/tmp/one.bmp" || value.RecentStudies[1].InputPath != "/tmp/two.bmp" {
		t.Fatalf("recent studies = %+v", value.RecentStudies)
	}
}

func TestRecordOpenedStudyTruncatesToTenEntries(t *testing.T) {
	catalog := NewCatalog(t.TempDir())

	for index := 0; index < 12; index++ {
		inputName := "study-" + twoDigits(index) + ".bmp"
		if err := catalog.RecordOpenedStudy(study("/tmp/"+inputName, inputName)); err != nil {
			t.Fatal(err)
		}
	}
	value, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(value.RecentStudies) != RecentStudyLimit {
		t.Fatalf("recent count = %d, want %d", len(value.RecentStudies), RecentStudyLimit)
	}
	if value.RecentStudies[0].InputName != "study-11.bmp" {
		t.Fatalf("newest = %q", value.RecentStudies[0].InputName)
	}
	if value.RecentStudies[RecentStudyLimit-1].InputName != "study-02.bmp" {
		t.Fatalf("oldest survivor = %q", value.RecentStudies[RecentStudyLimit-1].InputName)
	}
}

func TestRecordOpenedStudyPersistsMeasurementScaleAndRFC3339Timestamp(t *testing.T) {
	catalog := NewCatalog(t.TempDir())
	catalog.SetNow(fixedNow)
	scale := &contracts.MeasurementScale{
		RowSpacingMM:    0.2,
		ColumnSpacingMM: 0.3,
		Source:          "manualCalibration",
	}
	opened := study("/tmp/scaled.bmp", "scaled.bmp")
	opened.MeasurementScale = scale

	if err := catalog.RecordOpenedStudy(opened); err != nil {
		t.Fatal(err)
	}
	payloadBytes, err := os.ReadFile(catalog.Path())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatal(err)
	}
	recent := payload["recentStudies"].([]any)[0].(map[string]any)
	measurementScale := recent["measurementScale"].(map[string]any)
	if measurementScale["rowSpacingMm"] != 0.2 || measurementScale["columnSpacingMm"] != 0.3 || measurementScale["source"] != "manualCalibration" {
		t.Fatalf("measurement scale = %+v", measurementScale)
	}
	if recent["lastOpenedAt"] != "2026-01-02T03:04:05Z" {
		t.Fatalf("lastOpenedAt = %v", recent["lastOpenedAt"])
	}
}

func study(inputPath, inputName string) contracts.StudyRecord {
	return contracts.StudyRecord{
		StudyID:   "study-1",
		InputPath: inputPath,
		InputName: inputName,
	}
}

func fixedNow() time.Time {
	value, err := time.Parse(time.RFC3339, "2026-01-02T03:04:05Z")
	if err != nil {
		panic(err)
	}
	return value
}

func twoDigits(value int) string {
	return fmt.Sprintf("%02d", value)
}
