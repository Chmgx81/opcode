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
// 3.10: credentials never leak out through tilde's own records).
//
// It is the ONE list of what counts as a secret: the audit log and
// the session save both read it, and /login adds to it live — a key
// stored mid-session is redacted everywhere from that moment, and a
// key that was never active this session is still covered because
// every auth.json value is loaded at startup (audit findings S4/S5).
type Redactor struct {
	mu      sync.Mutex
	secrets []string
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
		}
	}
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

func (r *Redactor) Redact(s string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, "[redacted]")
	}
	return s
}

// AuditLog is a JSON-lines log of every tool call that passed the gate:
// what ran, when, at which tier, whether it was allowed, and what came
// back. One entry per call, written before the result is fed to the model.
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
// The audit entry is written even when logging fails — execution result
// is reported to the caller regardless, with the log error attached to
// the result only when the call itself succeeded.
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
	if g.Audit != nil {
		if logErr := g.Audit.record(entry); logErr != nil && err == nil {
			return result, fmt.Errorf("tool succeeded but audit log write failed: %w", logErr)
		}
	}
	return result, err
}
