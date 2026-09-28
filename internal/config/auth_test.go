package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolverPrefersAuthFileOverEnv(t *testing.T) {
	t.Setenv("TILDE_TEST_KEY", "env-key")
	auth := AuthConfig{"openrouter": "file-key"}
	r := NewResolver(auth, ProviderConfig{APIKeyEnv: "TILDE_TEST_KEY"})

	key, ok, err := r.APIKey("openrouter")
	if err != nil || !ok {
		t.Fatalf("APIKey: ok=%v err=%v", ok, err)
	}
	if key.Value != "file-key" {
		t.Errorf("Value = %q, want file-key (auth.json must win over env)", key.Value)
	}
	if key.Source != "auth.json" {
		t.Errorf("Source = %q, want auth.json", key.Source)
	}
}

func TestResolverFallsBackToEnv(t *testing.T) {
	t.Setenv("TILDE_TEST_KEY", "env-key")
	r := NewResolver(AuthConfig{}, ProviderConfig{APIKeyEnv: "TILDE_TEST_KEY"})

	key, ok, err := r.APIKey("openrouter")
	if err != nil || !ok {
		t.Fatalf("APIKey: ok=%v err=%v", ok, err)
	}
	if key.Value != "env-key" || key.Source != "environment" {
		t.Errorf("got %q from %q, want env-key from environment", key.Value, key.Source)
	}
}

func TestResolverNoKeyAnywhere(t *testing.T) {
	r := NewResolver(AuthConfig{}, ProviderConfig{APIKeyEnv: "TILDE_UNSET_VAR"})
	_, ok, err := r.APIKey("openrouter")
	if err != nil {
		t.Fatalf("APIKey: %v", err)
	}
	if ok {
		t.Error("ok = true, want false when neither auth.json nor env has a key")
	}
}

func TestResolverRunsCommandForm(t *testing.T) {
	auth := AuthConfig{"openrouter": "!printf 'cmd-key'"}
	r := NewResolver(auth, ProviderConfig{})

	key, ok, err := r.APIKey("openrouter")
	if err != nil || !ok {
		t.Fatalf("APIKey: ok=%v err=%v", ok, err)
	}
	if key.Value != "cmd-key" {
		t.Errorf("Value = %q, want cmd-key", key.Value)
	}
	if key.Source != "auth.json command" {
		t.Errorf("Source = %q, want auth.json command", key.Source)
	}
}

func TestResolverCachesCommandResult(t *testing.T) {
	// The command prints a counter file's contents only once; a second
	// resolution must not re-run it.
	dir := t.TempDir()
	marker := filepath.Join(dir, "runs")
	auth := AuthConfig{"openrouter": "!printf x >> " + marker + " && printf cmd-key"}
	r := NewResolver(auth, ProviderConfig{})

	for i := 0; i < 2; i++ {
		key, ok, err := r.APIKey("openrouter")
		if err != nil || !ok || key.Value != "cmd-key" {
			t.Fatalf("call %d: ok=%v err=%v key=%q", i, ok, err, key.Value)
		}
	}
	data, _ := os.ReadFile(marker)
	if got := len(strings.TrimSpace(string(data))); got != 1 {
		t.Errorf("command ran %d times, want 1 (cached for process lifetime)", got)
	}
}

func TestResolverFailedCommandIsErrorNotFallback(t *testing.T) {
	t.Setenv("TILDE_TEST_KEY", "env-key")
	// A failing secret-manager command must not silently fall back to a
	// less secure source (Section 3.10).
	auth := AuthConfig{"openrouter": "!printf ''"}
	r := NewResolver(auth, ProviderConfig{APIKeyEnv: "TILDE_TEST_KEY"})

	_, ok, err := r.APIKey("openrouter")
	if err == nil {
		t.Fatal("expected error for empty !command output, got nil")
	}
	if ok {
		t.Error("ok = true, want false")
	}
}

func TestResolverRefusesProjectLevelKey(t *testing.T) {
	// A project .tilde/auth.json exists with a key. Resolution must not
	// use it: no auth loaded from user level, no env var, so there is no
	// key — even though one is sitting right there in the project.
	project := t.TempDir()
	writeFile(t, filepath.Join(project, ".tilde", "auth.json"),
		`{"openrouter": "project-key"}`, 0o600)

	path, found := RefusesToLoad(project)
	if !found {
		t.Fatal("RefusesToLoad did not detect the project-level auth.json")
	}
	if filepath.Base(path) != "auth.json" {
		t.Errorf("reported path = %q", path)
	}

	r := NewResolver(AuthConfig{}, ProviderConfig{APIKeyEnv: "TILDE_UNSET_VAR"})
	key, ok, err := r.APIKey("openrouter")
	if err != nil {
		t.Fatalf("APIKey: %v", err)
	}
	if ok {
		t.Errorf("resolved a key (%q) — it must never come from a project directory", key.Source)
	}
	if _, found := RefusesToLoad(t.TempDir()); found {
		t.Error("RefusesToLoad found a file in an empty directory")
	}
}

func TestLoadAuthWarnsOnLoosePermissions(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "auth.json"), `{"openrouter": "file-key"}`, 0o644)

	auth, warnings, err := LoadAuth(dir)
	if err != nil {
		t.Fatalf("LoadAuth: %v", err)
	}
	if auth["openrouter"] != "file-key" {
		t.Errorf("auth.json not loaded: %v", auth)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v, want one for the directory and one for the file", warnings)
	}
	for _, w := range warnings {
		if strings.Contains(w, "file-key") {
			t.Errorf("warning leaks the key value: %q", w)
		}
	}
}

func TestLoadAuthQuietOnCorrectPermissions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "auth.json"), `{"openrouter": "file-key"}`, 0o600)
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	_, warnings, err := LoadAuth(dir)
	if err != nil {
		t.Fatalf("LoadAuth: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
}

func TestLoadAuthMissingFileIsEmpty(t *testing.T) {
	auth, warnings, err := LoadAuth(t.TempDir())
	if err != nil {
		t.Fatalf("LoadAuth: %v", err)
	}
	if auth != nil {
		t.Errorf("auth = %v, want nil", auth)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
}
