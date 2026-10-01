package tools

import "testing"

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
		{"git status\nrm -rf /", false}, // newline: two commands, one grant
		{"git status\rrm -rf /", false}, // CR is not a separator, so it also fails closed
		{"git\nstatus", false},          // newline inside the granted prefix itself
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
