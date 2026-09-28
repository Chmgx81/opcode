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
