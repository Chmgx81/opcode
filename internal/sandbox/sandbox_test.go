package sandbox

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestMain implements the re-exec child: the test binary runs itself
// with TILDE_SANDBOX_CHILD set; the child applies the real Landlock
// ruleset (env names the writable root and the write target) and
// reports whether the write went through. This exercises the real
// syscalls, not a mock — the enforcement claim rests on it.
func TestMain(m *testing.M) {
	if os.Getenv("TILDE_SANDBOX_CHILD") == "1" {
		writable := os.Getenv("TILDE_SANDBOX_WRITABLE")
		target := os.Getenv("TILDE_SANDBOX_TARGET")
		if err := Apply([]string{writable}); err != nil {
			os.Stdout.WriteString("APPLY-ERR:" + err.Error())
			os.Exit(2)
		}
		if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
			os.Stdout.WriteString("DENIED")
			os.Exit(3)
		}
		os.Stdout.WriteString("WROTE")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// childWrite runs the re-exec child and returns what it reported.
func childWrite(t *testing.T, writable, target string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run", "TestMain")
	cmd.Env = append(os.Environ(),
		"TILDE_SANDBOX_CHILD=1",
		"TILDE_SANDBOX_WRITABLE="+writable,
		"TILDE_SANDBOX_TARGET="+target)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run() // the exit code carries the verdict; the output says why
	return strings.TrimSpace(out.String())
}

func TestApplyConfinesWrites(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("landlock is linux-only")
	}
	if !Supported() {
		t.Skip("kernel without landlock")
	}
	inside := t.TempDir()
	outside := t.TempDir()

	// A write inside the writable root succeeds.
	if got := childWrite(t, inside, filepath.Join(inside, "ok.txt")); got != "WROTE" {
		t.Errorf("write inside writable root: got %q, want WROTE", got)
	}
	// A write outside is denied by the kernel — the whole point.
	if got := childWrite(t, inside, filepath.Join(outside, "no.txt")); got != "DENIED" {
		t.Errorf("write outside writable root: got %q, want DENIED", got)
	}
	if _, err := os.Stat(filepath.Join(outside, "no.txt")); err == nil {
		t.Error("the denied file exists — the sandbox did not confine")
	}
}

func TestWritableRoots(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	roots := WritableRoots()
	seen := map[string]bool{}
	for _, r := range roots {
		if seen[r] {
			t.Errorf("duplicate root %s", r)
		}
		seen[r] = true
		if st, err := os.Stat(r); err != nil || !st.IsDir() {
			t.Errorf("root %s is not an existing directory", r)
		}
	}
	if !seen[wd] {
		t.Errorf("cwd %s missing from roots: %v", wd, roots)
	}
	if !seen[os.TempDir()] {
		t.Errorf("temp dir %s missing from roots: %v", os.TempDir(), roots)
	}
	// PATH bin dirs are the executable-drop class: never writable.
	for _, forbidden := range []string{filepath.Join(os.Getenv("HOME"), "go", "bin"), "/usr/bin"} {
		if seen[forbidden] {
			t.Errorf("bin dir %s must not be a writable root", forbidden)
		}
	}
}

func TestRunnerCommandWrapsWhenEnabled(t *testing.T) {
	if runtime.GOOS != "linux" || !Supported() {
		t.Skip("wrap shape is only meaningful with landlock")
	}
	r, err := New(true)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Enabled() {
		t.Fatal("enabled runner reports disabled")
	}
	cmd := r.Command(context.Background(), "sh", "-c", "echo hi")
	// Args[0] is the binary; the wrap starts at Args[1].
	if cmd.Args[1] != "__sandbox" || !contains(cmd.Args, "sh") || !contains(cmd.Args, "--") {
		t.Errorf("command not wrapped for sandbox: %v", cmd.Args)
	}

	off, err := New(false)
	if err != nil {
		t.Fatal(err)
	}
	if off.Enabled() {
		t.Error("disabled runner reports enabled")
	}
	plain := off.Command(context.Background(), "sh", "-c", "echo hi")
	if plain.Args[0] != "sh" {
		t.Errorf("disabled runner must not wrap: %v", plain.Args)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestStatusNeverOverclaims(t *testing.T) {
	off, err := New(false)
	if err != nil {
		t.Fatal(err)
	}
	if s := off.Status(); !strings.Contains(s, "off") {
		t.Errorf("disabled status should say off: %s", s)
	}
	on, err := New(true)
	if err != nil {
		t.Fatal(err)
	}
	if on.Enabled() {
		if s := on.Status(); !strings.Contains(s, "landlock") {
			t.Errorf("enabled status should name landlock: %s", s)
		}
	} else if s := on.Status(); !strings.Contains(s, "unavailable") {
		t.Errorf("unsupported status should say unavailable: %s", s)
	}
}

func TestChildArgValidation(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--"},
		{"only-writable"},
		{"/tmp", "--"},
	} {
		if err := Child(args); err == nil {
			t.Errorf("Child(%v) must reject malformed args", args)
		}
	}
}
