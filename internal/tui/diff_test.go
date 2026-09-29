package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// gitRepo builds a real repository with one committed file, so /diff
// tests exercise the real git, the real bash path, and the real
// sanitizer — no fakes.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("old line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "tracked.txt")
	run("commit", "-q", "-m", "first")
	return dir
}

func lastEntry(t *testing.T, m *Model) entry {
	t.Helper()
	if len(m.entries) == 0 {
		t.Fatal("no entries")
	}
	return m.entries[len(m.entries)-1]
}

// TestRenderDiffBodyColors: each line class carries its verdict color —
// additions succeed, deletions danger, hunks info, headers chrome.
func TestRenderDiffBodyColors(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	body := renderDiffBody(strings.Join([]string{
		"diff --git a/a.txt b/a.txt",
		"index 1234567..89abcde 100644",
		"--- a/a.txt",
		"+++ b/a.txt",
		"@@ -1 +1 @@",
		"-old",
		"+new",
		" ctx",
	}, "\n"))
	if len(body) != 8 {
		t.Fatalf("body = %d lines, want 8", len(body))
	}
	checks := []struct {
		idx  int
		rgb  string
		name string
	}{
		{5, "38;2;248;113;113", "deleted line danger"}, // #f87171
		{6, "38;2;73;222;128", "added line success"},   // #4ade80 (lipgloss gamut-shifts 74->73)
		{4, "38;2;96;165;250", "hunk header info"},     // #60a5fa
		{0, "1m", "file header bold"},
	}
	for _, c := range checks {
		if !strings.Contains(body[c.idx], c.rgb) {
			t.Errorf("%s: line %q missing %s", c.name, stripANSI(body[c.idx]), c.rgb)
		}
	}
}

// TestRenderDiffBodyCap: a monster diff is bounded and says how much
// was cut — flooding the transcript is its own failure.
func TestRenderDiffBodyCap(t *testing.T) {
	lines := make([]string, diffLineCap+100)
	for i := range lines {
		lines[i] = "+line"
	}
	body := renderDiffBody(strings.Join(lines, "\n"))
	if len(body) != diffLineCap+1 {
		t.Fatalf("body = %d lines, want %d+1", len(body), diffLineCap)
	}
	if !strings.Contains(body[len(body)-1], "100 more lines") {
		t.Errorf("cap footer = %q", stripANSI(body[len(body)-1]))
	}
}

// TestShowDiffModifiedAndUntracked: the real git against a real repo —
// a modified tracked file renders as a colored diff and the untracked
// file is listed, because git diff alone would hide it.
func TestShowDiffModifiedAndUntracked(t *testing.T) {
	dir := gitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("new line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fresh.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := newText(t, dir, nil)
	m.opt.Cwd = dir
	m.showDiff()

	e := lastEntry(t, m)
	if e.kind != entryDiff {
		t.Fatalf("entry kind = %v, want entryDiff", e.kind)
	}
	plain := stripANSI(e.text)
	for _, want := range []string{
		"diff --git a/tracked.txt b/tracked.txt",
		"-old line",
		"+new line",
		"1 untracked",
		"fresh.txt",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("diff missing %q:\n%s", want, plain)
		}
	}
}

// TestShowDiffStagedToo: a staged change shows in the HEAD diff — the
// review covers the whole working tree, not just unstaged edits.
func TestShowDiffStagedToo(t *testing.T) {
	dir := gitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("staged line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", dir, "add", "tracked.txt")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	m, _ := newText(t, dir, nil)
	m.opt.Cwd = dir
	m.showDiff()

	plain := stripANSI(lastEntry(t, m).text)
	if !strings.Contains(plain, "+staged line") {
		t.Errorf("staged change missing from the diff:\n%s", plain)
	}
}

// TestShowDiffClean: nothing to report is a dim fact, not an error.
func TestShowDiffClean(t *testing.T) {
	dir := gitRepo(t)
	m, _ := newText(t, dir, nil)
	m.opt.Cwd = dir
	m.showDiff()

	e := lastEntry(t, m)
	if e.kind != entryDim || !strings.Contains(e.text, "working tree clean") {
		t.Errorf("clean tree entry = %+v", e)
	}
}

// TestShowDiffNotARepo: /diff names its requirement instead of dumping
// git's fatal at the user.
func TestShowDiffNotARepo(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.opt.Cwd = dir
	m.showDiff()

	e := lastEntry(t, m)
	if e.kind != entryDim || !strings.Contains(e.text, "not a git repository") {
		t.Errorf("non-repo entry = %+v", e)
	}
}

// TestShowDiffNoCommits: a repo with no HEAD falls back to the plain
// diff and still lists untracked files.
func TestShowDiffNoCommits(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("git", "-C", dir, "init", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := newText(t, dir, nil)
	m.opt.Cwd = dir
	m.showDiff()

	e := lastEntry(t, m)
	plain := stripANSI(e.text)
	if e.kind != entryDiff {
		t.Fatalf("entry kind = %v, want entryDiff", e.kind)
	}
	if !strings.Contains(plain, "new.txt") {
		t.Errorf("untracked file missing:\n%s", plain)
	}
	if strings.Contains(plain, "error") {
		t.Errorf("the empty repo produced an error:\n%s", plain)
	}
}

// TestDiffPayloadSanitized: filenames and contents pass safe.Text —
// a poisoned repo cannot drive the terminal through its own diff.
func TestDiffPayloadSanitized(t *testing.T) {
	dir := gitRepo(t)
	name := "evil\x1b]0;pwned\x07.txt"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := newText(t, dir, nil)
	m.opt.Cwd = dir
	m.showDiff()

	e := lastEntry(t, m)
	if strings.ContainsAny(e.text, "\x1b\x07") {
		t.Errorf("diff entry carries control bytes: %q", e.text)
	}
	// git porcelain quotes control characters C-style ("\033]0;..."),
	// so the hostile name arrives as inert printable text — visible,
	// named, and incapable of driving the terminal.
	if strings.Contains(e.text, "\x1b]0;") {
		t.Errorf("a live OSC sequence reached the diff entry")
	}
}
