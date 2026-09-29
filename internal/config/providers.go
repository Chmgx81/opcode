package config

import (
	"net"
	"net/url"
)

// IsLocalBaseURL reports whether a provider's base URL points at this
// machine (local servers need no API key). The URL's host is parsed
// and checked, not substring-matched: "https://localhost.evil.com"
// contains "localhost" but is not local (audit S9). An unparseable
// URL is not local — the check fails closed.
func IsLocalBaseURL(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ProviderSpec is one built-in provider: its base URL, its wire API
// ("openai" = any OpenAI-compatible chat/completions server, or
// "anthropic" = the Messages API), and an optional explicit env var.
// An empty APIKeyEnv means the derived rule applies (Phase 23:
// <NAME>_API_KEY, uppercased, dashes and dots to underscores).
type ProviderSpec struct {
	BaseURL   string
	API       string
	APIKeyEnv string
}

// BuiltInProviders is the catalog: name a provider in models.json's
// default_provider and it works with zero configuration — the base
// URL and credential rule are known. Explicit providers entries
// always win over the catalog, so custom endpoints and proxies stay
// in the user's hands. Phase 25 spec, docs/specs/phase25-providers.md.
var BuiltInProviders = map[string]ProviderSpec{
	"openrouter": {BaseURL: "https://openrouter.ai/api/v1", API: "openai", APIKeyEnv: "OPENROUTER_API_KEY"},
	"openai":     {BaseURL: "https://api.openai.com/v1", API: "openai"},
	"anthropic":  {BaseURL: "https://api.anthropic.com", API: "anthropic", APIKeyEnv: "ANTHROPIC_API_KEY"},
	"mistral":    {BaseURL: "https://api.mistral.ai/v1", API: "openai"},
	"google":     {BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", API: "openai", APIKeyEnv: "GEMINI_API_KEY"},
	"nvidia":     {BaseURL: "https://integrate.api.nvidia.com/v1", API: "openai"},
	"groq":       {BaseURL: "https://api.groq.com/openai/v1", API: "openai"},
	"deepseek":   {BaseURL: "https://api.deepseek.com/v1", API: "openai"},
	"together":   {BaseURL: "https://api.together.xyz/v1", API: "openai"},
	"cerebras":   {BaseURL: "https://api.cerebras.ai/v1", API: "openai"},
	"xai":        {BaseURL: "https://api.x.ai/v1", API: "openai"},
	"moonshot":   {BaseURL: "https://api.moonshot.ai/v1", API: "openai"},
	"fireworks":  {BaseURL: "https://api.fireworks.ai/inference/v1", API: "openai"},
	"qwen":       {BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", API: "openai"},
	"ollama":     {BaseURL: "http://localhost:11434/v1", API: "openai"},
}

// lookup fills a ProviderConfig from the catalog. Unknown names
// return false — a typo must not silently fall back to a default.
func lookup(name string) (ProviderConfig, bool) {
	spec, ok := BuiltInProviders[name]
	if !ok {
		return ProviderConfig{}, false
	}
	return ProviderConfig{
		BaseURL:   spec.BaseURL,
		API:       spec.API,
		APIKeyEnv: spec.APIKeyEnv,
	}, true
}
