package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultProviderName is used when models.json does not name one. Its
// settings below are also the built-in fallbacks, so tilde talks to
// OpenRouter with zero configuration.
const (
	DefaultProviderName = "openrouter"
	DefaultBaseURL      = "https://openrouter.ai/api/v1"
	DefaultAPIKeyEnv    = "OPENROUTER_API_KEY"
)

// ProviderConfig is one entry of models.json. BaseURL is any
// OpenAI-compatible chat/completions endpoint; APIKeyEnv names the
// environment variable used as the credential fallback (empty means the
// provider needs no key, e.g. a local server).
type ProviderConfig struct {
	BaseURL   string   `json:"base_url"`
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

// applyDefaults fills the default provider entry and selects the provider
// to use: an explicit choice, else the sole configured provider, else
// OpenRouter.
func (mc *ModelsConfig) applyDefaults(chosen string) {
	if mc.Providers == nil {
		mc.Providers = map[string]ProviderConfig{}
	}
	if _, ok := mc.Providers[DefaultProviderName]; !ok {
		mc.Providers[DefaultProviderName] = ProviderConfig{
			BaseURL:   DefaultBaseURL,
			APIKeyEnv: DefaultAPIKeyEnv,
		}
	}
	if chosen != "" {
		mc.DefaultProvider = chosen
		return
	}
	if len(mc.Providers) == 1 {
		for name := range mc.Providers {
			mc.DefaultProvider = name
		}
		return
	}
	mc.DefaultProvider = DefaultProviderName
}

// Provider returns the config of the provider that requests go to.
func (mc *ModelsConfig) Provider() ProviderConfig {
	return mc.Providers[mc.DefaultProvider]
}
