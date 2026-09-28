package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Chmgx81/tilde/internal/sandbox"
)

// RunShell executes a shell command with the user's shell and returns the
// combined output. Action-Allowed tier: it can mutate anything the user
// account can reach, so it is the highest-stakes built-in.
type RunShell struct{}

func (RunShell) Name() string { return "run_shell" }

func (RunShell) Description() string {
	return "Run a shell command (via sh -c) in the current working directory and return its combined stdout and stderr."
}

func (RunShell) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "The shell command to run"}
		},
		"required": ["command"]
	}`)
}

func (RunShell) Tier() Tier { return TierActionAllowed }

// runShellTimeout bounds a command so a hung process can't wedge the
// agent loop forever.
const runShellTimeout = 5 * time.Minute

func (RunShell) Execute(ctx context.Context, args string) (string, error) {
	var a struct {
		Command string `json:"command"`
	}
	if err := parseArgs(args, &a); err != nil {
		return "", err
	}
	if strings.TrimSpace(a.Command) == "" {
		return "", fmt.Errorf("run_shell: command is required")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, runShellTimeout)
		defer cancel()
	}
	cmd := sandbox.Command(ctx, "sh", "-c", a.Command)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("run_shell: timed out after %s", runShellTimeout)
	}
	if err != nil {
		// Nonzero exit is a tool result the model can act on, not a
		// harness failure: report the output and the exit status.
		if _, ok := err.(*exec.ExitError); ok {
			return string(out), fmt.Errorf("exit status: %s", err)
		}
		return string(out), fmt.Errorf("run_shell: %w", err)
	}
	return string(out), nil
}
