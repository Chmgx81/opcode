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
// anything is written to the audit log (Section 3.10: credentials never
// leak out through tilde's own records).
type Redactor struct {
	secrets []string
}

func NewRedactor(secrets ...string) *Redactor {
	var r Redactor
	for _, s := range secrets {
		if s != "" {
			r.secrets = append(r.secrets, s)
		}
	}
	return &r
}

func (r *Redactor) Redact(s string) string {
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
	// Decide returns true to allow the call. Never nil.
	Decide func(tool Tool, args string) bool
	// Audit receives one entry per gate pass. If nil, nothing is logged.
	Audit *AuditLog
	// Clock is injectable for tests; defaults to wall clock.
	Clock func() time.Time
}

func (g *Gate) now() time.Time {
	if g.Clock != nil {
		return g.Clock()
	}
	return time.Now().UTC()
}

func (g *Gate) allow(tool Tool, args string) bool {
	if g.Decide == nil {
		return true
	}
	return g.Decide(tool, args)
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
		if g.Audit != nil {
			_ = g.Audit.record(entry)
		}
		return "", fmt.Errorf("permission denied for %s (tier %s)", tool.Name(), tool.Tier())
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
