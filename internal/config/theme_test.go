package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestSaveTheme: the theme key lands in config.json, a missing file is
// created, a previous theme is overwritten, and keys opcode does not
// know about survive the write (the file is edited as a raw object,
// not rewritten from the struct).
func TestSaveTheme(t *testing.T) {
	dir := t.TempDir()

	// No config.json yet: SaveTheme creates it.
	if err := SaveTheme(dir, "green"); err != nil {
		t.Fatalf("SaveTheme (create): %v", err)
	}
	var cfg map[string]any
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("saved config is not JSON: %v", err)
	}
	if cfg["theme"] != "green" {
		t.Errorf("theme = %v, want green", cfg["theme"])
	}
	if fi, err := os.Stat(filepath.Join(dir, "config.json")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("config.json perms: %v %v", fi.Mode().Perm(), err)
	}

	// An unknown sibling key must survive; the theme must overwrite.
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"theme": "green", "future_key": true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveTheme(dir, "light"); err != nil {
		t.Fatalf("SaveTheme (overwrite): %v", err)
	}
	cfg = nil
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["theme"] != "light" {
		t.Errorf("theme = %v, want light", cfg["theme"])
	}
	if cfg["future_key"] != true {
		t.Errorf("unknown sibling key lost: %v", cfg["future_key"])
	}

	// A config.json that is not a JSON object is an error, not a
	// silently clobbered file.
	if err := os.WriteFile(path, []byte(`["not", "an", "object"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveTheme(dir, "dark"); err == nil {
		t.Error("SaveTheme over a non-object config.json should fail")
	}
}

// TestReasoningEffortValidation: the knob's names are validated at
// load time — an unknown value fails loudly instead of silently
// sending garbage to the provider.
func TestReasoningEffortValidation(t *testing.T) {
	dir := t.TempDir()
	for _, valid := range []string{"", "low", "medium", "high"} {
		cfg := fmt.Sprintf(`{"reasoning_effort": %q}`, valid)
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(dir); err != nil {
			t.Errorf("valid effort %q rejected: %v", valid, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"reasoning_effort": "maximum"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(dir); err == nil {
		t.Error("unknown effort accepted")
	}
}
