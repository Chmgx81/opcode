package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chmgx81/tilde/internal/sandbox"
)

// TestMain implements the re-exec child, the same pattern as the
// sandbox package's own tests: sandbox.Command execs this binary with
// "__sandbox" as the first argument, so the test binary must run the
// child instead of its own tests.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "__sandbox" {
		if err := sandbox.Child(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return // Child replaced the process on success
	}
	os.Exit(m.Run())
}

// TestLooksLikeSandboxDenial: the signature matcher — each denial
// phrase matches, the near-misses do not.
func TestLooksLikeSandboxDenial(t *testing.T) {
	for _, s := range []string{
		"touch: cannot touch '/etc/passwd': Permission denied",
		"cp: cannot create file: Read-only file system",
		"mount: Operation not permitted",
	} {
		if !looksLikeSandboxDenial(s) {
			t.Errorf("denial %q did not match", s)
		}
	}
	for _, s := range []string{
		"ls: cannot access '/nope': No such file or directory",
		"command not found",
		"all good",
	} {
		if looksLikeSandboxDenial(s) {
			t.Errorf("non-denial %q matched", s)
		}
	}
}

// installSandbox wires the real Landlock runner for the package, the
// same wiring cmd/tilde does, and un-wires it after the test.
func installSandbox(t *testing.T) {
	t.Helper()
	if !sandbox.Supported() {
		t.Skip("Landlock unsupported on this platform")
	}
	r, err := sandbox.New(true)
	if err != nil {
		t.Fatalf("sandbox runner: %v", err)
	}
	sandbox.Install(r)
	t.Cleanup(func() { sandbox.Install(nil) })
}

// TestSandboxDenialClassified: a sandboxed write outside the writable
// roots carries the plain-language cause and the documented escape —
// and keeps the shell's original complaint below the note.
func TestSandboxDenialClassified(t *testing.T) {
	installSandbox(t)

	out, err := (Bash{}).Execute(context.Background(),
		`{"command": "touch /etc/tilde-denial-probe"}`)
	if err == nil {
		t.Fatalf("out-of-scope write succeeded: %q", out)
	}
	if !strings.Contains(out, "ran inside the sandbox") {
		t.Errorf("denial note missing:\n%s", out)
	}
	if !strings.Contains(out, `"sandbox": false`) {
		t.Errorf("note does not name the escape:\n%s", out)
	}
	if !strings.Contains(out, "Permission denied") {
		t.Errorf("the original error was dropped:\n%s", out)
	}
}

// TestInScopeWriteUnclassified: a write inside the writable roots
// succeeds and carries no note — the classification must be invisible
// when nothing was denied.
func TestInScopeWriteUnclassified(t *testing.T) {
	installSandbox(t)

	target := filepath.Join(t.TempDir(), "ok.txt")
	out, err := (Bash{}).Execute(context.Background(),
		fmt.Sprintf(`{"command": "touch %s"}`, target))
	if err != nil {
		t.Fatalf("in-scope write failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "ran inside the sandbox") {
		t.Errorf("in-scope write carried the denial note:\n%s", out)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("the write did not land: %v", err)
	}
}

// TestUnsandboxedDenialUnclassified: a non-sandboxed EACCES is a real
// permission error and must not be mislabeled as the sandbox. The
// unwritable file is built with chmod so the test does not depend on
// any particular host's directory ownership.
func TestUnsandboxedDenialUnclassified(t *testing.T) {
	installSandbox(t)

	target := filepath.Join(t.TempDir(), "locked")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(target, 0o644) })

	out, err := (Bash{}).Execute(context.Background(),
		fmt.Sprintf(`{"command": "echo x > %s", "sandbox": false}`, target))
	if err == nil {
		t.Fatalf("unexpected success: %q", out)
	}
	if strings.Contains(out, "ran inside the sandbox") {
		t.Errorf("an unsandboxed denial was mislabeled as the sandbox:\n%s", out)
	}
	if !strings.Contains(out, "Permission denied") {
		t.Errorf("original error missing:\n%s", out)
	}
}
