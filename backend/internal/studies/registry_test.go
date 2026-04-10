package studies

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

func TestRegisterStoresStudyAndExtractsInputName(t *testing.T) {
	registry := newRegistryWithIDGenerator(func() (string, error) {
		return "study-1", nil
	})

	study, err := registry.Register("/tmp/example-study.dcm", nil)
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if got, want := study.InputName, "example-study.dcm"; got != want {
		t.Fatalf("InputName = %q, want %q", got, want)
	}

	stored, ok := registry.Get(study.StudyID)
	if !ok {
		t.Fatalf("registry missing study %q", study.StudyID)
	}

	if got, want := stored.InputPath, "/tmp/example-study.dcm"; got != want {
		t.Fatalf("InputPath = %q, want %q", got, want)
	}
}

func TestRegisterAssignsUniqueIDs(t *testing.T) {
	ids := []string{"study-1", "study-2"}
	registry := newRegistryWithIDGenerator(func() (string, error) {
		next := ids[0]
		ids = ids[1:]
		return next, nil
	})

	first, err := registry.Register("/tmp/one.dcm", nil)
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	second, err := registry.Register("/tmp/two.dcm", nil)
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if first.StudyID == second.StudyID {
		t.Fatalf("StudyID = %q for both records, want unique IDs", first.StudyID)
	}
}

func TestRegisterFallsBackToPathWhenFileNameIsUnavailable(t *testing.T) {
	registry := newRegistryWithIDGenerator(func() (string, error) {
		return "study-1", nil
	})

	study, err := registry.Register("", nil)
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if got, want := study.InputName, ""; got != want {
		t.Fatalf("InputName = %q, want %q", got, want)
	}
}

func TestRegisterBoundsRegistrySize(t *testing.T) {
	nextID := 0
	registry := newRegistryWithIDGenerator(func() (string, error) {
		nextID++
		return fmt.Sprintf("study-%03d", nextID), nil
	})

	for index := 0; index < maxRegisteredStudies+8; index++ {
		if _, err := registry.Register("/tmp/study.dcm", nil); err != nil {
			t.Fatalf("Register returned error: %v", err)
		}
	}

	if got, want := registry.Count(), maxRegisteredStudies; got != want {
		t.Fatalf("Count = %d, want %d", got, want)
	}
}

func TestRegisterPropagatesIDGenerationError(t *testing.T) {
	wantErr := errors.New("generate failed")
	registry := newRegistryWithIDGenerator(func() (string, error) {
		return "", wantErr
	})

	_, err := registry.Register("/tmp/example-study.dcm", nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Register error = %v, want %v", err, wantErr)
	}
	if got := registry.Count(); got != 0 {
		t.Fatalf("Count = %d, want 0 after failed registration", got)
	}
}

func TestInputNameFromPath(t *testing.T) {
	tests := []struct {
		name      string
		inputPath string
		want      string
	}{
		{
			name:      "absolute file path",
			inputPath: filepath.Join(string(filepath.Separator), "tmp", "study.dcm"),
			want:      "study.dcm",
		},
		{
			name:      "relative file path",
			inputPath: filepath.Join("nested", "study.dcm"),
			want:      "study.dcm",
		},
		{
			name:      "directory path with trailing separator",
			inputPath: filepath.Join(string(filepath.Separator), "tmp", "nested") + string(filepath.Separator),
			want:      "nested",
		},
		{
			name:      "root path",
			inputPath: string(filepath.Separator),
			want:      string(filepath.Separator),
		},
		{
			name:      "empty path",
			inputPath: "",
			want:      "",
		},
		{
			name:      "dot path",
			inputPath: ".",
			want:      ".",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := inputNameFromPath(test.inputPath); got != test.want {
				t.Fatalf("inputNameFromPath(%q) = %q, want %q", test.inputPath, got, test.want)
			}
		})
	}
}
