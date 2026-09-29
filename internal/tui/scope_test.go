package tui

import (
	"testing"

	"github.com/Chmgx81/tilde/internal/tools"
)

// TestAlwaysScopePrecision: the grant must be exactly as wide as what
// the dialog shows. A flag in second position carries the whole
// command — the old two-field rule would have approved every other
// value of that flag.
func TestAlwaysScopePrecision(t *testing.T) {
	cases := []struct {
		command, want string
	}{
		{`{"command": "cargo build --release"}`, "cargo build:*"},
		{`{"command": "npm init -y"}`, "npm init:*"},
		{`{"command": "go test"}`, "go test:*"},
		{`{"command": "make"}`, "make:*"},
		{`{"command": "git -C /tmp push"}`, "git -C /tmp push:*"},
		{`{"command": "sudo -u root systemctl status nginx"}`, "sudo -u root systemctl status nginx:*"},
	}
	for _, c := range cases {
		if got := alwaysScope("bash", c.command); got != c.want {
			t.Errorf("alwaysScope(%s) = %q, want %q", c.command, got, c.want)
		}
	}
	if got := alwaysScope("write_file", `{"path": "x"}`); got != "write_file" {
		t.Errorf("non-bash scope = %q", got)
	}
	if got := alwaysScope("bash", `not json`); got != "bash" {
		t.Errorf("unparseable args scope = %q", got)
	}
}

// TestFlagGrantDoesNotWiden: the security property this phase exists
// for — a grant from "git -C /tmp push" must not cover a different
// -C target, while still covering the approved command's longer
// forms.
func TestFlagGrantDoesNotWiden(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// The user approved "always" for git -C /tmp push.
	scope := alwaysScope("bash", `{"command": "git -C /tmp push"}`)
	m.grantAlways(&permRequest{
		tool: "bash", tier: tools.TierActionAllowed,
		args: `{"command": "git -C /tmp push"}`, scope: scope,
		reply: make(chan bool, 1),
	})

	covered := m.sessionGrants("bash", `{"command": "git -C /tmp push"}`)
	longer := m.sessionGrants("bash", `{"command": "git -C /tmp push --quiet"}`)
	widened := m.sessionGrants("bash", `{"command": "git -C /etc reset --hard"}`)
	otherFlag := m.sessionGrants("bash", `{"command": "git -C /tmp status"}`)

	if !covered || !longer {
		t.Errorf("the approved command or its longer form stopped matching: covered=%v longer=%v", covered, longer)
	}
	if widened {
		t.Error("CRITICAL: the grant widened past the approved command — git -C /etc reset --hard auto-ran")
	}
	if otherFlag {
		t.Error("the grant covered a different subcommand under the same flag — git -C /tmp status auto-ran")
	}
}

// TestPlainGrantStillCoversFlags: the unchanged plain form — "always
// allow cargo build" covers "cargo build --release".
func TestPlainGrantStillCoversFlags(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.grantAlways(&permRequest{
		tool: "bash", tier: tools.TierActionAllowed,
		args: `{"command": "cargo build"}`, scope: "cargo build:*",
		reply: make(chan bool, 1),
	})
	if !m.sessionGrants("bash", `{"command": "cargo build --release"}`) {
		t.Error("plain grant stopped covering longer forms")
	}
	if m.sessionGrants("bash", `{"command": "cargo publish"}`) {
		t.Error("plain grant covered a different subcommand")
	}
}
