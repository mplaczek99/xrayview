package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigUsesTempBaseDir(t *testing.T) {
	config := Default()
	baseDir := filepath.Join(os.TempDir(), "xrayview")

	if config.Logging.Level != "info" {
		t.Fatalf("level = %q, want info", config.Logging.Level)
	}
	if config.Paths.BaseDir != baseDir {
		t.Fatalf("base dir = %q, want %q", config.Paths.BaseDir, baseDir)
	}
	if config.Paths.CacheDir != filepath.Join(config.Paths.BaseDir, "cache") {
		t.Fatalf("cache dir = %q", config.Paths.CacheDir)
	}
	if config.Paths.PersistenceDir != filepath.Join(config.Paths.BaseDir, "state") {
		t.Fatalf("persistence dir = %q", config.Paths.PersistenceDir)
	}
}

func TestLoadFromLookupAppliesOverrides(t *testing.T) {
	config, err := LoadFromLookup(lookupFromMap(map[string]string{
		LogLevelEnvKey: "debug",
		BaseDirEnvKey:  "/tmp/xrayview-backend",
	}))
	if err != nil {
		t.Fatal(err)
	}

	if config.Logging.Level != "debug" {
		t.Fatalf("level = %q, want debug", config.Logging.Level)
	}
	if config.Paths.BaseDir != "/tmp/xrayview-backend" {
		t.Fatalf("base dir = %q", config.Paths.BaseDir)
	}
	if config.Paths.CacheDir != "/tmp/xrayview-backend/cache" {
		t.Fatalf("cache dir = %q", config.Paths.CacheDir)
	}
	if config.Paths.PersistenceDir != "/tmp/xrayview-backend/state" {
		t.Fatalf("persistence dir = %q", config.Paths.PersistenceDir)
	}
}

func TestLoadFromLookupAllowsExplicitCacheAndPersistenceOverrides(t *testing.T) {
	config, err := LoadFromLookup(lookupFromMap(map[string]string{
		BaseDirEnvKey:        "/tmp/xrayview-backend",
		CacheDirEnvKey:       "/var/tmp/xrayview-cache",
		PersistenceDirEnvKey: "/var/tmp/xrayview-state",
	}))
	if err != nil {
		t.Fatal(err)
	}

	if config.Paths.BaseDir != "/tmp/xrayview-backend" {
		t.Fatalf("base dir = %q", config.Paths.BaseDir)
	}
	if config.Paths.CacheDir != "/var/tmp/xrayview-cache" {
		t.Fatalf("cache dir = %q", config.Paths.CacheDir)
	}
	if config.Paths.PersistenceDir != "/var/tmp/xrayview-state" {
		t.Fatalf("persistence dir = %q", config.Paths.PersistenceDir)
	}
}

func TestLoadFromLookupIgnoresWhitespaceOnlyValues(t *testing.T) {
	config, err := LoadFromLookup(lookupFromMap(map[string]string{
		LogLevelEnvKey:       "   ",
		BaseDirEnvKey:        "\t",
		CacheDirEnvKey:       "\n",
		PersistenceDirEnvKey: "  \r\n",
	}))
	if err != nil {
		t.Fatal(err)
	}

	if config != Default() {
		t.Fatalf("config = %+v, want default %+v", config, Default())
	}
}

func TestLoadFromLookupTrimsLogLevel(t *testing.T) {
	config, err := LoadFromLookup(lookupFromMap(map[string]string{
		LogLevelEnvKey: " WaRn ",
	}))
	if err != nil {
		t.Fatal(err)
	}

	if config.Logging.Level != "warn" {
		t.Fatalf("level = %q, want warn", config.Logging.Level)
	}
}

func TestLoadFromLookupRejectsInvalidLogLevel(t *testing.T) {
	_, err := LoadFromLookup(lookupFromMap(map[string]string{
		LogLevelEnvKey: "trace",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), LogLevelEnvKey) {
		t.Fatalf("error = %q, want env key", err.Error())
	}
}

func lookupFromMap(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
