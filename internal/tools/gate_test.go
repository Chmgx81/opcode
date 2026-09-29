package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGateAlwaysAllowsAndLogs(t *testing.T) {
	dir := t.TempDir()
	log := NewAuditLog(filepath.Join(dir, "audit.jsonl"), nil)
	g := &Gate{Audit: log} // nil Decide = Phase 0 default: allow

	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	args := `{"path": "` + path + `"}`
	out, err := g.Execute(context.Background(), ReadFile{}, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "data" {
		t.Errorf("out = %q, want data", out)
	}

	// A failing call is also a gate pass and also gets logged: the
	// error string, not just successes.
	_, err = g.Execute(context.Background(), ReadFile{}, `{"path": "missing"}`)
	if err == nil {
		t.Fatal("expected error reading a missing file")
	}

	data, err := os.ReadFile(log.path)
	if err != nil {
		t.Fatalf("audit log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d audit lines, want 2", len(lines))
	}
	var entry auditEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("parse entry: %v", err)
	}
	if entry.Tool != "read_file" || entry.Tier != TierReadOnly || !entry.Allowed {
		t.Errorf("entry = %+v", entry)
	}
	if entry.Args != args {
		t.Errorf("Args = %q", entry.Args)
	}
	if entry.Result != "data" {
		t.Errorf("Result = %q", entry.Result)
	}
	var failed auditEntry
	if err := json.Unmarshal([]byte(lines[1]), &failed); err != nil {
		t.Fatalf("parse entry: %v", err)
	}
	if failed.Error == "" {
		t.Errorf("tool failure should be recorded in the audit entry: %+v", failed)
	}
}

func TestGateDeniesWithoutExecuting(t *testing.T) {
	dir := t.TempDir()
	log := NewAuditLog(filepath.Join(dir, "audit.jsonl"), nil)
	g := &Gate{Audit: log, Decide: func(Tool, string) bool { return false }}

	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("safe"), 0o644); err != nil {
		t.Fatal(err)
	}
	args := `{"path": "` + target + `", "content": "clobbered"}`
	_, err := g.Execute(context.Background(), WriteFile{}, args)
	if err == nil {
		t.Fatal("expected denial error")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("err = %v", err)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Fatalf("target disappeared: %v", statErr)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "safe" {
		t.Fatalf("denied call still executed: %q", string(data))
	}

	data, _ = os.ReadFile(log.path)
	if !strings.Contains(string(data), `"allowed":false`) {
		t.Errorf("denial not logged: %s", string(data))
	}
}

func TestGateRedactsSecretsInAuditLog(t *testing.T) {
	dir := t.TempDir()
	log := NewAuditLog(filepath.Join(dir, "audit.jsonl"),
		NewRedactor("sk-super-secret"))
	g := &Gate{Audit: log}

	// The model echoes a key it should never have seen; the audit log
	// must not record it.
	args := `{"path": "` + filepath.Join(dir, "out.txt") + `", "content": "key=sk-super-secret"}`
	if _, err := g.Execute(context.Background(), WriteFile{}, args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data, err := os.ReadFile(log.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-super-secret") {
		t.Errorf("audit log leaks the credential: %s", string(data))
	}
	if !strings.Contains(string(data), "[redacted]") {
		t.Errorf("audit log has no redaction marker: %s", string(data))
	}
}

// TestGateRedactsSecretInResult is the S1 boundary regression: the
// orchestrator feeds Gate.Execute's return value to the model verbatim
// (history and tool events), so a credential that reaches tool output —
// the proven attack was `bash {"command":"cat ~/.tilde/auth.json"}` in
// build and full-auto, which the path-args deny cannot see — must come
// back redacted from any tool in any mode. The audit log is redacted
// as before.
func TestGateRedactsSecretInResult(t *testing.T) {
	dir := t.TempDir()
	cred := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(cred, []byte(`{"openrouter": "sk-leak-me-123"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	log := NewAuditLog(filepath.Join(dir, "audit.jsonl"), NewRedactor("sk-leak-me-123"))
	g := &Gate{Audit: log} // nil Decide = allow, the full-auto posture

	// The exploit shape: a shell command, whose string the args-level
	// deny never parses.
	out, err := g.Execute(context.Background(), Bash{}, `{"command": "cat `+cred+`"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out, "sk-leak-me-123") {
		t.Errorf("model-facing result leaks the credential: %q", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Errorf("result should carry the redaction marker, got %q", out)
	}

	// The same boundary for a read-tier tool: legitimate content
	// stays, the credential value does not.
	out, err = g.Execute(context.Background(), ReadFile{}, `{"path": "`+cred+`"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out, "sk-leak-me-123") {
		t.Errorf("read result leaks the credential: %q", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Errorf("read result should carry the marker: %q", out)
	}

	// A failing call's output is re-embedded beside the error headline
	// by the orchestrator, so the failure result is redacted too.
	out, err = g.Execute(context.Background(), Bash{}, `{"command": "echo sk-leak-me-123 >&2; exit 3"}`)
	if err == nil {
		t.Fatal("expected the command to fail")
	}
	if strings.Contains(out, "sk-leak-me-123") {
		t.Errorf("failure result leaks the credential: %q", out)
	}

	// The audit log redacts its entries as before (S4): no copy of
	// the credential anywhere, and the boundary change did not alter
	// what the log records.
	data, err := os.ReadFile(log.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-leak-me-123") {
		t.Errorf("audit log leaks the credential: %s", string(data))
	}
}

// A gate without an audit log must not panic: the redaction lives on
// AuditLog, and nil degrades to pass-through — the test/CI posture
// wires gates without logs.
func TestGateNilAuditPassesResultThrough(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(path, []byte("clean output"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &Gate{}
	out, err := g.Execute(context.Background(), ReadFile{}, `{"path": "`+path+`"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "clean output" {
		t.Errorf("out = %q, want clean pass-through", out)
	}
}

func TestGateRecordUserAction(t *testing.T) {
	dir := t.TempDir()
	log := NewAuditLog(filepath.Join(dir, "audit.jsonl"),
		NewRedactor("sk-user-secret"))
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	// Decide denies everything: a user-typed action is recorded without
	// consulting the policy at all.
	g := &Gate{Audit: log, Clock: func() time.Time { return fixed },
		Decide: func(Tool, string) bool { return false }}

	if err := g.RecordUserAction("shell-escape",
		`{"command": "echo sk-user-secret"}`); err != nil {
		t.Fatalf("RecordUserAction: %v", err)
	}

	data, err := os.ReadFile(log.path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d audit lines, want 1", len(lines))
	}
	var entry auditEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("parse entry: %v", err)
	}
	if entry.Tool != "shell-escape" || !entry.Allowed {
		t.Errorf("entry = %+v", entry)
	}
	if entry.Time != "2026-01-02T03:04:05Z" {
		t.Errorf("Time = %q", entry.Time)
	}
	if strings.Contains(string(data), "sk-user-secret") {
		t.Errorf("audit log leaks the credential: %s", string(data))
	}
	if !strings.Contains(entry.Args, "[redacted]") {
		t.Errorf("args not redacted: %q", entry.Args)
	}

	// No audit log configured: recording is a no-op, not a panic.
	if err := (&Gate{}).RecordUserAction("shell-escape", "x"); err != nil {
		t.Errorf("nil Audit should be a no-op, got %v", err)
	}
}

func TestRedactor(t *testing.T) {
	r := NewRedactor("", "abc")
	if got := r.Redact("x abc y abc"); got != "x [redacted] y [redacted]" {
		t.Errorf("Redact = %q", got)
	}
	if got := r.Redact("clean"); got != "clean" {
		t.Errorf("Redact = %q", got)
	}
}

func TestGateUsesInjectedClock(t *testing.T) {
	dir := t.TempDir()
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	log := NewAuditLog(filepath.Join(dir, "audit.jsonl"), nil)
	g := &Gate{Audit: log, Clock: func() time.Time { return fixed }}

	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("d"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Execute(context.Background(), ReadFile{}, `{"path": "`+path+`"}`); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data, _ := os.ReadFile(log.path)
	if !strings.Contains(string(data), `"2026-01-02T03:04:05Z"`) {
		t.Errorf("timestamp not from injected clock: %s", string(data))
	}
}
