package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func loadOne(t *testing.T, root string) *Manager {
	t.Helper()
	m := &Manager{}
	if err := m.Load([]Source{{Dir: root, Scope: ScopeUser}}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return m
}

func skippedContains(m *Manager, parts ...string) bool {
	for _, s := range m.Skipped() {
		ok := true
		for _, p := range parts {
			if !strings.Contains(s, p) {
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func TestOversizedSkillFileIsSkippedNotLoaded(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "ok", map[string]string{"name": "ok", "description": "d"}, "fine")
	writeSkill(t, root, "huge", map[string]string{"name": "huge", "description": "d"},
		strings.Repeat("A", maxSkillFileBytes+1))

	m := loadOne(t, root)
	if _, ok := m.Get("huge"); ok {
		t.Error("a SKILL.md over the size cap was loaded into memory and the model's context")
	}
	if !skippedContains(m, "huge", "too large") {
		t.Errorf("oversize skill not reported: %v", m.Skipped())
	}
	if _, ok := m.Get("ok"); !ok {
		t.Error("one oversized skill must not take the others down")
	}
}

func TestSkillFileAtTheCapStillLoads(t *testing.T) {
	root := t.TempDir()
	prefix := "---\nname: edge\ndescription: d\n---\n"
	body := strings.Repeat("B", maxSkillFileBytes-len(prefix))
	if err := os.MkdirAll(filepath.Join(root, "edge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "edge", "SKILL.md"), []byte(prefix+body), 0o644); err != nil {
		t.Fatal(err)
	}
	m := loadOne(t, root)
	if _, ok := m.Get("edge"); !ok {
		t.Errorf("a file of exactly the cap must load: %v", m.Skipped())
	}
}

func TestSkillFileSymlinkedOutOfTreeIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "notes.md")
	if err := os.WriteFile(secret, []byte("---\nname: leak\ndescription: d\n---\nTOP SECRET BODY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "leak"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "leak", "SKILL.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	m := loadOne(t, root)
	if s, ok := m.Get("leak"); ok {
		t.Errorf("SKILL.md symlinked outside its folder was loaded; body=%q", s.Body)
	}
	if !skippedContains(m, "leak", "outside") {
		t.Errorf("escape not reported: %v", m.Skipped())
	}
}

func TestSkillFileSymlinkedWithinItsFolderIsAllowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	root := t.TempDir()
	dir := writeSkill(t, root, "linked", map[string]string{"name": "linked", "description": "d"}, "real body")
	real := filepath.Join(dir, "real.md")
	if err := os.Rename(filepath.Join(dir, "SKILL.md"), real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.md", filepath.Join(dir, "SKILL.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	m := loadOne(t, root)
	if s, ok := m.Get("linked"); !ok || s.Body != "real body" {
		t.Errorf("in-folder symlink rejected: %v %v", ok, m.Skipped())
	}
}

func TestSymlinkedSkillDirectoryIsReportedNotSilentlyDropped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	root := t.TempDir()
	elsewhere := t.TempDir()
	writeSkill(t, elsewhere, "real", map[string]string{"name": "real", "description": "d"}, "b")
	if err := os.Symlink(filepath.Join(elsewhere, "real"), filepath.Join(root, "real")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	m := loadOne(t, root)
	if _, ok := m.Get("real"); ok {
		t.Error("a symlinked skill directory must not be followed out of the skills tree")
	}
	if !skippedContains(m, "real", "symlink") {
		t.Errorf("the drop must be visible in Skipped: %v", m.Skipped())
	}
}

func TestHostileFrontmatterIsRejected(t *testing.T) {
	// The name keys the skill table and appears in the system prompt
	// index; the description is injected into every request.
	tests := []struct {
		name string
		fm   string
	}{
		{"path traversal in name", "name: ../../etc/passwd\ndescription: d"},
		{"slash in name", "name: a/b\ndescription: d"},
		{"backslash in name", "name: a\\b\ndescription: d"},
		{"dot-dot name", "name: ..\ndescription: d"},
		{"escape sequence in name", "name: evil\x1b]0;pwn\x07\ndescription: d"},
		{"carriage return in name", "name: a\rb\ndescription: d"},
		{"escape sequence in description", "name: ok\ndescription: hi\x1b[2Jthere"},
		{"NUL in description", "name: ok\ndescription: a\x00b"},
		{"description over the cap", "name: ok\ndescription: " + strings.Repeat("x", maxDescriptionBytes+1)},
		{"name over the cap", "name: " + strings.Repeat("n", maxNameBytes+1) + "\ndescription: d"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "folder")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			data := "---\n" + tc.fm + "\n---\nbody\n"
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
			m := loadOne(t, root)
			if names := m.Names(); len(names) != 0 {
				t.Errorf("hostile skill loaded as %q", names)
			}
			if len(m.Skipped()) != 1 {
				t.Errorf("rejection not reported: %v", m.Skipped())
			}
		})
	}
}

func TestOrdinaryNamesAndDescriptionsStillLoad(t *testing.T) {
	// Guard against over-tightening: real skills use punctuation,
	// unicode, quotes, colons, and hyphens.
	tests := []struct{ name, desc string }{
		{"deploy-prod", "Deploys the app: staging then prod"},
		{"code_review", `Reviews "risky" diffs (fast)`},
		{"Résumé.helper", "Édite le CV — with unicode ✓"},
		{"v2", "tabs\tare fine in descriptions"},
	}
	root := t.TempDir()
	for i, tc := range tests {
		dir := filepath.Join(root, "s"+string(rune('a'+i)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		data := "---\nname: " + tc.name + "\ndescription: " + tc.desc + "\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := loadOne(t, root)
	if len(m.Names()) != len(tests) {
		t.Errorf("loaded %v, skipped %v", m.Names(), m.Skipped())
	}
}

func TestParseFrontmatterToleratesCRLFAndBOM(t *testing.T) {
	// Skills authored on Windows, or checked out with autocrlf, must
	// not fail with "frontmatter must start with ---".
	tests := map[string]string{
		"CRLF":     "---\r\nname: win\r\ndescription: from windows\r\n---\r\n\r\nBody line.\r\n",
		"BOM":      "\ufeff---\nname: win\ndescription: from windows\n---\n\nBody line.\n",
		"BOM+CRLF": "\ufeff---\r\nname: win\r\ndescription: from windows\r\n---\r\nBody line.\r\n",
	}
	for label, data := range tests {
		t.Run(label, func(t *testing.T) {
			name, desc, body, err := parseFrontmatter(data)
			if err != nil {
				t.Fatalf("parseFrontmatter: %v", err)
			}
			if name != "win" || desc != "from windows" {
				t.Errorf("name/desc = %q/%q (stray \\r would make these differ)", name, desc)
			}
			if strings.Contains(body, "\r") {
				t.Errorf("body keeps carriage returns: %q", body)
			}
			if !strings.HasPrefix(body, "Body line.") {
				t.Errorf("body = %q", body)
			}
		})
	}
}

func TestScriptsListingIgnoresSubdirectoriesAndIsSorted(t *testing.T) {
	root := t.TempDir()
	dir := writeSkill(t, root, "s", map[string]string{"name": "s", "description": "d"}, "b", "zeta.sh", "alpha.sh")
	if err := os.MkdirAll(filepath.Join(dir, "scripts", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := loadOne(t, root)
	s, _ := m.Get("s")
	if got := strings.Join(s.Scripts, ","); got != "alpha.sh,zeta.sh" {
		t.Errorf("Scripts = %q, want sorted files only", got)
	}
}

func TestLoadUnreadableSourceDirIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	m := &Manager{}
	if err := m.Load([]Source{{Dir: root, Scope: ScopeUser}}); err == nil {
		t.Error("an unreadable skills dir must be reported, not treated as empty")
	}
}

func TestReloadReplacesTheSetAtomically(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "first", map[string]string{"name": "first", "description": "d"}, "b")
	m := loadOne(t, root)
	if err := os.RemoveAll(filepath.Join(root, "first")); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, root, "second", map[string]string{"name": "second", "description": "d"}, "b")
	if err := m.Load([]Source{{Dir: root, Scope: ScopeUser}}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(m.Names(), ","); got != "second" {
		t.Errorf("names after reload = %q, want only the new set", got)
	}
}
