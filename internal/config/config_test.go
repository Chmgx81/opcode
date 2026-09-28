package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Chmgx81/tilde/internal/tools"
)

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigMissingFileGivesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.PermissionMode != DefaultPermissionMode {
		t.Errorf("PermissionMode = %q, want default %q", cfg.PermissionMode, DefaultPermissionMode)
	}
	if cfg.Model != "" {
		t.Errorf("Model = %q, want empty", cfg.Model)
	}
}

func TestLoadConfigReadsPreferences(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.json"),
		`{"model": "anthropic/claude-sonnet-4.5", "permission_mode": "full-auto"}`, 0o600)
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Model != "anthropic/claude-sonnet-4.5" {
		t.Errorf("Model = %q", cfg.Model)
	}
	if cfg.PermissionMode != "full-auto" {
		t.Errorf("PermissionMode = %q", cfg.PermissionMode)
	}
}

func TestLoadConfigMalformedFileIsError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.json"), `{not json`, 0o600)
	if _, err := LoadConfig(dir); err == nil {
		t.Fatal("expected error for malformed config.json, got nil")
	}
}

func TestLoadModelsMissingFileGivesOpenRouterDefault(t *testing.T) {
	mc, err := LoadModels(t.TempDir())
	if err != nil {
		t.Fatalf("LoadModels: %v", err)
	}
	if mc.DefaultProvider != DefaultProviderName {
		t.Errorf("DefaultProvider = %q, want %q", mc.DefaultProvider, DefaultProviderName)
	}
	p := mc.Provider()
	if p.BaseURL != DefaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", p.BaseURL, DefaultBaseURL)
	}
	if p.APIKeyEnv != DefaultAPIKeyEnv {
		t.Errorf("APIKeyEnv = %q, want %q", p.APIKeyEnv, DefaultAPIKeyEnv)
	}
}

func TestLoadModelsCustomLocalProvider(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "models.json"),
		`{"default_provider": "local", "providers": {"local": {"base_url": "http://localhost:11434/v1", "models": ["llama3"]}}}`, 0o644)
	mc, err := LoadModels(dir)
	if err != nil {
		t.Fatalf("LoadModels: %v", err)
	}
	if mc.DefaultProvider != "local" {
		t.Fatalf("DefaultProvider = %q, want local", mc.DefaultProvider)
	}
	p := mc.Provider()
	if p.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("BaseURL = %q", p.BaseURL)
	}
	if p.APIKeyEnv != "" {
		t.Errorf("APIKeyEnv = %q, want empty (no key needed)", p.APIKeyEnv)
	}
}

func TestLoadConfigModeNormalizationAndValidation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.json"),
		`{"model": "m", "permission_mode": "ask"}`, 0o600)
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("legacy \"ask\" must keep working: %v", err)
	}
	if cfg.PermissionMode != tools.ModeAsk {
		t.Errorf("PermissionMode = %q, want ask", cfg.PermissionMode)
	}

	writeFile(t, filepath.Join(dir, "config.json"),
		`{"model": "m", "permission_mode": "full-auto"}`, 0o600)
	cfg, err = LoadConfig(dir)
	if err != nil || cfg.PermissionMode != tools.ModeFullAuto {
		t.Errorf("full-auto rejected or mangled: %v %q", err, cfg.PermissionMode)
	}

	writeFile(t, filepath.Join(dir, "config.json"),
		`{"model": "m", "permission_mode": "yolo"}`, 0o600)
	if _, err := LoadConfig(dir); err == nil {
		t.Error("unknown mode must fail at load time, not fail open later")
	}
}

func TestLoadConfigCompactionKeys(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.json"),
		`{"model": "m", "context_window": 128000, "compaction_model": "cheap/sum"}`, 0o600)
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ContextWindow != 128000 || cfg.CompactionModel != "cheap/sum" {
		t.Errorf("cfg = %+v", cfg)
	}
	// Default is 0 (disabled).
	writeFile(t, filepath.Join(dir, "config.json"), `{"model": "m"}`, 0o600)
	cfg, err = LoadConfig(dir)
	if err != nil || cfg.ContextWindow != 0 {
		t.Errorf("default context_window = %d err = %v", cfg.ContextWindow, err)
	}
	// Negative is rejected loudly.
	writeFile(t, filepath.Join(dir, "config.json"), `{"model": "m", "context_window": -5}`, 0o600)
	if _, err := LoadConfig(dir); err == nil {
		t.Error("negative context_window must fail at load")
	}
}

func TestScreenReaderActive(t *testing.T) {
	t.Setenv("SCREEN_READER", "")
	t.Setenv("GTK_MODULES", "")
	t.Setenv("ACCESSIBILITY_ENABLED", "")
	if ScreenReaderActive() {
		t.Error("no signals: not active")
	}
	t.Setenv("SCREEN_READER", "1")
	if !ScreenReaderActive() {
		t.Error("SCREEN_READER=1 must be active")
	}
	t.Setenv("SCREEN_READER", "0")
	if ScreenReaderActive() {
		t.Error("SCREEN_READER=0 must not be active")
	}
	t.Setenv("SCREEN_READER", "")
	t.Setenv("GTK_MODULES", "canberra-gtk-module:atk-bridge")
	if !ScreenReaderActive() {
		t.Error("atk-bridge in GTK_MODULES must be active")
	}
	t.Setenv("GTK_MODULES", "")
	t.Setenv("ACCESSIBILITY_ENABLED", "1")
	if !ScreenReaderActive() {
		t.Error("ACCESSIBILITY_ENABLED=1 must be active")
	}
}
