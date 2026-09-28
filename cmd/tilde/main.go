// Command tilde runs the terminal coding agent. Phase 1: a Bubble Tea
// TUI over the UI-independent orchestrator (the same loop the Phase 0
// bare loop proved). main is wiring only.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tilde/internal/config"
	"tilde/internal/llm"
	"tilde/internal/mcp"
	"tilde/internal/orchestrator"
	"tilde/internal/skills"
	"tilde/internal/subagent"
	"tilde/internal/tools"
	"tilde/internal/trust"
	"tilde/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "tilde: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// --trust (or TILDE_TRUST=1) pre-approves the project's executable
	// surface without prompting: the CI/headless posture (Section 7).
	preTrust := flag.Bool("trust", false, "trust the current project's executable surface without prompting")
	flag.Parse()

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

	// Project trust (Section 7): project-level skills only load once
	// the project's executable surface is approved. --trust pre-approves
	// for scripted use; otherwise the TUI asks, showing exactly what
	// would run.
	trustStore, err := trust.LoadStore(userDir)
	if err != nil {
		return err
	}
	if *preTrust || os.Getenv("TILDE_TRUST") != "" {
		if err := trustStore.Trust(cwd); err != nil {
			return err
		}
	}
	status, approved, err := trustStore.Status(cwd)
	if err != nil {
		return err
	}
	projectTrusted := status == trust.Trusted

	skillSources := func(trusted bool) []skills.Source {
		return []skills.Source{
			{Dir: filepath.Join(userDir, "skills"), Scope: skills.ScopeUser},
			{Dir: filepath.Join(cwd, ".tilde", "skills"), Scope: skills.ScopeProject, Trusted: trusted},
		}
	}
	var skillManager skills.Manager
	if err := skillManager.Load(skillSources(projectTrusted)); err != nil {
		return err
	}

	var registry tools.Registry
	registry.Register(tools.ReadFile{})
	registry.Register(tools.WriteFile{})
	registry.Register(tools.EditFile{})
	registry.Register(tools.RunShell{})
	registry.Register(tools.LoadSkill{Manager: &skillManager})
	registry.Register(tools.RunSkillScript{Manager: &skillManager})

	// MCP servers (Section 3.5): user-level always; the project's
	// mcp.json only when trusted — connecting it spawns a process,
	// which is exactly the trust gate's job. A grant mid-session
	// connects the project's servers without a restart.
	userMcp, err := mcp.LoadConfig(filepath.Join(userDir, "mcp.json"))
	if err != nil {
		return err
	}
	projectMcp, err := mcp.LoadConfig(filepath.Join(cwd, ".tilde", "mcp.json"))
	if err != nil {
		return err
	}
	mcpManager := mcp.NewManager()
	defer mcpManager.Close()

	var startupNotes []string
	userMcpTools, notes := mcpManager.Connect(context.Background(), userMcp)
	startupNotes = append(startupNotes, notes...)
	for _, t := range userMcpTools {
		registry.Register(t)
	}
	connectProjectMcp := func() {
		projectTools, notes := mcpManager.Connect(context.Background(), projectMcp)
		for _, t := range projectTools {
			registry.Register(t)
		}
		_ = notes // mid-session grants report through the transcript note
	}
	if projectTrusted {
		connectProjectMcp()
	}

	// Subagents (Section 3.3): another orchestrator instance per
	// spawn, same provider and gate (same trust boundary), narrower
	// prompt and tool subset. The emitter is settable so the TUI —
	// which does not exist yet — can become the sink right after it is
	// built.
	provider := llm.NewOpenAICompat(providerCfg.BaseURL, key.Value)
	spawnEmitter := &subagent.Emitter{}
	registry.Register(subagent.SpawnTool{Runner: &subagent.Runner{
		Provider: provider,
		Model:    cfg.Model,
		Registry: &registry,
		Gate:     gate,
	}, Emitter: spawnEmitter})

	orch := orchestrator.New(provider, cfg.Model, systemPrompt(cwd), &registry, gate)
	orch.SetMode(cfg.PermissionMode)
	orch.SkillsIndex = skillManager.Index()

	// If the project has an unapproved executable surface, the TUI asks
	// first. Granting persists the decision and re-discovers skills into
	// the same manager, so the tools and the system prompt pick up the
	// project's skills immediately. This must be part of Options before
	// tui.New: the prompt is shown from the first frame.
	var pending *tui.TrustDecision
	if status != trust.Trusted && len(approved) > 0 {
		pending = &tui.TrustDecision{
			ProjectDir: cwd,
			Approved:   approved,
			OnAnswer: func(trusted bool) {
				if !trusted {
					return
				}
				if err := trustStore.Trust(cwd); err != nil {
					return
				}
				if err := skillManager.Load(skillSources(true)); err != nil {
					return
				}
				orch.SkillsIndex = skillManager.Index()
				connectProjectMcp()
			},
		}
	}

	ui := tui.New(tui.Options{
		Orch:         orch,
		Model:        cfg.Model,
		Mode:         cfg.PermissionMode,
		Cwd:          cwd,
		TildeHome:    userDir,
		ProviderName: providerName,
		BaseURL:      providerCfg.BaseURL,
		AuditPath:    auditPath,
		PendingTrust: pending,
		StartupNotes: startupNotes,
	})
	// The gate's decision policy is wired after the UI exists: prompts
	// surface in the TUI and block the orchestrator until answered.
	gate.Decide = tools.PolicyDecide(cfg.PermissionMode, ui.Prompt())

	// Subagent progress flows into the transcript as labeled lines.
	sink := ui.SubagentSink()
	spawnEmitter.Set(func(ev subagent.Event) {
		sink(ev.Title, ev.Kind, ev.Text, ev.Usage)
	})

	return tui.Run(ui)
}

func systemPrompt(cwd string) string {
	return "You are tilde, a terminal-based coding agent. You are working in: " + cwd + "\n" +
		"Use the available tools to accomplish the user's request. Prefer the smallest set of\n" +
		"actions that completes the task, and describe what you did when you finish."
}
