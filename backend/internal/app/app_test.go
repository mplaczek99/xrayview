package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xrayview/backend/internal/config"
	"xrayview/backend/internal/contracts"
)

func TestNewServicePreparesDirectoriesAndLeavesHandlerNil(t *testing.T) {
	cfg := config.Default()
	baseDir := t.TempDir()
	cfg.Paths.CacheDir = filepath.Join(baseDir, "cache")
	cfg.Paths.PersistenceDir = filepath.Join(baseDir, "state")

	application, err := NewService(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if application.Handler() != nil {
		t.Fatal("Handler() != nil, want no HTTP handler for embedded service")
	}
	if got, want := application.Config().Paths.CacheDir, cfg.Paths.CacheDir; got != want {
		t.Fatalf("Config().Paths.CacheDir = %q, want %q", got, want)
	}
	if got, want := application.Config().Paths.PersistenceDir, cfg.Paths.PersistenceDir; got != want {
		t.Fatalf("Config().Paths.PersistenceDir = %q, want %q", got, want)
	}

	for _, path := range []string{cfg.Paths.CacheDir, cfg.Paths.PersistenceDir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%q) returned error: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("%q is not a directory", path)
		}
	}
}

func TestRunWithoutHTTPServerReturnsConfigurationError(t *testing.T) {
	cfg := config.Default()
	baseDir := t.TempDir()
	cfg.Paths.CacheDir = filepath.Join(baseDir, "cache")
	cfg.Paths.PersistenceDir = filepath.Join(baseDir, "state")

	application, err := NewService(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	if err := application.Run(context.Background()); err == nil || err.Error() != "backend HTTP server is not configured" {
		t.Fatalf("Run error = %v, want missing-server configuration error", err)
	}
}

func TestNewReturnsInternalErrorWhenPersistencePathCannotBeCreated(t *testing.T) {
	cfg := config.Default()
	baseDir := t.TempDir()
	blocked := filepath.Join(baseDir, "blocked")
	if err := os.WriteFile(blocked, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	cfg.Paths.CacheDir = filepath.Join(baseDir, "cache")
	cfg.Paths.PersistenceDir = filepath.Join(blocked, "state")

	_, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("New returned nil error, want internal error")
	}

	backendErr, ok := err.(contracts.BackendError)
	if !ok {
		t.Fatalf("error type = %T, want contracts.BackendError", err)
	}
	if got, want := backendErr.Code, contracts.BackendErrorCodeInternal; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if !strings.Contains(backendErr.Message, "failed to create state directory") {
		t.Fatalf("error message = %q, want state-directory context", backendErr.Message)
	}
}
