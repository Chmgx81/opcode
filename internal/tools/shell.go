package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Chmgx81/tilde/internal/sandbox"
)

// Bash executes a command with bash and returns the combined output.
// Action-Allowed tier: it can mutate anything the user account can
// reach, so it is the highest-stakes built-in.
type Bash struct{}

func (Bash) Name() string { return "bash" }

func (Bash) Description() string {
	return "Run a command in bash (bash -c) in the current working directory and return its combined stdout and stderr, capped at 2 MiB. Sandboxed by default: writes are kernel-confined to the working directory, /tmp, and dev caches, and network sockets are blocked. Pass {\"sandbox\": false} only when confinement breaks the command — that escape asks the user for approval."
}

func (Bash) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "The bash command to run"},
			"sandbox": {"type": "boolean", "description": "Keep the kernel confinement — writes and network (default true). false runs unsandboxed and requires approval in ask mode."}
		},
		"required": ["command"]
	}`)
}

func (Bash) Tier() Tier { return TierActionAllowed }

// bashTimeout bounds a command so a hung process can't wedge the
// agent loop forever.
const bashTimeout = 5 * time.Minute

// bashMaxBytes caps the combined output of one command. bash is
// Action-Allowed, but in build mode a sandboxed command runs without
// prompting and in full-auto every command does, so `yes` or
// `dd if=/dev/zero` would otherwise grow one bytes.Buffer until the
// process died — under a memory limit the Go runtime's OOM is a fatal,
// unrecoverable crash that loses the turn. 2 MiB is far above a real
// build or test log (web_fetch caps at 256 KiB, read_file at 1 MiB).
const bashMaxBytes = 2 << 20

// cappedWriter accepts the first limit bytes and then discards the
// rest, calling onFull exactly once — when data is actually dropped,
// not when the total merely reaches the cap. It never returns an error:
// the point is to keep the pipe drained, not to break the child's
// writes.
type cappedWriter struct {
	w         *bytes.Buffer
	limit     int
	written   int
	onFull    func()
	fullFired bool
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if room := c.limit - c.written; room > 0 {
		n := len(p)
		if n > room {
			n = room
		}
		c.w.Write(p[:n])
		c.written += n
		if n == len(p) {
			return len(p), nil
		}
	}
	if !c.fullFired {
		c.fullFired = true
		if c.onFull != nil {
			c.onFull()
		}
	}
	return len(p), nil
}

// humanBytes renders a byte cap the way web.go reports its own, so the
// two truncation notes read alike.
func humanBytes(n int) string {
	switch {
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%d MiB", n/(1<<20))
	case n >= 1<<10 && n%(1<<10) == 0:
		return fmt.Sprintf("%d KiB", n/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

func (Bash) Execute(ctx context.Context, args string) (string, error) {
	var a struct {
		Command string `json:"command"`
		Sandbox *bool  `json:"sandbox"`
	}
	if err := parseArgs(args, &a); err != nil {
		return "", err
	}
	if strings.TrimSpace(a.Command) == "" {
		return "", fmt.Errorf("bash: command is required")
	}
	// ownDeadline tracks whether the bound below is ours: a deadline
	// inherited from a parent context must not be reported as our
	// 5-minute timeout.
	ownDeadline := false
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, bashTimeout)
		defer cancel()
		ownDeadline = true
	}
	// runCtx carries the output cap: cancelling it stops the command
	// through the same process-group kill the timeout uses.
	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()
	var cmd *exec.Cmd
	// sandboxed tracks which branch built the command, so a denial
	// is only classified when the confinement was actually applied —
	// an unsandboxed EACCES is a real permission error.
	sandboxed := !(a.Sandbox != nil && !*a.Sandbox)
	if a.Sandbox != nil && !*a.Sandbox {
		cmd = sandbox.PlainCommand(runCtx, "bash", "-c", a.Command)
	} else {
		cmd = sandbox.Command(runCtx, "bash", "-c", a.Command)
	}
	// One buffer behind both streams, as CombinedOutput does: with
	// the same writer on Stdout and Stderr, exec copies them through a
	// single goroutine, so interleaving is preserved and the buffer
	// needs no lock.
	//
	// The buffer is capped, and exceeding the cap stops the command by
	// cancelling the context it runs under — which is the same
	// process-group kill the timeout uses, so a command that floods
	// stdout leaves no process group behind. Returning a write error
	// instead would make exec report "signal: broken pipe", which
	// reads like a harness failure rather than the honest reason.
	var buf bytes.Buffer
	var over atomic.Bool
	capped := &cappedWriter{w: &buf, limit: bashMaxBytes}
	capped.onFull = func() {
		over.Store(true)
		stopRun()
	}
	cmd.Stdout = capped
	cmd.Stderr = capped
	err := runKillingGroup(runCtx, cmd)
	out := buf.Bytes()
	if over.Load() {
		// Whatever the command managed to say before it was stopped is
		// still the useful part; the cap is the honest headline.
		return string(out) + fmt.Sprintf("\n… (output capped at %s; the command was stopped)", humanBytes(bashMaxBytes)),
			fmt.Errorf("bash: produced more than %s of output and was stopped", humanBytes(bashMaxBytes))
	}
	if ctx.Err() == context.DeadlineExceeded {
		if ownDeadline {
			return string(out), fmt.Errorf("bash: timed out after %s", bashTimeout)
		}
		return string(out), fmt.Errorf("bash: context deadline exceeded")
	}
	// A sandboxed denial gets the plain-language cause and the
	// documented escape, on both the exit-error and swallowed-error
	// paths ("|| true" can hide a denial behind exit 0).
	if sandboxed && sandbox.Active() && looksLikeSandboxDenial(string(out)) {
		out = []byte(sandboxDenialNote + string(out))
	}
	if err != nil {
		// Nonzero exit is a tool result the model can act on, not a
		// harness failure: report the output and the exit status.
		// exec's own error already reads "exit status N" — wrapping
		// it again printed "exit status: exit status N" (audit U7).
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return string(out), fmt.Errorf("exited with status %d", exitErr.ExitCode())
		}
		return string(out), fmt.Errorf("bash: %w", err)
	}
	return string(out), nil
}

// runKillingGroup starts cmd and waits for it, killing its whole
// process group the moment ctx ends (audit C7): a timeout or interrupt
// must not leave backgrounded work running, and exec's own context
// kill reaches only the direct child. Wait cannot return while a
// grandchild holds the output pipe (WaitDelay bounds that), so the
// group kill cannot wait for Wait — a watchdog fires it as soon as the
// context does.
//
// The watchdog starts only after Start succeeds: cmd.Process is
// written by Start, so reading it from another goroutine any earlier
// is a data race, and a failed Start has no process to kill. The pid
// is captured once so the watchdog never touches cmd. The watchdog is
// always joined before return — no goroutine outlives the call.
func runKillingGroup(ctx context.Context, cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	waited := make(chan struct{})
	watchdogDone := make(chan struct{})
	go func() {
		defer close(watchdogDone)
		select {
		case <-ctx.Done():
			// Wait may have reaped the child in the same instant;
			// the pid is not ours to signal once it has.
			select {
			case <-waited:
			default:
				_ = sandbox.KillGroup(pid)
			}
		case <-waited:
		}
	}()
	err := cmd.Wait()
	close(waited)
	<-watchdogDone
	return err
}

// sandboxDenialNote classifies a sandboxed EACCES/EROFS/EPERM or a
// seccomp EAFNOSUPPORT for the model in plain language. "Probably"
// is honest: the signature usually means a sandbox boundary, but a
// genuine permission error inside the writable roots looks
// identical — the original error always stays visible below the note.
const sandboxDenialNote = `note: this ran inside the sandbox, where writes are confined to the working directory, /tmp, and dev caches, and network sockets are blocked — the failure below is probably one of those boundaries. If this is legitimate, retry with {"sandbox": false} and the user will be asked to approve the unsandboxed run.

`

// looksLikeSandboxDenial matches the shell's EACCES/EROFS phrases and
// the seccomp filter's EAFNOSUPPORT wording.
func looksLikeSandboxDenial(out string) bool {
	return strings.Contains(out, "Permission denied") ||
		strings.Contains(out, "Read-only file system") ||
		strings.Contains(out, "Operation not permitted") ||
		strings.Contains(out, "Address family not supported")
}
