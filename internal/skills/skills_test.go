package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, root, name string, fm map[string]string, body string, scripts ...string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("---\n")
	for k, v := range fm {
		b.WriteString(k + ": " + v + "\n")
	}
	b.WriteString("---\n")
	b.WriteString(body)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, s := range scripts {
		if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "scripts", s), []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestParseFrontmatter(t *testing.T) {
	name, desc, body, err := parseFrontmatter("---\nname: deploy\ndescription: \"Deploys the app\"\n---\n\nStep one.\n")
	if err != nil {
		t.Fatalf("parseFrontmatter: %v", err)
	}
	if name != "deploy" || desc != "Deploys the app" {
		t.Errorf("name/desc = %q/%q", name, desc)
	}
	if body != "Step one." {
		t.Errorf("body = %q", body)
	}
}

func TestParseFrontmatterRejectsBadFiles(t *testing.T) {
	cases := map[string]string{
		"no frontmatter": "just a body\n",
		"unclosed":       "---\nname: x\n",
		"missing name":   "---\ndescription: d\n---\nbody\n",
		"missing desc":   "---\nname: x\n---\nbody\n",
	}
	for label, data := range cases {
		if _, _, _, err := parseFrontmatter(data); err == nil {
			t.Errorf("%s: expected error", label)
		}
	}
}

func TestDiscoveryScopesAndPrecedence(t *testing.T) {
	user := t.TempDir()
	project := t.TempDir()

	writeSkill(t, user, "deploy", map[string]string{
		"name": "deploy", "description": "user-level deploy"}, "USER BODY")
	writeSkill(t, user, "only-user", map[string]string{
		"name": "only-user", "description": "user only"}, "U")
	writeSkill(t, project, "deploy", map[string]string{
		"name": "deploy", "description": "project deploy"}, "PROJECT BODY", "run.sh")
	writeSkill(t, project, "only-project", map[string]string{
		"name": "only-project", "description": "project only"}, "P", "go.sh")

	m := &Manager{}
	err := m.Load([]Source{
		{Dir: user, Scope: ScopeUser},
		{Dir: project, Scope: ScopeProject, Trusted: true},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Project wins on the shared name; both scopes' uniques load.
	if s, _ := m.Get("deploy"); s == nil || s.Body != "PROJECT BODY" || s.Scope != ScopeProject {
		t.Errorf("project skill should override the user's: %+v", s)
	}
	if s, _ := m.Get("only-user"); s == nil || s.Scope != ScopeUser {
		t.Error("user skill missing")
	}
	if s, _ := m.Get("only-project"); s == nil {
		t.Error("trusted project skill missing")
	}
	if s, _ := m.Get("deploy"); len(s.Scripts) != 1 || s.Scripts[0] != "run.sh" {
		t.Errorf("scripts = %v", s.Scripts)
	}
}

func TestUntrustedProjectSkillsNeverLoad(t *testing.T) {
	user := t.TempDir()
	project := t.TempDir()
	writeSkill(t, user, "safe", map[string]string{"name": "safe", "description": "d"}, "user")
	writeSkill(t, project, "dangerous", map[string]string{
		"name": "dangerous", "description": "runs scripts"}, "P", "go.sh")

	m := &Manager{}
	if err := m.Load([]Source{
		{Dir: user, Scope: ScopeUser},
		{Dir: project, Scope: ScopeProject, Trusted: false},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Get("dangerous"); ok {
		t.Error("untrusted project skill loaded — the trust gate must exclude it entirely")
	}
	if _, ok := m.Get("safe"); !ok {
		t.Error("user skill should load regardless of project trust")
	}
	if names := m.Names(); len(names) != 1 || names[0] != "safe" {
		t.Errorf("names = %v", names)
	}
}

func TestBadSkillSkippedNotFatal(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "good", map[string]string{"name": "good", "description": "d"}, "body")
	// A folder with no SKILL.md, and one with broken frontmatter.
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, root, "broken", map[string]string{"name": "broken"}, "body")

	m := &Manager{}
	if err := m.Load([]Source{{Dir: root, Scope: ScopeUser}}); err != nil {
		t.Fatalf("one bad skill must not fail discovery: %v", err)
	}
	if _, ok := m.Get("good"); !ok {
		t.Error("good skill lost because of bad siblings")
	}
	if skipped := m.Skipped(); len(skipped) != 2 {
		t.Errorf("skipped = %v, want both bad folders reported", skipped)
	}
}

func TestIndexIsMetadataOnly(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "deploy", map[string]string{
		"name": "deploy", "description": "Deploys things"}, "SECRET BODY INSTRUCTIONS", "run.sh")

	m := &Manager{}
	if err := m.Load([]Source{{Dir: root, Scope: ScopeUser}}); err != nil {
		t.Fatal(err)
	}
	idx := m.Index()
	if !strings.Contains(idx, "deploy") || !strings.Contains(idx, "Deploys things") {
		t.Errorf("index missing metadata: %q", idx)
	}
	if !strings.Contains(idx, "load_skill") {
		t.Errorf("index must tell the model how to load a body: %q", idx)
	}
	// Tier 1 must not leak the body — that is the whole point of
	// progressive disclosure.
	if strings.Contains(idx, "SECRET BODY INSTRUCTIONS") {
		t.Error("index leaks the skill body")
	}
}

func TestIndexEmptyWithNoSkills(t *testing.T) {
	m := &Manager{}
	if err := m.Load([]Source{{Dir: t.TempDir(), Scope: ScopeUser}}); err != nil {
		t.Fatal(err)
	}
	if m.Index() != "" {
		t.Errorf("empty manager index = %q, want empty", m.Index())
	}
}
