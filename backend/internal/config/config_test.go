package config

import (
	"log/slog"
	"os"
	"path/filepath"
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

	if got, want := cfg.Paths.BaseDir, filepath.Join(os.TempDir(), "xrayview"); got != want {
		t.Fatalf("BaseDir = %q, want %q", got, want)
	}

	if got, want := cfg.Paths.CacheDir, filepath.Join(cfg.Paths.BaseDir, "cache"); got != want {
		t.Fatalf("CacheDir = %q, want %q", got, want)
	}

	if got, want := cfg.Paths.PersistenceDir, filepath.Join(cfg.Paths.BaseDir, "state"); got != want {
		t.Fatalf("PersistenceDir = %q, want %q", got, want)
	}
}

func TestLoadFromLookupAppliesOverrides(t *testing.T) {
	cfg, err := LoadFromLookup(lookupFromMap(map[string]string{
		HostEnvKey:            "::1",
		PortEnvKey:            "39123",
		LogLevelEnvKey:        "debug",
		BaseDirEnvKey:         "/tmp/xrayview-go",
		ShutdownTimeoutEnvKey: "9s",
	}))
	if err != nil {
		t.Fatalf("LoadFromLookup returned error: %v", err)
	}

	if got, want := cfg.Server.Host, "::1"; got != want {
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
	if got, want := cfg.Paths.CacheDir, "/tmp/xrayview-go/cache"; got != want {
		t.Fatalf("CacheDir = %q, want %q", got, want)
	}
	if got, want := cfg.Paths.PersistenceDir, "/tmp/xrayview-go/state"; got != want {
		t.Fatalf("PersistenceDir = %q, want %q", got, want)
	}

	if got, want := cfg.Server.ShutdownTimeout, 9*time.Second; got != want {
		t.Fatalf("ShutdownTimeout = %s, want %s", got, want)
	}
}

func TestLoadFromLookupAllowsExplicitCacheAndPersistenceOverrides(t *testing.T) {
	cfg, err := LoadFromLookup(lookupFromMap(map[string]string{
		BaseDirEnvKey:        "/tmp/xrayview-go",
		CacheDirEnvKey:       "/var/tmp/xrayview-cache",
		PersistenceDirEnvKey: "/var/tmp/xrayview-state",
	}))
	if err != nil {
		t.Fatalf("LoadFromLookup returned error: %v", err)
	}

	if got, want := cfg.Paths.BaseDir, "/tmp/xrayview-go"; got != want {
		t.Fatalf("BaseDir = %q, want %q", got, want)
	}
	if got, want := cfg.Paths.CacheDir, "/var/tmp/xrayview-cache"; got != want {
		t.Fatalf("CacheDir = %q, want %q", got, want)
	}
	if got, want := cfg.Paths.PersistenceDir, "/var/tmp/xrayview-state"; got != want {
		t.Fatalf("PersistenceDir = %q, want %q", got, want)
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

func TestLoadFromLookupRejectsNonLoopbackHost(t *testing.T) {
	_, err := LoadFromLookup(lookupFromMap(map[string]string{
		HostEnvKey: "0.0.0.0",
	}))
	if err == nil {
		t.Fatal("expected non-loopback host to fail")
	}
}

func TestLoadFromLookupSupportsLegacyEnvKeys(t *testing.T) {
	cfg, err := LoadFromLookup(lookupFromMap(map[string]string{
		LegacyHostEnvKey:            "::1",
		LegacyPortEnvKey:            "40123",
		LegacyBaseDirEnvKey:         "/tmp/xrayview-legacy",
		LegacyShutdownTimeoutEnvKey: "7s",
	}))
	if err != nil {
		t.Fatalf("LoadFromLookup returned error: %v", err)
	}

	if got, want := cfg.Server.Host, "::1"; got != want {
		t.Fatalf("Host = %q, want %q", got, want)
	}

	if got, want := cfg.Server.Port, 40123; got != want {
		t.Fatalf("Port = %d, want %d", got, want)
	}

	if got, want := cfg.Paths.BaseDir, "/tmp/xrayview-legacy"; got != want {
		t.Fatalf("BaseDir = %q, want %q", got, want)
	}

	if got, want := cfg.Server.ShutdownTimeout, 7*time.Second; got != want {
		t.Fatalf("ShutdownTimeout = %s, want %s", got, want)
	}
}
