// Package config loads tilde's user-level configuration.
//
// Phase 0 scope: user-level only (~/.tilde/ or $TILDE_HOME). Project-level
// config is not read at all; in particular, credentials are never loaded
// from a project-level .tilde/ directory (see auth.go).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultPermissionMode is used when config.json does not set one.
// Phase 0's gate always allows and only logs, so this value is carried but
// not yet enforced; permission modes land in Phase 2.
const DefaultPermissionMode = "ask"

// Config holds the preferences from ~/.tilde/config.json.
type Config struct {
	Model          string `json:"model"`
	PermissionMode string `json:"permission_mode"`
}

// UserDir returns the user-level tilde directory: $TILDE_HOME if set,
// otherwise ~/.tilde. It does not create the directory.
func UserDir() (string, error) {
	if dir := os.Getenv("TILDE_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".tilde"), nil
}

// LoadConfig reads config.json from dir. A missing file yields defaults,
// which is the common first-run case. A malformed file is an error: guessing
// what the user meant is worse than stopping.
func LoadConfig(dir string) (Config, error) {
	cfg := Config{PermissionMode: DefaultPermissionMode}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config.json: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config.json: %w", err)
	}
	if cfg.PermissionMode == "" {
		cfg.PermissionMode = DefaultPermissionMode
	}
	return cfg, nil
}
