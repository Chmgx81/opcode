// Command tilde runs the terminal coding agent. Phase 1: a Bubble Tea
// TUI over the UI-independent orchestrator (the same loop the Phase 0
// bare loop proved). main is wiring only.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tilde/internal/config"
	"tilde/internal/llm"
	"tilde/internal/orchestrator"
	"tilde/internal/tools"
	"tilde/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "tilde: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	userDir, err := config.UserDir()
	if err != nil {
		return err
	}
	cfg, err := config.LoadConfig(userDir)
	if err != nil {
		return err
	}
	models, err := config.LoadModels(userDir)
	if err != nil {
		return err
	}

	auth, warnings, err := config.LoadAuth(userDir)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	cwd, _ := os.Getwd()
	if path, found := config.RefusesToLoad(cwd); found {
		fmt.Fprintf(os.Stderr,
			"warning: ignoring credentials at %s — tilde never loads credentials from a project directory\n", path)
	}

	if cfg.Model == "" {
		return fmt.Errorf("no model configured; set \"model\" (e.g. \"anthropic/claude-sonnet-4.5\") in %s",
			filepath.Join(userDir, "config.json"))
	}

	providerName := models.DefaultProvider
	providerCfg := models.Provider()
	resolver := config.NewResolver(auth, providerCfg)
	key, hasKey, err := resolver.APIKey(providerName)
	if err != nil {
		return err
	}
	if providerCfg.BaseURL == "" {
		return fmt.Errorf("provider %q has no base_url configured in models.json", providerName)
	}
	// A keyless local server is fine; a keyless remote one will fail on
	// the first request, so say so up front instead.
	isLocal := strings.Contains(providerCfg.BaseURL, "localhost") ||
		strings.Contains(providerCfg.BaseURL, "127.0.0.1")
	if !hasKey && !isLocal && providerCfg.APIKeyEnv != "" {
		fmt.Fprintf(os.Stderr,
			"warning: no API key for provider %q (set %s, run tilde and use /login, or add it to %s)\n",
			providerName, providerCfg.APIKeyEnv, filepath.Join(userDir, "auth.json"))
	}

	if err := os.MkdirAll(userDir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", userDir, err)
	}
	auditPath := filepath.Join(userDir, "audit.jsonl")
	gate := &tools.Gate{
		Audit: tools.NewAuditLog(auditPath, tools.NewRedactor(key.Value)),
	}

	var registry tools.Registry
	registry.Register(tools.ReadFile{})
	registry.Register(tools.WriteFile{})
	registry.Register(tools.EditFile{})
	registry.Register(tools.RunShell{})

	provider := llm.NewOpenAICompat(providerCfg.BaseURL, key.Value)
	orch := orchestrator.New(provider, cfg.Model, systemPrompt(cwd), &registry, gate)

	ui := tui.New(tui.Options{
		Orch:         orch,
		Model:        cfg.Model,
		Mode:         cfg.PermissionMode,
		Cwd:          cwd,
		TildeHome:    userDir,
		ProviderName: providerName,
		BaseURL:      providerCfg.BaseURL,
		AuditPath:    auditPath,
	})
	// The gate's decision policy is wired after the UI exists: prompts
	// surface in the TUI and block the orchestrator until answered.
	gate.Decide = tools.PolicyDecide(cfg.PermissionMode, ui.Prompt())

	return tui.Run(ui)
}

func systemPrompt(cwd string) string {
	return "You are tilde, a terminal-based coding agent. You are working in: " + cwd + "\n" +
		"Use the available tools to accomplish the user's request. Prefer the smallest set of\n" +
		"actions that completes the task, and describe what you did when you finish."
}
