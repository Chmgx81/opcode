// Command tilde is Phase 0's bare input/output loop: type a message, watch
// the streamed response and tool calls. No TUI framework — logic lives in
// the packages it drives, so the Phase 1 Bubble Tea front end and the
// headless mode can reuse it unchanged.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tilde/internal/config"
	"tilde/internal/llm"
	"tilde/internal/orchestrator"
	"tilde/internal/tools"
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
			"warning: no API key for provider %q (set %s or add it to %s)\n",
			providerName, providerCfg.APIKeyEnv, filepath.Join(userDir, "auth.json"))
	}

	if err := os.MkdirAll(userDir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", userDir, err)
	}
	gate := &tools.Gate{
		Audit: tools.NewAuditLog(filepath.Join(userDir, "audit.jsonl"),
			tools.NewRedactor(key.Value)),
	}

	var registry tools.Registry
	registry.Register(tools.ReadFile{})
	registry.Register(tools.WriteFile{})
	registry.Register(tools.EditFile{})
	registry.Register(tools.RunShell{})

	provider := llm.NewOpenAICompat(providerCfg.BaseURL, key.Value)
	orch := orchestrator.New(provider, cfg.Model, systemPrompt(cwd), &registry, gate)

	keyNote := key.Source
	if keyNote == "" {
		keyNote = "no key"
	}
	fmt.Printf("tilde — model %s via %s (key from %s)\n", cfg.Model, providerCfg.BaseURL, keyNote)
	fmt.Println("Type a message; 'exit' or Ctrl-D to quit.")

	ctx := context.Background()
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n> ")
		if !scanner.Scan() {
			fmt.Println()
			return nil
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			return nil
		}
		render(orch.Send(ctx, input))
	}
}

// render prints one turn's events: text as it streams, one line per tool
// call and result, errors on stderr.
func render(events <-chan orchestrator.Event) {
	for ev := range events {
		switch ev.Kind {
		case orchestrator.EventText:
			fmt.Print(ev.Text)
		case orchestrator.EventToolStart:
			fmt.Printf("\n[tool] %s %s\n", ev.ToolCall.Name, ev.ToolCall.Arguments)
		case orchestrator.EventToolResult:
			fmt.Printf("[tool result] %s\n", oneline(ev.ToolResult))
		case orchestrator.EventTurnComplete:
			fmt.Println()
		case orchestrator.EventError:
			fmt.Fprintf(os.Stderr, "error: %v\n", ev.Err)
		}
	}
}

func oneline(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return strings.ReplaceAll(s, "\n", " \\n ")
}

func systemPrompt(cwd string) string {
	return "You are tilde, a terminal-based coding agent. You are working in: " + cwd + "\n" +
		"Use the available tools to accomplish the user's request. Prefer the smallest set of\n" +
		"actions that completes the task, and describe what you did when you finish."
}
