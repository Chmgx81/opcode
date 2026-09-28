package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltInCatalogResolution(t *testing.T) {
	dir := t.TempDir()
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "models.json"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// default_provider names a catalog entry with no providers block.
	write(`{"default_provider": "mistral"}`)
	mc, err := LoadModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mc.DefaultProvider != "mistral" {
		t.Fatalf("provider = %s", mc.DefaultProvider)
	}
	pc := mc.Provider()
	if pc.BaseURL != "https://api.mistral.ai/v1" {
		t.Errorf("mistral base URL = %s", pc.BaseURL)
	}

	// Anthropic resolves its wire API and explicit env var.
	write(`{"default_provider": "anthropic"}`)
	mc, err = LoadModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pc := mc.Provider(); pc.API != "anthropic" || pc.APIKeyEnv != "ANTHROPIC_API_KEY" ||
		pc.BaseURL != "https://api.anthropic.com" {
		t.Errorf("anthropic = %+v", pc)
	}

	// Google names the conventional Gemini variable.
	write(`{"default_provider": "google"}`)
	mc, err = LoadModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pc := mc.Provider(); pc.APIKeyEnv != "GEMINI_API_KEY" ||
		pc.BaseURL != "https://generativelanguage.googleapis.com/v1beta/openai" {
		t.Errorf("google = %+v", pc)
	}

	// An explicit entry wins over the catalog; empty fields merge.
	write(`{"default_provider": "mistral", "providers": {
		"mistral": {"base_url": "http://my-proxy:9/v1"},
		"groq": {}
	}}`)
	mc, err = LoadModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pc := mc.Provider(); pc.BaseURL != "http://my-proxy:9/v1" {
		t.Errorf("explicit base URL lost: %+v", pc)
	}
	if g := mc.Providers["groq"]; g.BaseURL != "https://api.groq.com/openai/v1" {
		t.Errorf("empty groq entry not merged from catalog: %+v", g)
	}

	// A sole configured provider is selected without default_provider.
	write(`{"providers": {"deepseek": {}}}`)
	mc, err = LoadModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mc.DefaultProvider != "deepseek" {
		t.Errorf("sole provider not selected: %s", mc.DefaultProvider)
	}
}
