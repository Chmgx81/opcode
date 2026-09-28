package tools

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/Chmgx81/tilde/internal/sandbox"
)

// Permission modes (Section 7). The mode sets the posture; the gate
// still checks each action's tier per call, so the promises below
// are enforced per call, not hoped for.
//
// Phase 30 adopts Codex's posture — the sandbox is the safety,
// approval the exception. The modes are look, plan, ask, act:
//
//   - read-only: read tools run free; every action-tier call asks
//     the user. The tools are still offered, so the model can
//     propose a write — the approval dialog is the posture.
//   - plan: like read-only for actions, plus Draft-Only tools
//     (present_plan) run free — the mechanical exit from planning.
//   - ask: what the sandbox bounds runs without prompting —
//     Landlock-confined shell commands and file writes inside the
//     same writable roots. Everything that escapes (unsandboxed
//     shell calls, writes outside the roots, or any action when
//     Landlock is unavailable) asks.
//   - full-auto: everything runs, still logged.
const (
	ModeReadOnly = "read-only"
	ModePlan     = "plan"
	ModeAsk      = "ask"
	ModeFullAuto = "full-auto"

	// Legacy spellings from earlier phases; NormalizeMode maps both
	// to ModeAsk. ask-every-time's Phase 2 semantics (prompt on
	// every action) became ask's bounded-run semantics in Phase 30.
	ModeAskEveryTime   = "ask-every-time"
	ModeAutoAcceptSafe = "auto-accept-safe-ops"
)

// Modes is the complete set, in display order: look, plan, ask, act.
var Modes = []string{ModeReadOnly, ModePlan, ModeAsk, ModeFullAuto}

// NormalizeMode canonicalizes a configured mode name: the legacy
// ask-every-time and auto-accept-safe-ops spellings map to ask.
// Anything else unknown stays unknown (and behaves as ask in the
// policy) rather than being silently coerced to something
// permissive.
func NormalizeMode(mode string) string {
	switch mode {
	case ModeAskEveryTime, ModeAutoAcceptSafe:
		return ModeAsk
	}
	return mode
}

// ValidMode reports whether mode is one of the four real modes.
// It checks the raw name: legacy spellings are not real modes —
// callers normalize first (config loading does).
func ValidMode(mode string) bool {
	switch mode {
	case ModeReadOnly, ModePlan, ModeAsk, ModeFullAuto:
		return true
	}
	return false
}

// ModeAllowsTool reports whether a tool is offered to the model at
// all under this mode. Since Phase 30 every mode offers every
// tier: offering is not permission, the gate is the single
// enforcement point — a read-only session can propose a write and
// the approval dialog (or a headless denial) is where the posture
// bites.
func ModeAllowsTool(mode string, t Tool) bool {
	return ModeAllowsTier(mode, t.Tier())
}

// ModeAllowsTier is the tier-level form, for callers (like the
// orchestrator's tool defs) that hold a Def, not a Tool.
func ModeAllowsTier(mode string, tier Tier) bool {
	return true
}

// ModeInstruction is the mode's contribution to the system prompt, so
// the model knows the posture it runs under (Section 3.2: mode
// determines the system prompt).
func ModeInstruction(mode string) string {
	switch NormalizeMode(mode) {
	case ModeReadOnly:
		return "Permission mode is read-only: read tools run freely. You may propose writes, edits, or shell commands, but each one asks the user for approval before running — propose only what the task truly needs."
	case ModePlan:
		return "Permission mode is plan: you cannot change anything yet. Research the codebase with read tools, then present exactly one plan with the present_plan tool — markdown with the goal, concrete steps, and risks — and stop. Wait for the user's decision; do not act before it. Any write or command you call asks the user."
	case ModeAsk:
		return "Permission mode is ask: shell commands run sandboxed — their writes are kernel-confined to the working directory, /tmp, and dev caches — and file writes inside those roots run too, both without prompting. Anything that escapes the bound (run_shell with \"sandbox\": false, writes outside those roots) asks the user first."
	case ModeFullAuto:
		return "Permission mode is full-auto: tool calls run without prompting and are logged."
	}
	return "Permission mode is unknown: state-changing tool calls will ask the user. Ask the user how to proceed if unsure."
}

// PolicyDecide returns a Gate.Decide function implementing the
// Phase 30 decision matrix:
//
//   - Read-Only and Draft-Only tools are always allowed — running
//     them cannot change state.
//   - Action-Allowed tools in full-auto are always allowed.
//   - Action-Allowed tools in ask mode run without prompting when
//     the call is bounded (sandbox.Active() Landlock-confined shell
//     command, or a file write inside the sandbox's writable
//     roots); escapes prompt.
//   - read-only and plan prompt for every action-tier call.
//   - A nil prompt denies: fail closed, never open. Unknown modes
//     behave as ask — a config typo must not widen permissions.
func PolicyDecide(mode string, prompt func(tool Tool, args string) bool) func(Tool, string) bool {
	return func(tool Tool, args string) bool {
		switch tool.Tier() {
		case TierReadOnly, TierDraftOnly:
			return true
		case TierActionAllowed:
			switch NormalizeMode(mode) {
			case ModeFullAuto:
				return true
			case ModeAsk:
				if boundedAction(tool, args) {
					return true
				}
				return askPrompt(prompt, tool, args)
			default:
				return askPrompt(prompt, tool, args)
			}
		default:
			return false
		}
	}
}

func askPrompt(prompt func(Tool, string) bool, tool Tool, args string) bool {
	if prompt == nil {
		return false
	}
	return prompt(tool, args)
}

// boundedAction reports whether a call's effects are already
// confined, so ask mode can run it without prompting: a sandboxed
// shell command under an active Landlock ruleset, or an
// in-process file write inside the same writable roots the sandbox
// grants. Anything else — the explicit {"sandbox": false} escape,
// an out-of-roots path, unparsable args, or an unavailable
// sandbox — is unbounded and asks.
func boundedAction(tool Tool, args string) bool {
	switch tool.(type) {
	case RunShell:
		if ShellEscaped(args) {
			return false
		}
		// Credit the sandbox only when it is actually enforced;
		// without Landlock a "sandboxed" command runs free.
		return sandbox.Active()
	case WriteFile, EditFile:
		var a struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(args), &a) != nil || a.Path == "" {
			return false
		}
		return pathInWritableRoots(a.Path)
	}
	return false
}

// ShellEscaped reports whether a run_shell call opted out of the
// sandbox. Unparsable args read as an escape — fail closed. Shared
// with the TUI, whose approval dialog names the escape for what it
// is.
func ShellEscaped(args string) bool {
	var a struct {
		Sandbox *bool `json:"sandbox"`
	}
	if json.Unmarshal([]byte(args), &a) != nil {
		return true
	}
	return a.Sandbox != nil && !*a.Sandbox
}

// pathInWritableRoots reports whether path resolves inside one of
// the sandbox's writable roots — the identical bound a sandboxed
// shell command gets. In-process writes cannot be Landlocked, so
// the gate bounds them by path, resolving the parent directory's
// symlinks first: a lexical in-tree path that points outside must
// not auto-run.
func pathInWritableRoots(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	dir, base := filepath.Split(abs)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		abs = filepath.Join(resolved, base)
	}
	for _, root := range sandbox.WritableRoots() {
		if withinDir(abs, root) {
			return true
		}
	}
	return false
}

func withinDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
