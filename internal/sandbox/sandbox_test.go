package sandbox

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// TestMain implements the re-exec child: the test binary runs itself
// with OPCODE_SANDBOX_CHILD set; the child applies the real Landlock
// ruleset (env names the writable root and the write target) and
// reports whether the write went through. This exercises the real
// syscalls, not a mock — the enforcement claim rests on it.
func TestMain(m *testing.M) {
	if os.Getenv("OPCODE_SANDBOX_CHILD") == "1" {
		writable := os.Getenv("OPCODE_SANDBOX_WRITABLE")
		target := os.Getenv("OPCODE_SANDBOX_TARGET")
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
	// Network child: after the full confinement, an AF_INET socket
	// must be denied and an AF_UNIX one must not.
	if os.Getenv("OPCODE_SANDBOX_NET") == "1" {
		writable := os.Getenv("OPCODE_SANDBOX_WRITABLE")
		if err := Apply([]string{writable}); err != nil {
			os.Stdout.WriteString("APPLY-ERR:" + err.Error())
			os.Exit(2)
		}
		if _, err := net.Dial("tcp", "127.0.0.1:1"); err == nil {
			os.Stdout.WriteString("OPEN-INET")
			os.Exit(4)
		} else if !errors.Is(err, syscall.EAFNOSUPPORT) {
			os.Stdout.WriteString("UNEXPECTED:" + err.Error())
			os.Exit(5)
		}
		// A dial to a socket path that cannot exist must fail with
		// ENOENT, never the filter's EAFNOSUPPORT — proving the
		// AF_UNIX socket itself was created.
		if _, err := net.Dial("unix", filepath.Join(writable, "absent.sock")); err == nil ||
			errors.Is(err, syscall.EAFNOSUPPORT) {
			os.Stdout.WriteString("UNIX-BLOCKED")
			os.Exit(6)
		}
		os.Stdout.WriteString("DENIED-INET")
		os.Exit(0)
	}
	// Exec child: the full confinement, then a real command — the
	// filter must not break plain commands.
	if os.Getenv("OPCODE_SANDBOX_EXEC") == "1" {
		writable := os.Getenv("OPCODE_SANDBOX_WRITABLE")
		if err := Apply([]string{writable}); err != nil {
			os.Stdout.WriteString("APPLY-ERR:" + err.Error())
			os.Exit(2)
		}
		if err := Exec([]string{"sh", "-c", "echo sandbox-ok"}); err != nil {
			os.Stdout.WriteString("EXEC-ERR:" + err.Error())
			os.Exit(7)
		}
	}
	os.Exit(m.Run())
}

// childWrite runs the re-exec child and returns what it reported.
func childWrite(t *testing.T, writable, target string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run", "TestMain")
	cmd.Env = append(os.Environ(),
		"OPCODE_SANDBOX_CHILD=1",
		"OPCODE_SANDBOX_WRITABLE="+writable,
		"OPCODE_SANDBOX_TARGET="+target)
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

// childRun runs a re-exec child mode (the env var named by mode, e.g.
// "NET", selects the behavior in TestMain) and returns its combined
// output and exit error.
func childRun(t *testing.T, mode, writable string) (string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run", "TestMain")
	cmd.Env = append(os.Environ(),
		"OPCODE_SANDBOX_"+mode+"=1",
		"OPCODE_SANDBOX_WRITABLE="+writable)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

// TestSeccompBlocksNetwork proves the network half of the sandbox
// through the same re-exec child as the write test: a confined
// process cannot create an AF_INET socket — failing with the
// filter's own EAFNOSUPPORT, not some unrelated error — an AF_UNIX
// socket still can, and a plain command still runs.
func TestSeccompBlocksNetwork(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("landlock is linux-only")
	}
	if !Supported() {
		t.Skip("kernel without landlock")
	}
	if runtime.GOARCH != "amd64" {
		t.Skip("seccomp filter is x86_64-only")
	}
	dir := t.TempDir()

	if got, err := childRun(t, "NET", dir); got != "DENIED-INET" {
		t.Errorf("network probe: got %q (err %v), want DENIED-INET", got, err)
	}
	got, err := childRun(t, "EXEC", dir)
	if err != nil {
		t.Errorf("sandboxed command failed: %v (output %q)", err, got)
	}
	if got != "sandbox-ok" {
		t.Errorf("sandboxed command output: got %q, want sandbox-ok", got)
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
	// Compared the way the gate compares them: through the symlink
	// resolution. macOS's TMPDIR is /var/folders/... while the real
	// path is /private/var/folders/... (/var is a link), and the cwd
	// has the same shape inside the runner's build directory. The roots
	// list is deliberately stored as the environment spells it, so
	// resolving the expectation is what proves the list is right.
	seenResolved := map[string]bool{}
	for r := range seen {
		seenResolved[resolveSymlinks(r)] = true
	}
	for _, want := range []struct{ what, path string }{
		{"cwd", wd}, {"temp dir", os.TempDir()},
	} {
		if !seenResolved[resolveSymlinks(want.path)] {
			t.Errorf("%s %s missing from roots: %v", want.what, want.path, roots)
		}
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

// A user who set "sandbox": true on a platform without Landlock must be
// told the setting is unavailable, not that the sandbox is off — the second
// reading says they never asked for it, and invites them to look elsewhere.
func TestStatusSaysUnavailableWhenAskedButUnsupported(t *testing.T) {
	if Supported() {
		t.Skip("this platform has landlock, so the sandbox is never unavailable")
	}
	on, err := New(true)
	if err != nil {
		t.Fatal(err)
	}
	if on.Enabled() {
		t.Fatal("sandbox reports enabled on an unsupported platform")
	}
	if s := on.Status(); !strings.Contains(s, "unavailable") {
		t.Errorf("status = %q, want it to say unavailable", s)
	}
	// Asked-off stays a plain "off", not an alarming unavailability notice.
	off, err := New(false)
	if err != nil {
		t.Fatal(err)
	}
	if s := off.Status(); !strings.Contains(s, "off") {
		t.Errorf("status = %q, want it to say off", s)
	}
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

// The network claim in Status() must track what Apply enforces on
// this platform — said exactly when the seccomp step exists, so an
// arm64 or non-Linux build never announces a block it does not have.
func TestStatusNetworkClaimMatchesEnforcement(t *testing.T) {
	on, err := New(true)
	if err != nil {
		t.Fatal(err)
	}
	if !on.Enabled() {
		t.Skip("sandbox unsupported here; status makes no enforcement claim")
	}
	claims := strings.Contains(on.Status(), "network sockets blocked")
	if claims != confinedNetwork() {
		t.Errorf("status claims network block = %v, but confinedNetwork() = %v: %s",
			claims, confinedNetwork(), on.Status())
	}
	if want := runtime.GOOS == "linux" && runtime.GOARCH == "amd64"; confinedNetwork() != want {
		t.Errorf("confinedNetwork() = %v on %s/%s, want %v", confinedNetwork(), runtime.GOOS, runtime.GOARCH, want)
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

// resolveSymlinks is the comparison the tests need on a platform where a
// path and its real path differ (macOS /var -> /private/var). A path that
// cannot be resolved is compared as written.
func resolveSymlinks(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}
