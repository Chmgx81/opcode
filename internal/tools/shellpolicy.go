package tools

import (
	"strings"
)

// ShellAllowlist is the matcher behind the TUI's session-scoped
// "don't ask again" grants: command prefixes the user approved
// once. (Config-driven safe_commands was removed in Phase 30 —
// sandboxed commands auto-run in ask mode, so a per-command
// allowlist had nothing left to do.) Matching is token-wise
// prefix: the command's first tokens must equal the configured
// tokens verbatim — "git status" matches "git status --short" but
// not "git push", and no shell metacharacter can smuggle past it
// (any parse ambiguity fails closed to "not safe").
type ShellAllowlist struct {
	prefixes [][]string
}

// flagSynonyms are long/short flag pairs that mean the same thing
// across every major tool that has them. Applied to both stored
// grants and checked commands at match time, so a grant written one
// way matches its synonym written the other. Deliberately tiny: a
// pair that differs in ANY common tool would widen a grant past what
// the user read (--all/-a is excluded — grep -a is --text, not
// --all).
var flagSynonyms = map[string]string{
	"--yes":       "-y",
	"--quiet":     "-q",
	"--force":     "-f",
	"--verbose":   "-v",
	"--recursive": "-r",
}

// canonicalTokens normalizes flag tokens through the synonym table.
// The same pass runs on grants and on checked commands, so both
// sides agree; everything else stays verbatim.
func canonicalTokens(toks []string) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		if s, ok := flagSynonyms[t]; ok {
			t = s
		}
		out[i] = t
	}
	return out
}

// NewShellAllowlist tokenizes each configured prefix. Prefixes that do
// not tokenize cleanly are dropped, not guessed at.
func NewShellAllowlist(raw []string) *ShellAllowlist {
	a := &ShellAllowlist{}
	for _, p := range raw {
		if toks := canonicalTokens(shellWords(p)); len(toks) > 0 {
			a.prefixes = append(a.prefixes, toks)
		}
	}
	if len(a.prefixes) == 0 {
		return nil
	}
	return a
}

// unsafeShellChars make a command impossible to vouch for as written:
// operators, redirections, and substitution triggers. They are checked
// after quote stripping, so even a quoted metacharacter denies —
// conservative by design: "git status ; rm -rf /" and
// "git status $(curl evil)" must never auto-run because they start
// with two innocent tokens.
const unsafeShellChars = ";|&$`<>()"

// Allows reports whether command matches any configured prefix. Both
// sides pass the synonym table, so a grant stored one way matches its
// flags written the other.
func (a *ShellAllowlist) Allows(command string) bool {
	if a == nil {
		return false
	}
	toks := canonicalTokens(shellWords(command))
	if len(toks) == 0 {
		return false
	}
	for _, tok := range toks {
		if strings.ContainsAny(tok, unsafeShellChars) {
			return false
		}
	}
	for _, p := range a.prefixes {
		if len(toks) < len(p) {
			continue
		}
		match := true
		for i := range p {
			if toks[i] != p[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// shellWords splits a command into shell words with single/double
// quoting. An unterminated quote yields nil — a command we cannot
// tokenize exactly is a command we cannot vouch for.
func shellWords(s string) []string {
	var out []string
	var cur strings.Builder
	inWord := false
	inSingle, inDouble := false, false
	for _, r := range s {
		switch {
		case inSingle:
			if r == '\'' {
				inSingle = false
			} else {
				cur.WriteRune(r)
			}
		case inDouble:
			if r == '"' {
				inDouble = false
			} else {
				cur.WriteRune(r)
			}
		case r == '\'':
			inSingle = true
			inWord = true
		case r == '"':
			inDouble = true
			inWord = true
		case r == ' ' || r == '\t' || r == '\n':
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inSingle || inDouble {
		return nil // unterminated quote: fail closed
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out
}
