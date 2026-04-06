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

	"xrayview/go-backend/internal/contracts"
)

const (
	HostEnvKey            = "XRAYVIEW_GO_BACKEND_HOST"
	PortEnvKey            = "XRAYVIEW_GO_BACKEND_PORT"
	LogLevelEnvKey        = "XRAYVIEW_GO_BACKEND_LOG_LEVEL"
	BaseDirEnvKey         = "XRAYVIEW_GO_BACKEND_BASE_DIR"
	CacheDirEnvKey        = "XRAYVIEW_GO_BACKEND_CACHE_DIR"
	PersistenceDirEnvKey  = "XRAYVIEW_GO_BACKEND_PERSISTENCE_DIR"
	ShutdownTimeoutEnvKey = "XRAYVIEW_GO_BACKEND_SHUTDOWN_TIMEOUT"
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

func Default() Config {
	baseDir := filepath.Join(os.TempDir(), "xrayview-go-backend")

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
			PersistenceDir: filepath.Join(baseDir, "persistence"),
		},
	}
}

func Load() (Config, error) {
	return LoadFromLookup(os.LookupEnv)
}

func LoadFromLookup(lookup lookupEnvFunc) (Config, error) {
	cfg := Default()

	if value, ok := lookup(HostEnvKey); ok && value != "" {
		cfg.Server.Host = value
	}

	if value, ok := lookup(PortEnvKey); ok && value != "" {
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("%s must be a valid TCP port: %q", PortEnvKey, value)
		}
		cfg.Server.Port = port
	}

	if value, ok := lookup(LogLevelEnvKey); ok && value != "" {
		var level slog.Level
		if err := level.UnmarshalText([]byte(value)); err != nil {
			return Config{}, fmt.Errorf("%s must be a valid slog level: %w", LogLevelEnvKey, err)
		}
		cfg.Logging.Level = level
	}

	if value, ok := lookup(BaseDirEnvKey); ok && value != "" {
		cfg.Paths.BaseDir = filepath.Clean(value)
		cfg.Paths.CacheDir = filepath.Join(cfg.Paths.BaseDir, "cache")
		cfg.Paths.PersistenceDir = filepath.Join(cfg.Paths.BaseDir, "persistence")
	}

	if value, ok := lookup(CacheDirEnvKey); ok && value != "" {
		cfg.Paths.CacheDir = filepath.Clean(value)
	}

	if value, ok := lookup(PersistenceDirEnvKey); ok && value != "" {
		cfg.Paths.PersistenceDir = filepath.Clean(value)
	}

	if value, ok := lookup(ShutdownTimeoutEnvKey); ok && value != "" {
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
