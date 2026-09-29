package tools

import "testing"

// TestShellAllowlistSynonyms: grants and checked commands both pass
// the flag-synonym table, so "always allow cargo test --quiet" also
// covers "cargo test -q" — the adoption doc's canonicalization win,
// at the width of a curated, genuinely universal table.
func TestShellAllowlistSynonyms(t *testing.T) {
	a := NewShellAllowlist([]string{"cargo test --quiet"})
	cases := []struct {
		command string
		want    bool
	}{
		{"cargo test -q", true},
		{"cargo test --quiet", true},
		{"cargo test -q --color never", true},
		{"cargo test --release", false}, // a different flag is a different command
		{"cargo test", false},           // the grant includes the flag; the command must too
		{"cargo run", false},
	}
	for _, c := range cases {
		if got := a.Allows(c.command); got != c.want {
			t.Errorf("Allows(%q) = %v, want %v", c.command, got, c.want)
		}
	}
}

// TestShellAllowlistSynonymBothDirections: a grant stored in the short
// form matches the long form too — the table runs on both sides.
func TestShellAllowlistSynonymBothDirections(t *testing.T) {
	a := NewShellAllowlist([]string{"npm init -y"})
	if !a.Allows("npm init --yes") {
		t.Error("short-form grant does not cover the long form")
	}
	b := NewShellAllowlist([]string{"npm init --yes"})
	if !b.Allows("npm init -y") {
		t.Error("long-form grant does not cover the short form")
	}
}

// TestShellAllowlistNoFalseSynonyms: pairs that differ across tools
// must not merge — grep -a is --text, not --all. The table is the
// whole canonicalization surface; anything outside it stays verbatim.
func TestShellAllowlistNoFalseSynonyms(t *testing.T) {
	a := NewShellAllowlist([]string{"grep --all pattern"})
	if a.Allows("grep -a pattern") {
		t.Error("--all and -a merged for grep: a distinct flag was widened")
	}
	if !a.Allows("grep --all pattern file") {
		t.Error("verbatim grant stopped covering its longer form")
	}
}

// TestShellAllowlistFailClosedUnchanged: canonicalization must not
// weaken the fail-closed rules — metacharacters and untokenizable
// commands still deny.
func TestShellAllowlistFailClosedUnchanged(t *testing.T) {
	a := NewShellAllowlist([]string{"cargo test --quiet"})
	if a.Allows("cargo test --quiet ; rm -rf /") {
		t.Error("a metacharacter command auto-ran")
	}
	if a.Allows("cargo test --quiet $(curl evil)") {
		t.Error("a substitution command auto-ran")
	}
	if NewShellAllowlist([]string{`cargo test "unclosed`}) != nil {
		t.Error("an untokenizable grant was stored")
	}
	if a.Allows(`cargo test "unclosed`) {
		t.Error("an untokenizable command matched")
	}
}
