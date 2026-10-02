package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeTool lets the matrix tests pin a tier without a real tool.
type fakeTool struct{ tier Tier }

func (fakeTool) Name() string                                             { return "fake" }
func (fakeTool) Description() string                                      { return "fake tool" }
func (fakeTool) Parameters() json.RawMessage                              { return json.RawMessage(`{}`) }
func (t fakeTool) Tier() Tier                                             { return t.tier }
func (fakeTool) Execute(ctx context.Context, args string) (string, error) { return "", nil }

func TestDecisionMatrix(t *testing.T) {
	promptCalled := false
	allow := func(Tool, string) bool { promptCalled = true; return true }
	denyPrompt := func(Tool, string) bool { promptCalled = true; return false }

	// fakeTool is not a bounded action, so in ask mode it prompts —
	// the bounded auto-run cells are covered by
	// TestAskModeBoundedActions below.
	cases := []struct {
		mode string
		tier Tier
		want string // "allow", "deny", "prompt"
	}{
		// Read-Only tools always run, in every mode.
		{ModeReadOnly, TierReadOnly, "allow"},
		{ModeAsk, TierReadOnly, "allow"},
		{ModeAutoAcceptSafe, TierReadOnly, "allow"},
		{ModeFullAuto, TierReadOnly, "allow"},
		// Draft-Only tools always run: they propose, they don't apply.
		{ModeReadOnly, TierDraftOnly, "allow"},
		{ModeAsk, TierDraftOnly, "allow"},
		{ModeAutoAcceptSafe, TierDraftOnly, "allow"},
		{ModeFullAuto, TierDraftOnly, "allow"},
		// Plan mode: read and draft (present_plan) run, actions prompt.
		{ModePlan, TierReadOnly, "allow"},
		{ModePlan, TierDraftOnly, "allow"},
		{ModePlan, TierActionAllowed, "prompt"},
		// Action-Allowed: the mode actually bites here. read-only
		// prompts too — the model may propose, the user decides.
		{ModeReadOnly, TierActionAllowed, "prompt"},
		{ModeAsk, TierActionAllowed, "prompt"},
		{ModeAutoAcceptSafe, TierActionAllowed, "prompt"},
		{ModeFullAuto, TierActionAllowed, "allow"},
		// Unknown mode fails closed to prompting.
		{"nonsense", TierActionAllowed, "prompt"},
	}

	for _, c := range cases {
		for _, prompt := range []func(Tool, string) bool{allow, denyPrompt} {
			promptCalled = false
			decide := PolicyDecide(c.mode, prompt)
			got := decide(fakeTool{c.tier}, `{}`)
			want := c.want
			if want == "prompt" {
				// The decision must come from the prompt.
				if !promptCalled {
					t.Errorf("mode %s tier %s: prompt not consulted", c.mode, c.tier)
					continue
				}
				if got != prompt(fakeTool{}, `{}`) {
					t.Errorf("mode %s tier %s: gate overrode the prompt (got %v)", c.mode, c.tier, got)
				}
				continue
			}
			if promptCalled {
				t.Errorf("mode %s tier %s: prompt consulted for a non-prompt cell", c.mode, c.tier)
			}
			if wantAllow := want == "allow"; got != wantAllow {
				t.Errorf("mode %s tier %s: got %v, want %v", c.mode, c.tier, got, wantAllow)
			}
		}
	}
}

func TestPolicyNilPromptFailsClosed(t *testing.T) {
	// No one to ask: deny, never silently allow — in every mode
	// that prompts.
	for _, mode := range []string{ModeAsk, ModeReadOnly, ModePlan, "unknown"} {
		decide := PolicyDecide(mode, nil)
		if decide(fakeTool{TierActionAllowed}, `{}`) {
			t.Errorf("nil prompt must deny action-tier calls in mode %s", mode)
		}
		if !decide(fakeTool{TierReadOnly}, `{}`) {
			t.Errorf("nil prompt must still allow read-tier calls in mode %s", mode)
		}
		if !decide(fakeTool{TierDraftOnly}, `{}`) {
			t.Errorf("nil prompt must still allow draft-tier calls in mode %s", mode)
		}
	}
}

func TestAskModeBoundedActions(t *testing.T) {
	promptCalled := false
	prompt := func(Tool, string) bool { promptCalled = true; return true }
	decide := PolicyDecide(ModeAsk, prompt)

	dir := t.TempDir() // inside os.TempDir — a writable root

	// An in-root write is bounded: it runs without prompting.
	promptCalled = false
	args, _ := json.Marshal(map[string]string{"path": filepath.Join(dir, "out.txt")})
	if !decide(WriteFile{}, string(args)) {
		t.Fatal("in-root write denied in ask mode")
	}
	if promptCalled {
		t.Error("in-root write prompted — the path bound makes it safe")
	}

	// A write outside every writable root is unbounded: it prompts.
	promptCalled = false
	args, _ = json.Marshal(map[string]string{"path": "/etc/opcode-should-not-write.txt"})
	if !decide(WriteFile{}, string(args)) || !promptCalled {
		t.Error("out-of-root write must prompt in ask mode")
	}

	// The same bound applies to edits.
	promptCalled = false
	args, _ = json.Marshal(map[string]string{"path": filepath.Join(dir, "edit.txt"), "old": "a", "new": "b"})
	if !decide(EditFile{}, string(args)) {
		t.Fatal("in-root edit denied in ask mode")
	}
	if promptCalled {
		t.Error("in-root edit prompted — the path bound makes it safe")
	}

	// A lexical in-root path that resolves outside (symlink) must
	// not auto-run: the parent's symlinks are resolved first. The
	// link's target must be outside every writable root — pointing
	// it at another temp dir would still be bounded.
	outside := "/var"
	if _, err := os.Stat(outside); err != nil {
		t.Skip("/var unavailable")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	promptCalled = false
	args, _ = json.Marshal(map[string]string{"path": filepath.Join(link, "out.txt")})
	if !decide(WriteFile{}, string(args)) || !promptCalled {
		t.Error("symlinked write path auto-ran; the resolved target is outside the roots")
	}

	// A relative in-root path (no prefix) is bounded too.
	promptCalled = false
	if !decide(WriteFile{}, `{"path": "local.txt"}`) {
		t.Fatal("relative in-cwd write denied in ask mode")
	}
	if promptCalled {
		t.Error("relative in-cwd write prompted")
	}

	// A shell call in tests runs with no Landlock ruleset installed,
	// so it is unbounded and prompts — the sandbox must be credited
	// only when it is actually enforced.
	promptCalled = false
	if !decide(Bash{}, `{"command": "ls"}`) || !promptCalled {
		t.Error("shell call without an active sandbox must prompt in ask mode")
	}
}

// rootSandbox narrows the writable roots to one directory tree so a
// path genuinely outside the project is outside every root.
// WritableRoots is the working directory plus os.TempDir() plus the dev
// caches, so a plain t.TempDir() for the "outside" tree would sit
// inside /tmp — which IS a root — and the escape tests would prove
// nothing. Repointing TMPDIR (and HOME, for the caches) at a fresh
// temp dir leaves the real temp dir outside the bound.
func rootSandbox(t *testing.T) (project, outside string) {
	t.Helper()
	realTmp := os.TempDir()
	newTmp := t.TempDir()
	t.Setenv("TMPDIR", newTmp)
	t.Setenv("TMP", newTmp)
	t.Setenv("TEMP", newTmp)
	t.Setenv("HOME", newTmp)
	t.Setenv("USERPROFILE", newTmp)
	var err error
	if outside, err = os.MkdirTemp(realTmp, "opcode-outside-"); err != nil {
		t.Fatalf("outside dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(outside) })
	project = t.TempDir()
	return project, outside
}

// TestWriteThroughSymlinkDotDotStaysInsideRoots is the bypass that
// let a build-mode write land outside the writable roots with no
// prompt: filepath.Abs cleans "docs/.." lexically, so the gate saw
// "project/secret.txt" (in root) while the kernel resolved the symlink
// first and popped to the target's parent. The gate's answer has to be
// the kernel's answer, so the write is no longer bounded — the dialog
// appears, or the headless run denies.
// join is filepath.Join without the cleaning: "docs/../x" must reach
// the gate exactly as written, since the cleaning is the bug.
func join(parts ...string) string { return strings.Join(parts, string(filepath.Separator)) }

func TestWriteThroughSymlinkDotDotStaysInsideRoots(t *testing.T) {
	project, outside := rootSandbox(t)
	vault := filepath.Join(outside, "vault")
	if err := os.Mkdir(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{"docs", "cfg"} {
		if err := os.Symlink(vault, filepath.Join(project, link)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	t.Chdir(project)
	prompted := false
	decide := PolicyDecide(ModeBuild, func(Tool, string) bool { prompted = true; return true })

	args, _ := json.Marshal(map[string]string{"path": join("docs", "..", "secret.txt")})
	if !decide(WriteFile{}, string(args)) || !prompted {
		t.Error("write_file through docs/.. was credited as bounded; the write lands in the vault's parent")
	}

	// The same spelling through edit_file — the other write tool the
	// gate credits the same bound for.
	prompted = false
	args, _ = json.Marshal(map[string]string{
		"path": join("cfg", "..", "authorized_keys"),
		"old":  "a", "new": "b",
	})
	if !decide(EditFile{}, string(args)) || !prompted {
		t.Error("edit_file through cfg/.. was credited as bounded")
	}
}

// pathInWritableRoots must agree with the kernel for every shape a
// model emits: existing file, new file, new nested dirs, the traversal
// escapes, symlink loops, dangling links, and the ordinary "." and
// ".." that stay inside.
// resolveSymlinks is how a test builds an expected path on a platform
// where a directory and its real path differ (macOS /var ->
// /private/var). The parent is what gets resolved, never the path
// itself: the leaf is the file that does not exist yet, which is the
// whole point of the test, and EvalSymlinks fails on a missing final
// component and would hand back the unresolved path.
func resolveSymlinks(path string) string {
	parent, leaf := filepath.Split(path)
	if resolved, err := filepath.EvalSymlinks(parent); err == nil {
		return filepath.Join(resolved, leaf)
	}
	return path
}

func TestPathInWritableRoots(t *testing.T) {
	root, outside := rootSandbox(t)
	mk := func(path, content string) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	mk(filepath.Join(root, "file.txt"), "x")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mk(filepath.Join(root, "sub", "nested.txt"), "x")
	// A symlink out of the root, a loop, and a link to nowhere.
	if err := os.Symlink(outside, filepath.Join(root, "docs")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "loop-a"), filepath.Join(root, "loop-b")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "loop-b"), filepath.Join(root, "loop-a")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "gone.txt"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	// And one pointing back inside: the in-project symlinked directory.
	if err := os.Symlink(filepath.Join(root, "sub"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}

	// The project is the only writable root: outside lives in the real
	// temp dir, which rootSandbox moved out of the bound.
	t.Chdir(root) // a relative path resolves against the project

	inside := []string{
		"file.txt",
		filepath.Join("sub", "nested.txt"),
		"new.txt",                               // does not exist yet
		filepath.Join("src", "pkg", "thing.go"), // nested dirs that do not exist
		".",
		filepath.Join("sub", "..", "file.txt"), // stays inside
		filepath.Join(".", "file.txt"),
		filepath.Join(root, "file.txt"),
		filepath.Join(root, "sub", "nested.txt"),
		// A symlinked directory that points back inside the project is
		// ordinary layout (monorepos, docs -> ../docs). Creating a new
		// file through one is ordinary work and must not be refused —
		// the resolved path is still inside, and the kernel would put
		// it there.
		filepath.Join("alias", "fresh.txt"),
		filepath.Join("alias", "deep", "fresh.txt"),
	}
	for _, path := range inside {
		if !pathInWritableRoots(path) {
			t.Errorf("pathInWritableRoots(%q) = false, want true (ordinary in-root work)", path)
		}
	}

	outsidePaths := map[string]string{
		"symlink then parent": join("docs", "..", "secret.txt"),
		"parent":              join("..", "secret.txt"),
		"two parents":         join("..", "..", "secret.txt"),
		"absolute outside":    filepath.Join(outside, "secret.txt"),
		"through symlink":     filepath.Join("docs", "secret.txt"),
		"dangling symlink":    "dangling",
		"root escape":         string(filepath.Separator),
	}
	for name, path := range outsidePaths {
		if pathInWritableRoots(path) {
			t.Errorf("%s: pathInWritableRoots(%q) = true, want false", name, path)
		}
	}
}

// A symlink loop must terminate, not spin: the resolution walks with a
// bounded hop count, like the kernel's own ELOOP limit.
// A symlink whose target runs through another symlink — /var -> /private/var
// on macOS, /home -> /var/home on Fedora Atomic — must still resolve, and a
// new file created through it is ordinary work. The link target's components
// are consumed as they are popped, so the count of outstanding link
// components reaches zero before the file that does not exist yet.
func TestResolveLikeKernelThroughSymlinkedAncestor(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real", "project", "sub")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	// dir/var -> dir/real, so dir/var/project/sub resolves through a link.
	if err := os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "var")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	viaLink := filepath.Join(dir, "var", "project", "sub")

	resolved, ok := resolveLikeKernel(filepath.Join(viaLink, "fresh.txt"))
	if !ok {
		t.Fatal("a new file under an existing symlinked directory was refused")
	}
	// The expectation is resolved too: on macOS the temp dir is itself
	// reached through /var -> /private/var, so the path the test built
	// and the path the resolver returns spell the same place two ways.
	if want := resolveSymlinks(filepath.Join(real, "fresh.txt")); resolved != want {
		t.Errorf("resolved = %q, want %q", resolved, want)
	}
}

// A symlink whose target ends in ".." consumes that component without an
// Lstat. The outstanding-link count must drain on it too, or every later
// missing component reads as a dangling link and honest work is refused.
func TestResolveLikeKernelLinkTargetEndingInDotDot(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	if err := os.MkdirAll(filepath.Join(project, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	// alias -> project/sub/.. , i.e. back to project.
	if err := os.Symlink(filepath.Join(project, "sub", ".."), filepath.Join(project, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	resolved, ok := resolveLikeKernel(filepath.Join(project, "alias", "new.txt"))
	if !ok {
		t.Fatal("a new file under a link whose target ends in .. was refused")
	}
	if want := resolveSymlinks(filepath.Join(project, "new.txt")); resolved != want {
		t.Errorf("resolved = %q, want %q", resolved, want)
	}
	// A link that really does dangle is still refused: that is the case
	// the counter exists for.
	if err := os.Symlink(filepath.Join(dir, "nowhere"), filepath.Join(project, "dangling")); err != nil {
		t.Fatal(err)
	}
	if _, ok := resolveLikeKernel(filepath.Join(project, "dangling", "new.txt")); ok {
		t.Error("a file under a dangling symlink was accepted")
	}
}

func TestPathInWritableRootsTerminatesOnSymlinkLoop(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	if err := os.Symlink(filepath.Join(dir, "b"), a); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(a, filepath.Join(dir, "b")); err != nil {
		t.Fatal(err)
	}
	done := make(chan bool, 1)
	go func() { done <- pathInWritableRoots(a) }()
	select {
	case in := <-done:
		if in {
			t.Error("a symlink loop resolved as inside the roots")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("pathInWritableRoots did not terminate on a symlink loop")
	}
}

func TestShellEscaped(t *testing.T) {
	if !ShellEscaped(`{"command": "ls", "sandbox": false}`) {
		t.Error(`{"sandbox": false} must read as an escape`)
	}
	if ShellEscaped(`{"command": "ls", "sandbox": true}`) {
		t.Error(`{"sandbox": true} is sandboxed`)
	}
	if ShellEscaped(`{"command": "ls"}`) {
		t.Error("absent sandbox means sandboxed (the default)")
	}
	if !ShellEscaped(`not json`) {
		t.Error("unparsable args read as an escape — fail closed")
	}
}

func TestModeInstructionAndNormalization(t *testing.T) {
	if NormalizeMode("build") != ModeBuild {
		t.Error(`"build" is the canonical spelling`)
	}
	if NormalizeMode(ModeReadOnly) != ModePlan {
		t.Error(`"read-only" is the legacy plan spelling`)
	}
	for _, mode := range Modes {
		if !ValidMode(mode) {
			t.Errorf("Modes entry %q fails ValidMode", mode)
		}
	}
	if ValidMode("yolo") {
		t.Error("unknown mode must not validate")
	}
	for _, mode := range append(Modes, "unknown-mode") {
		if ModeInstruction(mode) == "" {
			t.Errorf("mode %q has no system-prompt instruction", mode)
		}
	}
}

func TestLegacyModeAliases(t *testing.T) {
	// The Phase 2/30 spellings keep old configs working, mapping
	// onto the three-mode set: read-only onto plan, the ask family
	// onto build.
	if m := NormalizeMode(ModeReadOnly); m != ModePlan {
		t.Errorf("read-only = %q, want plan", m)
	}
	for _, legacy := range []string{ModeAsk, ModeAskEveryTime, ModeAutoAcceptSafe} {
		if m := NormalizeMode(legacy); m != ModeBuild {
			t.Errorf("%s = %q, want build", legacy, m)
		}
	}
	if ValidMode(ModeAutoAcceptSafe) {
		t.Error("auto-accept-safe-ops is not a real mode anymore")
	}
	if len(Modes) != 3 {
		t.Errorf("Modes = %v, want exactly three (plan, build, full-auto)", Modes)
	}
}
