package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultProviderName is used when models.json does not name one. Its
// settings below are also the built-in fallbacks, so opcode talks to
// OpenRouter with zero configuration.
const (
	DefaultProviderName = "openrouter"
	DefaultBaseURL      = "https://openrouter.ai/api/v1"
	DefaultAPIKeyEnv    = "OPENROUTER_API_KEY"
)

// ProviderConfig is one entry of models.json. BaseURL is any
// OpenAI-compatible chat/completions endpoint (or a Messages-API
// endpoint when API is "anthropic"); APIKeyEnv names the
// environment variable used as the credential fallback (empty means
// the derived <NAME>_API_KEY rule, and for a local server it means
// no key needed).
type ProviderConfig struct {
	BaseURL   string   `json:"base_url"`
	API       string   `json:"api"` // "openai" (default) or "anthropic"
	APIKeyEnv string   `json:"api_key_env"`
	Models    []string `json:"models"`
}

// ModelsConfig is the parsed models.json.
type ModelsConfig struct {
	DefaultProvider string                    `json:"default_provider"`
	Providers       map[string]ProviderConfig `json:"providers"`
}

// LoadModels reads models.json from dir. A missing file yields the built-in
// OpenRouter default, so the local-model case (Ollama, vLLM, ...) is a
// config edit rather than a code change.
func LoadModels(dir string) (ModelsConfig, error) {
	mc := ModelsConfig{Providers: map[string]ProviderConfig{}}
	data, err := os.ReadFile(filepath.Join(dir, "models.json"))
	if os.IsNotExist(err) {
		mc.applyDefaults("")
		return mc, nil
	}
	if err != nil {
		return mc, fmt.Errorf("read models.json: %w", err)
	}
	if err := json.Unmarshal(data, &mc); err != nil {
		return mc, fmt.Errorf("parse models.json: %w", err)
	}
	mc.applyDefaults(mc.DefaultProvider)
	return mc, nil
}

// applyDefaults fills provider entries from the built-in catalog and
// selects the provider to use: an explicit default_provider, else the
// sole configured provider, else OpenRouter. An explicit providers
// entry always wins over the catalog unless it is empty, in which case
// the catalog fills in what the user left out.
func (mc *ModelsConfig) applyDefaults(chosen string) {
	if mc.Providers == nil {
		mc.Providers = map[string]ProviderConfig{}
	}
	// Catalog merge: a named provider with no explicit settings (or
	// one that names only some) gets the catalog's base URL, wire API,
	// and credential rule for whatever it left empty.
	for name, pc := range mc.Providers {
		if cat, ok := lookup(name); ok {
			if pc.BaseURL == "" {
				pc.BaseURL = cat.BaseURL
			}
			if pc.API == "" {
				pc.API = cat.API
			}
			if pc.APIKeyEnv == "" {
				pc.APIKeyEnv = cat.APIKeyEnv
			}
			mc.Providers[name] = pc
		}
	}

	switch {
	case chosen != "":
		if _, ok := mc.Providers[chosen]; !ok {
			if cat, ok := lookup(chosen); ok {
				mc.Providers[chosen] = cat
			}
		}
		mc.DefaultProvider = chosen
	case len(mc.Providers) == 1:
		for name := range mc.Providers {
			mc.DefaultProvider = name
		}
	default:
		mc.DefaultProvider = DefaultProviderName
	}

	// The OpenRouter default must always exist as a usable entry.
	if _, ok := mc.Providers[DefaultProviderName]; !ok {
		mc.Providers[DefaultProviderName] = ProviderConfig{
			BaseURL:   DefaultBaseURL,
			APIKeyEnv: DefaultAPIKeyEnv,
		}
	}
}

// Provider returns the config of the provider that requests go to.
func (mc *ModelsConfig) Provider() ProviderConfig {
	return mc.Providers[mc.DefaultProvider]
}
