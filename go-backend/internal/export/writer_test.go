package export

import (
	"reflect"
	"testing"

	"xrayview/go-backend/internal/rustexport"
)

func TestNewWriterFromEnvironmentDefaultsToGoWriter(t *testing.T) {
	t.Setenv(ExporterModeEnv, "")

	writer, err := NewWriterFromEnvironment()
	if err != nil {
		t.Fatalf("NewWriterFromEnvironment returned error: %v", err)
	}

	if _, ok := writer.(GoWriter); !ok {
		t.Fatalf("writer type = %T, want GoWriter", writer)
	}
}

func TestNewWriterFromEnvironmentUsesRustHelperWhenConfigured(t *testing.T) {
	t.Setenv(ExporterModeEnv, ExporterModeRustHelper)
	t.Setenv(rustexport.HelperBinaryEnv, "/tmp/custom-export-helper")

	writer, err := NewWriterFromEnvironment()
	if err != nil {
		t.Fatalf("NewWriterFromEnvironment returned error: %v", err)
	}

	helper, ok := writer.(*rustexport.Helper)
	if !ok {
		t.Fatalf("writer type = %T, want *rustexport.Helper", writer)
	}
	if got, want := helper.Command(), []string{"/tmp/custom-export-helper"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("helper.Command() = %#v, want %#v", got, want)
	}
}

func TestNewWriterFromEnvironmentRejectsUnknownMode(t *testing.T) {
	t.Setenv(ExporterModeEnv, "legacy-rust")

	_, err := NewWriterFromEnvironment()
	if err == nil {
		t.Fatal("NewWriterFromEnvironment returned nil error for unknown mode")
	}
}
