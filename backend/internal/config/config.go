package config

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"xrayview/backend/internal/cache"
	"xrayview/backend/internal/contracts"
)

// Env-var keys. The Legacy* aliases are the pre-rename
// XRAYVIEW_GO_BACKEND_* names — still honored so existing installs
// and scripts don't break silently. lookupFirst reads the canonical
// key first, then falls back to the legacy one.
const (
	HostEnvKey                  = "XRAYVIEW_BACKEND_HOST"
	LegacyHostEnvKey            = "XRAYVIEW_GO_BACKEND_HOST"
	PortEnvKey                  = "XRAYVIEW_BACKEND_PORT"
	LegacyPortEnvKey            = "XRAYVIEW_GO_BACKEND_PORT"
	LogLevelEnvKey              = "XRAYVIEW_BACKEND_LOG_LEVEL"
	LegacyLogLevelEnvKey        = "XRAYVIEW_GO_BACKEND_LOG_LEVEL"
	BaseDirEnvKey               = "XRAYVIEW_BACKEND_BASE_DIR"
	LegacyBaseDirEnvKey         = "XRAYVIEW_GO_BACKEND_BASE_DIR"
	CacheDirEnvKey              = "XRAYVIEW_BACKEND_CACHE_DIR"
	LegacyCacheDirEnvKey        = "XRAYVIEW_GO_BACKEND_CACHE_DIR"
	PersistenceDirEnvKey        = "XRAYVIEW_BACKEND_PERSISTENCE_DIR"
	LegacyPersistenceDirEnvKey  = "XRAYVIEW_GO_BACKEND_PERSISTENCE_DIR"
	ShutdownTimeoutEnvKey       = "XRAYVIEW_BACKEND_SHUTDOWN_TIMEOUT"
	LegacyShutdownTimeoutEnvKey = "XRAYVIEW_GO_BACKEND_SHUTDOWN_TIMEOUT"
)

type Config struct {
	ServiceName string        `json:"serviceName"`
	Server      ServerConfig  `json:"server"`
	Logging     LoggingConfig `json:"logging"`
	Paths       PathsConfig   `json:"paths"`
}

type ServerConfig struct {
	Host            string        `json:"host"`
	Port            int           `json:"port"`
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`
}

type LoggingConfig struct {
	Level slog.Level `json:"level"`
}

type PathsConfig struct {
	BaseDir        string `json:"baseDir"`
	CacheDir       string `json:"cacheDir"`
	PersistenceDir string `json:"persistenceDir"`
}

type lookupEnvFunc func(string) (string, bool)

func lookupFirst(lookup lookupEnvFunc, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := lookup(key); ok && value != "" {
			return value, true
		}
	}

	return "", false
}

func Default() Config {
	baseDir := cache.DefaultRootDir()

	return Config{
		ServiceName: contracts.ServiceName,
		Server: ServerConfig{
			Host:            "127.0.0.1",
			Port:            38181,
			ShutdownTimeout: 5 * time.Second,
		},
		Logging: LoggingConfig{
			Level: slog.LevelInfo,
		},
		Paths: PathsConfig{
			BaseDir:        baseDir,
			CacheDir:       filepath.Join(baseDir, "cache"),
			PersistenceDir: filepath.Join(baseDir, "state"),
		},
	}
}

func Load() (Config, error) {
	return LoadFromLookup(os.LookupEnv)
}

func LoadFromLookup(lookup lookupEnvFunc) (Config, error) {
	cfg := Default()

	if value, ok := lookupFirst(lookup, HostEnvKey, LegacyHostEnvKey); ok {
		cfg.Server.Host = value
	}

	if value, ok := lookupFirst(lookup, PortEnvKey, LegacyPortEnvKey); ok {
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("%s must be a valid TCP port: %q", PortEnvKey, value)
		}
		cfg.Server.Port = port
	}

	if value, ok := lookupFirst(lookup, LogLevelEnvKey, LegacyLogLevelEnvKey); ok {
		var level slog.Level
		if err := level.UnmarshalText([]byte(value)); err != nil {
			return Config{}, fmt.Errorf("%s must be a valid slog level: %w", LogLevelEnvKey, err)
		}
		cfg.Logging.Level = level
	}

	if value, ok := lookupFirst(lookup, BaseDirEnvKey, LegacyBaseDirEnvKey); ok {
		cfg.Paths.BaseDir = filepath.Clean(value)
		cfg.Paths.CacheDir = filepath.Join(cfg.Paths.BaseDir, "cache")
		cfg.Paths.PersistenceDir = filepath.Join(cfg.Paths.BaseDir, "state")
	}

	if value, ok := lookupFirst(lookup, CacheDirEnvKey, LegacyCacheDirEnvKey); ok {
		cfg.Paths.CacheDir = filepath.Clean(value)
	}

	if value, ok := lookupFirst(lookup, PersistenceDirEnvKey, LegacyPersistenceDirEnvKey); ok {
		cfg.Paths.PersistenceDir = filepath.Clean(value)
	}

	if value, ok := lookupFirst(lookup, ShutdownTimeoutEnvKey, LegacyShutdownTimeoutEnvKey); ok {
		timeout, err := time.ParseDuration(value)
		if err != nil || timeout <= 0 {
			return Config{}, fmt.Errorf("%s must be a positive duration: %q", ShutdownTimeoutEnvKey, value)
		}
		cfg.Server.ShutdownTimeout = timeout
	}

	if !isLoopbackHost(cfg.Server.Host) {
		return Config{}, fmt.Errorf(
			"%s must be a loopback host for the local sidecar transport: %q",
			HostEnvKey,
			cfg.Server.Host,
		)
	}

	return cfg, nil
}

func (cfg Config) ListenAddress() string {
	return net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port))
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
