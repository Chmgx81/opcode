package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Redactor replaces known credential values with a placeholder before
// anything is written to the audit log or a session file (Section
// 3.10: credentials never leak out through opcode's own records).
//
// It is the ONE list of what counts as a secret: the audit log and
// the session save both read it, and /login adds to it live — a key
// stored mid-session is redacted everywhere from that moment, and a
// key that was never active this session is still covered because
// every auth.json value is loaded at startup (audit findings S4/S5).
//
// Redaction matches every secret in the forms it takes in output, not
// just the raw value (see redactionForms): a credentials file cat'd to
// the terminal shows JSON-escaped values, and a multi-line secret
// comes back with different line endings or a grep prefix. Matches are
// found for all forms at once and merged before replacing, so
// overlapping secrets cannot leave a partial fragment behind and the
// result never depends on the order secrets were added.
//
// What it cannot do is recognise a secret the shell has transformed
// (base64, hex, reversed, split across lines): it is a backstop for
// accidents and naive exfiltration, not a substitute for keeping the
// file away from the tools.
type Redactor struct {
	mu      sync.Mutex
	secrets []string // as registered; Secrets() returns exactly these
	forms   []string // every form to match, deduplicated
}

func NewRedactor(secrets ...string) *Redactor {
	var r Redactor
	r.Add(secrets...)
	return &r
}

// Add registers more secret values. Empty values are ignored; the
// same value twice is harmless (Redact is idempotent).
func (r *Redactor) Add(secrets ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range secrets {
		if s != "" {
			r.secrets = append(r.secrets, s)
			r.forms = addForms(r.forms, redactionForms(s))
		}
	}
}

// minLineForm is the shortest line of a multi-line secret that is
// matched on its own. Shorter lines ("-----END-----" fragments, blank
// or one-word lines) would redact ordinary text.
const minLineForm = 8

// redactionForms lists the spellings of s to look for: the value; the
// value trimmed of surrounding whitespace (a key stored with a
// trailing newline still appears without one); its JSON-escaped
// spelling with and without Go's HTML escaping (\u003c) — what an
// auth.json holding a value with quotes, backslashes or newlines
// looks like; with CRLF line endings; and, for a multi-line secret,
// each line on its own, so a grep -n or an indent prefix does not
// hide it.
func redactionForms(s string) []string {
	forms := []string{s}
	if t := strings.TrimSpace(s); t != "" && t != s {
		forms = append(forms, t)
	}
	for _, v := range append([]string(nil), forms...) {
		forms = append(forms, jsonEscaped(v, false), jsonEscaped(v, true))
		if strings.Contains(v, "\n") {
			forms = append(forms, strings.ReplaceAll(v, "\n", "\r\n"))
			for _, line := range strings.FieldsFunc(v, func(r rune) bool { return r == '\n' || r == '\r' }) {
				if line = strings.TrimSpace(line); len(line) >= minLineForm {
					forms = append(forms, line)
				}
			}
		}
	}
	return forms
}

// jsonEscaped is s as it appears inside a JSON string literal, quotes
// stripped.
func jsonEscaped(s string, escapeHTML bool) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(escapeHTML)
	if err := enc.Encode(s); err != nil {
		return s
	}
	out := strings.TrimSuffix(b.String(), "\n")
	return out[1 : len(out)-1]
}

func addForms(have, more []string) []string {
	seen := make(map[string]bool, len(have)+len(more))
	for _, f := range have {
		seen[f] = true
	}
	for _, f := range more {
		if f != "" && !seen[f] {
			seen[f] = true
			have = append(have, f)
		}
	}
	return have
}

// Secrets returns a copy of everything currently known to be secret
// — session save passes it to the writer.
func (r *Redactor) Secrets() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.secrets))
	copy(out, r.secrets)
	return out
}

// Redact replaces every occurrence of every known secret (in every
// form) with a placeholder. Occurrences are marked for all forms
// first — including overlapping ones — and each marked run becomes one
// placeholder, so "abc123" is fully hidden even when "c12" is also a
// secret, whichever was registered first, and the placeholder itself
// is never re-scanned.
func (r *Redactor) Redact(s string) string {
	r.mu.Lock()
	forms := r.forms
	r.mu.Unlock()
	var hit []bool
	for _, f := range forms {
		for off := 0; off < len(s); {
			i := strings.Index(s[off:], f)
			if i < 0 {
				break
			}
			if hit == nil {
				hit = make([]bool, len(s))
			}
			start := off + i
			for j := start; j < start+len(f); j++ {
				hit[j] = true
			}
			off = start + 1
		}
	}
	if hit == nil {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if !hit[i] {
			b.WriteByte(s[i])
			i++
			continue
		}
		b.WriteString("[redacted]")
		for i < len(s) && hit[i] {
			i++
		}
	}
	return b.String()
}

// AuditLog is a JSON-lines log of every tool call that passed the gate:
// what ran, when, at which tier, whether it was allowed, and what came
// back. One entry per call, written before the result is fed to the model.
// It also owns the redaction applied at the model boundary: Gate.Execute
// runs results through Redact before handing them back, so a secret that
// reaches tool output (a bash cat of the credentials file) never rides
// into the model's context (audit S1).
type AuditLog struct {
	mu       sync.Mutex
	path     string
	redactor *Redactor
}

type auditEntry struct {
	Time    string `json:"time"`
	Tool    string `json:"tool"`
	Tier    Tier   `json:"tier"`
	Allowed bool   `json:"allowed"`
	Args    string `json:"args"`
	Result  string `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`
}

func NewAuditLog(path string, redactor *Redactor) *AuditLog {
	return &AuditLog{path: path, redactor: redactor}
}

// Redact replaces known credential values with a placeholder. This is
// the one redaction the orchestrator-facing tool result goes through —
// the same live list the audit log uses, so /login's Add covers both
// from that moment.
func (l *AuditLog) Redact(s string) string {
	if l.redactor == nil {
		return s
	}
	return l.redactor.Redact(s)
}

func (l *AuditLog) record(e auditEntry) error {
	if l.redactor != nil {
		e.Args = l.redactor.Redact(e.Args)
		e.Result = l.redactor.Redact(e.Result)
		e.Error = l.redactor.Redact(e.Error)
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// Gate is the single permission decision point for every tool call
// (Section 3.6): Decide says allow/deny, and every call is audited
// with its tier. A nil Decide allows everything (the test/CI
// posture); production wires PolicyDecide.
type Gate struct {
	// Decide returns true to allow the call. Never nil. Writers must
	// go through SetDecide: the UI can rebuild the policy mid-turn
	// while the dispatch goroutine reads it here.
	Decide func(tool Tool, args string) bool
	// Audit receives one entry per gate pass. If nil, nothing is logged.
	Audit *AuditLog
	// Clock is injectable for tests; defaults to wall clock.
	Clock func() time.Time

	decideMu sync.RWMutex
}

// SetDecide swaps the policy callback safely against a live turn.
func (g *Gate) SetDecide(fn func(tool Tool, args string) bool) {
	g.decideMu.Lock()
	defer g.decideMu.Unlock()
	g.Decide = fn
}

func (g *Gate) now() time.Time {
	if g.Clock != nil {
		return g.Clock()
	}
	return time.Now().UTC()
}

func (g *Gate) allow(tool Tool, args string) bool {
	g.decideMu.RLock()
	fn := g.Decide
	g.decideMu.RUnlock()
	if fn == nil {
		return true
	}
	return fn(tool, args)
}

// Execute is the one path a tool call takes: permission decision, then
// execution, then the audit entry. A denied call never reaches the tool.
// The result handed back is redacted against the credential list before
// it returns — the orchestrator feeds it to the model verbatim — while
// the audit entry is redacted as it always was. The entry is written
// even when logging fails — execution result is reported to
// the caller regardless, with the log error attached to the result only
// when the call itself succeeded.
func (g *Gate) Execute(ctx context.Context, tool Tool, args string) (string, error) {
	allowed := g.allow(tool, args)
	entry := auditEntry{
		Time:    g.now().Format(time.RFC3339),
		Tool:    tool.Name(),
		Tier:    tool.Tier(),
		Allowed: allowed,
		Args:    args,
	}
	if !allowed {
		entry.Error = "denied by permission gate"
		denied := fmt.Errorf("permission denied for %s (tier %s)", tool.Name(), tool.Tier())
		if g.Audit != nil {
			// A denied call is exactly the entry a security review
			// wants on record; a failed write must not be silent
			// (audit C9) — but it must also not mask the denial.
			if err := g.Audit.record(entry); err != nil {
				return "", fmt.Errorf("%w (audit log write failed: %v)", denied, err)
			}
		}
		return "", denied
	}

	result, err := tool.Execute(ctx, args)
	entry.Result = result
	if err != nil {
		entry.Error = err.Error()
		// The output stays: it is the evidence of what failed —
		// a shell's stderr, a sandbox-denial note — and the
		// orchestrator forwards it beside the error headline.
	}
	// The model edge: the orchestrator puts the result in the model's
	// context verbatim (history and tool events), and on error it is
	// re-embedded beside the error headline — so this return value IS
	// the boundary. Nothing a known credential can appear here, in
	// any mode, from any tool (audit S1; the path-args deny only sees
	// "path" arguments, not a bash command string).
	if g.Audit != nil {
		result = g.Audit.Redact(result)
		// The error text crosses the same boundary: tool errors embed
		// paths, patterns, URLs and file fragments the model supplied
		// or the tool read. Wrapped only when redaction changed it,
		// with the original still on the Unwrap chain, so errors.Is/As
		// (context.Canceled, BadArgumentsError) keep working.
		if err != nil {
			if msg := g.Audit.Redact(err.Error()); msg != err.Error() {
				err = &redactedError{msg: msg, err: err}
			}
		}
		if logErr := g.Audit.record(entry); logErr != nil && err == nil {
			return result, fmt.Errorf("tool succeeded but audit log write failed: %w", logErr)
		}
	}
	return result, err
}

// redactedError carries an error's redacted text while keeping the
// original reachable through errors.Is/As.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// RecordUserAction writes an audit entry for an action the user took
// directly — the TUI "!" shell escape. It takes no permission decision
// (the user typed it, so it is allowed by definition), but it lands in
// the same forensic record as every gated call, or the audit log stops
// being a complete account of what ran on this machine (audit C13).
// The "user" tier marks the actor, not a rung on the permission ladder.
func (g *Gate) RecordUserAction(action, args string) error {
	if g.Audit == nil {
		return nil
	}
	return g.Audit.record(auditEntry{
		Time:    g.now().Format(time.RFC3339),
		Tool:    action,
		Tier:    Tier("user"),
		Allowed: true,
		Args:    args,
	})
}
