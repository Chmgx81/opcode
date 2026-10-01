package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPatchRefusesRelativePathThroughSymlinkOutOfProject is the escape
// safePatchPath's own comment claimed to refuse: a relative path with
// no ".." component returned true unconditionally, so in full-auto —
// where the gate's bound check is deliberately skipped — a patch wrote
// and deleted files outside the project through a symlink. Absolute
// paths went through the writable-roots bound; relative ones must too.
func TestPatchRefusesRelativePathThroughSymlinkOutOfProject(t *testing.T) {
	project, outside := rootSandbox(t)
	// docs and assets are symlinks out of the project, each naming a
	// real file outside it that must survive.
	victim := filepath.Join(outside, "authorized_keys")
	if err := os.WriteFile(victim, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	important := filepath.Join(outside, "important.txt")
	if err := os.WriteFile(important, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{"docs", "assets"} {
		if err := os.Symlink(outside, filepath.Join(project, link)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	t.Chdir(project)

	if safePatchPath(filepath.Join("docs", "authorized_keys")) {
		t.Error("a relative path through a symlink out of the project passed the floor")
	}
	if safePatchPath(filepath.Join("assets", "important.txt")) {
		t.Error("a relative delete path through a symlink out of the project passed the floor")
	}
	// Ordinary in-tree paths still pass: the model emits relative
	// paths for in-tree work, and full-auto must keep doing that.
	for _, p := range []string{"local.go", "sub/dir/f.go", "sub/../f.go", filepath.Join(project, "local.go")} {
		if !safePatchPath(p) {
			t.Errorf("safePatchPath(%q) = false, want true", p)
		}
	}

	// End to end with no gate in front of it: the tool itself refuses
	// both the write and the delete, and the files outside are intact.
	cases := map[string]string{
		"add":    "*** Begin Patch\n*** Add File: docs/authorized_keys\n+stolen\n*** End Patch",
		"delete": "*** Begin Patch\n*** Delete File: assets/important.txt\n*** End Patch",
		"update": "*** Begin Patch\n*** Update File: docs/authorized_keys\n@@\n-original\n+stolen\n*** End Patch",
	}
	for kind, patch := range cases {
		args, _ := json.Marshal(map[string]string{"patch": patch})
		if _, err := run(t, ApplyPatch{}, string(args)); err == nil {
			t.Errorf("%s through a symlink out of the project was applied", kind)
		}
	}
	if data, _ := os.ReadFile(victim); string(data) != "original" {
		t.Errorf("the file outside the project was rewritten: %q", data)
	}
	if _, err := os.Stat(important); err != nil {
		t.Errorf("the file outside the project was deleted: %v", err)
	}
}

// The bound is the same resolved one the gate uses, so a relative path
// whose ".." lands back inside the project is ordinary work and keeps
// working in full-auto.
func TestPatchAllowsDotDotThatResolvesInsideProject(t *testing.T) {
	project, _ := rootSandbox(t)
	if err := os.MkdirAll(filepath.Join(project, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	for _, p := range []string{
		filepath.Join("sub", "..", "file.go"),
		filepath.Join("sub", "..", "sub", "file.go"),
		filepath.Join("sub", "..", "new", "nested", "file.go"),
	} {
		if !safePatchPath(p) {
			t.Errorf("safePatchPath(%q) = false, want true (it resolves inside the project)", p)
		}
	}
	// A relative path that genuinely climbs out is still refused on its
	// own terms, before any resolution.
	if safePatchPath(join("..", "escape.txt")) {
		t.Error("parent traversal must be refused outright")
	}
}

// The tool-level error for an out-of-project path must say so plainly,
// and must not echo a model-controlled path into the message in a way
// that would matter — it is a message, not a write.
func TestPatchOutOfProjectErrorNamesThePath(t *testing.T) {
	project, outside := rootSandbox(t)
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "docs")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Chdir(project)
	args, _ := json.Marshal(map[string]string{
		"patch": "*** Begin Patch\n*** Add File: docs/secret.txt\n+x\n*** End Patch",
	})
	_, err := run(t, ApplyPatch{}, string(args))
	if err == nil {
		t.Fatal("the patch was applied outside the project")
	}
	if !strings.Contains(err.Error(), "outside the project") {
		t.Errorf("error = %v, want it to name the refusal", err)
	}
}
