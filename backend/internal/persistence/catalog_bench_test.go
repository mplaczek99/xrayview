package persistence

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"xrayview/backend/internal/contracts"
)

func BenchmarkCatalogRecordOpenedStudy(b *testing.B) {
	catalog := New(b.TempDir())
	catalog.now = func() time.Time {
		return time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	}

	seedCatalog(b, catalog, recentStudyLimit)

	studies := make([]contracts.StudyRecord, 128)
	for index := range studies {
		inputName := fmt.Sprintf("bench-%03d.dcm", index)
		studies[index] = contracts.StudyRecord{
			StudyID:   fmt.Sprintf("bench-%03d", index),
			InputPath: filepath.Join("/tmp", inputName),
			InputName: inputName,
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		if err := catalog.RecordOpenedStudy(studies[index%len(studies)]); err != nil {
			b.Fatalf("RecordOpenedStudy returned error: %v", err)
		}
	}
}

func seedCatalog(b *testing.B, catalog *Catalog, count int) {
	b.Helper()

	for index := 0; index < count; index++ {
		inputName := fmt.Sprintf("seed-%03d.dcm", index)
		if err := catalog.RecordOpenedStudy(contracts.StudyRecord{
			StudyID:   fmt.Sprintf("seed-%03d", index),
			InputPath: filepath.Join("/tmp", inputName),
			InputName: inputName,
		}); err != nil {
			b.Fatalf("RecordOpenedStudy returned error: %v", err)
		}
	}
}
