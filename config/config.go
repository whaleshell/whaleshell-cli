// Package config loads CLI process configuration from env and files.
package config

import (
	"os"
	"path/filepath"
	"strings"
)

// Config is the process-level CLI configuration (not per-command argv).
type Config struct {
	// ConfigDir is ~/.config/whaleshell (or WHALESHELL_CONFIG_DIR).
	ConfigDir string
	// Driver overrides WHALESHELL_DRIVER (docker|podman|…).
	Driver string
	// GatewayURL overrides WHALESHELL_GATEWAY_URL.
	GatewayURL string
	// HostInternal is the ExtraHosts name for host services (default host.whaleshell.internal).
	HostInternal string
	// LogLevel from WHALESHELL_LOG_LEVEL.
	LogLevel string
	// LogFormat from WHALESHELL_LOG_FORMAT.
	LogFormat string
}

// Load reads env defaults. File-backed gateway tokens live in storage/gwconfig.
func Load() Config {
	cfg := Config{
		ConfigDir:    strings.TrimSpace(os.Getenv("WHALESHELL_CONFIG_DIR")),
		Driver:       strings.TrimSpace(os.Getenv("WHALESHELL_DRIVER")),
		GatewayURL:   strings.TrimSpace(os.Getenv("WHALESHELL_GATEWAY_URL")),
		HostInternal: strings.TrimSpace(os.Getenv("WHALESHELL_HOST_INTERNAL")),
		LogLevel:     strings.TrimSpace(os.Getenv("WHALESHELL_LOG_LEVEL")),
		LogFormat:    strings.TrimSpace(os.Getenv("WHALESHELL_LOG_FORMAT")),
	}
	if cfg.ConfigDir == "" {
		home, _ := os.UserHomeDir()
		cfg.ConfigDir = filepath.Join(home, ".config", "whaleshell")
	}
	if cfg.HostInternal == "" {
		cfg.HostInternal = "host.whaleshell.internal"
	}
	return cfg
}
