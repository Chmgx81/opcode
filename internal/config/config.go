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

	"github.com/Chmgx81/tilde/internal/tools"
)

// DefaultPermissionMode is used when config.json does not set one.
// Phase 0's gate always allows and only logs, so this value is carried but
// not yet enforced; permission modes land in Phase 2.
const DefaultPermissionMode = "ask"

// Config holds the preferences from ~/.tilde/config.json.
type Config struct {
	Model          string `json:"model"`
	PermissionMode string `json:"permission_mode"`
	// SafeCommands are shell command prefixes that run without
	// prompting even in ask mode (token-wise prefix match; see
	// tools.ShellAllowlist). Empty means every shell call prompts.
	SafeCommands []string `json:"safe_commands"`
	// ContextWindow is the model's context size in tokens; 0 (the
	// default) disables compaction. CompactionModel optionally names a
	// cheaper model for the summarizer round (empty = the main model).
	ContextWindow   int    `json:"context_window"`
	CompactionModel string `json:"compaction_model"`
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
	if cfg.ContextWindow < 0 {
		return cfg, fmt.Errorf("config.json: context_window must be 0 or positive")
	}
	// Canonicalize ("ask" -> ask-every-time) and refuse names that are
	// neither a mode nor an alias: an unrecognized mode must fail closed
	// at load time, not silently behave like something permissive.
	cfg.PermissionMode = tools.NormalizeMode(cfg.PermissionMode)
	if !tools.ValidMode(cfg.PermissionMode) {
		return cfg, fmt.Errorf("config.json: unknown permission_mode %q (valid: read-only, plan, ask-every-time, full-auto; legacy: ask, auto-accept-safe-ops)",
			cfg.PermissionMode)
	}
	return cfg, nil
}
