// Package config loads opcode's user-level configuration.
//
// Phase 0 scope: user-level only (~/.opcode/ or $OPCODE_HOME). Project-level
// config is not read at all; in particular, credentials are never loaded
// from a project-level .opcode/ directory (see auth.go).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Chmgx81/opcode/internal/tools"
)

// DefaultPermissionMode is used when config.json does not set one;
// the gate enforces it on every call.
const DefaultPermissionMode = "build"

// Config holds the preferences from ~/.opcode/config.json.
type Config struct {
	Model          string `json:"model"`
	PermissionMode string `json:"permission_mode"`
	// Animations disables animated UI (spinner, toast glyph burst)
	// when false. Nil/absent means animated — the default.
	Animations *bool `json:"animations"`
	// ContextWindow is the model's context size in tokens; 0 (the
	// default) disables compaction. CompactionModel optionally names a
	// cheaper model for the summarizer round (empty = the main model).
	ContextWindow   int    `json:"context_window"`
	CompactionModel string `json:"compaction_model"`
	// Sandbox confines shell-command writes with Landlock (Linux):
	// read+execute anywhere, writes only to the project dir, temp,
	// and dev caches. Nil/absent means enabled when the kernel
	// supports it; false opts out. Unavailable kernels degrade to
	// unsandboxed and say so in the startup notes.
	Sandbox *bool `json:"sandbox"`
	// Theme names the TUI palette (the tui package owns the valid
	// names; cmd/opcode validates at startup). Empty means auto: the
	// terminal's background is probed and the dark or light posture
	// follows it.
	Theme string `json:"theme"`
	// ReasoningEffort seeds the thinking knob: low, medium, high, or
	// empty for the provider's default. Unknown values fail loudly
	// below, like permission_mode.
	ReasoningEffort string `json:"reasoning_effort"`
	// UpdateChecks controls the startup update notice: opcode compares
	// the running version against the latest GitHub release (cached
	// 24h, a few KB, failures silent) and says so when a newer
	// release exists. Nil/absent means enabled; false opts out
	// entirely. OPCODE_NO_UPDATE_CHECK=1 opts out for one process.
	UpdateChecks *bool `json:"update_checks"`
}

// UserDir returns the user-level opcode directory: $OPCODE_HOME if set,
// otherwise ~/.opcode. It does not create the directory.
func UserDir() (string, error) {
	if dir := os.Getenv("OPCODE_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".opcode"), nil
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
	// Canonicalize legacy spellings ("ask" -> build, "read-only" ->
	// plan) and refuse names that are neither a mode nor an alias:
	// an unrecognized mode must fail closed at load time, not
	// silently behave like something permissive.
	cfg.PermissionMode = tools.NormalizeMode(cfg.PermissionMode)
	if !tools.ValidMode(cfg.PermissionMode) {
		return cfg, fmt.Errorf("config.json: unknown permission_mode %q (valid: plan, build, full-auto; legacy: ask, read-only, ask-every-time, auto-accept-safe-ops)",
			cfg.PermissionMode)
	}
	switch cfg.ReasoningEffort {
	case "", "low", "medium", "high":
	default:
		return cfg, fmt.Errorf("config.json: unknown reasoning_effort %q (valid: low, medium, high; empty = the provider default)",
			cfg.ReasoningEffort)
	}
	return cfg, nil
}

// SaveTheme writes the "theme" key into dir/config.json. The file is
// edited as a raw JSON object, not rewritten from the Config struct,
// so keys opcode does not know about survive; a missing file is
// created. The theme name itself is validated by the caller (the tui
// package owns the list) — this function persists, it does not judge.
func SaveTheme(dir, theme string) error {
	return saveJSONKey(filepath.Join(dir, "config.json"), "theme", theme)
}

// SaveModel persists the picked model into dir/config.json, so a model
// chosen in the UI (/model, /models) survives a restart instead of
// silently reverting to whatever the config file named — a first-run
// user who sets everything up in the UI must not find it gone. Same
// raw-edit rules as SaveTheme: unknown keys survive, a missing file is
// created, an existing file keeps its permissions.
func SaveModel(dir, model string) error {
	return saveJSONKey(filepath.Join(dir, "config.json"), "model", model)
}

// SaveDefaultProvider persists the picked provider into dir/models.json.
// The provider cannot live in config.json: models.json owns provider
// resolution (base URL, wire API, credential rule), and a model id alone
// is meaningless without its provider — persisting only the model would
// send, say, mistral's model id to openrouter on the next launch. The
// raw edit preserves any providers the user configured by hand.
func SaveDefaultProvider(dir, provider string) error {
	return saveJSONKey(filepath.Join(dir, "models.json"), "default_provider", provider)
}

// saveJSONKey sets one top-level key in a JSON object file, creating a
// missing file and preserving every other key, including ones opcode
// does not model. An existing file keeps its permissions; a new one
// starts private.
func saveJSONKey(path, key, value string) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	// A file the user made shareable (0644) stays that way; a new one
	// starts private.
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	raw := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parse %s: %w", filepath.Base(path), err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	raw[key] = value
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, append(data, '\n'), mode)
}

// ScreenReaderActive reports whether the environment indicates a
// screen reader. Detection is deliberately conservative — only the
// conventional signals (the SCREEN_READER variable, AT-SPI's
// atk-bridge in GTK_MODULES, the Flatpak accessibility flag) — because
// a false positive removes animation the user may want, while a false
// negative leaves a usable (if busier) UI. Codex probes and persists
// the preference; opcode seeds the session default and lets the
// config key win.
func ScreenReaderActive() bool {
	if v := os.Getenv("SCREEN_READER"); v != "" && v != "0" && v != "false" {
		return true
	}
	if strings.Contains(os.Getenv("GTK_MODULES"), "atk-bridge") {
		return true
	}
	return os.Getenv("ACCESSIBILITY_ENABLED") == "1"
}
