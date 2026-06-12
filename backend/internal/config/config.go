package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

// Default places backend data under the per-user cache directory (e.g.
// ~/.cache/xrayview on Linux). A world-shared location like /tmp/xrayview is
// unsafe on multi-user systems: another user could pre-create the directory or
// read rendered X-ray artifacts, so the temp fallback is suffixed with the UID
// and only used when no user cache directory can be resolved.
func Default() Config {
	baseDir := defaultBaseDir()
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

func defaultBaseDir() string {
	if userCacheDir, err := os.UserCacheDir(); err == nil && strings.TrimSpace(userCacheDir) != "" {
		return filepath.Join(userCacheDir, "xrayview")
	}
	return filepath.Join(os.TempDir(), "xrayview-"+strconv.Itoa(os.Getuid()))
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
