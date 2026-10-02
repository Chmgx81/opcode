package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// doctorText returns the text of the last transcript entry (the report)
// after running /doctor, failing if no entry landed.
func doctorText(t *testing.T, m *Model) string {
	t.Helper()
	before := len(m.entries)
	m.doctor()
	if len(m.entries) != before+1 {
		t.Fatalf("doctor added %d entries, want 1", len(m.entries)-before)
	}
	return m.entries[len(m.entries)-1].text
}

// TestDoctorHealthyReport: with a key present, an audit log on disk, and
// no config overrides, every row renders — and the resolved key never
// appears in the report.
func TestDoctorHealthyReport(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	const key = "sk-doctor-secret"
	m.opt.KeyFor = func(provider string) (string, bool) { return key, true }
	if err := os.WriteFile(filepath.Join(dir, "audit.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report := doctorText(t, m)
	for _, want := range []string{
		"opcode " + version,
		"model test-model",
		"api key resolved for openrouter",
		"sandbox:",
		"project trust:",
		"skills:",
		"mcp:",
		"audit log:",
		"terminal:",
		"no config.json — defaults in effect",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, key) {
		t.Errorf("the report leaks the api key:\n%s", report)
	}
	if strings.Contains(report, "· \n") || strings.HasSuffix(report, "·") {
		t.Errorf("empty row in report:\n%s", report)
	}
}

// TestDoctorMissingKey: a provider without a key is a warning that names
// the next step, not a dead end.
func TestDoctorMissingKey(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.opt.KeyFor = func(provider string) (string, bool) { return "", false }

	report := doctorText(t, m)
	if !strings.Contains(report, "no api key for openrouter — /login openrouter") {
		t.Errorf("missing-key row = %q", report)
	}
}

// TestDoctorUnwiredKey: a nil KeyFor (headless wiring) reports honestly
// instead of pretending to know.
func TestDoctorUnwiredKey(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	report := doctorText(t, m)
	if !strings.Contains(report, "api key: resolution not wired") {
		t.Errorf("nil-key row = %q", report)
	}
}

// TestDoctorBrokenConfig: a malformed config.json is reported verbatim,
// and the command survives it.
func TestDoctorBrokenConfig(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	report := doctorText(t, m)
	if !strings.Contains(report, "parse config.json") {
		t.Errorf("broken-config row = %q", report)
	}
	if !strings.Contains(report, "opcode "+version) {
		t.Errorf("the version row vanished under the failure:\n%s", report)
	}
}

// TestDoctorMissingModel: an empty model is the one hard failure row —
// nothing can run until one is picked — and the way out is the UI's own
// picker, not a file path.
func TestDoctorMissingModel(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.opt.Model = ""
	m.opt.ProviderName = ""

	report := doctorText(t, m)
	if !strings.Contains(report, `no model picked yet — /models opens the picker`) {
		t.Errorf("no-model row = %q", report)
	}
}

// TestDoctorMissingAuditFile: an audit path that does not exist yet is a
// dim fact (it is created on the first gated action), not an error.
func TestDoctorMissingAuditFile(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.opt.AuditPath = filepath.Join(dir, "never-created.jsonl")

	report := doctorText(t, m)
	if !strings.Contains(report, "created on the first gated action") {
		t.Errorf("missing-audit row = %q", report)
	}
}
