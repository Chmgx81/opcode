package tools

import (
	"context"
	"encoding/json"
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

	cases := []struct {
		mode string
		tier Tier
		want string // "allow", "deny", "prompt-allow", "prompt-deny"
	}{
		// Read-Only tools always run, in every mode.
		{ModeReadOnly, TierReadOnly, "allow"},
		{ModeAskEveryTime, TierReadOnly, "allow"},
		{ModeAutoAcceptSafe, TierReadOnly, "allow"},
		{ModeFullAuto, TierReadOnly, "allow"},
		// Draft-Only tools always run: they propose, they don't apply.
		{ModeReadOnly, TierDraftOnly, "allow"},
		{ModeAskEveryTime, TierDraftOnly, "allow"},
		{ModeAutoAcceptSafe, TierDraftOnly, "allow"},
		{ModeFullAuto, TierDraftOnly, "allow"},
		// Action-Allowed: the mode actually bites here.
		{ModeReadOnly, TierActionAllowed, "deny"},
		{ModeAskEveryTime, TierActionAllowed, "prompt"},
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
	// No one to ask: deny, never silently allow.
	decide := PolicyDecide(ModeAskEveryTime, nil)
	if decide(fakeTool{TierActionAllowed}, `{}`) {
		t.Error("nil prompt must deny action-tier calls")
	}
	if !decide(fakeTool{TierReadOnly}, `{}`) {
		t.Error("nil prompt must still allow read-tier calls")
	}
	if !decide(fakeTool{TierDraftOnly}, `{}`) {
		t.Error("nil prompt must still allow draft-tier calls")
	}
	// Even in read-only mode (deny cell), a nil prompt changes nothing.
	if decide(fakeTool{TierActionAllowed}, `{}`) {
		t.Error("read-only mode must deny action-tier calls")
	}
}

func TestModeAllowsTool(t *testing.T) {
	read := ReadFile{}
	write := WriteFile{}

	for _, mode := range []string{ModeAskEveryTime, ModeAutoAcceptSafe, ModeFullAuto, "unknown"} {
		if !ModeAllowsTool(mode, write) {
			t.Errorf("mode %s must still offer action tools to the model", mode)
		}
		if !ModeAllowsTool(mode, read) {
			t.Errorf("mode %s must offer read tools", mode)
		}
	}
	if ModeAllowsTool(ModeReadOnly, write) {
		t.Error("read-only mode must not offer action-tier tools at all")
	}
	if !ModeAllowsTool(ModeReadOnly, read) {
		t.Error("read-only mode must offer read-tier tools")
	}
}

func TestModeInstructionAndNormalization(t *testing.T) {
	if NormalizeMode("ask") != ModeAskEveryTime {
		t.Error(`"ask" must normalize to ask-every-time`)
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
