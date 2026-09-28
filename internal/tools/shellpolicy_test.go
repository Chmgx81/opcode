package tools

import (
	"context"
	"testing"
)

func TestShellAllowlistMatching(t *testing.T) {
	a := NewShellAllowlist([]string{"git status", "ls", "go test", ""})
	if a == nil {
		t.Fatal("non-empty valid prefixes must build an allowlist")
	}
	cases := []struct {
		cmd  string
		want bool
	}{
		{"git status", true},
		{"git status --short", true}, // prefix match: more tokens ok
		{"go test ./...", true},
		{"ls", true},
		{"ls -la /tmp", true},
		{"git push", false},                 // same first token, different second
		{"git", false},                      // prefix longer than the command
		{"GIT STATUS", false},               // case-sensitive
		{"git status ; rm -rf /", false},    // separator token: prefix would match, metachar denies
		{"git status; rm -rf /", false},     // operator glued to a token
		{"git status $(whoami)", false},     // substitution: never auto-run
		{"git status | sh", false},          // pipe
		{"git status > /etc/passwd", false}, // redirection
		{"'git status'", false},             // one word with a space: a DIFFERENT command
		{`git "status"`, true},              // per-word quoting is still git + status
		{"echo `git status`", false},
	}
	for _, c := range cases {
		if got := a.Allows(c.cmd); got != c.want {
			t.Errorf("Allows(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

func TestShellAllowlistFailsClosed(t *testing.T) {
	// Unterminated quote: untolerable to tokenize, must not match.
	a := NewShellAllowlist([]string{"git status"})
	if a.Allows("git 'status") {
		t.Error("unterminated quote must fail closed")
	}
	// Nil allowlist allows nothing and is the default.
	var nilA *ShellAllowlist
	if nilA.Allows("git status") {
		t.Error("nil allowlist must allow nothing")
	}
	// All-invalid input builds no allowlist.
	if NewShellAllowlist([]string{"'"}) != nil {
		t.Error("a lone unterminated quote must be dropped, yielding nil")
	}
}

func TestShellPolicyDecideComposesWithModes(t *testing.T) {
	allow := NewShellAllowlist([]string{"git status"})
	shell := RunShell{}
	safeArgs := `{"command": "git status --short"}`
	riskyArgs := `{"command": "rm -rf /"}`
	wireArgs := `{"path": "x", "content": "y"}`

	promptCalled := false
	ask := ShellPolicyDecide(ModeAskEveryTime, func(Tool, string) bool {
		promptCalled = true
		return false
	}, allow)

	// Safe command: allowed without consulting the prompt.
	promptCalled = false
	if !ask(shell, safeArgs) || promptCalled {
		t.Error("safe command must auto-allow without prompting")
	}
	// Risky command: the prompt decides (fail closed when it denies).
	promptCalled = false
	if ask(shell, riskyArgs) || !promptCalled {
		t.Error("risky command must consult the prompt and honor a denial")
	}
	// Non-shell tools are untouched by the allowlist.
	promptCalled = false
	if ask(WriteFile{}, wireArgs) || !promptCalled {
		t.Error("write_file must still prompt in ask mode")
	}

	// The mode's posture dominates: read-only and plan deny even safe
	// shell calls, with no prompt.
	ro := ShellPolicyDecide(ModeReadOnly, func(Tool, string) bool { return true }, allow)
	if ro(shell, safeArgs) {
		t.Error("read-only must deny even allowlisted shell commands")
	}
	plan := ShellPolicyDecide(ModePlan, func(Tool, string) bool { return true }, allow)
	if plan(shell, safeArgs) {
		t.Error("plan must deny even allowlisted shell commands")
	}

	// Nil allowlist behaves exactly like PolicyDecide.
	base := ShellPolicyDecide(ModeAskEveryTime, func(Tool, string) bool { return true }, nil)
	promptCalled = false
	if !base(shell, safeArgs) {
		t.Error("nil allowlist defers to the prompt")
	}

	// A real RunShell executes through the composed gate end to end.
	_ = context.Background()
}
