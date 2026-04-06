package persistence

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"xrayview/go-backend/internal/contracts"
)

func TestRecordOpenedStudyKeepsMostRecentEntryFirst(t *testing.T) {
	catalog := New(t.TempDir())
	catalog.now = func() time.Time {
		return time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	}

	if err := catalog.RecordOpenedStudy(contracts.StudyRecord{
		StudyID:   "study-1",
		InputPath: "/tmp/one.dcm",
		InputName: "one.dcm",
	}); err != nil {
		t.Fatalf("RecordOpenedStudy returned error: %v", err)
	}

	if err := catalog.RecordOpenedStudy(contracts.StudyRecord{
		StudyID:   "study-2",
		InputPath: "/tmp/two.dcm",
		InputName: "two.dcm",
	}); err != nil {
		t.Fatalf("RecordOpenedStudy returned error: %v", err)
	}

	value, err := catalog.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if got, want := len(value.RecentStudies), 2; got != want {
		t.Fatalf("recent study count = %d, want %d", got, want)
	}

	if got, want := value.RecentStudies[0].InputName, "two.dcm"; got != want {
		t.Fatalf("first recent study = %q, want %q", got, want)
	}

	if got, want := value.RecentStudies[1].InputName, "one.dcm"; got != want {
		t.Fatalf("second recent study = %q, want %q", got, want)
	}
}

func TestLoadTreatsInvalidCatalogAsCorruptedCache(t *testing.T) {
	rootDir := t.TempDir()
	catalogPath := filepath.Join(rootDir, "catalog.json")
	if err := os.WriteFile(catalogPath, []byte("{ not json"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	catalog := New(rootDir)
	_, err := catalog.Load()
	if err == nil {
		t.Fatal("Load returned nil error, want cache-corrupted error")
	}

	backendErr, ok := err.(contracts.BackendError)
	if !ok {
		t.Fatalf("error type = %T, want contracts.BackendError", err)
	}

	if got, want := backendErr.Code, contracts.BackendErrorCodeCacheCorrupted; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}

	if _, statErr := os.Stat(filepath.Join(rootDir, "catalog.corrupt.json")); statErr != nil {
		t.Fatalf("corrupt catalog was not renamed: %v", statErr)
	}
}
