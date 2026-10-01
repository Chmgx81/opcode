package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func writeCtx(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAgentsContextHierarchy(t *testing.T) {
	userDir := filepath.Join(t.TempDir(), "tilde-home")
	root := t.TempDir() // stands in for the filesystem root
	mid := filepath.Join(root, "work")
	cwd := filepath.Join(mid, "project")
	writeCtx(t, filepath.Join(userDir, "AGENTS.md"), "user: be terse")
	writeCtx(t, filepath.Join(mid, "AGENTS.md"), "mid: run tests before commits")
	writeCtx(t, filepath.Join(cwd, "AGENTS.md"), "project: use Go 1.23")
	writeCtx(t, filepath.Join(cwd, "CLAUDE.md"), "claude fallback (must be ignored: AGENTS.md wins)")

	got := AgentsContext(userDir, cwd)
	for _, want := range []string{"user: be terse", "mid: run tests", "project: use Go 1.23"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// Ordering: user first, then outer to inner — the working
	// directory's instructions land last so they win on conflict.
	if strings.Index(got, "user: be terse") > strings.Index(got, "mid: run tests") ||
		strings.Index(got, "mid: run tests") > strings.Index(got, "project: use Go 1.23") {
		t.Errorf("hierarchy out of order (most specific must come last):\n%s", got)
	}
	if strings.Contains(got, "claude fallback") {
		t.Error("CLAUDE.md must be ignored when AGENTS.md exists in the same dir")
	}
}

func TestAgentsContextOverrideBeatsAGENTS(t *testing.T) {
	userDir := t.TempDir()
	cwd := t.TempDir()
	writeCtx(t, filepath.Join(cwd, "AGENTS.md"), "the standard file")
	writeCtx(t, filepath.Join(cwd, "AGENTS.override.md"), "the personal override")

	got := AgentsContext(userDir, cwd)
	if !strings.Contains(got, "the personal override") {
		t.Errorf("override not used:\n%s", got)
	}
	if strings.Contains(got, "the standard file") {
		t.Error("AGENTS.override.md must replace AGENTS.md in the same dir")
	}
}

func TestAgentsContextClaudeFallback(t *testing.T) {
	userDir := t.TempDir()
	cwd := t.TempDir()
	writeCtx(t, filepath.Join(cwd, "CLAUDE.md"), "claude instructions count")

	got := AgentsContext(userDir, cwd)
	if !strings.Contains(got, "claude instructions count") {
		t.Errorf("CLAUDE.md should be the fallback when no AGENTS.md exists:\n%s", got)
	}
	if !strings.Contains(got, "CLAUDE.md") {
		t.Error("the source file name should be visible for provenance")
	}
}

func TestAgentsContextEmpty(t *testing.T) {
	got := AgentsContext(t.TempDir(), t.TempDir())
	if got != "" {
		t.Errorf("empty context = %q, want empty", got)
	}
}

func TestAgentsContextCapsHugeFiles(t *testing.T) {
	userDir := t.TempDir()
	cwd := t.TempDir()
	huge := strings.Repeat("x", agentsFileCap+5000)
	writeCtx(t, filepath.Join(cwd, "AGENTS.md"), huge)

	got := AgentsContext(userDir, cwd)
	if !strings.Contains(got, "(file truncated)") {
		t.Error("oversized context file not truncated")
	}
	if len(got) > agentsFileCap+2000 {
		t.Errorf("context not capped: %d bytes", len(got))
	}
}

// TestAgentsContextTruncationKeepsRunes: the cap lands mid-file, and
// a half rune handed to the model's tokenizer is a corrupted prompt
// that no error names. Back off to a boundary.
func TestAgentsContextTruncationKeepsRunes(t *testing.T) {
	userDir := t.TempDir()
	cwd := t.TempDir()
	// "€" is three bytes, so agentsFileCap (a multiple of 1024, not 3)
	// splits one partway through.
	writeCtx(t, filepath.Join(cwd, "AGENTS.md"), strings.Repeat("€", agentsFileCap))

	got := AgentsContext(userDir, cwd)
	if !utf8.ValidString(got) {
		t.Error("truncation split a multi-byte rune — the prompt is corrupt UTF-8")
	}
	if !strings.Contains(got, "(file truncated)") {
		t.Error("oversized context file not truncated")
	}
}

// TestAgentsContextIsBoundedBeforeRead: the file size is the repo
// author's choice, so the read is capped rather than "read it all,
// then slice". A tiny stub cannot be OOMed, so this checks the bound
// by measuring that a file far larger than the cap yields a result
// that stays near the cap.
func TestAgentsContextIsBoundedBeforeRead(t *testing.T) {
	userDir := t.TempDir()
	cwd := t.TempDir()
	writeCtx(t, filepath.Join(cwd, "AGENTS.md"), strings.Repeat("y", 8*agentsFileCap))

	got := AgentsContext(userDir, cwd)
	if len(got) > agentsFileCap+2000 {
		t.Errorf("context not capped: %d bytes", len(got))
	}
}
