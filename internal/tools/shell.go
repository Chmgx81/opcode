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
	if a.Sandbox != nil && !*a.Sandbox {
		cmd = sandbox.PlainCommand(ctx, "bash", "-c", a.Command)
	} else {
		cmd = sandbox.Command(ctx, "bash", "-c", a.Command)
	}
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("bash: timed out after %s", bashTimeout)
	}
	if err != nil {
		// Nonzero exit is a tool result the model can act on, not a
		// harness failure: report the output and the exit status.
		if _, ok := err.(*exec.ExitError); ok {
			return string(out), fmt.Errorf("exit status: %s", err)
		}
		return string(out), fmt.Errorf("bash: %w", err)
	}
	return string(out), nil
}
