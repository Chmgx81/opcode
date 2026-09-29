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
	"runtime/debug"
	"strings"

	"github.com/Chmgx81/tilde/internal/config"
	"github.com/Chmgx81/tilde/internal/headless"
	"github.com/Chmgx81/tilde/internal/llm"
	"github.com/Chmgx81/tilde/internal/mcp"
	"github.com/Chmgx81/tilde/internal/orchestrator"
	"github.com/Chmgx81/tilde/internal/sandbox"
	"github.com/Chmgx81/tilde/internal/session"
	"github.com/Chmgx81/tilde/internal/skills"
	"github.com/Chmgx81/tilde/internal/subagent"
	"github.com/Chmgx81/tilde/internal/tools"
	"github.com/Chmgx81/tilde/internal/trust"
	"github.com/Chmgx81/tilde/internal/tui"
)

func main() {
	// The __sandbox child comes first, before anything spawns threads:
	// Landlock confines the calling thread, so the child must be
	// single-threaded when it applies the ruleset (Phase 21 spec).
	if len(os.Args) > 1 && os.Args[1] == "__sandbox" {
		if err := sandbox.Child(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "tilde __sandbox: %v\n", err)
			os.Exit(1)
		}
		return // Child replaced the process on success
	}
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "tilde: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// --trust (or TILDE_TRUST=1) pre-approves the project's executable
	// surface without prompting: the CI/headless posture (Section 7).
	preTrust := flag.Bool("trust", false, "trust the current project's executable surface without prompting")
	prompt := flag.String("p", "", "headless: run one turn with this prompt and exit")
	jsonOut := flag.Bool("json", false, "headless: emit one JSON event object per line")
	resume := flag.String("resume", "", "resume the session at this path")
	cont := flag.Bool("continue", false, "resume the latest session")
	showVersion := flag.Bool("version", false, "print the version and exit")
	plain := flag.Bool("plain", false, "ASCII glyphs and no animation — the screen-reader posture")
	showHelp := flag.Bool("help", false, "show usage and exit")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `tilde — a terminal coding agent

Usage:
  tilde [flags]              start the interactive TUI in this directory
  tilde -p "prompt"          run one headless turn and exit

Flags:
  -p <prompt>    headless: run one turn with this prompt
  --json         headless: emit one JSON event object per line
  --resume PATH  resume the session at this path
  --continue     resume the latest session
  --trust        pre-approve the project's executable surface (CI posture)
  --version      print the version and exit
  --help         show this help and exit

Everything is configured in ~/.tilde/ (config.json, models.json,
auth.json). First run? Set a model, then start tilde in a project
directory — see https://github.com/Chmgx81/tilde#quick-start
`)
	}
	flag.Parse()

	if *showHelp {
		flag.Usage()
		return nil
	}

	if *showVersion {
		fmt.Printf("tilde %s\n", buildVersion())
		return nil
	}

	userDir, err := config.UserDir()
	if err != nil {
		return err
	}
	cfg, err := config.LoadConfig(userDir)
	if err != nil {
		return err
	}
	// The tui package owns the palette list; an unknown name fails
	// loudly here rather than silently probing (an explicit choice
	// that renders wrong is a bug report in the making).
	if cfg.Theme != "" && !tui.ValidTheme(cfg.Theme) {
		return fmt.Errorf("config.json: unknown theme %q (valid: %s)",
			cfg.Theme, strings.Join(tui.ThemeNames(), ", "))
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
	registry.Register(tools.ListDir{})
	registry.Register(tools.Grep{})
	registry.Register(tools.WebFetch{})
	registry.Register(tools.Glob{})
	registry.Register(tools.CurrentTime{})
	registry.Register(tools.ApplyPatch{})
	registry.Register(tools.WriteFile{})
	registry.Register(tools.EditFile{})
	registry.Register(tools.Bash{})
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
	provider := llm.New(providerCfg.API, providerCfg.BaseURL, key.Value)
	spawnEmitter := &subagent.Emitter{}
	// orch is created below, after the runner: the subagent's effort
	// inheritance reads it through this closure, so mid-session
	// cycles reach later spawns too.
	var orch *orchestrator.Orchestrator
	subRunner := &subagent.Runner{
		Provider: provider,
		Model:    cfg.Model,
		Registry: &registry,
		Gate:     gate,
		EffortOf: func() string { return orch.ReasoningEffort },
	}
	registry.Register(subagent.SpawnTool{Runner: subRunner, Emitter: spawnEmitter})

	// present_plan (Draft-Only): plan mode's exit. The Approve
	// callback is wired after the UI exists, like the gate's prompt.
	planTool := &tools.PresentPlan{}
	registry.Register(planTool)

	// todo_write (Draft-Only): the model's live task list. The shared
	// state's OnChange is wired after the UI exists; headless keeps it
	// nil (state updates, nothing to repaint).
	todoList := &tools.TodoList{}
	registry.Register(tools.TodoWrite{List: todoList})

	orch = orchestrator.New(provider, cfg.Model, systemPrompt(userDir, cwd), &registry, gate)
	orch.ReasoningEffort = cfg.ReasoningEffort
	orch.SetMode(cfg.PermissionMode)
	orch.SkillsIndex = skillManager.Index()
	orch.ContextWindow = cfg.ContextWindow
	orch.CompactionModel = cfg.CompactionModel

	// Session persistence (Section 3.7): --resume/--continue seeds
	// the orchestrator's history from a saved session; every run saves
	// its tree on exit with credentials redacted.
	var resumeNote string
	if *resume != "" || *cont {
		var s *session.Session
		var path string
		var err error
		if *resume != "" {
			s, err = session.Load(*resume)
			path = *resume
		} else {
			s, path, err = session.Latest(session.Dir(userDir))
			if s == nil && err == nil {
				return fmt.Errorf("no saved sessions in %s", session.Dir(userDir))
			}
		}
		if err != nil {
			return err
		}
		orch.Seed(s.History())
		resumeNote = fmt.Sprintf("resumed session %s (%d messages)", filepath.Base(path), len(s.History()))
	}

	// Screen-reader posture (Codex borrow): a detected reader — or
	// --plain, or TILDE_PLAIN — reduces motion for the session when
	// the user has not chosen explicitly; the config key always
	// wins, and nothing is persisted silently.
	plainMode := *plain || os.Getenv("TILDE_PLAIN") != "" || config.ScreenReaderActive()
	if plainMode {
		adapted := "--plain posture: ASCII glyphs"
		if cfg.Animations == nil {
			off := false
			cfg.Animations = &off
			adapted += ", animations off"
		}
		startupNotes = append(startupNotes, adapted)
	} else if cfg.Animations == nil && config.ScreenReaderActive() {
		off := false
		cfg.Animations = &off
		startupNotes = append(startupNotes, "screen reader detected — animations off (set \"animations\": true to override)")
	}

	// Landlock sandbox (Phase 21): confine shell-command writes to the
	// project dir, temp, and dev caches. Enabled by default where the
	// kernel supports it; the note always states the real posture.
	sandboxOn := cfg.Sandbox == nil || *cfg.Sandbox
	runner, err := sandbox.New(sandboxOn)
	if err != nil {
		// Not fatal: run unsandboxed and say so.
		startupNotes = append(startupNotes, "sandbox: unavailable ("+err.Error()+") — commands run unsandboxed")
		runner = nil
	}
	sandbox.Install(runner)
	startupNotes = append(startupNotes, runner.Status())

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

	// saveSession snapshots the conversation into the tree-structured
	// session file, credentials redacted (Section 3.7).
	saveSession := func() {
		s := session.FromHistory(cfg.Model, cfg.PermissionMode, orch.History())
		if err := s.Save(session.NewFile(userDir), []string{key.Value}); err != nil {
			fmt.Fprintf(os.Stderr, "tilde: could not save session: %v\n", err)
		}
	}

	// switchModel backs the TUI's /model picker: it rebuilds the
	// provider for the new provider/model pair everywhere it lives —
	// orchestrator, subagent runner, audit redactor — resolving the new
	// provider's key the same way startup does.
	switchModel := func(providerName, model string) error {
		pc, ok := models.Providers[providerName]
		if !ok {
			// A built-in catalog provider that models.json doesn't list
			// (the /models picker offers all of them).
			spec, inCatalog := config.BuiltInProviders[providerName]
			if !inCatalog {
				return fmt.Errorf("unknown provider %q", providerName)
			}
			pc = config.ProviderConfig{BaseURL: spec.BaseURL, API: spec.API, APIKeyEnv: spec.APIKeyEnv}
		}
		if pc.BaseURL == "" {
			return fmt.Errorf("provider %q has no base_url configured", providerName)
		}
		if model == "" {
			return fmt.Errorf("no model given")
		}
		r := config.NewResolver(auth, pc)
		k, hasKey, err := r.APIKey(providerName)
		if err != nil {
			return err
		}
		if !hasKey && pc.APIKeyEnv != "" &&
			!strings.Contains(pc.BaseURL, "localhost") && !strings.Contains(pc.BaseURL, "127.0.0.1") {
			return fmt.Errorf("no API key for provider %q (set %s, /login, or auth.json)",
				providerName, pc.APIKeyEnv)
		}
		newProvider := llm.New(pc.API, pc.BaseURL, k.Value)
		orch.Provider = newProvider
		orch.Model = model
		subRunner.Provider = newProvider
		subRunner.Model = model
		// The redactor protects the stored key; the new key is a new
		// secret to redact.
		gate.Audit = tools.NewAuditLog(auditPath, tools.NewRedactor(k.Value))
		return nil
	}

	// resumeSession re-seeds the orchestrator from a saved session.
	resumeSession := func(path string) (int, error) {
		s, err := session.Load(path)
		if err != nil {
			return 0, err
		}
		orch.Seed(s.History())
		return len(s.History()), nil
	}

	// Headless (Section 3.9): one turn, no UI. Permissions fail closed
	// with nobody to ask; use full-auto for unattended automation.
	if *prompt != "" {
		gate.Decide = tools.PolicyDecide(cfg.PermissionMode, nil)
		if resumeNote != "" && !*jsonOut {
			fmt.Fprintf(os.Stderr, "~ %s\n", resumeNote)
		}
		spawnEmitter.Set(func(ev subagent.Event) {
			if *jsonOut {
				fmt.Printf("{\"kind\":\"subagent\",\"title\":%q,\"event\":%q,\"text\":%q}\n",
					ev.Title, ev.Kind, ev.Text)
			} else {
				fmt.Fprintf(os.Stderr, "[subagent %s] %s %s\n", ev.Title, ev.Kind, ev.Text)
			}
		})
		err := headless.Run(context.Background(), orch, *prompt, headless.Options{
			JSON: *jsonOut,
			Out:  os.Stdout,
		})
		saveSession()
		return err
	}

	if resumeNote != "" {
		startupNotes = append(startupNotes, resumeNote)
	}
	ui := tui.New(tui.Options{
		Orch:         orch,
		Effort:       cfg.ReasoningEffort,
		Model:        cfg.Model,
		Mode:         cfg.PermissionMode,
		Cwd:          cwd,
		TildeHome:    userDir,
		ProviderName: providerName,
		BaseURL:      providerCfg.BaseURL,
		API:          providerCfg.API,
		AuditPath:    auditPath,
		Animations:   cfg.Animations == nil || *cfg.Animations,
		Plain:        plainMode,
		Skills:       &skillManager,
		MCPNames: func() []string {
			var names []string
			for _, note := range mcpManager.Notes() {
				names = append(names, note)
			}
			return names
		},
		Models:      models,
		SwitchModel: switchModel,
		Theme:       cfg.Theme,
		SetTheme: func(name string) error {
			return config.SaveTheme(userDir, name)
		},
		KeyFor: func(provider string) (string, bool) {
			pc := models.Providers[provider]
			if pc.BaseURL == "" {
				if spec, ok := config.BuiltInProviders[provider]; ok {
					pc = config.ProviderConfig{BaseURL: spec.BaseURL, API: spec.API, APIKeyEnv: spec.APIKeyEnv}
				}
			}
			r := config.NewResolver(auth, pc)
			k, hasKey, err := r.APIKey(provider)
			if err != nil || !hasKey {
				return "", false
			}
			return k.Value, true
		},
		SaveCurrentSession: saveSession,
		ResumeSession:      resumeSession,
		PendingTrust:       pending,
		StartupNotes:       startupNotes,
	})
	// The gate's decision policy is wired after the UI exists: prompts
	// surface in the TUI and block the orchestrator until answered.
	gate.Decide = tools.PolicyDecide(cfg.PermissionMode, ui.Prompt())
	planTool.Approve = ui.PlanApprove()
	todoList.OnChange = ui.TodosChanged()

	// Subagent progress flows into the transcript as labeled lines.
	sink := ui.SubagentSink()
	spawnEmitter.Set(func(ev subagent.Event) {
		sink(ev.Title, ev.Kind, ev.Text, ev.Usage)
	})

	runErr := tui.Run(ui)
	saveSession()
	// The graceful exit: one line that says what happened (the
	// session was saved) and the way back in. Codex names itself in
	// everything it shows; so does tilde.
	if runErr == nil {
		fmt.Println("~ tilde — session saved · resume it with /sessions")
	}
	return runErr
}

// version is overridable at link time by the release pipeline
// (-ldflags "-X main.version=v0.2.0"); a source build reports (devel).
var version = "(devel)"

// buildVersion prefers the linked-in version, then the module version
// the toolchain recorded (go install sets it), then (devel).
func buildVersion() string {
	if version != "(devel)" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" &&
		bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

func systemPrompt(userDir, cwd string) string {
	p := "You are tilde, a terminal-based coding agent. You are working in: " + cwd + "\n" +
		"Use the available tools to accomplish the user's request. Prefer the smallest set of\n" +
		"actions that completes the task, and describe what you did when you finish."
	// Hierarchical AGENTS.md context (Section 5, step 3): inert text,
	// loaded regardless of project trust.
	if ctx := config.AgentsContext(userDir, cwd); ctx != "" {
		p += "\n\n" + ctx
	}
	return p
}
