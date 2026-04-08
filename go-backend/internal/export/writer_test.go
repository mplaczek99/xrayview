package export

import (
	"testing"
)

func TestNewWriterFromEnvironmentDefaultsToGoWriter(t *testing.T) {
	writer, err := NewWriterFromEnvironment()
	if err != nil {
		t.Fatalf("NewWriterFromEnvironment returned error: %v", err)
	}

	if _, ok := writer.(GoWriter); !ok {
		t.Fatalf("writer type = %T, want GoWriter", writer)
	}
}
