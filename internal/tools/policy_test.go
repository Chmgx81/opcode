package tools

import "testing"

func TestPolicyReadOnlyAlwaysAllowed(t *testing.T) {
	// A prompt callback that always denies: Read-Only must still pass,
	// in every mode.
	never := func(Tool, string) bool { return false }
	for _, mode := range []string{ModeAsk, ModeFullAuto, "read-only", "unknown-mode"} {
		decide := PolicyDecide(mode, never)
		if !decide(ReadFile{}, `{"path": "x"}`) {
			t.Errorf("mode %q: read-only denied", mode)
		}
	}
}

func TestPolicyAskPromptsForActionTier(t *testing.T) {
	var promptedWith Tool
	decide := PolicyDecide(ModeAsk, func(tool Tool, args string) bool {
		promptedWith = tool
		return true
	})
	if !decide(WriteFile{}, `{"path": "x"}`) {
		t.Error("prompt said allow, but call denied")
	}
	if promptedWith.Name() != "write_file" {
		t.Errorf("prompt did not receive the calling tool: %v", promptedWith)
	}

	decideDeny := PolicyDecide(ModeAsk, func(Tool, string) bool { return false })
	if decideDeny(RunShell{}, `{"command": "ls"}`) {
		t.Error("prompt said deny, but call allowed")
	}
}

func TestPolicyFullAutoSkipsPrompt(t *testing.T) {
	called := false
	decide := PolicyDecide(ModeFullAuto, func(Tool, string) bool {
		called = true
		return false
	})
	if !decide(WriteFile{}, `{}`) {
		t.Error("full-auto must allow Action-Allowed tools without prompting")
	}
	if called {
		t.Error("full-auto still consulted the prompt")
	}
}

func TestPolicyNilPromptFailsClosed(t *testing.T) {
	// No one to ask (e.g. a future headless run in ask mode): deny,
	// never silently allow.
	decide := PolicyDecide(ModeAsk, nil)
	if decide(WriteFile{}, `{}`) {
		t.Error("nil prompt must fail closed for Action-Allowed tools")
	}
	if !decide(ReadFile{}, `{}`) {
		t.Error("nil prompt must still allow Read-Only tools")
	}
}
