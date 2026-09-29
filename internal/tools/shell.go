package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Chmgx81/tilde/internal/sandbox"
)

// Bash executes a command with bash and returns the and returns the
// combined output. Action-Allowed tier: it can mutate anything the user
// account can reach, so it is the highest-stakes built-in.
type Bash struct{}

func (Bash) Name() string { return "bash" }

func (Bash) Description() string {
	return "Run a command in bash (bash -c) in the current working directory and return its combined stdout and stderr. Sandboxed by default: writes are kernel-confined to the working directory, /tmp, and dev caches. Pass {\"sandbox\": false} only when confinement breaks the command — that escape asks the user for approval."
}

func (Bash) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "The bash command to run"},
			"sandbox": {"type": "boolean", "description": "Keep the kernel write-confinement (default true). false runs unsandboxed and requires approval in ask mode."}
		},
		"required": ["command"]
	}`)
}

func (Bash) Tier() Tier { return TierActionAllowed }

// bashTimeout bounds a command so a hung process can't wedge the
// agent loop forever.
const bashTimeout = 5 * time.Minute

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
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, bashTimeout)
		defer cancel()
	}
	var cmd *exec.Cmd
	// sandboxed tracks which branch built the command, so a denial
	// is only classified when the confinement was actually applied —
	// an unsandboxed EACCES is a real permission error.
	sandboxed := !(a.Sandbox != nil && !*a.Sandbox)
	if a.Sandbox != nil && !*a.Sandbox {
		cmd = sandbox.PlainCommand(ctx, "bash", "-c", a.Command)
	} else {
		cmd = sandbox.Command(ctx, "bash", "-c", a.Command)
	}
	// C7: a timeout or interrupt must kill the whole process group,
	// not just the direct bash child — a command that backgrounded
	// work would otherwise outlive the turn. CombinedOutput cannot
	// even return while a grandchild holds the pipe open, so the kill
	// cannot wait for it: a watchdog fires the group kill the moment
	// the context does (WaitDelay bounds the wait regardless).
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				_ = sandbox.KillGroup(cmd.Process.Pid)
			}
		case <-watchDone:
		}
	}()
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("bash: timed out after %s", bashTimeout)
	}
	// A sandboxed write denial gets the plain-language cause and the
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

// sandboxDenialNote classifies a sandboxed EACCES/EROFS for the model
// in plain language. "Probably" is honest: the signature usually
// means the write boundary, but a genuine permission error inside
// the writable roots looks identical — the original error always
// stays visible below the note.
const sandboxDenialNote = `note: this ran inside the sandbox, where writes are confined to the working directory, /tmp, and dev caches — the failure below is probably that boundary. If this write is legitimate, retry with {"sandbox": false} and the user will be asked to approve the unsandboxed run.

`

// looksLikeSandboxDenial matches the shell's EACCES/EROFS phrases.
func looksLikeSandboxDenial(out string) bool {
	return strings.Contains(out, "Permission denied") ||
		strings.Contains(out, "Read-only file system") ||
		strings.Contains(out, "Operation not permitted")
}
