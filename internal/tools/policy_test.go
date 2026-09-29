package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fakeTool lets the matrix tests pin a tier without a real tool.
type fakeTool struct{ tier Tier }

func (fakeTool) Name() string                                             { return "fake" }
func (fakeTool) Description() string                                      { return "fake tool" }
func (fakeTool) Parameters() json.RawMessage                              { return json.RawMessage(`{}`) }
func (t fakeTool) Tier() Tier                                             { return t.tier }
func (fakeTool) Execute(ctx context.Context, args string) (string, error) { return "", nil }

func TestDecisionMatrix(t *testing.T) {
	promptCalled := false
	allow := func(Tool, string) bool { promptCalled = true; return true }
	denyPrompt := func(Tool, string) bool { promptCalled = true; return false }

	// fakeTool is not a bounded action, so in ask mode it prompts —
	// the bounded auto-run cells are covered by
	// TestAskModeBoundedActions below.
	cases := []struct {
		mode string
		tier Tier
		want string // "allow", "deny", "prompt"
	}{
		// Read-Only tools always run, in every mode.
		{ModeReadOnly, TierReadOnly, "allow"},
		{ModeAsk, TierReadOnly, "allow"},
		{ModeAutoAcceptSafe, TierReadOnly, "allow"},
		{ModeFullAuto, TierReadOnly, "allow"},
		// Draft-Only tools always run: they propose, they don't apply.
		{ModeReadOnly, TierDraftOnly, "allow"},
		{ModeAsk, TierDraftOnly, "allow"},
		{ModeAutoAcceptSafe, TierDraftOnly, "allow"},
		{ModeFullAuto, TierDraftOnly, "allow"},
		// Plan mode: read and draft (present_plan) run, actions prompt.
		{ModePlan, TierReadOnly, "allow"},
		{ModePlan, TierDraftOnly, "allow"},
		{ModePlan, TierActionAllowed, "prompt"},
		// Action-Allowed: the mode actually bites here. read-only
		// prompts too — the model may propose, the user decides.
		{ModeReadOnly, TierActionAllowed, "prompt"},
		{ModeAsk, TierActionAllowed, "prompt"},
		{ModeAutoAcceptSafe, TierActionAllowed, "prompt"},
		{ModeFullAuto, TierActionAllowed, "allow"},
		// Unknown mode fails closed to prompting.
		{"nonsense", TierActionAllowed, "prompt"},
	}

	for _, c := range cases {
		for _, prompt := range []func(Tool, string) bool{allow, denyPrompt} {
			promptCalled = false
			decide := PolicyDecide(c.mode, prompt)
			got := decide(fakeTool{c.tier}, `{}`)
			want := c.want
			if want == "prompt" {
				// The decision must come from the prompt.
				if !promptCalled {
					t.Errorf("mode %s tier %s: prompt not consulted", c.mode, c.tier)
					continue
				}
				if got != prompt(fakeTool{}, `{}`) {
					t.Errorf("mode %s tier %s: gate overrode the prompt (got %v)", c.mode, c.tier, got)
				}
				continue
			}
			if promptCalled {
				t.Errorf("mode %s tier %s: prompt consulted for a non-prompt cell", c.mode, c.tier)
			}
			if wantAllow := want == "allow"; got != wantAllow {
				t.Errorf("mode %s tier %s: got %v, want %v", c.mode, c.tier, got, wantAllow)
			}
		}
	}
}

func TestPolicyNilPromptFailsClosed(t *testing.T) {
	// No one to ask: deny, never silently allow — in every mode
	// that prompts.
	for _, mode := range []string{ModeAsk, ModeReadOnly, ModePlan, "unknown"} {
		decide := PolicyDecide(mode, nil)
		if decide(fakeTool{TierActionAllowed}, `{}`) {
			t.Errorf("nil prompt must deny action-tier calls in mode %s", mode)
		}
		if !decide(fakeTool{TierReadOnly}, `{}`) {
			t.Errorf("nil prompt must still allow read-tier calls in mode %s", mode)
		}
		if !decide(fakeTool{TierDraftOnly}, `{}`) {
			t.Errorf("nil prompt must still allow draft-tier calls in mode %s", mode)
		}
	}
}

func TestAskModeBoundedActions(t *testing.T) {
	promptCalled := false
	prompt := func(Tool, string) bool { promptCalled = true; return true }
	decide := PolicyDecide(ModeAsk, prompt)

	dir := t.TempDir() // inside os.TempDir — a writable root

	// An in-root write is bounded: it runs without prompting.
	promptCalled = false
	args, _ := json.Marshal(map[string]string{"path": filepath.Join(dir, "out.txt")})
	if !decide(WriteFile{}, string(args)) {
		t.Fatal("in-root write denied in ask mode")
	}
	if promptCalled {
		t.Error("in-root write prompted — the path bound makes it safe")
	}

	// A write outside every writable root is unbounded: it prompts.
	promptCalled = false
	args, _ = json.Marshal(map[string]string{"path": "/etc/tilde-should-not-write.txt"})
	if !decide(WriteFile{}, string(args)) || !promptCalled {
		t.Error("out-of-root write must prompt in ask mode")
	}

	// The same bound applies to edits.
	promptCalled = false
	args, _ = json.Marshal(map[string]string{"path": filepath.Join(dir, "edit.txt"), "old": "a", "new": "b"})
	if !decide(EditFile{}, string(args)) {
		t.Fatal("in-root edit denied in ask mode")
	}
	if promptCalled {
		t.Error("in-root edit prompted — the path bound makes it safe")
	}

	// A lexical in-root path that resolves outside (symlink) must
	// not auto-run: the parent's symlinks are resolved first. The
	// link's target must be outside every writable root — pointing
	// it at another temp dir would still be bounded.
	outside := "/var"
	if _, err := os.Stat(outside); err != nil {
		t.Skip("/var unavailable")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	promptCalled = false
	args, _ = json.Marshal(map[string]string{"path": filepath.Join(link, "out.txt")})
	if !decide(WriteFile{}, string(args)) || !promptCalled {
		t.Error("symlinked write path auto-ran; the resolved target is outside the roots")
	}

	// A relative in-root path (no prefix) is bounded too.
	promptCalled = false
	if !decide(WriteFile{}, `{"path": "local.txt"}`) {
		t.Fatal("relative in-cwd write denied in ask mode")
	}
	if promptCalled {
		t.Error("relative in-cwd write prompted")
	}

	// A shell call in tests runs with no Landlock ruleset installed,
	// so it is unbounded and prompts — the sandbox must be credited
	// only when it is actually enforced.
	promptCalled = false
	if !decide(RunShell{}, `{"command": "ls"}`) || !promptCalled {
		t.Error("shell call without an active sandbox must prompt in ask mode")
	}
}

func TestShellEscaped(t *testing.T) {
	if !ShellEscaped(`{"command": "ls", "sandbox": false}`) {
		t.Error(`{"sandbox": false} must read as an escape`)
	}
	if ShellEscaped(`{"command": "ls", "sandbox": true}`) {
		t.Error(`{"sandbox": true} is sandboxed`)
	}
	if ShellEscaped(`{"command": "ls"}`) {
		t.Error("absent sandbox means sandboxed (the default)")
	}
	if !ShellEscaped(`not json`) {
		t.Error("unparsable args read as an escape — fail closed")
	}
}

func TestModeAllowsTool(t *testing.T) {
	// Phase 30: offering is not permission. Every mode offers every
	// tier; the gate is the single enforcement point.
	tools := []Tool{ReadFile{}, ListDir{}, WriteFile{}, EditFile{}, RunShell{}}
	for _, mode := range append(append([]string{}, Modes...), "unknown") {
		for _, tool := range tools {
			if !ModeAllowsTool(mode, tool) {
				t.Errorf("mode %s must offer %s — the gate, not the tool list, is the posture", mode, tool.Name())
			}
		}
	}
}

func TestModeInstructionAndNormalization(t *testing.T) {
	if NormalizeMode("build") != ModeBuild {
		t.Error(`"build" is the canonical spelling`)
	}
	if NormalizeMode(ModeReadOnly) != ModePlan {
		t.Error(`"read-only" is the legacy plan spelling`)
	}
	for _, mode := range Modes {
		if !ValidMode(mode) {
			t.Errorf("Modes entry %q fails ValidMode", mode)
		}
	}
	if ValidMode("yolo") {
		t.Error("unknown mode must not validate")
	}
	for _, mode := range append(Modes, "unknown-mode") {
		if ModeInstruction(mode) == "" {
			t.Errorf("mode %q has no system-prompt instruction", mode)
		}
	}
}

func TestLegacyModeAliases(t *testing.T) {
	// The Phase 2/30 spellings keep old configs working, mapping
	// onto the three-mode set: read-only onto plan, the ask family
	// onto build.
	if m := NormalizeMode(ModeReadOnly); m != ModePlan {
		t.Errorf("read-only = %q, want plan", m)
	}
	for _, legacy := range []string{ModeAsk, ModeAskEveryTime, ModeAutoAcceptSafe} {
		if m := NormalizeMode(legacy); m != ModeBuild {
			t.Errorf("%s = %q, want build", legacy, m)
		}
	}
	if ValidMode(ModeAutoAcceptSafe) {
		t.Error("auto-accept-safe-ops is not a real mode anymore")
	}
	if len(Modes) != 3 {
		t.Errorf("Modes = %v, want exactly three (plan, build, full-auto)", Modes)
	}
}
