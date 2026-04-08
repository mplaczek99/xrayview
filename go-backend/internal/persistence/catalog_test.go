package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

func TestLoadMissingCatalogReturnsEmptyRecentStudiesArray(t *testing.T) {
	catalog := New(t.TempDir())

	value, err := catalog.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if value.RecentStudies == nil {
		t.Fatal("RecentStudies = nil, want empty slice")
	}
	if got, want := len(value.RecentStudies), 0; got != want {
		t.Fatalf("recent study count = %d, want %d", got, want)
	}
}

func TestRecordOpenedStudyReordersExistingStudyWithoutDuplicate(t *testing.T) {
	catalog := New(t.TempDir())
	nowCalls := 0
	catalog.now = func() time.Time {
		nowCalls++
		return time.Date(2026, time.January, 2, 3, 4, nowCalls, 0, time.UTC)
	}

	for _, study := range []contracts.StudyRecord{
		{
			StudyID:   "study-1",
			InputPath: "/tmp/one.dcm",
			InputName: "one.dcm",
		},
		{
			StudyID:   "study-2",
			InputPath: "/tmp/two.dcm",
			InputName: "two.dcm",
		},
		{
			StudyID:   "study-3",
			InputPath: "/tmp/one.dcm",
			InputName: "one.dcm",
		},
	} {
		if err := catalog.RecordOpenedStudy(study); err != nil {
			t.Fatalf("RecordOpenedStudy returned error: %v", err)
		}
	}

	value, err := catalog.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if got, want := len(value.RecentStudies), 2; got != want {
		t.Fatalf("recent study count = %d, want %d", got, want)
	}
	if got, want := value.RecentStudies[0].InputPath, "/tmp/one.dcm"; got != want {
		t.Fatalf("first recent study path = %q, want %q", got, want)
	}
	if got, want := value.RecentStudies[1].InputPath, "/tmp/two.dcm"; got != want {
		t.Fatalf("second recent study path = %q, want %q", got, want)
	}
}

func TestRecordOpenedStudyTruncatesToTenEntries(t *testing.T) {
	catalog := New(t.TempDir())
	catalog.now = func() time.Time {
		return time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	}

	for index := 0; index < 12; index++ {
		inputName := fmt.Sprintf("study-%02d.dcm", index)
		if err := catalog.RecordOpenedStudy(contracts.StudyRecord{
			StudyID:   fmt.Sprintf("study-%02d", index),
			InputPath: filepath.Join("/tmp", inputName),
			InputName: inputName,
		}); err != nil {
			t.Fatalf("RecordOpenedStudy returned error at index %d: %v", index, err)
		}
	}

	value, err := catalog.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if got, want := len(value.RecentStudies), recentStudyLimit; got != want {
		t.Fatalf("recent study count = %d, want %d", got, want)
	}
	if got, want := value.RecentStudies[0].InputName, "study-11.dcm"; got != want {
		t.Fatalf("first recent study = %q, want %q", got, want)
	}
	if got, want := value.RecentStudies[recentStudyLimit-1].InputName, "study-02.dcm"; got != want {
		t.Fatalf("last retained recent study = %q, want %q", got, want)
	}
}

func TestRecordOpenedStudyMatchesPhase1ParityFixture(t *testing.T) {
	repoRoot := repoRootForTest(t)
	catalog := New(t.TempDir())
	catalog.now = func() time.Time {
		return time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	}
	samplePath := filepath.Join(repoRoot, "images", "sample-dental-radiograph.dcm")

	if err := catalog.RecordOpenedStudy(contracts.StudyRecord{
		StudyID:   "phase24-study",
		InputPath: samplePath,
		InputName: filepath.Base(samplePath),
	}); err != nil {
		t.Fatalf("RecordOpenedStudy returned error: %v", err)
	}

	actual := normalizeRecentStudyCatalogFixture(t, repoRoot, catalog.Path())
	expected := decodeJSONFixture(
		t,
		filepath.Join(
			repoRoot,
			"backend",
			"tests",
			"fixtures",
			"parity",
			"sample-dental-radiograph",
			"recent-study-catalog.json",
		),
	)

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("normalized catalog fixture = %#v, want %#v", actual, expected)
	}
}

func repoRootForTest(t *testing.T) string {
	t.Helper()

	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("Abs returned error: %v", err)
	}

	return repoRoot
}

func normalizeRecentStudyCatalogFixture(t *testing.T, repoRoot string, path string) any {
	t.Helper()

	value := decodeJSONFixture(t, path)
	rootMap, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("catalog payload type = %T, want map[string]any", value)
	}

	recentStudies, ok := rootMap["recentStudies"].([]any)
	if !ok {
		t.Fatalf("recentStudies type = %T, want []any", rootMap["recentStudies"])
	}

	normalizedEntries := make([]any, 0, len(recentStudies))
	for _, rawEntry := range recentStudies {
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			t.Fatalf("recent study entry type = %T, want map[string]any", rawEntry)
		}

		measurementScale, ok := entry["measurementScale"]
		if !ok {
			t.Fatal("measurementScale key missing from persisted catalog entry")
		}

		lastOpenedAt, ok := entry["lastOpenedAt"].(string)
		if !ok {
			t.Fatalf("lastOpenedAt type = %T, want string", entry["lastOpenedAt"])
		}

		inputPath, ok := entry["inputPath"].(string)
		if !ok {
			t.Fatalf("inputPath type = %T, want string", entry["inputPath"])
		}

		relativeInputPath, err := filepath.Rel(repoRoot, inputPath)
		if err != nil {
			t.Fatalf("Rel returned error: %v", err)
		}

		normalizedEntries = append(normalizedEntries, map[string]any{
			"inputPath":             filepath.ToSlash(relativeInputPath),
			"inputName":             entry["inputName"],
			"measurementScale":      measurementScale,
			"lastOpenedAtIsRfc3339": timeValidRFC3339(lastOpenedAt),
		})
	}

	return map[string]any{
		"recentStudies": normalizedEntries,
	}
}

func decodeJSONFixture(t *testing.T, path string) any {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error for %s: %v", path, err)
	}

	var value any
	if err := json.Unmarshal(contents, &value); err != nil {
		t.Fatalf("Unmarshal returned error for %s: %v", path, err)
	}

	return value
}

func timeValidRFC3339(value string) bool {
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}
