package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"xrayview/backend/internal/contracts"
)

const (
	LogLevelEnvKey       = "XRAYVIEW_BACKEND_LOG_LEVEL"
	BaseDirEnvKey        = "XRAYVIEW_BACKEND_BASE_DIR"
	CacheDirEnvKey       = "XRAYVIEW_BACKEND_CACHE_DIR"
	PersistenceDirEnvKey = "XRAYVIEW_BACKEND_PERSISTENCE_DIR"
)

// Config is the backend runtime configuration assembled from defaults and
// XRAYVIEW_BACKEND_* environment overrides.
type Config struct {
	ServiceName string        `json:"serviceName"`
	Logging     LoggingConfig `json:"logging"`
	Paths       PathsConfig   `json:"paths"`
}

type LoggingConfig struct {
	Level string `json:"level"`
}

type PathsConfig struct {
	BaseDir        string `json:"baseDir"`
	CacheDir       string `json:"cacheDir"`
	PersistenceDir string `json:"persistenceDir"`
}

// Default returns the same ephemeral tempdir-based defaults as the Rust
// backend. The desktop shell is expected to override BaseDir in production.
func Default() Config {
	baseDir := filepath.Join(os.TempDir(), "xrayview")
	return Config{
		ServiceName: contracts.ServiceName,
		Logging: LoggingConfig{
			Level: "info",
		},
		Paths: PathsConfig{
			BaseDir:        baseDir,
			CacheDir:       filepath.Join(baseDir, "cache"),
			PersistenceDir: filepath.Join(baseDir, "state"),
		},
	}
}

// Load reads configuration from the process environment.
func Load() (Config, error) {
	return LoadFromLookup(func(key string) (string, bool) {
		return os.LookupEnv(key)
	})
}

// LoadFromLookup is the testable implementation of Load. Empty or whitespace
// only values are treated as unset, matching the Rust behavior.
func LoadFromLookup(lookup func(string) (string, bool)) (Config, error) {
	config := Default()
	lookupNonEmpty := func(key string) (string, bool) {
		value, ok := lookup(key)
		if !ok || strings.TrimSpace(value) == "" {
			return "", false
		}
		return value, true
	}

	if value, ok := lookupNonEmpty(LogLevelEnvKey); ok {
		trimmed := strings.TrimSpace(value)
		lower := strings.ToLower(trimmed)
		if lower != "debug" && lower != "info" && lower != "warn" && lower != "error" {
			return Config{}, fmt.Errorf("%s must be a valid log level: %s", LogLevelEnvKey, value)
		}
		config.Logging.Level = lower
	}

	if value, ok := lookupNonEmpty(BaseDirEnvKey); ok {
		config.Paths.BaseDir = value
		config.Paths.CacheDir = filepath.Join(config.Paths.BaseDir, "cache")
		config.Paths.PersistenceDir = filepath.Join(config.Paths.BaseDir, "state")
	}
	if value, ok := lookupNonEmpty(CacheDirEnvKey); ok {
		config.Paths.CacheDir = value
	}
	if value, ok := lookupNonEmpty(PersistenceDirEnvKey); ok {
		config.Paths.PersistenceDir = value
	}

	return config, nil
}
