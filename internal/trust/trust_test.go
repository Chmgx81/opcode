package trust

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestSurfaceListsExecutableFiles(t *testing.T) {
	project := t.TempDir()
	write(t, filepath.Join(project, ".tilde/skills/deploy/scripts/run.sh"), "echo hi")
	write(t, filepath.Join(project, ".tilde/skills/deploy/SKILL.md"), "body")
	write(t, filepath.Join(project, ".tilde/skills/deploy/references/api.md"), "docs")
	write(t, filepath.Join(project, ".tilde/config.json"), `{}`)
	write(t, filepath.Join(project, "notes.txt"), "not executable surface")

	files, err := Surface(project)
	if err != nil {
		t.Fatalf("Surface: %v", err)
	}
	want := []string{
		".tilde/config.json",
		".tilde/skills/deploy/scripts/run.sh",
	}
	if len(files) != 2 {
		t.Fatalf("surface = %v, want %v", files, want)
	}
	for i, f := range want {
		if files[i] != f {
			t.Errorf("surface[%d] = %q, want %q", i, files[i], f)
		}
	}
}

func TestSurfaceEmptyProjectIsTrustedByDefault(t *testing.T) {
	project := t.TempDir()
	store, err := LoadStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	status, files, err := store.Status(project)
	if err != nil {
		t.Fatal(err)
	}
	if status != Trusted {
		t.Errorf("empty project status = %v, want Trusted (nothing to run)", status)
	}
	if len(files) != 0 {
		t.Errorf("files = %v, want empty", files)
	}
}

func TestTrustLifecycle(t *testing.T) {
	project := t.TempDir()
	write(t, filepath.Join(project, ".tilde/skills/x/scripts/go.sh"), "echo one")
	userDir := t.TempDir()

	store, err := LoadStore(userDir)
	if err != nil {
		t.Fatal(err)
	}

	// Never seen: untrusted, and the surface list is available for the
	// prompt to show.
	status, files, err := store.Status(project)
	if err != nil || status != Untrusted {
		t.Fatalf("status = %v err = %v, want Untrusted", status, err)
	}
	if len(files) != 1 || files[0] != ".tilde/skills/x/scripts/go.sh" {
		t.Errorf("files = %v", files)
	}

	// Approve, then the same project is trusted — and the decision
	// survives a store reload.
	if err := store.Trust(project); err != nil {
		t.Fatalf("Trust: %v", err)
	}
	if status, _, _ := store.Status(project); status != Trusted {
		t.Fatalf("status after trust = %v", status)
	}
	reloaded, err := LoadStore(userDir)
	if err != nil {
		t.Fatal(err)
	}
	if status, _, _ := reloaded.Status(project); status != Trusted {
		t.Error("trust decision did not persist")
	}

	// Change the script: the fingerprint no longer matches, so the
	// project asks again rather than inheriting old trust.
	write(t, filepath.Join(project, ".tilde/skills/x/scripts/go.sh"), "echo two")
	if status, _, _ := reloaded.Status(project); status != Changed {
		t.Errorf("status after surface change = %v, want Changed", status)
	}

	// A new script appearing is also a change.
	if err := reloaded.Trust(project); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(project, ".tilde/skills/x/scripts/new.sh"), "echo new")
	if status, _, _ := reloaded.Status(project); status != Changed {
		t.Errorf("status after new script = %v, want Changed", status)
	}
}

func TestFingerprintStableAcrossRuns(t *testing.T) {
	project := t.TempDir()
	write(t, filepath.Join(project, ".tilde/skills/a/scripts/1.sh"), "x")
	write(t, filepath.Join(project, ".tilde/skills/b/scripts/2.sh"), "y")

	f1, _, err := Fingerprint(project)
	if err != nil {
		t.Fatal(err)
	}
	f2, _, err := Fingerprint(project)
	if err != nil {
		t.Fatal(err)
	}
	if f1 != f2 {
		t.Error("fingerprint is not deterministic for an unchanged surface")
	}

	// A different project with the same content should differ only by
	// nothing (paths are relative), so identical trees share a
	// fingerprint — that is fine: the store keys by path.
	other := t.TempDir()
	write(t, filepath.Join(other, ".tilde/skills/a/scripts/1.sh"), "x")
	write(t, filepath.Join(other, ".tilde/skills/b/scripts/2.sh"), "y")
	f3, _, _ := Fingerprint(other)
	if f1 != f3 {
		t.Log("note: identical trees share a fingerprint; the store keys by path")
	}
}

func TestUntrust(t *testing.T) {
	project := t.TempDir()
	write(t, filepath.Join(project, ".tilde/skills/x/scripts/go.sh"), "echo one")
	userDir := t.TempDir()
	store, _ := LoadStore(userDir)
	if err := store.Trust(project); err != nil {
		t.Fatal(err)
	}
	if err := store.Untrust(project); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := store.Status(project); status != Untrusted {
		t.Errorf("status after untrust = %v", status)
	}
	// Untrusting something never trusted is a no-op, not an error.
	if err := store.Untrust(project); err != nil {
		t.Errorf("second Untrust: %v", err)
	}
}
