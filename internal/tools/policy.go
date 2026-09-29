package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Chmgx81/tilde/internal/sandbox"
)

// Permission modes (Section 7). The mode sets the posture; the gate
// still checks each action's tier per call, so the promises below
// are enforced per call, not hoped for.
//
// Three modes, Codex's preset triad (Read Only / Default / Full
// Access) and Claude Code's plan/default/bypass at tilde's scale:
//
//   - plan: research posture. Read tools run free; Draft-Only tools
//     (present_plan) run free — the mechanical exit from planning;
//     every action-tier call asks the user. The tools are still
//     offered, so the model can propose a write — the approval
//     dialog is the posture.
//   - build: what the sandbox bounds runs without prompting —
//     Landlock-confined shell commands, file writes and patches
//     inside the same writable roots. Everything that escapes
//     (unsandboxed shell calls, writes outside the roots, web
//     fetches, or any action when Landlock is unavailable) asks.
//   - full-auto: everything runs, still logged.
const (
	ModePlan     = "plan"
	ModeBuild    = "build"
	ModeFullAuto = "full-auto"

	// Legacy spellings from earlier phases; NormalizeMode maps them
	// so every existing config keeps working: Phase 30's read-only
	// folded into plan (they were the same posture in the gate), and
	// Phase 30's ask became build.
	ModeReadOnly       = "read-only"
	ModeAsk            = "ask"
	ModeAskEveryTime   = "ask-every-time"
	ModeAutoAcceptSafe = "auto-accept-safe-ops"
)

// Modes is the complete set, in display order: plan, build, act.
var Modes = []string{ModePlan, ModeBuild, ModeFullAuto}

// NormalizeMode canonicalizes a configured mode name: the legacy
// read-only spelling maps to plan (the merged research posture),
// and ask / ask-every-time / auto-accept-safe-ops map to build.
// Anything else unknown stays unknown (and behaves as build in the
// policy) rather than being silently coerced to something
// permissive.
func NormalizeMode(mode string) string {
	switch mode {
	case ModeReadOnly:
		return ModePlan
	case ModeAsk, ModeAskEveryTime, ModeAutoAcceptSafe:
		return ModeBuild
	}
	return mode
}

// ValidMode reports whether mode is one of the three real modes.
// It checks the raw name: legacy spellings are not real modes —
// callers normalize first (config loading does).
func ValidMode(mode string) bool {
	switch mode {
	case ModePlan, ModeBuild, ModeFullAuto:
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
	case ModePlan:
		return "Permission mode is plan: research with the read tools (read_file, grep, glob, list_dir) and do not change anything yet — any write, command, or web fetch you call asks the user first. When you understand the task, present exactly one plan with the present_plan tool — markdown with the goal, concrete steps, and risks — and stop. Wait for the user's decision; do not act before it."
	case ModeBuild:
		return "Permission mode is build: sandboxed shell commands — writes kernel-confined to the working directory, /tmp, and dev caches — and file writes or patches inside those roots run without prompting. Anything that escapes the bound (bash with \"sandbox\": false, writes outside those roots, web fetches) asks the user first."
	case ModeFullAuto:
		return "Permission mode is full-auto: tool calls run without prompting and are logged."
	}
	return "Permission mode is unknown: state-changing tool calls will ask the user. Ask the user how to proceed if unsure."
}

// PolicyDecide returns a Gate.Decide function implementing the
// Phase 30 decision matrix:
//
//   - The credentials file is denied in every mode, before the
//     matrix, for every tool that names it in a path argument (read,
//     write, edit, patch, grep, glob, list): no posture — not even
//     full-auto — hands the user's keys to the model's context or
//     lets the model rewrite them. A user who wants to see them can
//     run "!cat ~/.tilde/auth.json" themselves.
//   - Read-Only and Draft-Only tools are always allowed — running
//     them cannot change state.
//   - Action-Allowed tools in full-auto are always allowed.
//   - Action-Allowed tools in build mode run without prompting when
//     the call is bounded (sandbox.Active() Landlock-confined shell
//     command, or a file write inside the sandbox's writable
//     roots); escapes prompt.
//   - plan prompts for every action-tier call.
//   - A nil prompt denies: fail closed, never open. Unknown modes
//     behave as build — a config typo must not widen permissions.
func PolicyDecide(mode string, prompt func(tool Tool, args string) bool) func(Tool, string) bool {
	return func(tool Tool, args string) bool {
		if touchesCredentials(tool, args) {
			return false
		}
		switch tool.Tier() {
		case TierReadOnly, TierDraftOnly:
			return true
		case TierActionAllowed:
			switch NormalizeMode(mode) {
			case ModeFullAuto:
				return true
			case ModeBuild:
				if boundedAction(tool, args) {
					return true
				}
				return askPrompt(prompt, tool, args)
			default: // plan, unknown
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
	case Bash:
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
	case ApplyPatch:
		// A patch is bounded only when every file it touches is
		// inside the roots — a partial bound is no bound.
		paths := patchPaths(args)
		if len(paths) == 0 {
			return false
		}
		for _, p := range paths {
			if !pathInWritableRoots(p) {
				return false
			}
		}
		return true
	}
	return false
}

// ShellEscaped reports whether a bash call opted out of the
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
// the gate bounds them by path, resolving symlinks along the whole
// path: a lexical in-tree path that points outside must not
// auto-run.
//
// The path need not exist yet — writing a file that is being created,
// or one whose directory is being created, is ordinary work. So the
// longest EXISTING prefix is resolved and the missing tail is
// re-appended. Resolving only the immediate parent (as an earlier
// revision did) refused every "Add File: src/pkg/thing.go" as being
// outside the project; the parts that do not exist cannot be
// symlinks, so they need no resolution, and every component that does
// exist is still resolved. A path that exists but will not resolve —
// a symlink loop, a dangling link — fails closed.
func pathInWritableRoots(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	existing, rest := abs, ""
	for {
		resolved, err := filepath.EvalSymlinks(existing)
		if err == nil {
			abs = filepath.Join(resolved, rest)
			break
		}
		if _, statErr := os.Lstat(existing); statErr == nil {
			// It exists but will not resolve — a loop or a dangling
			// link. Nothing honest to check; refuse.
			return false
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return false
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
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
