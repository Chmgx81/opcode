// Package sandbox confines subprocess writes with Linux Landlock:
// read and execute everywhere, writes only beneath the working
// directory, the temp dir, and known dev caches. The ruleset mirrors
// Codex's landlock.rs (Phase 21 spec).
//
// Landlock applies to the calling thread, and Go's runtime has
// several threads before main — so commands run through a self
// re-exec (`tilde __sandbox <writable…> -- cmd`): a fresh
// single-threaded child applies the ruleset and immediately execs
// the command, which inherits the rules process-wide.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// ErrUnsupported is returned by Apply on platforms without Landlock
// (non-Linux, or a kernel without it). Callers degrade to running
// unsandboxed and must say so.
var ErrUnsupported = errors.New("sandbox: landlock unavailable on this system")

// installed is the process-wide runner main sets once; the tools
// reach it through Command without holding a reference. nil (before
// main wires it, or in tests) means plain unsandboxed commands.
var installed *Runner

// Install sets the process-wide runner. Called once from main.
func Install(r *Runner) { installed = r }

// armForDeath points a command at its death semantics: it leads its
// own process group (so the whole tree can be killed together —
// audit C7), and its Wait gives up on lingering pipe holders after
// a short grace period — a backgrounded grandchild holding stdout
// open must not wedge the caller past the timeout.
func armForDeath(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = deathAttr()
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

// Command builds the command through the installed runner — the one
// entry point the tools use. Works with nothing installed.
func Command(ctx context.Context, name string, arg ...string) *exec.Cmd {
	if installed == nil {
		return armForDeath(exec.CommandContext(ctx, name, arg...))
	}
	return installed.Command(ctx, name, arg...)
}

// PlainCommand builds a command that is never sandboxed — the
// the bash {"sandbox": false} escape. The permission gate treats
// such calls as the approval-triggering escape; here it just means
// the Landlock wrapper is skipped.
func PlainCommand(ctx context.Context, name string, arg ...string) *exec.Cmd {
	return armForDeath(exec.CommandContext(ctx, name, arg...))
}

// Active reports whether subprocesses built through Command are
// actually confined. The permission gate consults it before
// crediting the sandbox: on a platform without Landlock a
// "sandboxed" command runs free, and auto-running it would be a
// lie.
func Active() bool {
	return installed != nil && installed.enabled
}

// Runner builds subprocess commands, optionally wrapped in the
// Landlock sandbox. A nil *Runner or a disabled one builds plain
// commands — every call site works unchanged.
type Runner struct {
	self    string // this binary's path, for the __sandbox re-exec
	enabled bool
}

// New returns a Runner. enabled=false means every command runs
// exactly as it did before this package existed. The runner is
// disabled automatically when the platform lacks support, so callers
// never need their own probe.
func New(enabled bool) (*Runner, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("sandbox: cannot resolve own binary: %w", err)
	}
	return &Runner{self: self, enabled: enabled && Supported()}, nil
}

// Enabled reports whether commands will actually be sandboxed —
// false on any platform without support, whatever the config said.
func (r *Runner) Enabled() bool {
	return r != nil && r.enabled
}

// Status is the one-line posture for startup notes: what is enforced
// and where writes go. It never claims enforcement that isn't real.
func (r *Runner) Status() string {
	switch {
	case r == nil || !r.enabled:
		return "sandbox: off (\"sandbox\": true in config.json confines shell writes)"
	case !Supported():
		return "sandbox: unavailable — shell commands run unsandboxed"
	default:
		abi, _ := ProbeABI()
		return fmt.Sprintf(
			"sandbox: landlock v%d — reads anywhere, writes confined to this directory, %s, and dev caches",
			abi, os.TempDir())
	}
}

// Command returns the exec.Cmd for name with args, wrapped in the
// sandbox when enabled. The child dies with this process either way.
func (r *Runner) Command(ctx context.Context, name string, arg ...string) *exec.Cmd {
	if !r.Enabled() {
		return armForDeath(exec.CommandContext(ctx, name, arg...))
	}
	writable := WritableRoots()
	argv := make([]string, 0, len(writable)+len(arg)+3)
	argv = append(argv, "__sandbox")
	argv = append(argv, writable...)
	argv = append(argv, "--", name)
	argv = append(argv, arg...)
	return armForDeath(exec.CommandContext(ctx, r.self, argv...))
}

// WritableRoots computes the directories a sandboxed command may
// write: the working directory (recursive), the temp dir, and the
// dev caches that exist on this machine. Everything else stays
// read-only — including PATH bin dirs like ~/go/bin and
// ~/.local/bin, which is the executable-drop attack class.
func WritableRoots() []string {
	var roots []string
	add := func(dir string) {
		if dir == "" {
			return
		}
		if abs, err := filepath.Abs(dir); err == nil {
			roots = append(roots, abs)
		}
	}
	addDir := func(dir string) {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			add(dir)
		}
	}

	wd, err := os.Getwd()
	if err == nil {
		add(wd)
	}
	add(os.TempDir())
	home, err := os.UserHomeDir()
	if err == nil {
		addDir(cacheDir(home))
		addDir(filepath.Join(home, "go", "pkg", "mod"))
		addDir(filepath.Join(home, ".cargo", "registry"))
		addDir(filepath.Join(home, ".npm"))
	}
	// Explicit env vars win over the home-based guesses.
	add(os.Getenv("GOCACHE"))
	add(os.Getenv("GOMODCACHE"))
	return dedup(roots)
}

// cacheDir is the XDG cache root (which holds go-build by default),
// or ~/.cache when XDG_CACHE_HOME is unset.
func cacheDir(home string) string {
	if x := os.Getenv("XDG_CACHE_HOME"); x != "" {
		return x
	}
	return filepath.Join(home, ".cache")
}

func dedup(dirs []string) []string {
	seen := make(map[string]bool, len(dirs))
	out := dirs[:0]
	for _, d := range dirs {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

// Child is the `tilde __sandbox` entry: args are writable roots, a
// "--" separator, then the command. It applies the ruleset and
// replaces this process with the command — success never returns.
func Child(args []string) error {
	sep := -1
	for i, a := range args {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 1 || sep == len(args)-1 {
		return errors.New("__sandbox: usage: tilde __sandbox <writable-dir>… -- <command> [args…]")
	}
	writable, argv := args[:sep], args[sep+1:]
	if err := Apply(writable); err != nil {
		return fmt.Errorf("__sandbox: %w", err)
	}
	return Exec(argv)
}
