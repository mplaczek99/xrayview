package config

import (
	"log/slog"
	"testing"
	"time"
)

func lookupFromMap(values map[string]string) lookupEnvFunc {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func TestDefaultConfigMatchesFrontendSidecarDefaults(t *testing.T) {
	cfg := Default()

	if got, want := cfg.Server.Host, "127.0.0.1"; got != want {
		t.Fatalf("Host = %q, want %q", got, want)
	}

	if got, want := cfg.Server.Port, 38181; got != want {
		t.Fatalf("Port = %d, want %d", got, want)
	}

	if got, want := cfg.Logging.Level, slog.LevelInfo; got != want {
		t.Fatalf("Level = %v, want %v", got, want)
	}

	if cfg.Paths.CacheDir == "" || cfg.Paths.PersistenceDir == "" {
		t.Fatal("default paths should not be empty")
	}
}

func TestLoadFromLookupAppliesOverrides(t *testing.T) {
	cfg, err := LoadFromLookup(lookupFromMap(map[string]string{
		HostEnvKey:            "0.0.0.0",
		PortEnvKey:            "39123",
		LogLevelEnvKey:        "debug",
		BaseDirEnvKey:         "/tmp/xrayview-go",
		ShutdownTimeoutEnvKey: "9s",
	}))
	if err != nil {
		t.Fatalf("LoadFromLookup returned error: %v", err)
	}

	if got, want := cfg.Server.Host, "0.0.0.0"; got != want {
		t.Fatalf("Host = %q, want %q", got, want)
	}

	if got, want := cfg.Server.Port, 39123; got != want {
		t.Fatalf("Port = %d, want %d", got, want)
	}

	if got, want := cfg.Logging.Level, slog.LevelDebug; got != want {
		t.Fatalf("Level = %v, want %v", got, want)
	}

	if got, want := cfg.Paths.BaseDir, "/tmp/xrayview-go"; got != want {
		t.Fatalf("BaseDir = %q, want %q", got, want)
	}

	if got, want := cfg.Server.ShutdownTimeout, 9*time.Second; got != want {
		t.Fatalf("ShutdownTimeout = %s, want %s", got, want)
	}
}

func TestLoadFromLookupRejectsInvalidPort(t *testing.T) {
	_, err := LoadFromLookup(lookupFromMap(map[string]string{
		PortEnvKey: "abc",
	}))
	if err == nil {
		t.Fatal("expected invalid port to fail")
	}
}
