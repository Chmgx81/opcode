package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chmgx81/tilde/internal/skills"
)

func skillFixture(t *testing.T) (*skills.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "wordcount")
	if err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillMD := `---
name: wordcount
description: Counts words in text
---
Count the words with the script.`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nwc -w\n"
	if err := os.WriteFile(filepath.Join(skillDir, "scripts", "count.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	m := &skills.Manager{}
	if err := m.Load([]skills.Source{{Dir: dir, Scope: skills.ScopeUser}}); err != nil {
		t.Fatal(err)
	}
	return m, dir
}

func TestLoadSkillReturnsBody(t *testing.T) {
	m, _ := skillFixture(t)
	tool := LoadSkill{Manager: m}

	out, err := tool.Execute(context.Background(), `{"name": "wordcount"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Count the words") {
		t.Errorf("body missing: %q", out)
	}
	if !strings.Contains(out, "count.sh") {
		t.Errorf("script listing missing: %q", out)
	}

	if _, err := tool.Execute(context.Background(), `{"name": "nope"}`); err == nil {
		t.Error("unknown skill must error")
	}
}

func TestRunSkillScriptContract(t *testing.T) {
	m, _ := skillFixture(t)
	tool := RunSkillScript{Manager: m}

	// First a script that answers with plain prose, which violates the
	// I/O contract — stdout must be JSON.
	wc, _ := m.Get("wordcount")
	bad := filepath.Join(wc.Dir, "scripts", "count.sh")
	if err := os.WriteFile(bad, []byte("#!/bin/sh\necho sorry not json\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(),
		`{"skill": "wordcount", "script": "count.sh", "input": "{\"n\": 1}"}`); err == nil {
		t.Error("non-JSON stdout must be a contract violation")
	}

	// Replace with a JSON-answering script.
	if err := os.WriteFile(bad, []byte("#!/bin/sh\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := tool.Execute(context.Background(),
		`{"skill": "wordcount", "script": "count.sh", "input": "{\"n\": 3}"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !json.Valid([]byte(out)) || !strings.Contains(out, `"n": 3`) {
		t.Errorf("script output = %q", out)
	}
}

func TestRunSkillScriptNoTraversal(t *testing.T) {
	m, _ := skillFixture(t)
	tool := RunSkillScript{Manager: m}

	// Path traversal and unlisted scripts must not run — only discovered
	// basenames are acceptable.
	for _, script := range []string{"../SKILL.md", "subdir/x.sh", "not-there.sh"} {
		if _, err := tool.Execute(context.Background(),
			`{"skill": "wordcount", "script": "`+script+`", "input": "{}"}`); err == nil {
			t.Errorf("script %q must be rejected", script)
		}
	}
}

func TestRunSkillScriptRejectsBadInput(t *testing.T) {
	m, _ := skillFixture(t)
	tool := RunSkillScript{Manager: m}
	if _, err := tool.Execute(context.Background(),
		`{"skill": "wordcount", "script": "count.sh", "input": "not json"}`); err == nil {
		t.Error("non-JSON input must be rejected")
	}
	// Empty input defaults to an empty JSON object, not an error.
	wc, _ := m.Get("wordcount")
	dir := filepath.Join(wc.Dir, "scripts", "count.sh")
	if err := os.WriteFile(dir, []byte("#!/bin/sh\necho {}\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(),
		`{"skill": "wordcount", "script": "count.sh"}`); err != nil {
		t.Errorf("empty input should default to {}: %v", err)
	}
}

func TestSkillToolTiers(t *testing.T) {
	m, _ := skillFixture(t)
	if (LoadSkill{Manager: m}).Tier() != TierReadOnly {
		t.Error("load_skill must be Read-Only")
	}
	if (RunSkillScript{Manager: m}).Tier() != TierActionAllowed {
		t.Error("run_skill_script must be Action-Allowed")
	}
}
